// Package bs imports a Blessing Skin site (design 02 §4, §5, 04 §10): accounts with
// their passwords, players with their UUIDs, textures with their files, closets,
// reports, unread notifications, OAuth applications, settings, reworded terms and the
// Yggdrasil signing key. The BS database is only read. Everything is written in one
// transaction into an empty SkinLoom site, so a dry run is the real import rolled back
// and a failed import can simply be run again.
package bs

import (
	"cmp"
	"context"
	"errors"
	"os"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/settings"
	"github.com/Entropy-Increase-Team/skinloom/internal/storage"
)

// Target is the SkinLoom site imported into.
type Target struct {
	DB    *db.DB
	Store storage.Store
	// StoreDir is the local texture directory ("" with S3). When it is the BS texture
	// directory itself (mounted or copied into place), files are not copied again.
	StoreDir string
	Locales  []string
	// Settings is the site's settings registry: imported settings are checked against
	// it as the admin panel would.
	Settings *settings.Registry
}

// Options control one run.
type Options struct {
	DryRun bool
	// Progress, if set, receives one line per step.
	Progress func(format string, args ...any)
}

// Run imports src into t and returns the report, also when the import was refused for
// BLOCKING entries (the error is then nil and Report.Blocked true) and when it failed
// after the plan was made (the report then has Failed set, and nothing was written).
func Run(ctx context.Context, src Source, t Target, o Options) (*Report, error) {
	say := o.Progress
	if say == nil {
		say = func(string, ...any) {}
	}
	if src.Location == nil {
		src.Location = shanghai
	}
	if t.Settings == nil {
		return nil, errors.New("bs: Target.Settings is required")
	}
	if err := checkEmpty(ctx, t.DB); err != nil { // before reading or copying anything
		return nil, err
	}
	pool, err := src.open(ctx)
	if err != nil {
		return nil, err
	}
	defer pool.Close()

	say("reading the Blessing Skin database")
	data, err := src.read(ctx, pool)
	if err != nil {
		return nil, err
	}
	say("  %d users, %d players, %d textures, %d closet items", len(data.Users), len(data.Players), len(data.Textures), len(data.Closet))

	var hashes []string
	seen := map[string]bool{}
	for _, tx := range data.Textures {
		if storage.ValidKey(tx.Hash) && !seen[tx.Hash] {
			seen[tx.Hash] = true
			hashes = append(hashes, tx.Hash)
		}
	}
	say("checking %d texture files in %s", len(hashes), cmp.Or(src.TexturesDir, "(no directory)"))
	files := scanFiles(ctx, src.TexturesDir, hashes)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	now := time.Now()
	r := &Report{DryRun: o.DryRun, Files: len(hashes), Imported: map[string]int{}}
	p := plan(data, files, planOptions{Location: src.Location, PasswordMethod: src.PasswordMethod, Salt: src.Salt,
		Transition: src.Transition, Locales: t.Locales, Now: now, Settings: t.Settings, Mail: src.Mail}, r)
	if r.Blocked() {
		return r, nil
	}
	for name, n := range map[string]int{"users": len(p.Users), "blobs": len(p.Blobs), "textures": len(p.Textures), "players": len(p.Players),
		"closet_items": len(p.Closet), "reports": len(p.Reports), "notifications": len(p.Notifications),
		"oauth_clients": len(p.Clients), "settings": len(p.Settings)} {
		r.Imported[name] = n
	}
	r.Superadmins = p.Superadmins

	fail := func(err error) (*Report, error) {
		r.Failed = err
		return r, err
	}
	if !o.DryRun {
		say("copying texture files")
		n, err := copyFiles(ctx, src.TexturesDir, t.Store, p.Blobs, sameDir(src.TexturesDir, t.StoreDir), func(done, total int) { say("  %d / %d", done, total) })
		r.FilesCopied = n
		if err != nil {
			return fail(err)
		}
	}
	say("writing to the SkinLoom database")
	if err := write(ctx, t.DB, p, o.DryRun, now); err != nil {
		return fail(err)
	}
	return r, nil
}

// shanghai is BS's time zone unless the site changed config/app.php.
var shanghai = func() *time.Location {
	l, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("CST", 8*3600)
	}
	return l
}()

func sameDir(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	fa, err1 := os.Stat(a)
	fb, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(fa, fb)
}
