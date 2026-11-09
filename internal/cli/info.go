package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Entropy-Increase-Team/skinloom/internal/httpapi"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/buildinfo"
)

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
