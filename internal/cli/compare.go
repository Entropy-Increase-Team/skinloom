package cli

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/Entropy-Increase-Team/skinloom/internal/contract"
)

func importCompareCmd() *cobra.Command {
	var oldURL, newURL string
	cmd := &cobra.Command{
		Use:   "compare --bs <url> --skinloom <url> <player>...",
		Short: "Compare what launchers and game servers get from Blessing Skin and from this SkinLoom",
		Long: `Asks the Blessing Skin site and the SkinLoom imported from it the same questions about
the named players, the way launchers and game servers ask them: CustomSkinLoader,
UniSkinMod and legacy skin URLs, texture files, and the Yggdrasil profile lookups (with
their signatures checked against each site's own key). Run it after an import, before
switching the domain over: every line should say "same".

Only what clients read is compared: status, content type and body, with each site's
address, times and signatures masked. Error messages and cache times differ on purpose.
Players changed in BS after the import differ too, of course.`,
		Example: `  skinloom import compare --bs https://skins.example.com --skinloom http://127.0.0.1:17546 Steve Alex Notch`,
		Args:    cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, names []string) error {
			if oldURL == "" || newURL == "" {
				return errors.New("give both --bs and --skinloom")
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Minute)
			defer cancel()
			client := &http.Client{Timeout: 30 * time.Second}
			res, err := contract.Compare(ctx, client, &contract.Site{Base: oldURL}, &contract.Site{Base: newURL}, names)
			out := cmd.OutOrStdout()
			differ := 0
			for _, r := range res {
				if r.Same {
					fmt.Fprintf(out, "same     %s\n", r.Request)
					continue
				}
				differ++
				fmt.Fprintf(out, "DIFFERS  %s\n  Blessing Skin:\n%s  SkinLoom:\n%s", r.Request, indent(r.Old), indent(r.New))
			}
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "\n%d requests, %d differ\n", len(res), differ)
			if differ > 0 {
				return fmt.Errorf("%d answers differ", differ)
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&oldURL, "bs", "", "address of the Blessing Skin site")
	f.StringVar(&newURL, "skinloom", "", "address of the SkinLoom site")
	return cmd
}

func indent(s string) string {
	var b strings.Builder
	for _, l := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		b.WriteString("    " + l + "\n")
	}
	return b.String()
}
