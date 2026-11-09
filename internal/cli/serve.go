package cli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/Entropy-Increase-Team/skinloom/internal/app"
)

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
