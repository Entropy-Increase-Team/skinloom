package app

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/internal/config"
	"github.com/Entropy-Increase-Team/skinloom/internal/gateway"
	"github.com/Entropy-Increase-Team/skinloom/internal/httpapi"
	"github.com/Entropy-Increase-Team/skinloom/internal/launcher"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/account"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/yggdrasil"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/buildinfo"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/datalock"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/logging"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/pubsub"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/realip"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/secrets"
)

// RunOptions control `skinloom up` / `skinloom serve`.
type RunOptions struct {
	Paths Paths
	// NoWeb never starts or proxies the frontend (backend only).
	NoWeb bool
	// Dev forces web.mode = dev (runs the frontend dev server with bun, pnpm or npm).
	Dev bool
	// Open opens the site in the default browser once it is listening.
	Open bool
	// Ready, if set, is called with the listening address once requests are accepted.
	Ready func(addr net.Addr)
}

// Run starts the server and blocks until ctx is cancelled.
func Run(ctx context.Context, o RunOptions) error {
	pretty, color := logging.ConsoleStyle(os.Stderr)
	writeBanner(os.Stderr, pretty, color, buildinfo.Version)
	p, err := o.Paths.Resolve()
	if err != nil {
		return err
	}
	p.Serve = true
	// Held while the site runs, so skinloom restore refuses to replace its files.
	lk, err := lockDataDir(p, datalock.Shared)
	if errors.Is(err, datalock.ErrInUse) {
		return errors.New("a restore is running on this data directory: start the site after it has finished")
	}
	if err != nil {
		return err
	}
	defer lk.Release()

	envOv := config.EnvOverrides()
	envDBSet := envOv["database.driver"] || envOv["database.dsn"]
	_, statErr := os.Stat(p.ConfigPath)
	if statErr != nil && !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}
	configExists := statErr == nil

	baseCfg, err := config.Load(p.ConfigPath)
	if err != nil {
		return err
	}
	cfg := adjust(baseCfg, o)

	enterSetup := true
	initDBError := ""
	if configExists || envDBSet {
		probe, perr := db.Probe(ctx, db.Options{Driver: cfg.Database.Driver, DSN: cfg.Database.DSN, DataDir: p.DataDir})
		switch {
		case perr == nil && probe.OK:
			if probe.State == db.ProbeSkinLoomInstalled {
				enterSetup = false
				_ = secrets.ClearSetupToken(p.DataDir)
				_ = secrets.ClearSetupPending(p.DataDir)
				_ = secrets.MarkSetupDone(p.DataDir)
			} else if probe.State == db.ProbeSuspectedBS {
				initDBError = probe.Summary
			}
		default:
			isInstalled := (configExists && !secrets.IsSetupPending(p.DataDir)) || secrets.IsSetupDone(p.DataDir)
			if isInstalled {
				return fmt.Errorf("database unavailable for installed site: %w", cmp.Or(perr, errors.New(probe.ErrorDetail)))
			}
			if probe.ErrorDetail != "" {
				initDBError = redactSecretErr(probe.ErrorDetail)
			} else if perr != nil {
				initDBError = redactSecretErr(perr.Error())
			}
		}
	}

	logCtl, log, logFile, err := initLogger(cfg, p)
	if err != nil {
		return err
	}
	if logFile != nil {
		defer logFile.Close()
	}

	broker := pubsub.New()
	themesReg, settingsReg, err := OpenThemesAndRegistry(p.DataDir, log.With("component", "theme"))
	if err != nil {
		return err
	}

	var (
		coreMu     sync.Mutex
		activeCore *Core
	)
	defer func() {
		coreMu.Lock()
		c := activeCore
		coreMu.Unlock()
		if c != nil {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			c.Close(shutdownCtx)
		}
	}()

	runCtx, stop := context.WithCancel(ctx)
	defer stop()

	resolver, err := realip.New(cfg.Server.TrustedProxies) // validated by config.Load
	if err != nil {
		return err
	}

	var (
		gw        *gateway.Gateway
		localWeb  atomic.Pointer[string]
		watchOnce sync.Once
	)
	webReady := func() bool {
		addr := localWeb.Load()
		if addr == nil {
			return true
		}
		c, err := net.DialTimeout("tcp", *addr, 500*time.Millisecond)
		if err != nil {
			return false
		}
		_ = c.Close()
		return true
	}
	gw = gateway.New(http.NotFoundHandler(), log.With("component", "gateway"), resolver)
	web := launcher.NewWeb(log.With("component", "launcher"), log.With("component", "web"))
	gw.SetWebStatus(func() gateway.WebStatus {
		st := web.Status()
		return gateway.WebStatus{
			Mode:      st.Mode,
			State:     st.State,
			Attempts:  st.Attempts,
			Failures:  st.Failures,
			StartedAt: st.StartedAt,
			LastErr:   st.LastErr,
			NextRetry: st.NextRetry,
			Output:    st.Output,
		}
	})
	webStatusInfo := func() httpapi.WebStatusInfo {
		st := web.Status()
		var nextRetryIn int
		if !st.NextRetry.IsZero() {
			if rem := time.Until(st.NextRetry); rem > 0 {
				nextRetryIn = int(math.Ceil(rem.Seconds()))
			}
		}
		return httpapi.WebStatusInfo{
			Mode:        st.Mode,
			State:       st.State,
			Attempts:    st.Attempts,
			Failures:    st.Failures,
			LastErr:     st.LastErr,
			NextRetryIn: nextRetryIn,
			Output:      st.Output,
		}
	}

	var applyWeb func(config.Config)
	startConfigWatch := func(c *Core) {
		watchOnce.Do(func() {
			config.Watch(runCtx, c.Paths.ConfigPath, c.Config, log.With("component", "config"), func(_, next config.Config, changed []string) {
				next = adjust(next, o)
				var cold []string
				webChanged := false
				for _, k := range changed {
					if config.IsCold(k) {
						cold = append(cold, k)
					}
					if strings.HasPrefix(k, "web.") {
						webChanged = true
					}
				}
				c.LogCtl.Configure(next.Log.Level, next.Log.Format)
				if res, err := realip.New(next.Server.TrustedProxies); err == nil {
					gw.SetResolver(res)
				}
				warnUnknownKeys(c.Paths.ConfigPath, log)
				if webChanged && applyWeb != nil {
					applyWeb(next)
				}
				log.Info("config file reloaded", "changed", changed)
				if len(cold) > 0 {
					log.Warn("these keys only take effect after `skinloom restart`", "keys", cold)
				}
			})
		})
	}

	mountRunningCore := func(core *Core, setupCtl httpapi.SetupController) error {
		sec, err := secrets.Ensure(core.Paths.DataDir)
		if err != nil {
			return err
		}
		nextCfg := adjust(core.Config, o)
		if res, err := realip.New(nextCfg.Server.TrustedProxies); err == nil {
			gw.SetResolver(res)
		}
		deps := core.APIDeps(log.With("component", "http"))
		deps.Setup = setupCtl
		deps.AdminToken = sec.AdminToken
		deps.ClientInfo = func(r *http.Request) realip.Info { return gw.ClientInfo(r) }
		deps.Runtime = httpapi.RuntimeInfo{
			Started: time.Now(),
			Role:    nextCfg.Server.Role,
			Web:     nextCfg.Web.Mode,
			Storage: cmp.Or(nextCfg.Storage.Driver, "local"),
		}
		deps.WebReady = webReady
		deps.WebStatus = webStatusInfo
		fullAPI, _ := httpapi.New(deps)

		gw.SetHeaders(func(h http.Header) {
			if yggdrasil.PolicyFrom(core.Settings.Current()).EnableALI {
				h.Set("X-Authlib-Injector-API-Location", httpapi.YggdrasilPrefix) // relative: the same site
			}
		})
		if core.Plugins != nil {
			gw.SetPublicRouteMatcher(core.Plugins.HasPublicRoute)
		} else {
			gw.SetPublicRouteMatcher(nil)
		}
		gw.SetAPI(fullAPI)

		coreMu.Lock()
		activeCore = core
		coreMu.Unlock()

		startBackgroundWorkers(runCtx, core, nextCfg)
		startConfigWatch(core)
		return nil
	}

	var setupToken string
	if !enterSetup {
		core, err := OpenCoreWithOptions(ctx, p, CoreOptions{
			Broker:              broker,
			Themes:              themesReg,
			SettingsReg:         settingsReg,
			LogCtl:              logCtl,
			Log:                 log,
			SkipConfigTemplate:  true,
			AllowUnsignedNative: cfg.Plugins.AllowUnsignedNative || cfg.Web.Mode == "dev",
		})
		if err != nil {
			return err
		}
		if err := mountRunningCore(core, nil); err != nil {
			return err
		}
	} else {
		setupToken, err = secrets.CreateSetupToken(p.DataDir, account.SetupTokenTTL)
		if err != nil {
			return err
		}
		var sc *setupController
		sc = newSetupController(
			p,
			cfg,
			envOv,
			initDBError,
			logCtl,
			log,
			broker,
			themesReg,
			settingsReg,
			func(newCore *Core) error {
				if err := mountRunningCore(newCore, sc); err != nil {
					return err
				}
				logging.Notice(log, "setup completed; switched to running mode",
					"database", newCore.DB.Dialect, "storage", cmp.Or(newCore.Config.Storage.Driver, "local"))
				return nil
			},
		)
		setupDeps := httpapi.Deps{
			Log:         log.With("component", "http"),
			Setup:       sc,
			SettingsReg: settingsReg,
			Themes:      themesReg,
			Broker:      broker,
			ClientInfo:  func(r *http.Request) realip.Info { return gw.ClientInfo(r) },
			Runtime:     httpapi.RuntimeInfo{Started: time.Now(), Role: cfg.Server.Role, Web: cfg.Web.Mode, Storage: cmp.Or(cfg.Storage.Driver, "local")},
			WebReady:    webReady,
			WebStatus:   webStatusInfo,
		}
		setupAPI, _ := httpapi.New(setupDeps)
		gw.SetAPI(setupAPI)
	}

	ln, err := net.Listen("tcp", cfg.Server.Listen)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.Server.Listen, err)
	}
	port := ln.Addr().(*net.TCPAddr).Port

	var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gw.ServeHTTP(w, r)
	})
	if cfg.Server.Role != "all" {
		// In API-only mode, still serve the swapped API directly without frontend proxy.
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gw.ServeAPI(w, r)
		})
	}
	conns := newConnTracker()
	srv := &http.Server{
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    64 << 10,
		ConnState:         conns.ConnState,
	}
	// Long-lived SSE streams and never-used pre-connected sockets would otherwise keep
	// Shutdown waiting (for its whole timeout and for 5s respectively).
	srv.RegisterOnShutdown(broker.Close)
	srv.RegisterOnShutdown(conns.CloseFresh)
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	apiInternal := "http://127.0.0.1:" + strconv.Itoa(port)
	applyWeb = func(c config.Config) {
		if c.Server.Role != "all" {
			return
		}
		applyWebConfig(runCtx, c, apiInternal, gw, web, &localWeb, log)
	}
	applyWeb(cfg)
	defer web.Stop()

	local := fmt.Sprintf("http://localhost:%d/", port)
	if !enterSetup {
		coreMu.Lock()
		c := activeCore
		coreMu.Unlock()
		logging.Notice(log, "SkinLoom is running",
			"version", buildinfo.Version, "url", local, "role", cfg.Server.Role, "web", cfg.Web.Mode,
			"database", c.DB.Dialect, "data", p.DataDir)
		if configExists {
			warnUnknownKeys(p.ConfigPath, log)
		}
		log.Debug("admin API token for scripts", "file", secrets.AdminTokenFile(p.DataDir))
	} else {
		logging.Notice(log, "SkinLoom is running in setup mode",
			"version", buildinfo.Version, "url", local, "role", cfg.Server.Role, "web", cfg.Web.Mode, "data", p.DataDir)
		if initDBError != "" {
			log.Warn("initial database probe did not succeed; open the setup wizard to diagnose or reconfigure", "err", initDBError)
		}
	}

	open := local
	if setupToken != "" {
		link := strings.TrimRight(local, "/") + "/setup?token=" + setupToken
		// Only someone who can read this console (or the logs) can claim the new site.
		logging.Notice(log, "first run: open this one-time link to set up SkinLoom", "url", link)
		open = link
	}
	if o.Open {
		go func(target string) {
			deadline := time.NewTimer(3 * time.Second)
			defer deadline.Stop()
			tick := time.NewTicker(150 * time.Millisecond)
			defer tick.Stop()
			for !webReady() {
				select {
				case <-runCtx.Done():
					return
				case <-deadline.C:
					launcher.OpenBrowser(target)
					return
				case <-tick.C:
				}
			}
			launcher.OpenBrowser(target)
		}(open)
	}
	if o.Ready != nil {
		o.Ready(ln.Addr())
	}

	select {
	case <-ctx.Done():
	case err := <-serveErr:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}
	logging.Notice(log, "shutting down")
	stop()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	return nil
}

