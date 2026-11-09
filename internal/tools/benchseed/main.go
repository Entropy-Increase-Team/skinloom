// Command benchseed fills an empty SkinLoom database with a large site for the load
// tests in bench/k6 (design 04 §5.4, ADR 0038): users, a skin library of a million
// textures, players wearing real skin files, and the setup marker. It writes through
// ent in batches and refuses a database that already has users.
//
//	go run ./internal/tools/benchseed -data ./bench-data -textures 1000000
//
// Every account's password is bench-pass-1; account 1 (admin@bench.test) is the super
// administrator. Players are named P000000, P000001…; the first files players wear are
// in <data>/textures.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"image"
	"image/color"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/texture"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/account"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/player"
	"github.com/Entropy-Increase-Team/skinloom/internal/modules/render"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/migrate"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/storage"
)

// Password is every seeded account's password.
const Password = "bench-pass-1"

func main() {
	var (
		dataDir, driver, dsn     string
		users, textures, players int
		files                    int
	)
	flag.StringVar(&dataDir, "data", "bench-data", "SkinLoom data directory (SQLite database and textures)")
	flag.StringVar(&driver, "driver", "sqlite", "database driver: sqlite, mysql or postgres")
	flag.StringVar(&dsn, "dsn", "", "database DSN (SQLite: skinloom.db under -data)")
	flag.IntVar(&users, "users", 10000, "accounts")
	flag.IntVar(&textures, "textures", 1000000, "textures in the skin library")
	flag.IntVar(&players, "players", 100000, "players")
	flag.IntVar(&files, "files", 64, "distinct skin files that players wear")
	flag.Parse()
	if driver == "sqlite" && dsn == "" {
		dsn = "skinloom.db"
	}
	if users < 1 || files < 1 || textures < files || players < 1 {
		log.Fatal("need at least 1 user, 1 player and as many textures as files")
	}
	ctx := context.Background()
	start := time.Now()
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		log.Fatal(err)
	}
	d, err := db.Open(ctx, db.Options{Driver: driver, DSN: dsn, DataDir: dataDir})
	if err != nil {
		log.Fatal(err)
	}
	defer d.Close()
	if err := migrate.Up(ctx, d, dataDir, slog.New(slog.DiscardHandler)); err != nil {
		log.Fatal(err)
	}
	if n, err := d.Ent.User.Query().Count(ctx); err != nil || n > 0 {
		log.Fatalf("the database must be empty (users: %d, %v)", n, err)
	}
	store, err := storage.NewLocal(filepath.Join(dataDir, "textures"))
	if err != nil {
		log.Fatal(err)
	}
	hash, err := account.NewHasher(1).Hash(ctx, Password)
	if err != nil {
		log.Fatal(err)
	}
	c := d.Ent
	now := time.Now()

	// Accounts.
	batch(users, 2000, func(lo, hi int) error {
		b := make([]*ent.UserCreate, 0, hi-lo)
		for i := lo; i < hi; i++ {
			email := fmt.Sprintf("u%d@bench.test", i+1)
			role := user.RoleUser
			if i == 0 {
				email, role = "admin@bench.test", user.RoleSuperadmin
			}
			b = append(b, c.User.Create().SetID(int64(i+1)).SetEmail(email).SetEmailNormalized(email).SetNickname(fmt.Sprintf("user%d", i+1)).
				SetPasswordHash(hash).SetRole(role).SetScore(100000).SetEmailVerifiedAt(now).SetCreatedAt(now).SetUpdatedAt(now))
		}
		return c.User.CreateBulk(b...).Exec(ctx)
	})
	log.Printf("%d users", users)

	// Real skin files, the first textures; the rest of the library has blobs without
	// files (the list never reads them).
	realHash := make([]string, files)
	for i := range files {
		png, err := render.Encode(skinImage(i), "png")
		if err != nil {
			log.Fatal(err)
		}
		sum := sha256.Sum256(png)
		realHash[i] = hex.EncodeToString(sum[:])
		if err := store.Put(ctx, realHash[i], png); err != nil {
			log.Fatal(err)
		}
	}
	hashOf := func(i int) string {
		if i < files {
			return realHash[i]
		}
		sum := sha256.Sum256(fmt.Appendf(nil, "bench texture %d", i))
		return hex.EncodeToString(sum[:])
	}
	batch(textures, 3000, func(lo, hi int) error {
		blobs := make([]*ent.BlobCreate, 0, hi-lo)
		texs := make([]*ent.TextureCreate, 0, hi-lo)
		for i := lo; i < hi; i++ {
			h := hashOf(i)
			blobs = append(blobs, c.Blob.Create().SetID(h).SetSize(2048).SetWidth(64).SetHeight(64).SetRefCount(1).SetCreatedAt(now))
			typ := []texture.Type{texture.TypeSteve, texture.TypeAlex, texture.TypeSteve, texture.TypeCape}[i%4]
			if i < files {
				typ = []texture.Type{texture.TypeSteve, texture.TypeAlex}[i%2]
			}
			vis := texture.VisibilityPublic
			if i%10 == 9 {
				vis = texture.VisibilityPrivate
			}
			at := now.Add(-time.Duration(textures-i) * time.Minute)
			texs = append(texs, c.Texture.Create().SetID(int64(i+1)).SetName(fmt.Sprintf("Texture %d", i+1)).SetType(typ).SetHash(h).
				SetSize(2048).SetUploaderID(int64(i%users+1)).SetVisibility(vis).SetLikes((i*7919)%1000).SetCreatedAt(at).SetUpdatedAt(at))
		}
		if err := c.Blob.CreateBulk(blobs...).Exec(ctx); err != nil {
			return err
		}
		return c.Texture.CreateBulk(texs...).Exec(ctx)
	})
	log.Printf("%d textures (%d with files)", textures, files)

	batch(players, 3000, func(lo, hi int) error {
		b := make([]*ent.PlayerCreate, 0, hi-lo)
		for i := lo; i < hi; i++ {
			name := fmt.Sprintf("P%06d", i)
			p := c.Player.Create().SetID(int64(i + 1)).SetUserID(int64(i%users + 1)).SetName(name).SetNameLower(strings.ToLower(name)).
				SetUUID(player.OfflineUUID(name)).SetSkinID(int64(i%files + 1)).SetCreatedAt(now).SetUpdatedAt(now)
			b = append(b, p)
		}
		return c.Player.CreateBulk(b...).Exec(ctx)
	})
	log.Printf("%d players", players)

	if err := c.SystemState.Create().SetID("setup.completed").SetIntValue(1).Exec(ctx); err != nil {
		log.Fatal(err)
	}
	if d.Dialect == db.Postgres {
		for _, t := range []string{"users", "textures", "players"} {
			if _, err := d.SQL.ExecContext(ctx, fmt.Sprintf("SELECT setval(pg_get_serial_sequence('%s', 'id'), (SELECT MAX(id) FROM %s))", t, t)); err != nil {
				log.Fatal(err)
			}
		}
	}
	// Statistics now, so the planner has them and autovacuum does not run during the
	// load test (on PostgreSQL it did, and every scenario slowed down).
	analyze := map[db.Dialect]string{db.Postgres: "VACUUM ANALYZE", db.SQLite: "ANALYZE", db.MySQL: "ANALYZE TABLE users, blobs, textures, players"}[d.Dialect]
	if _, err := d.SQL.ExecContext(ctx, analyze); err != nil {
		log.Fatal(err)
	}
	log.Printf("done in %s", time.Since(start).Round(time.Second))
}

// batch calls fn for [lo, hi) chunks of n.
func batch(n, size int, fn func(lo, hi int) error) {
	for lo := 0; lo < n; lo += size {
		if err := fn(lo, min(n, lo+size)); err != nil {
			log.Fatal(err)
		}
	}
}

// skinImage is a distinct 64×64 skin.
func skinImage(i int) image.Image {
	img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := range 64 {
		for x := range 64 {
			img.SetNRGBA(x, y, color.NRGBA{uint8(x*4 + i), uint8(y*4 + i*3), uint8(i * 37), 255})
		}
	}
	return img
}
