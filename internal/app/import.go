package app

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/Entropy-Increase-Team/skinloom/internal/config"
	"github.com/Entropy-Increase-Team/skinloom/internal/httpapi"
	"github.com/Entropy-Increase-Team/skinloom/internal/importer/bs"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/migrate"
	"github.com/Entropy-Increase-Team/skinloom/internal/sitesettings"
)

// ImportBS imports a Blessing Skin site into the install at p, creating the data
// directory, config and database first when they do not exist yet (the import replaces
// the setup page). The site must not be running: it caches that setup is pending.
func ImportBS(ctx context.Context, p Paths, src bs.Source, o bs.Options) (*bs.Report, error) {
	p, err := p.Resolve()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(p.DataDir, 0o750); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	if _, err := config.WriteTemplate(p.ConfigPath); err != nil {
		return nil, fmt.Errorf("write default config: %w", err)
	}
	cfg, err := config.Load(p.ConfigPath)
	if err != nil {
		return nil, err
	}
	database, err := db.Open(ctx, db.Options{Driver: cfg.Database.Driver, DSN: cfg.Database.DSN, DataDir: p.DataDir, MaxOpenConns: 4})
	if err != nil {
		return nil, err
	}
	defer database.Close()
	if err := migrate.Up(ctx, database, p.DataDir, slog.New(slog.NewTextHandler(io.Discard, nil))); err != nil {
		return nil, err
	}
	store, err := openStore(ctx, cfg.Storage, p.DataDir)
	if err != nil {
		return nil, err
	}
	reg, err := sitesettings.Builtin()
	if err != nil {
		return nil, err
	}
	t := bs.Target{DB: database, Store: store, Settings: reg}
	if cfg.Storage.Driver != "s3" {
		t.StoreDir = cfg.Storage.Dir(p.DataDir)
	}
	for _, l := range httpapi.Locales {
		t.Locales = append(t.Locales, l.Code)
	}
	return bs.Run(ctx, src, t, o)
}
