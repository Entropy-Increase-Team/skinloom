package cli

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/Entropy-Increase-Team/skinloom/internal/app"
	"github.com/Entropy-Increase-Team/skinloom/internal/importer/bs"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/fsperm"
)

func importCmd(paths *app.Paths) *cobra.Command {
	cmd := &cobra.Command{Use: "import", Short: "Import another site's data into a new SkinLoom site"}
	cmd.AddCommand(importBSCmd(paths), importCompareCmd())
	return cmd
}

func importBSCmd(paths *app.Paths) *cobra.Command {
	var (
		envPath, reportPath, tz string
		over                    bs.Source
		dryRun                  bool
	)
	cmd := &cobra.Command{
		Use:   "bs",
		Short: "Import a Blessing Skin site (accounts, players, textures, settings, applications)",
		Long: `Reads a Blessing Skin site and writes it into this SkinLoom install, which must be new:
no accounts or textures yet. The import brings BS's administrators along, so skip the
setup page. The BS database is only read; run with --dry-run first, fix what the report
lists as BLOCKING in BS, then run without it. Stop the SkinLoom site while importing.

Passwords, player names and UUIDs, texture IDs and files stay the same, so skin links,
launchers and game servers keep working. Sessions and launcher tokens are not imported:
everyone signs in once more. Site settings, mail settings (from .env), renamed terms
such as the score's name, OAuth applications and the Yggdrasil signing key come along;
the report lists every option and text SkinLoom has no place for.

The database, table prefix, password method and texture directory are read from BS's
.env (--env); the flags override single values. Keep secrets off the command line:
SKINLOOM_BS_DSN and SKINLOOM_BS_SALT are read like --dsn and --salt.`,
		Example: `  skinloom import bs --env /var/www/blessing-skin/.env --dry-run
  skinloom import bs --env /var/www/blessing-skin/.env
  SKINLOOM_BS_DSN='bs:secret@tcp(127.0.0.1:3306)/blessingskin' skinloom import bs --driver mysql --textures ./textures`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			f := cmd.Flags()
			// Secrets may come from the environment, so they stay out of ps and the
			// shell history; a flag still wins.
			dsnSet, saltSet := f.Changed("dsn"), f.Changed("salt")
			if v, ok := os.LookupEnv("SKINLOOM_BS_DSN"); ok && !dsnSet {
				over.DSN, dsnSet = v, true
			}
			if v, ok := os.LookupEnv("SKINLOOM_BS_SALT"); ok && !saltSet {
				over.Salt, saltSet = v, true
			}
			var src bs.Source
			if envPath != "" {
				s, err := bs.FromEnv(envPath)
				if err != nil {
					return fmt.Errorf("read %s: %w", envPath, err)
				}
				src = s
			} else if over.DSN == "" || over.Driver == "" {
				return errors.New("give the BS site's .env with --env (or --driver and --dsn)")
			}
			if f.Changed("driver") {
				src.Driver = over.Driver
			}
			if dsnSet {
				src.DSN = over.DSN
			}
			if f.Changed("prefix") {
				src.Prefix = over.Prefix
			}
			if f.Changed("textures") {
				src.TexturesDir = over.TexturesDir
			}
			if f.Changed("pwd-method") {
				src.PasswordMethod = over.PasswordMethod
			}
			if saltSet {
				src.Salt = over.Salt
			}
			if f.Changed("pwd-method") || saltSet {
				src.Transition = nil // the flags name one method, not .env's list
			}
			if src.TexturesDir == "" {
				return errors.New("no texture directory: give it with --textures (BS keeps the files in storage/textures; when FS_DRIVER puts them in object storage, download them first)")
			}
			loc, err := time.LoadLocation(tz)
			if err != nil {
				return fmt.Errorf("--timezone: %w", err)
			}
			src.Location = loc

			r, err := app.ImportBS(cmd.Context(), *paths, src, bs.Options{DryRun: dryRun,
				Progress: func(format string, args ...any) { fmt.Fprintf(os.Stderr, format+"\n", args...) }})
			if r == nil {
				return err // stopped before a plan was made: nothing to report
			}
			// A failed import still writes its report: the entries say what to fix.
			if werr := writeReport(paths, r, dryRun, &reportPath); werr != nil {
				return errors.Join(err, werr)
			}
			if err != nil {
				return err
			}
			if r.Blocked() {
				return fmt.Errorf("import refused: %d BLOCKING entries", r.Count(bs.Blocking))
			}
			if !dryRun && r.Superadmins > 0 {
				fmt.Println("start the site and sign in with a BS super administrator account")
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&envPath, "env", "", "the Blessing Skin site's .env file")
	f.BoolVar(&dryRun, "dry-run", false, "do everything but write: the report shows what an import would do")
	f.StringVar(&reportPath, "report", "", "where to write the report (default <data>/import/bs-<time>.txt)")
	f.StringVar(&tz, "timezone", "Asia/Shanghai", "the time zone BS stored times in (its config/app.php)")
	f.StringVar(&over.Driver, "driver", "", "source database: mysql, postgres or sqlite (overrides DB_CONNECTION)")
	f.StringVar(&over.DSN, "dsn", "", "source database DSN (go-sql-driver/mysql format, a postgres:// URL or the SQLite file; or SKINLOOM_BS_DSN)")
	f.StringVar(&over.Prefix, "prefix", "", "table prefix (overrides DB_PREFIX)")
	f.StringVar(&over.TexturesDir, "textures", "", "directory with the texture files (default <BS>/storage/textures)")
	f.StringVar(&over.PasswordMethod, "pwd-method", "", "password method (overrides PWD_METHOD)")
	f.StringVar(&over.Salt, "salt", "", "password salt (overrides SALT; or SKINLOOM_BS_SALT)")
	return cmd
}

// writeReport writes r to *path (default <data>/import/bs-<time>[-dry-run].txt), prints
// its head and where it is.
func writeReport(paths *app.Paths, r *bs.Report, dryRun bool, path *string) error {
	var text bytes.Buffer
	if err := r.WriteText(&text); err != nil {
		return err
	}
	if *path == "" {
		p, err := paths.Resolve()
		if err != nil {
			return err
		}
		name := "bs-" + time.Now().Format("20060102-150405")
		if dryRun {
			name += "-dry-run"
		}
		*path = filepath.Join(p.DataDir, "import", name+".txt")
	}
	if err := os.MkdirAll(filepath.Dir(*path), 0o750); err != nil {
		return err
	}
	// The report names accounts and email addresses: readable by the owner only, also
	// when --report names a file that already exists (WriteFile keeps its mode), and
	// restricted while still empty, so its text is never readable under the folder's
	// permissions (Windows ignores the mode: fsperm).
	if err := os.WriteFile(*path, nil, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(*path, 0o600); err != nil {
		return err
	}
	if err := fsperm.Private(*path); err != nil {
		return err
	}
	if err := os.WriteFile(*path, text.Bytes(), 0o600); err != nil {
		return err
	}
	// The head holds the summary and the data left in BS (listed right after it), which
	// is shown whole however long: it is what to check before switching.
	printHead(text.Bytes(), 40+len(r.LeftBehind()))
	fmt.Printf("\nfull report: %s\n", *path)
	return nil
}

// printHead prints the first n lines of text and how many more there are.
func printHead(text []byte, n int) {
	sc := bufio.NewScanner(bytes.NewReader(text))
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	i := 0
	for ; sc.Scan(); i++ {
		if i < n {
			fmt.Println(sc.Text())
		}
	}
	if i > n {
		fmt.Printf("... %d more lines\n", i-n)
	}
}
