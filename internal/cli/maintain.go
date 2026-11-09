package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Entropy-Increase-Team/skinloom/internal/app"
	"github.com/Entropy-Increase-Team/skinloom/internal/daemon"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/migrate"
)

func serviceCmd(paths *app.Paths) *cobra.Command {
	user := ""
	if runtime.GOOS == "linux" {
		user = "skinloom"
	}
	options := func() (daemon.Options, error) {
		p, err := paths.Resolve()
		if err != nil {
			return daemon.Options{}, err
		}
		exe, err := os.Executable()
		if err != nil {
			return daemon.Options{}, err
		}
		if exe, err = filepath.EvalSymlinks(exe); err != nil {
			return daemon.Options{}, err
		}
		o := daemon.Options{Exe: exe, DataDir: p.DataDir, User: user}
		if paths.ConfigPath != "" { // a config outside the data directory
			o.ConfigPath = p.ConfigPath
		}
		return o, nil
	}
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Run SkinLoom as a background service (systemd, launchd or a Windows service)",
		Long: `Registers this binary with the system's service manager: it starts at boot, restarts
after a crash and logs to <data>/logs/skinloom.log (unless log.file says otherwise).
Run install from an administrator terminal on Windows, with sudo elsewhere.`,
	}
	install := &cobra.Command{
		Use:   "install",
		Short: "Install the service (then: skinloom service start)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			o, err := options()
			if err != nil {
				return err
			}
			if err := daemon.Install(o); err != nil {
				return err
			}
			fmt.Printf("installed service %s: %s %s\n", daemon.Name, o.Exe, strings.Join([]string{"service", "run", "--data", o.DataDir}, " "))
			fmt.Printf("start it with: skinloom service start --data %s\n", o.DataDir)
			fmt.Printf("first start: the one-time setup link is in %s\n", filepath.Join(o.DataDir, filepath.FromSlash(daemon.LogFile)))
			return nil
		},
	}
	if runtime.GOOS != "windows" {
		install.Flags().StringVar(&user, "user", user, `system user the service runs as ("" = root); it must exist and is given the data directory`)
	}
	cmd.AddCommand(install)
	for _, action := range []struct{ name, short string }{
		{"uninstall", "Remove the service (the data directory is kept)"},
		{"start", "Start the service"},
		{"stop", "Stop the service"},
		{"restart", "Restart the service"},
	} {
		cmd.AddCommand(&cobra.Command{
			Use:   action.name,
			Short: action.short,
			RunE: func(cmd *cobra.Command, _ []string) error {
				o, err := options()
				if err != nil {
					return err
				}
				if err := daemon.Control(o, action.name); err != nil {
					return err
				}
				fmt.Printf("%s: done\n", action.name)
				return nil
			},
		})
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show whether the service is installed and running",
		RunE: func(cmd *cobra.Command, _ []string) error {
			o, err := options()
			if err != nil {
				return err
			}
			st, err := daemon.Status(o)
			if errors.Is(err, daemon.ErrNotInstalled) {
				fmt.Println("not installed")
				return nil
			}
			if err != nil {
				return err
			}
			fmt.Println(st)
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:    "run",
		Short:  "Serve under the service manager (what the installed service runs)",
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			o, err := options()
			if err != nil {
				return err
			}
			run := *paths
			if !daemon.Interactive() {
				run.LogFile = daemon.LogFile // nobody reads a service's console
			}
			s, err := daemon.New(o, func(ctx context.Context) error { return app.Run(ctx, app.RunOptions{Paths: run}) })
			if err != nil {
				return err
			}
			return daemon.Run(s)
		},
	})
	return cmd
}

func backupCmd(paths *app.Paths) *cobra.Command {
	var o app.BackupOptions
	noTextures := false
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Write the database (SQLite), config, secrets and textures to one .tar.gz",
		Long: `Writes <data>/backups/skinloom-<time>.tar.gz (or into --out). The site may keep running:
SQLite is copied with VACUUM INTO. With MySQL or PostgreSQL the archive holds everything
but the database; dump that with mysqldump or pg_dump. The archive holds the secrets:
keep it as safe as the data directory.`,
		Example: "  skinloom backup --keep 7\n  skinloom backup --out /mnt/backups --no-textures",
		RunE: func(cmd *cobra.Command, _ []string) error {
			o.Textures = !noTextures
			path, m, err := app.Backup(cmd.Context(), *paths, o)
			if err != nil {
				return err
			}
			fmt.Printf("backup written: %s\n  database: %s, migration %d", path, m.Database, m.Migration)
			if !m.HasDatabase {
				fmt.Print(" (not included: dump it with its own tools)")
			}
			fmt.Printf("\n  textures: %d files, %d bytes\n", m.Textures, m.TextureBytes)
			return nil
		},
	}
	cmd.Flags().StringVar(&o.OutDir, "out", "", "directory to write to (default <data>/backups)")
	cmd.Flags().BoolVar(&noTextures, "no-textures", false, "leave the texture files out (e.g. when they live in S3)")
	cmd.Flags().IntVar(&o.Keep, "keep", 0, "keep only the newest N archives in the directory (0 = keep all)")
	return cmd
}

func restoreCmd(paths *app.Paths) *cobra.Command {
	force := false
	cmd := &cobra.Command{
		Use:   "restore <archive>",
		Short: "Restore a backup into a data directory (stop the site first)",
		Long: `Unpacks an archive written by "skinloom backup" into the data directory: config, secrets,
the SQLite database and the textures (through the configured storage, local or S3). An
existing install is refused unless --force is given. Start the site afterwards; it
applies any newer migrations itself.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := app.Restore(cmd.Context(), *paths, args[0], force)
			if err != nil {
				return err
			}
			fmt.Printf("restored a backup of SkinLoom %s from %s\n", res.Manifest.Version, res.Manifest.CreatedAt.Local().Format(time.DateTime))
			if res.Database != "" {
				fmt.Printf("  database: %s\n", res.Database)
			} else {
				fmt.Printf("  database: not in the backup (%s): restore it from your own dump\n", res.Manifest.Database)
			}
			fmt.Printf("  textures: %d files\n", res.Textures)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing config and database")
	return cmd
}

func migrateCmd(paths *app.Paths) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "Apply database migrations (also done automatically on start)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			core, err := app.OpenCore(cmd.Context(), *paths) // OpenCore migrates
			if err != nil {
				return err
			}
			defer core.Close(cmd.Context())
			fmt.Println("database is up to date")
			return nil
		},
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "List migrations and whether they are applied",
		RunE: func(cmd *cobra.Command, _ []string) error {
			core, err := app.OpenCore(cmd.Context(), *paths)
			if err != nil {
				return err
			}
			defer core.Close(cmd.Context())
			list, err := migrate.Status(cmd.Context(), core.DB)
			if err != nil {
				return err
			}
			for _, m := range list {
				applied := "pending"
				if !m.AppliedAt.IsZero() {
					applied = m.AppliedAt.Local().Format(time.DateTime)
				}
				fmt.Printf("%-16d %-28s %s\n", m.Source.Version, baseName(m.Source.Path), applied)
			}
			return nil
		},
	})
	return cmd
}
