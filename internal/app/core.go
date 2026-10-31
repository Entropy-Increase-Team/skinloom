// Package app wires the services together. Core is shared by the server and by CLI
// commands that need the database (migrate, settings).
package app

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Entropy-Increase-Team/skinloom/internal/account"
	"github.com/Entropy-Increase-Team/skinloom/internal/captcha"
	"github.com/Entropy-Increase-Team/skinloom/internal/cdn"
	"github.com/Entropy-Increase-Team/skinloom/internal/config"
	"github.com/Entropy-Increase-Team/skinloom/internal/hook"
	"github.com/Entropy-Increase-Team/skinloom/internal/jobs"
	"github.com/Entropy-Increase-Team/skinloom/internal/logging"
	"github.com/Entropy-Increase-Team/skinloom/internal/mail"
	"github.com/Entropy-Increase-Team/skinloom/internal/notify"
	"github.com/Entropy-Increase-Team/skinloom/internal/oauth"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/migrate"
	"github.com/Entropy-Increase-Team/skinloom/internal/player"
	"github.com/Entropy-Increase-Team/skinloom/internal/pubsub"
	"github.com/Entropy-Increase-Team/skinloom/internal/render"
	"github.com/Entropy-Increase-Team/skinloom/internal/report"
	"github.com/Entropy-Increase-Team/skinloom/internal/score"
	"github.com/Entropy-Increase-Team/skinloom/internal/settings"
	"github.com/Entropy-Increase-Team/skinloom/internal/social"
	"github.com/Entropy-Increase-Team/skinloom/internal/storage"
	"github.com/Entropy-Increase-Team/skinloom/internal/texture"
	"github.com/Entropy-Increase-Team/skinloom/internal/theme"
	"github.com/Entropy-Increase-Team/skinloom/internal/yggdrasil"
	"github.com/Entropy-Increase-Team/skinloom/themes"
)

// Paths locate the data directory and config file.
type Paths struct {
	DataDir    string
	ConfigPath string // empty = <DataDir>/config.toml
	// LogFile is used when the config sets no log.file (a background service, whose
	// console nobody reads); relative to DataDir.
	LogFile string
	// Serve is set by the process that serves the site: only it writes (and rotates)
	// the log file. A CLI command run next to it logs to the console.
	Serve bool
}

// Resolve fills defaults and makes paths absolute.
func (p Paths) Resolve() (Paths, error) {
	if p.DataDir == "" {
		p.DataDir = "data"
	}
	abs, err := filepath.Abs(p.DataDir)
	if err != nil {
		return p, err
	}
	p.DataDir = abs
	if p.ConfigPath == "" {
		p.ConfigPath = filepath.Join(p.DataDir, "config.toml")
	}
	return p, nil
}

// Core holds the long-lived services.
type Core struct {
	Paths    Paths
	Config   config.Config
	LogCtl   *logging.Logger
	Log      *slog.Logger
	DB       *db.DB
	Broker   *pubsub.Broker
	Bus      *hook.Bus
	Settings *settings.Service
	Themes   *theme.Registry
	Accounts *account.Service
	Captcha  *captcha.Service
	Mailer   *mail.Mailer
	// Jobs runs background work; Run starts its worker (CLI commands do not).
	Jobs      *jobs.Queue
	Textures  *texture.Service
	Players   *player.Service
	Reports   *report.Service
	Renders   *render.Service
	Notify    *notify.Service
	Yggdrasil *yggdrasil.Service
	OAuth     *oauth.Service
	Social    *social.Service
	CDN       *cdn.Service
	// FreshConfig is true when this start wrote the default config file.
	FreshConfig bool

	logFile *os.File // nil without log.file
}

