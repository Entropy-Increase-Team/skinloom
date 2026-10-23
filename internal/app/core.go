// Package app wires the services together. Core is shared by the server and by CLI
// commands that need the database (migrate, settings).
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Entropy-Increase-Team/skinloom/internal/account"
	"github.com/Entropy-Increase-Team/skinloom/internal/captcha"
	"github.com/Entropy-Increase-Team/skinloom/internal/config"
	"github.com/Entropy-Increase-Team/skinloom/internal/hook"
	"github.com/Entropy-Increase-Team/skinloom/internal/jobs"
	"github.com/Entropy-Increase-Team/skinloom/internal/logging"
	"github.com/Entropy-Increase-Team/skinloom/internal/mail"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/migrate"
	"github.com/Entropy-Increase-Team/skinloom/internal/pubsub"
	"github.com/Entropy-Increase-Team/skinloom/internal/settings"
	"github.com/Entropy-Increase-Team/skinloom/internal/theme"
	"github.com/Entropy-Increase-Team/skinloom/themes"
)

// Paths locate the data directory and config file.
type Paths struct {
	DataDir    string
	ConfigPath string // empty = <DataDir>/config.toml
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
	Jobs *jobs.Queue
	// FreshConfig is true when this start wrote the default config file.
	FreshConfig bool
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
	logCtl, log := logging.New(os.Stderr, cfg.Log.Level, cfg.Log.Format)

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
	reg := settings.NewRegistry()
	defs := slices.Concat(account.SettingDefs(), captcha.SettingDefs(), mail.SettingDefs())
	if err := reg.Register(defs...); err != nil {
		database.Close()
		return nil, err
	}
	reg.SetValidator("theme.active", themeReg.ValidateActive)
	svc := settings.NewService(reg, database.Ent, broker, bus, log.With("component", "settings"))
	if err := svc.Load(ctx); err != nil {
		database.Close()
		return nil, err
	}
	c := &Core{
		Paths: p, Config: cfg, LogCtl: logCtl, Log: log, DB: database,
		Broker: broker, Bus: bus, Settings: svc, Themes: themeReg, FreshConfig: fresh,
		Accounts: account.NewService(database.Ent, log.With("component", "account")),
		Captcha:  captcha.NewService(captcha.Options{Log: log.With("component", "captcha")}),
		Mailer:   mail.New(log.With("component", "mail")),
		Jobs:     jobs.New(database.Ent, log.With("component", "jobs")),
	}
	c.Jobs.Register(account.MailJobKind, c.sendMail)
	c.Accounts.SetJobNotifier(c.Jobs.Kick)
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
}