func startBackgroundWorkers(ctx context.Context, core *Core, cfg config.Config) {
	go core.Settings.Watch(ctx, 2*time.Second)
	if cfg.Web.Mode == "dev" {
		go core.Themes.Watch(ctx, func() { _, _ = core.Settings.Touch(ctx) })
	}
	go core.Jobs.Run(ctx, 5*time.Second)
	go purgeDeletedAccounts(ctx, core)
	go dailyUpkeep(ctx, core)
	go yggdrasilUpkeep(ctx, core)
}

// adjust applies command-line overrides on top of the file/env config.
func adjust(c config.Config, o RunOptions) config.Config {
	if o.Dev {
		c.Web.Mode = "dev"
	}
	if o.NoWeb {
		c.Web.Mode = "off"
	}
	return c
}

func applyWebConfig(ctx context.Context, c config.Config, apiInternal string, gw *gateway.Gateway, web *launcher.Web,
	localWeb *atomic.Pointer[string], log interface {
		Info(string, ...any)
		Warn(string, ...any)
	}) {
	localWeb.Store(nil) // set again below once a local frontend is started
	started := func(port int) {
		addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
		localWeb.Store(&addr)
	}
	switch c.Web.Mode {
	case "off":
		web.Stop()
		gw.ClearWeb("前端已关闭（web.mode = off）。只提供 API 与游戏接口。 / Frontend disabled (web.mode = off).")
	case "external":
		web.Stop()
		u, err := url.Parse(c.Web.URL)
		if err != nil || u.Host == "" {
			gw.ClearWeb("web.url 无效 / invalid web.url: " + c.Web.URL)
			return
		}
		gw.SetWeb(u, "")
		log.Info("proxying pages to external frontend", "url", u.String())
	case "dev":
		devDir := c.Web.DevDir
		if !filepath.IsAbs(devDir) {
			devDir, _ = filepath.Abs(devDir)
		}
		port, ok := frontendPort(c.Web.Port, gw, web, log)
		if !ok {
			return
		}
		spec := launcher.Spec{Mode: "dev", DevDir: devDir, PackageManager: c.Web.PackageManager, Port: port, APIInternal: apiInternal}
		gw.SetWeb(spec.Upstream(), "")
		web.Start(ctx, spec)
		started(port)
	case "managed":
		dir := c.Web.Dir
		if dir == "" {
			dir = launcher.DefaultWebDir()
		}
		serverDir, err := launcher.ResolveServerDir(dir)
		if err != nil {
			web.Stop()
			log.Warn("frontend bundle not found; serving API only", "dir", dir,
				"hint", "use a release bundle, set web.dir, or run `skinloom up --dev` from the repository")
			gw.ClearWeb("未找到前端程序（" + dir + "）。发行包请保持 web/ 目录与 skinloom 同级；开发时运行 skinloom up --dev。")
			return
		}
		node, err := launcher.FindNode(c.Web.Node)
		if err != nil {
			web.Stop()
			gw.ClearWeb(err.Error())
			log.Warn("cannot start frontend", "err", err)
			return
		}
		port, ok := frontendPort(c.Web.Port, gw, web, log)
		if !ok {
			return
		}
		spec := launcher.Spec{Mode: "managed", ServerDir: serverDir, Node: node, Port: port, APIInternal: apiInternal}
		gw.SetWeb(spec.Upstream(), serverDir)
		web.Start(ctx, spec)
		started(port)
	}
}

