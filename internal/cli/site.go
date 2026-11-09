package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"

	"github.com/spf13/cobra"

	"github.com/Entropy-Increase-Team/skinloom/internal/app"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/audit"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/secrets"
)

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
