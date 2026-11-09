// Package app wires the services together. Core is shared by the server and by CLI
// commands that need the database (migrate, settings).
package app

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Entropy-Increase-Team/skinloom/internal/config"
	"github.com/Entropy-Increase-Team/skinloom/internal/hook"
	"github.com/Entropy-Increase-Team/skinloom/internal/httpapi"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/account"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/captcha"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/cdn"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/mail"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/notify"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/oauth"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/player"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/render"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/report"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/settings"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/sitesettings"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/social"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/texture"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/theme"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/yggdrasil"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/jobs"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/logging"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/migrate"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/pubsub"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/storage"
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
	c := &Core{Paths: p, Config: cfg, FreshConfig: fresh}
	ok := false
	defer func() { // every error return below
		if !ok {
			c.Close(context.Background())
		}
	}()
	if name := cmp.Or(cfg.Log.File, p.LogFile); name != "" && p.Serve {
		if !filepath.IsAbs(name) {
			name = filepath.Join(p.DataDir, name)
		}
		if c.logFile, err = logging.OpenFile(name); err != nil {
			return nil, fmt.Errorf("open log file: %w", err)
		}
	}
	logOpts := logging.Options{Console: os.Stderr, Level: cfg.Log.Level, Format: cfg.Log.Format}
	logOpts.Pretty, logOpts.Color = logging.ConsoleStyle(os.Stderr)
	if c.logFile != nil { // a nil *os.File would still be a non-nil io.Writer
		logOpts.File = c.logFile
	}
	c.LogCtl, c.Log = logging.New(logOpts)
	log := c.Log

	if c.DB, err = db.Open(ctx, db.Options{
		Driver: cfg.Database.Driver, DSN: cfg.Database.DSN, DataDir: p.DataDir, MaxOpenConns: cfg.Database.MaxOpenConns,
	}); err != nil {
		return nil, err
	}
	if err := migrate.Up(ctx, c.DB, p.DataDir, log.With("component", "migrate")); err != nil {
		return nil, err
	}
	client := c.DB.Ent

	c.Broker = pubsub.New()
	c.Bus = hook.NewBus(log.With("component", "hook"), 4)
	c.Themes = theme.NewRegistry()
	if err := c.Themes.LoadDir(themes.Builtin, "builtin"); err != nil {
		return nil, fmt.Errorf("load built-in themes: %w", err)
	}
	store, err := openStore(ctx, cfg.Storage, p.DataDir)
	if err != nil {
		return nil, err
	}
	reg, err := sitesettings.Registry(c.Themes)
	if err != nil {
		return nil, err
	}
	c.Settings = settings.NewService(reg, client, c.Broker, c.Bus, log.With("component", "settings"))
	if err := c.Settings.Load(ctx); err != nil {
		return nil, err
	}

	c.Accounts = account.NewService(client, log.With("component", "account"))
	c.Captcha = captcha.NewService(captcha.Options{Log: log.With("component", "captcha")})
	c.Mailer = mail.New(log.With("component", "mail"))
	c.Jobs = jobs.New(client, log.With("component", "jobs"))
	c.Textures = texture.NewService(client, store, log.With("component", "texture"))
	c.Players = player.NewService(client, log.With("component", "player"))
	c.Reports = report.NewService(client, c.Textures, log.With("component", "report"))
	// Rendering is CPU-bound: one render per core at most, 64 MiB of results.
	c.Renders = render.NewService(runtime.GOMAXPROCS(0), 64<<20)
	c.Notify = notify.NewService(client, log.With("component", "notify"))
	c.Yggdrasil = yggdrasil.NewService(client, c.Accounts, c.Players, c.Textures, log.With("component", "yggdrasil"))
	c.OAuth = oauth.NewService(client, log.With("component", "oauth"))
	c.Social = social.NewService(client, c.Accounts, log.With("component", "social"))

	// Background jobs, and the calls modules make into each other.
	c.Jobs.Register(account.MailJobKind, c.sendMail)
	c.Jobs.Register(texture.SweepJobKind, c.Textures.HandleSweepJob)
	// Domain events go out through the job queue (hook.Event.Publish): the outbox.
	c.Jobs.Register(hook.OutboxJobKind, c.Bus.Deliver)
	c.Accounts.SetJobNotifier(c.Jobs.Kick)
	c.Textures.SetJobNotifier(c.Jobs.Kick)
	c.Accounts.SetBeforeDelete(c.Textures.BeforeUserDelete)
	c.Accounts.SetHooks(c.Bus)
	c.Textures.SetHooks(c.Bus)
	c.Players.SetHooks(c.Bus)
	c.Reports.SetHooks(c.Bus)
	c.CDN = cdn.NewService(c.Bus, c.Settings, log.With("component", "cdn"))
	if err := c.CDN.Start(); err != nil {
		return nil, err
	}
	ok = true
	return c, nil
}

// APIDeps returns the services the HTTP API uses. The caller adds what depends on how
// the process serves (client address resolution, runtime info, frontend readiness).
func (c *Core) APIDeps(log *slog.Logger) httpapi.Deps {
	return httpapi.Deps{
		Log:       log,
		DB:        c.DB,
		Settings:  c.Settings,
		Themes:    c.Themes,
		Broker:    c.Broker,
		Accounts:  c.Accounts,
		Captcha:   c.Captcha,
		Mailer:    c.Mailer,
		Textures:  c.Textures,
		Players:   c.Players,
		Reports:   c.Reports,
		Renders:   c.Renders,
		Notify:    c.Notify,
		Yggdrasil: c.Yggdrasil,
		OAuth:     c.OAuth,
		Social:    c.Social,
		CDN:       c.CDN,
	}
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

// Close releases resources, also those of a Core that OpenCore did not finish.
func (c *Core) Close(ctx context.Context) {
	if c.Bus != nil {
		c.Bus.Close(ctx)
	}
	if c.DB != nil {
		_ = c.DB.Close()
	}
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