// frontendPort stops the running frontend (freeing its port) and returns a port that is
// free now, so the gateway never proxies to an unrelated program on a busy port.
func frontendPort(want int, gw *gateway.Gateway, web *launcher.Web, log interface {
	Info(string, ...any)
	Warn(string, ...any)
}) (int, bool) {
	web.Stop()
	port, changed, err := launcher.ResolvePort(want)
	if err != nil {
		gw.ClearWeb(err.Error())
		log.Warn("cannot start frontend", "err", err)
		return 0, false
	}
	if changed && want != 0 {
		log.Warn("web.port is in use by another program; using a free port instead", "configured", want, "using", port)
	}
	return port, true
}

func warnUnknownKeys(path string, log interface{ Warn(string, ...any) }) {
	if keys, err := config.UnknownKeys(path); err == nil && len(keys) > 0 {
		log.Warn("unknown keys in config file (typo?); they are ignored", "path", path, "keys", keys)
	}
}

// purgeDeletedAccounts removes accounts whose deletion grace period is over, at start
// and then hourly. Every node may do it; deleting twice is harmless.
func purgeDeletedAccounts(ctx context.Context, core *Core) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		if n, err := core.Accounts.PurgeDeleted(ctx); err != nil && ctx.Err() == nil {
			core.Log.Warn("purge deleted accounts", "err", err)
		} else if n > 0 {
			core.Log.Info("purged deleted accounts", "count", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// yggdrasilUpkeep loads (on a new site, generates) the Yggdrasil signing key before the
// first launcher needs it, then hourly deletes join sessions, launcher tokens and
// OAuth codes and tokens past use, and unfinished social sign-ins.
func yggdrasilUpkeep(ctx context.Context, core *Core) {
	if err := core.Yggdrasil.Warm(ctx); err != nil && ctx.Err() == nil {
		core.Log.Warn("yggdrasil keys", "err", err)
	}
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if _, err := core.Yggdrasil.Cleanup(ctx); err != nil && ctx.Err() == nil {
			core.Log.Warn("yggdrasil cleanup", "err", err)
		}
		if _, err := core.OAuth.Cleanup(ctx); err != nil && ctx.Err() == nil {
			core.Log.Warn("oauth cleanup", "err", err)
		}
		if _, err := core.Social.Cleanup(ctx); err != nil && ctx.Err() == nil {
			core.Log.Warn("social sign-in cleanup", "err", err)
		}
	}
}

// dailyUpkeep deletes stored texture files that no texture uses (uploads whose
// transaction failed, lost sweep jobs) and sets drifted like counts back to the
// closet counts, daily. Every node may run it; both are safe to run twice.
func dailyUpkeep(ctx context.Context, core *Core) {
	t := time.NewTicker(24 * time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if n, err := core.Textures.SweepOrphans(ctx, time.Now()); err != nil && ctx.Err() == nil {
			core.Log.Warn("sweep orphan texture files", "err", err)
		} else if n > 0 {
			core.Log.Info("deleted orphan texture files", "count", n)
		}
		if n, err := core.Textures.CalibrateLikes(ctx); err != nil && ctx.Err() == nil {
			core.Log.Warn("calibrate texture likes", "err", err)
		} else if n > 0 {
			core.Log.Info("corrected texture like counts", "count", n)
		}
	}
}