// OpenCore loads config, connects and migrates the database and loads settings and themes.
func OpenCore(ctx context.Context, p Paths) (*Core, error) {
	p, err := p.Resolve()
	if err != nil {
		return nil, err
	}
	// 0750: the data dir holds the database and secrets; other local users stay out.
	if err := os.MkdirAll(p.DataDir, 0o750); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	fresh, err := config.WriteTemplate(p.ConfigPath)
	if err != nil {
		return nil, fmt.Errorf("write default config: %w", err)
	}
	cfg, err := config.Load(p.ConfigPath)
	if err != nil {
		return nil, err
	}
	var (
		out     io.Writer = os.Stderr
		logFile *os.File
	)
	if name := cmp.Or(cfg.Log.File, p.LogFile); name != "" && p.Serve {
		if !filepath.IsAbs(name) {
			name = filepath.Join(p.DataDir, name)
		}
		if logFile, err = logging.OpenFile(name); err != nil {
			return nil, fmt.Errorf("open log file: %w", err)
		}
		out = logging.Tee(logFile, os.Stderr)
	}
	opened := false
	defer func() { // every error return below
		if !opened && logFile != nil {
			logFile.Close()
		}
	}()
	logCtl, log := logging.New(out, cfg.Log.Level, cfg.Log.Format)

	database, err := db.Open(ctx, db.Options{
		Driver: cfg.Database.Driver, DSN: cfg.Database.DSN, DataDir: p.DataDir, MaxOpenConns: cfg.Database.MaxOpenConns,
	})
	if err != nil {
		return nil, err
	}
	if err := migrate.Up(ctx, database, p.DataDir, log.With("component", "migrate")); err != nil {
		database.Close()
		return nil, err
	}

	broker := pubsub.New()
	bus := hook.NewBus(log.With("component", "hook"), 4)
	themeReg := theme.NewRegistry()
	if err := themeReg.LoadDir(themes.Builtin, "builtin"); err != nil {
		database.Close()
		return nil, fmt.Errorf("load built-in themes: %w", err)
	}
	store, err := openStore(ctx, cfg.Storage, p.DataDir)
	if err != nil {
		database.Close()
		return nil, err
	}
	reg := settings.NewRegistry()
	defs := slices.Concat(account.SettingDefs(), captcha.SettingDefs(), mail.SettingDefs(),
		score.SettingDefs(), texture.SettingDefs(), player.SettingDefs(), yggdrasil.SettingDefs(), oauth.SettingDefs(), social.SettingDefs(), cdn.SettingDefs())
	if err := reg.Register(defs...); err != nil {
		database.Close()
		return nil, err
	}
	reg.SetValidator("theme.active", themeReg.ValidateActive)
	reg.SetValidator("theme.overrides", theme.ValidateOverrides)
	reg.SetValidator("home.blocks", theme.ValidateHome)
	svc := settings.NewService(reg, database.Ent, broker, bus, log.With("component", "settings"))
	if err := svc.Load(ctx); err != nil {
		database.Close()
		return nil, err
	}
	c := &Core{
		Paths: p, Config: cfg, LogCtl: logCtl, Log: log, logFile: logFile, DB: database,
		Broker: broker, Bus: bus, Settings: svc, Themes: themeReg, FreshConfig: fresh,
		Accounts: account.NewService(database.Ent, log.With("component", "account")),
		Captcha:  captcha.NewService(captcha.Options{Log: log.With("component", "captcha")}),
		Mailer:   mail.New(log.With("component", "mail")),
		Jobs:     jobs.New(database.Ent, log.With("component", "jobs")),
		Textures: texture.NewService(database.Ent, store, log.With("component", "texture")),
		Players:  player.NewService(database.Ent, log.With("component", "player")),
	}
	c.Reports = report.NewService(database.Ent, c.Textures, log.With("component", "report"))
	c.Renders = render.NewService(4, 64<<20) // 4 renders at once, 64 MiB of results
	c.Notify = notify.NewService(database.Ent, log.With("component", "notify"))
	c.Yggdrasil = yggdrasil.NewService(database.Ent, c.Accounts, c.Players, c.Textures, log.With("component", "yggdrasil"))
	c.OAuth = oauth.NewService(database.Ent, log.With("component", "oauth"))
	c.Social = social.NewService(database.Ent, c.Accounts, log.With("component", "social"))
	c.Jobs.Register(account.MailJobKind, c.sendMail)
	c.Accounts.SetJobNotifier(c.Jobs.Kick)
	c.Jobs.Register(texture.SweepJobKind, c.Textures.HandleSweepJob)
	c.Textures.SetJobNotifier(c.Jobs.Kick)
	c.Accounts.SetBeforeDelete(c.Textures.BeforeUserDelete)
	// Domain events go out through the job queue (hook.Event.Publish): the outbox.
	c.Jobs.Register(hook.OutboxJobKind, bus.Deliver)
	c.Accounts.SetHooks(bus)
	c.Textures.SetHooks(bus)
	c.Players.SetHooks(bus)
	c.Reports.SetHooks(bus)
	c.CDN = cdn.NewService(bus, svc, log.With("component", "cdn"))
	if err := c.CDN.Start(); err != nil {
		database.Close()
		return nil, err
	}
	opened = true
	return c, nil
}

// sendMail is the background job that delivers a queued mail with the mail settings
// in effect when it runs.
func (c *Core) sendMail(ctx context.Context, payload json.RawMessage) error {
	var m mail.Message
	if err := json.Unmarshal(payload, &m); err != nil {
		return jobs.Permanent(err)
	}
	err := c.Mailer.Send(ctx, mail.ConfigFrom(c.Settings.Current()), m)
	if errors.Is(err, mail.ErrDisabled) {
		return jobs.Permanent(err)
	}
	return err
}

// SetupLink returns a one-time link to the first-run setup page, or "" when setup is
// done. base is used when site.url is not configured.
func (c *Core) SetupLink(ctx context.Context, base string) (string, error) {
	token, err := c.Accounts.NewSetupToken(ctx)
	if err != nil || token == "" {
		return "", err
	}
	if site := c.Settings.Current().String("site.url"); site != "" {
		base = site
	}
	return strings.TrimRight(base, "/") + "/setup?token=" + token, nil
}

// Close releases resources.
func (c *Core) Close(ctx context.Context) {
	c.Bus.Close(ctx)
	_ = c.DB.Close()
	if c.logFile != nil {
		_ = c.logFile.Close()
	}
}

// openStore opens the texture store the config names.
func openStore(ctx context.Context, c config.Storage, dataDir string) (storage.Store, error) {
	if c.Driver == "s3" {
		return storage.NewS3(ctx, storage.S3Options{Endpoint: c.Endpoint, Bucket: c.Bucket, Region: c.Region,
			AccessKey: c.AccessKey, SecretKey: c.SecretKey, Prefix: c.Prefix, Insecure: c.Insecure,
			PathStyle: c.PathStyle, PublicURL: c.PublicURL})
	}
	return storage.NewLocal(c.Dir(dataDir))
}
