package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Entropy-Increase-Team/skinloom/internal/app"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/theme"
	"github.com/Entropy-Increase-Team/skinloom/themes"
)

func themeCmd(paths *app.Paths) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "theme",
		Short: "Create, validate and reload local themes",
	}

	var extends string
	newCmd := &cobra.Command{
		Use:   "new <id>",
		Short: "Scaffold a new theme in <data>/themes/<id>",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := paths.Resolve()
			if err != nil {
				return err
			}
			themesDir := filepath.Join(p.DataDir, "themes")
			if extends != "" {
				reg := theme.NewRegistry()
				if err := reg.LoadDir(themes.Builtin, "builtin"); err != nil {
					return err
				}
				reg.SetLocalDir(themesDir, nil)
				parent, ok := reg.Get(extends)
				if !ok {
					return fmt.Errorf("unknown parent theme %q", extends)
				}
				if parent.Extends != "" {
					return fmt.Errorf("cannot extend %q which itself extends %q (only single-level inheritance is allowed)", extends, parent.Extends)
				}
			}
			dir, err := theme.Scaffold(themesDir, args[0], extends)
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), dir)
			return nil
		},
	}
	newCmd.Flags().StringVar(&extends, "extends", theme.DefaultID, "parent theme to extend (empty for standalone)")

	checkCmd := &cobra.Command{
		Use:   "check <dir>",
		Short: "Validate a theme package and check contrast ratios",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			reg := theme.NewRegistry()
			if err := reg.LoadDir(themes.Builtin, "builtin"); err != nil {
				return err
			}
			if p, err := paths.Resolve(); err == nil {
				reg.SetLocalDir(filepath.Join(p.DataDir, "themes"), nil)
			}
			t, warnings, err := theme.Check(os.DirFS(args[0]), reg)
			if err != nil {
				return err
			}
			for _, w := range warnings {
				fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", w)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "ok: %s %s\n", t.ID, t.Version)
			return nil
		},
	}

	reloadCmd := &cobra.Command{
		Use:   "reload",
		Short: "Rescan <data>/themes and notify the running server",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c, err := app.OpenCore(cmd.Context(), *paths)
			if err != nil {
				return err
			}
			defer c.Close(cmd.Context())
			c.Themes.Reload()
			v, err := c.Settings.Touch(cmd.Context())
			if err != nil {
				return err
			}
			for _, t := range c.Themes.List() {
				if t.Error != "" {
					fmt.Fprintf(cmd.ErrOrStderr(), "error\t%s\t(%s)\t%s\n", t.ID, t.Source, t.Error)
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "ok\t%s\t%s\t(%s)\n", t.ID, t.Version, t.Source)
				}
			}
			fmt.Fprintf(cmd.OutOrStdout(), "config_version=%d\n", v)
			return nil
		},
	}

	cmd.AddCommand(newCmd, checkCmd, reloadCmd)
	return cmd
}
