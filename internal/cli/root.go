// Package cli implements the skinloom command line.
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/Entropy-Increase-Team/skinloom/internal/app"
	"github.com/Entropy-Increase-Team/skinloom/internal/buildinfo"
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
		adminCmd(&paths), openapiCmd(), versionCmd())
	return root
}

func upCmd(paths *app.Paths) *cobra.Command {
	var o app.RunOptions
	cmd := &cobra.Command{
		Use:   "up",
		Short: "Start the whole site: backend, frontend and gateway on one port",
		Long: `Start the backend and supervise the Next.js frontend, serving both on one port.

Release bundles ship the frontend in web/ and Node.js in runtime/node next to the binary;
the start scripts (start.bat, start.command, start.sh) just run "skinloom up --open".
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
			snap, err := core.Settings.Set(cmd.Context(), nil, map[string]json.RawMessage{args[0]: raw})
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
		Short: "Print the temporary admin API token (until accounts land in M1)",
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
