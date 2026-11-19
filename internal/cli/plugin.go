package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/Entropy-Increase-Team/skinloom/internal/app"
	"github.com/Entropy-Increase-Team/skinloom/internal/plugin"
)

func pluginCmd(paths *app.Paths) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plugin",
		Short: "Install, list, enable, disable, reload, and remove plugins and themes",
	}

	var listJSON bool
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List installed plugins and themes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			core, err := app.OpenCoreWithOptions(cmd.Context(), *paths, app.CoreOptions{SkipPluginStart: true})
			if err != nil {
				return err
			}
			defer core.Close(context.Background())

			items, err := core.Plugins.List(cmd.Context())
			if err != nil {
				return err
			}
			if listJSON {
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(items)
			}
			tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 4, 2, ' ', 0)
			fmt.Fprintln(tw, "ID\tKIND\tVERSION\tSTATUS\tSOURCE")
			for _, it := range items {
				fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", it.ID, it.Kind, it.Version, it.Status, it.Source)
			}
			return tw.Flush()
		},
	}
	listCmd.Flags().BoolVar(&listJSON, "json", false, "Output JSON")

	var (
		allowUnsigned bool
		assumeYes     bool
		disabled      bool
	)
	installCmd := &cobra.Command{
		Use:   "install <.slp>",
		Short: "Verify and install or upgrade a .slp plugin/theme package",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pkgBytes, err := os.ReadFile(args[0])
			if err != nil {
				return fmt.Errorf("read package %s: %w", args[0], err)
			}
			core, err := app.OpenCoreWithOptions(cmd.Context(), *paths, app.CoreOptions{SkipPluginStart: disabled})
			if err != nil {
				return err
			}
			defer core.Close(context.Background())

			inspect, err := core.Plugins.InspectPackage(cmd.Context(), pkgBytes, "en")
			if err != nil {
				return err
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Package: %s (%s) v%s", inspect.ID, inspect.Kind, inspect.Version)
			if inspect.IsUpgrade {
				fmt.Fprintf(out, " [upgrade from %s]", inspect.CurrentVersion)
			}
			fmt.Fprintln(out)
			if inspect.Verification.Trusted {
				fmt.Fprintf(out, "Signature: TRUSTED (%s, %s)\n", inspect.Verification.Signer, inspect.Verification.KeyID)
			} else if inspect.Verification.Signed {
				fmt.Fprintln(out, "Signature: UNTRUSTED (unknown public key)")
			} else {
				fmt.Fprintln(out, "Signature: UNSIGNED")
			}
			for _, w := range inspect.Warnings {
				fmt.Fprintf(out, "Warning: %s\n", w)
			}
			printPermGroup := func(label string, items []plugin.PermissionItem) {
				if len(items) == 0 {
					return
				}
				names := make([]string, len(items))
				for i, it := range items {
					names[i] = it.Name
				}
				fmt.Fprintf(out, "Permissions (%s): %s\n", label, strings.Join(names, ", "))
			}
			printPermGroup("normal", inspect.Permissions.Normal)
			printPermGroup("sensitive", inspect.Permissions.Sensitive)
			printPermGroup("high_risk", inspect.Permissions.HighRisk)

			if inspect.NativeInstallBlocked {
				return fmt.Errorf("%w: %s", plugin.ErrUnsignedNativeForbidden, inspect.NativeBlockReason)
			}
			if !inspect.PlatformCompatible {
				return fmt.Errorf("%w: native plugin %q does not contain an executable for host platform %s", plugin.ErrBadPackage, inspect.ID, inspect.HostPlatform)
			}
			if !inspect.Verification.Trusted && !allowUnsigned {
				return fmt.Errorf("%w: re-run with --allow-unsigned to explicitly trust this unsigned/unverified local package", plugin.ErrUntrustedPackage)
			}

			if !assumeYes && (!inspect.Verification.Trusted || len(inspect.Permissions.Sensitive) > 0 || len(inspect.Permissions.HighRisk) > 0) {
				reader := bufio.NewReader(cmd.InOrStdin())
				if inspect.RequiresHighRiskConfirm || !inspect.Verification.Trusted {
					fmt.Fprintf(out, "Type %q to confirm high-risk/untrusted installation: ", inspect.ID)
					line, _ := reader.ReadString('\n')
					if strings.TrimSpace(line) != inspect.ID {
						return errors.New("aborted: confirmation ID did not match")
					}
				} else {
					fmt.Fprint(out, "Proceed with installation and grant declared permissions? [y/N]: ")
					line, _ := reader.ReadString('\n')
					ans := strings.ToLower(strings.TrimSpace(line))
					if ans != "y" && ans != "yes" {
						return errors.New("aborted")
					}
				}
			}

			info, err := core.Plugins.InstallPackage(cmd.Context(), pkgBytes, plugin.InstallOptions{
				GrantAllDeclared: true,
				AllowUnsigned:    allowUnsigned,
				Enable:           !disabled,
				Source:           "package",
			})
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "installed %s v%s (%s)\n", info.ID, info.Version, info.Status)
			return nil
		},
	}
	installCmd.Flags().BoolVar(&allowUnsigned, "allow-unsigned", false, "Allow installing unsigned or untrusted .slp packages")
	installCmd.Flags().BoolVarP(&assumeYes, "yes", "y", false, "Confirm sensitive/high-risk permission grants non-interactively")
	installCmd.Flags().BoolVar(&disabled, "disabled", false, "Install without immediately enabling")

	enableCmd := &cobra.Command{
		Use:   "enable <id>",
		Short: "Enable an installed plugin or theme",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			core, err := app.OpenCore(cmd.Context(), *paths)
			if err != nil {
				return err
			}
			defer core.Close(context.Background())

			if _, ok := core.Plugins.Get(cmd.Context(), args[0]); !ok {
				return fmt.Errorf("unknown plugin %q", args[0])
			}
			if err := core.Plugins.Enable(cmd.Context(), args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "enabled %s\n", args[0])
			return nil
		},
	}

	disableCmd := &cobra.Command{
		Use:   "disable <id>",
		Short: "Disable an active plugin or theme",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			core, err := app.OpenCoreWithOptions(cmd.Context(), *paths, app.CoreOptions{SkipPluginStart: true})
			if err != nil {
				return err
			}
			defer core.Close(context.Background())

			if _, ok := core.Plugins.Get(cmd.Context(), args[0]); !ok {
				return fmt.Errorf("unknown plugin %q", args[0])
			}
			if err := core.Plugins.Disable(cmd.Context(), args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "disabled %s\n", args[0])
			return nil
		},
	}

	var purge bool
	var removeYes bool
	removeCmd := &cobra.Command{
		Use:   "remove <id>",
		Short: "Uninstall a plugin or theme",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			core, err := app.OpenCoreWithOptions(cmd.Context(), *paths, app.CoreOptions{SkipPluginStart: true})
			if err != nil {
				return err
			}
			defer core.Close(context.Background())

			if _, ok := core.Plugins.Get(cmd.Context(), args[0]); !ok {
				return fmt.Errorf("unknown plugin %q", args[0])
			}
			if purge && !removeYes {
				fmt.Fprintf(cmd.OutOrStdout(), "Purge all KV, document, and file data for %q? [y/N]: ", args[0])
				line, _ := bufio.NewReader(cmd.InOrStdin()).ReadString('\n')
				ans := strings.ToLower(strings.TrimSpace(line))
				if ans != "y" && ans != "yes" {
					return errors.New("aborted")
				}
			}
			if err := core.Plugins.Uninstall(cmd.Context(), args[0], purge); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "removed %s (purge=%v)\n", args[0], purge)
			return nil
		},
	}
	removeCmd.Flags().BoolVar(&purge, "purge", false, "Also delete plugin KV, documents, and private files")
	removeCmd.Flags().BoolVarP(&removeYes, "yes", "y", false, "Skip purge confirmation prompt")

	reloadCmd := &cobra.Command{
		Use:   "reload [id]",
		Short: "Reload one plugin or trigger cluster-wide plugin synchronization",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			core, err := app.OpenCore(cmd.Context(), *paths)
			if err != nil {
				return err
			}
			defer core.Close(context.Background())

			if len(args) == 1 {
				info, err := core.Plugins.ReloadPlugin(cmd.Context(), args[0])
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "reloaded %s v%s (%s)\n", info.ID, info.Version, info.Status)
				return nil
			}
			if err := core.Plugins.SyncCluster(cmd.Context()); err != nil {
				return err
			}
			v, err := core.Settings.Touch(cmd.Context())
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "config_version=%d\n", v)
			return nil
		},
	}

	cmd.AddCommand(listCmd, installCmd, enableCmd, disableCmd, removeCmd, reloadCmd)
	return cmd
}
