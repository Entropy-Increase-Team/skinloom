// Package cli implements the skinloom command line.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/Entropy-Increase-Team/skinloom/internal/app"
	"github.com/Entropy-Increase-Team/skinloom/internal/audit"
	"github.com/Entropy-Increase-Team/skinloom/internal/buildinfo"
	"github.com/Entropy-Increase-Team/skinloom/internal/daemon"
	"github.com/Entropy-Increase-Team/skinloom/internal/httpapi"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/migrate"
	"github.com/Entropy-Increase-Team/skinloom/internal/secrets"
)

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := newRoot().ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	return 0
}

func newRoot() *cobra.Command {
	var paths app.Paths
	root := &cobra.Command{
		Use:           "skinloom",
		Short:         "SkinLoom (云织) — an open-source, self-hosted Minecraft skin site",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	defaultData := os.Getenv("SKINLOOM_DATA")
	if defaultData == "" {
		defaultData = "data"
	}
	root.PersistentFlags().StringVar(&paths.DataDir, "data", defaultData, "data directory (database, config, secrets, uploads); env SKINLOOM_DATA")
	root.PersistentFlags().StringVar(&paths.ConfigPath, "config", os.Getenv("SKINLOOM_CONFIG"), "config file (default <data>/config.toml); env SKINLOOM_CONFIG")

	root.AddCommand(upCmd(&paths), serveCmd(&paths), migrateCmd(&paths), settingsCmd(&paths),
		adminCmd(&paths), setupLinkCmd(&paths), backupCmd(&paths), restoreCmd(&paths), serviceCmd(&paths), healthcheckCmd(), openapiCmd(), versionCmd())
	return root
}

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

// healthcheckCmd lets container images without a shell or curl (the distroless
// backend) declare a HEALTHCHECK, and gives compose / Kubernetes an exec probe.
func healthcheckCmd() *cobra.Command {
	url := os.Getenv("SKINLOOM_HEALTHCHECK_URL")
	if url == "" {
		url = "http://127.0.0.1:17546/readyz"
	}
	cmd := &cobra.Command{
		Use:   "healthcheck",
		Short: "Exit 0 when the local server is ready (for Docker / Kubernetes health checks)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				return err
			}
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				return err
			}
			_ = res.Body.Close()
			if res.StatusCode != http.StatusOK {
				return fmt.Errorf("%s: %s", url, res.Status)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&url, "url", url, "readiness URL; env SKINLOOM_HEALTHCHECK_URL")
	return cmd
}

func upCmd(paths *app.Paths) *cobra.Command {
	var o app.RunOptions
	cmd := &cobra.Command{
		Use:   "up",
		Short: "Start the whole site: backend, frontend and gateway on one port",
		Long: `Start the backend and supervise the Next.js frontend, serving both on one port.

Release bundles ship the frontend in web/ and Node.js in runtime/node next to the binary;
the start scripts just run "skinloom up" (start.bat and start.command add --open).
In a source checkout use --dev, which runs the frontend dev server from ./web with
bun (or pnpm / npm when bun is not installed; see web.package_manager).`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			o.Paths = *paths
			return app.Run(cmd.Context(), o)
		},
	}
	cmd.Flags().BoolVar(&o.Open, "open", false, "open the site in the default browser")
	cmd.Flags().BoolVar(&o.Dev, "dev", false, "development: run the frontend dev server from ./web (bun, pnpm or npm)")
	cmd.Flags().BoolVar(&o.NoWeb, "no-web", false, "backend only; same as `skinloom serve`")
	return cmd
}

func serveCmd(paths *app.Paths) *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the backend only (frontend deployed separately)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return app.Run(cmd.Context(), app.RunOptions{Paths: *paths, NoWeb: true})
		},
	}
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

