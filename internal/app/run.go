package app

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/internal/buildinfo"
	"github.com/Entropy-Increase-Team/skinloom/internal/config"
	"github.com/Entropy-Increase-Team/skinloom/internal/gateway"
	"github.com/Entropy-Increase-Team/skinloom/internal/httpapi"
	"github.com/Entropy-Increase-Team/skinloom/internal/launcher"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/realip"
	"github.com/Entropy-Increase-Team/skinloom/internal/secrets"
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
	core, err := OpenCore(ctx, o.Paths)
	if err != nil {
		return err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		core.Close(shutdownCtx)
	}()
	log := core.Log
	cfg := adjust(core.Config, o)

	sec, err := secrets.Ensure(core.Paths.DataDir)
	if err != nil {
		return err
	}

	runCtx, stop := context.WithCancel(ctx)
	defer stop()
	go core.Settings.Watch(runCtx, 2*time.Second)

	api, _ := httpapi.New(httpapi.Deps{
		Log: log.With("component", "http"), DB: core.DB, Settings: core.Settings,
		Themes: core.Themes, Broker: core.Broker, AdminToken: sec.AdminToken,
	})

	ln, err := net.Listen("tcp", cfg.Server.Listen)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.Server.Listen, err)
	}
	port := ln.Addr().(*net.TCPAddr).Port

	resolver, err := realip.New(cfg.Server.TrustedProxies) // validated by config.Load
	if err != nil {
		return err
	}
	var handler http.Handler = api
	gw := gateway.New(api, log.With("component", "gateway"), resolver)
	web := launcher.NewWeb(log.With("component", "launcher"), log.With("component", "web"))
	if cfg.Server.Role == "all" {
		handler = gw
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
	srv.RegisterOnShutdown(core.Broker.Close)
	srv.RegisterOnShutdown(conns.CloseFresh)
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	apiInternal := "http://127.0.0.1:" + strconv.Itoa(port)
	applyWeb := func(c config.Config) {
		if c.Server.Role != "all" {
			return
		}
		applyWebConfig(runCtx, c, apiInternal, gw, web, log)
	}
	applyWeb(cfg)
	defer web.Stop()

	config.Watch(runCtx, core.Paths.ConfigPath, core.Config, log.With("component", "config"), func(_, next config.Config, changed []string) {
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
		core.LogCtl.Configure(next.Log.Level, next.Log.Format)
		if res, err := realip.New(next.Server.TrustedProxies); err == nil {
			gw.SetResolver(res)
		}
		warnUnknownKeys(core.Paths.ConfigPath, log)
		if webChanged {
			applyWeb(next)
		}
		log.Info("config file reloaded", "changed", changed)
		if len(cold) > 0 {
			log.Warn("these keys only take effect after `skinloom restart`", "keys", cold)
		}
	})

	local := fmt.Sprintf("http://localhost:%d/", port)
	log.Info("SkinLoom is running",
		"version", buildinfo.Version, "url", local, "role", cfg.Server.Role, "web", cfg.Web.Mode,
		"database", core.DB.Dialect, "data", core.Paths.DataDir)
	if core.FreshConfig {
		log.Info("wrote default config; edit it any time, most keys apply without a restart", "path", core.Paths.ConfigPath)
	}
	warnUnknownKeys(core.Paths.ConfigPath, log)
	log.Info("admin API token (temporary until accounts land in M1)", "file", secrets.AdminTokenFile(core.Paths.DataDir))
	if o.Open {
		launcher.OpenBrowser(local)
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
	log.Info("shutting down")
	stop()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	return nil
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

func applyWebConfig(ctx context.Context, c config.Config, apiInternal string, gw *gateway.Gateway, web *launcher.Web, log interface {
	Info(string, ...any)
	Warn(string, ...any)
}) {
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
