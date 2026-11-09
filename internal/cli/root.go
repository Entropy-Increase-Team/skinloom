// Package cli implements the skinloom command line.
package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/Entropy-Increase-Team/skinloom/internal/app"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/logging"
)

// Execute runs the CLI and returns the process exit code.
func Execute() int {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := newRoot().ExecuteContext(ctx); err != nil {
		label := "error:"
		if _, color := logging.ConsoleStyle(os.Stderr); color {
			label = "\x1b[1;31merror:\x1b[0m" // red, as errors in the log
		}
		fmt.Fprintln(os.Stderr, label, err)
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
		adminCmd(&paths), setupLinkCmd(&paths), backupCmd(&paths), restoreCmd(&paths), importCmd(&paths), serviceCmd(&paths), healthcheckCmd(), openapiCmd(), versionCmd())
	return root
}

func baseName(p string) string {
	i := strings.LastIndexAny(p, `/\`)
	return p[i+1:]
}