func settingsCmd(paths *app.Paths) *cobra.Command {
	cmd := &cobra.Command{Use: "settings", Short: "Read or change runtime settings (running servers pick changes up within seconds)"}
	cmd.AddCommand(&cobra.Command{
		Use:   "get [key]",
		Short: "Print one setting or all of them",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			core, err := app.OpenCore(cmd.Context(), *paths)
			if err != nil {
				return err
			}
			defer core.Close(cmd.Context())
			snap := core.Settings.Current()
			if len(args) == 1 {
				if _, ok := core.Settings.Registry().Def(args[0]); !ok {
					return fmt.Errorf("unknown setting %q", args[0])
				}
				fmt.Println(string(snap.Raw(args[0])))
				return nil
			}
			for _, d := range core.Settings.Registry().All() {
				marker := " "
				if snap.IsSet(d.Key) {
					marker = "*"
				}
				fmt.Printf("%s %-22s %s\n", marker, d.Key, snap.Raw(d.Key))
			}
			fmt.Printf("\nconfig version %d (* = changed from default)\n", snap.Version)
			return nil
		},
	})
	cmd.AddCommand(&cobra.Command{
		Use:     "set <key> <json-value>",
		Short:   "Change a setting; the value is JSON (strings need quotes), null resets it",
		Example: `  skinloom settings set site.name '{"zh-CN":"我的皮肤站","en":"My Skins"}'` + "\n" + `  skinloom settings set theme.overrides '{"light":{"primary":"oklch(0.6 0.2 300)"}}'`,
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			core, err := app.OpenCore(cmd.Context(), *paths)
			if err != nil {
				return err
			}
			defer core.Close(cmd.Context())
			raw := json.RawMessage(args[1])
			if !json.Valid(raw) {
				raw, _ = json.Marshal(args[1]) // allow unquoted plain strings
			}
			// Recorded like a change in the admin panel, without an actor (the console).
			rec := &audit.Entry{Action: "settings.update", TargetType: "settings", Meta: map[string]any{"via": "cli"}}
			snap, err := core.Settings.Set(cmd.Context(), nil, map[string]json.RawMessage{args[0]: raw}, rec)
			if err != nil {
				return err
			}
			fmt.Printf("%s = %s (config version %d)\n", args[0], snap.Raw(args[0]), snap.Version)
			return nil
		},
	})
	return cmd
}

func adminCmd(paths *app.Paths) *cobra.Command {
	cmd := &cobra.Command{Use: "admin", Short: "Administration helpers"}
	cmd.AddCommand(&cobra.Command{
		Use:   "token",
		Short: "Print the admin API token for scripts (browsers sign in as an administrator instead)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := paths.Resolve()
			if err != nil {
				return err
			}
			s, err := secrets.Ensure(p.DataDir)
			if err != nil {
				return err
			}
			fmt.Println(s.AdminToken)
			return nil
		},
	})
	return cmd
}

func setupLinkCmd(paths *app.Paths) *cobra.Command {
	return &cobra.Command{
		Use:   "setup-link",
		Short: "Print a new one-time link to the first-run setup page (valid 24 hours)",
		Long: `Before the first administrator exists, the setup page only opens with a one-time
token. "skinloom up" prints a link on every start; this command makes another one,
e.g. inside a container: docker exec <container> skinloom setup-link`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			core, err := app.OpenCore(cmd.Context(), *paths)
			if err != nil {
				return err
			}
			defer core.Close(cmd.Context())
			base := "http://localhost:17546"
			if _, port, err := net.SplitHostPort(core.Config.Server.Listen); err == nil && port != "" {
				base = "http://localhost:" + port
			}
			link, err := core.SetupLink(cmd.Context(), base)
			if err != nil {
				return err
			}
			if link == "" {
				return errors.New("setup is already complete; sign in with the administrator account")
			}
			fmt.Println(link)
			return nil
		},
	}
}

func openapiCmd() *cobra.Command {
	var format, output string
	cmd := &cobra.Command{
		Use:   "openapi",
		Short: "Print the OpenAPI 3.1 document (used to generate the frontend client)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, api := httpapi.New(httpapi.Deps{})
			var (
				out []byte
				err error
			)
			switch format {
			case "json":
				out, err = json.MarshalIndent(api.OpenAPI(), "", "  ")
			case "yaml":
				out, err = api.OpenAPI().YAML()
			default:
				return fmt.Errorf("--format must be json or yaml")
			}
			if err != nil {
				return err
			}
			out = append(out, '\n')
			if output != "" {
				return os.WriteFile(output, out, 0o644)
			}
			_, err = os.Stdout.Write(out)
			return err
		},
	}
	cmd.Flags().StringVar(&format, "format", "json", "json | yaml")
	cmd.Flags().StringVarP(&output, "output", "o", "", "write to a file instead of stdout")
	return cmd
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(*cobra.Command, []string) {
			parts := []string{"SkinLoom " + buildinfo.Version}
			if buildinfo.Commit != "" {
				parts = append(parts, "commit "+buildinfo.Commit)
			}
			if buildinfo.Date != "" {
				parts = append(parts, "built "+buildinfo.Date)
			}
			fmt.Println(strings.Join(parts, ", "))
		},
	}
}

func baseName(p string) string {
	i := strings.LastIndexAny(p, `/\`)
	return p[i+1:]
}
