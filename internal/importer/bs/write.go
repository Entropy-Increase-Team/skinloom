package bs

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/oauthclient"
	"github.com/Entropy-Increase-Team/skinloom/ent/report"
	"github.com/Entropy-Increase-Team/skinloom/ent/setting"
	"github.com/Entropy-Increase-Team/skinloom/ent/sitesecret"
	"github.com/Entropy-Increase-Team/skinloom/ent/systemstate"
	"github.com/Entropy-Increase-Team/skinloom/ent/texture"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/player"
	"github.com/Entropy-Increase-Team/skinloom/internal/settings"
)

// ErrNotEmpty refuses an import into a site that already has accounts or textures.
var ErrNotEmpty = errors.New("the SkinLoom database already has users or textures: import into a new, empty site (the import brings BS's administrators along, so skip the setup page)")

// setupKey is account's "setup completed" marker: the imported super administrators
// replace the setup page.
const setupKey = "setup.completed"

// batch rows per INSERT: well under every database's limit on bound parameters.
const batch = 500

// importLock is a system_state row the import inserts first and deletes before it
// commits. A second import started at the same time waits on the uncommitted row (the
// unique key), then finds the site no longer empty; nothing of it stays behind.
const importLock = "import.bs.running"

// write inserts the plan in one transaction, so a failed import leaves nothing behind
// and can simply be run again. A dry run rolls the same transaction back.
//
// The transaction is a plain sql.Tx with an ent client on top: PostgreSQL's ID
// sequences are moved on inside it too (ent's Tx cannot run raw SQL), so a commit can no
// longer be followed by a failure.
func write(ctx context.Context, d *db.DB, p *planned, dryRun bool, now time.Time) error {
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() // no-op after Commit
	c := ent.NewClient(ent.Driver(entsql.NewDriver(entDialect(d.Dialect), entsql.Conn{ExecQuerier: tx})))

	// Locks first (ADR 0026): the import's only lock, before any read.
	if err := c.SystemState.Create().SetID(importLock).SetIntValue(1).Exec(ctx); err != nil {
		if db.IsConstraint(err) {
			return ErrNotEmpty // a row left by a crash cannot exist: it is never committed
		}
		return fmt.Errorf("lock: %w", err)
	}
	if err := checkEmpty(ctx, &db.DB{Ent: c}); err != nil {
		return err
	}

	err = insert(ctx, "users", p.Users, func(u tUser) *ent.UserCreate {
		return c.User.Create().SetID(u.ID).SetEmail(u.Email).SetEmailNormalized(u.EmailNormalized).SetNickname(u.Nickname).
			SetPasswordHash(u.PasswordHash).SetPasswordAlgo(u.PasswordAlgo).SetRole(user.Role(u.Role)).SetStatus(user.Status(u.Status)).
			SetScore(u.Score).SetLocale(u.Locale).SetRegisterIP(u.RegisterIP).SetNillableAvatarTextureID(u.Avatar).
			SetNillableEmailVerifiedAt(u.VerifiedAt).SetNillableLastSignAt(u.LastSignAt).SetCreatedAt(u.CreatedAt).SetUpdatedAt(now)
	}, func(b []*ent.UserCreate) error { return c.User.CreateBulk(b...).Exec(ctx) })
	if err != nil {
		return err
	}
	err = insert(ctx, "blobs", p.Blobs, func(b tBlob) *ent.BlobCreate {
		return c.Blob.Create().SetID(b.Hash).SetSize(b.Size).SetWidth(b.Width).SetHeight(b.Height).SetRefCount(b.Refs).SetCreatedAt(now)
	}, func(b []*ent.BlobCreate) error { return c.Blob.CreateBulk(b...).Exec(ctx) })
	if err != nil {
		return err
	}
	err = insert(ctx, "textures", p.Textures, func(t tTexture) *ent.TextureCreate {
		vis := texture.VisibilityPrivate
		if t.Public {
			vis = texture.VisibilityPublic
		}
		return c.Texture.Create().SetID(t.ID).SetName(t.Name).SetType(texture.Type(t.Type)).SetHash(t.Hash).SetSize(t.Size).
			SetNillableUploaderID(t.Uploader).SetVisibility(vis).SetLikes(t.Likes).SetCreatedAt(t.CreatedAt).SetUpdatedAt(t.CreatedAt)
	}, func(b []*ent.TextureCreate) error { return c.Texture.CreateBulk(b...).Exec(ctx) })
	if err != nil {
		return err
	}
	err = insert(ctx, "players", p.Players, func(pl tPlayer) *ent.PlayerCreate {
		return c.Player.Create().SetID(pl.ID).SetUserID(pl.UserID).SetName(pl.Name).SetNameLower(player.Key(pl.Name)).SetUUID(pl.UUID).
			SetNillableSkinID(pl.Skin).SetNillableCapeID(pl.Cape).SetCreatedAt(pl.UpdatedAt).SetUpdatedAt(pl.UpdatedAt)
	}, func(b []*ent.PlayerCreate) error { return c.Player.CreateBulk(b...).Exec(ctx) })
	if err != nil {
		return err
	}
	err = insert(ctx, "closet_items", p.Closet, func(ci tCloset) *ent.ClosetItemCreate {
		return c.ClosetItem.Create().SetUserID(ci.UserID).SetTextureID(ci.TextureID).SetName(ci.Name).SetCreatedAt(now)
	}, func(b []*ent.ClosetItemCreate) error { return c.ClosetItem.CreateBulk(b...).Exec(ctx) })
	if err != nil {
		return err
	}
	err = insert(ctx, "reports", p.Reports, func(r tReport) *ent.ReportCreate {
		return c.Report.Create().SetID(r.ID).SetNillableTextureID(r.Texture).SetNillableUploaderID(r.Uploader).
			SetNillableReporterID(r.Reporter).SetReason(r.Reason).SetStatus(report.Status(r.Status)).SetCreatedAt(r.CreatedAt)
	}, func(b []*ent.ReportCreate) error { return c.Report.CreateBulk(b...).Exec(ctx) })
	if err != nil {
		return err
	}
	err = insert(ctx, "notifications", p.Notifications, func(n tNotification) *ent.NotificationCreate {
		return c.Notification.Create().SetUserID(n.UserID).SetTitle(n.Title).SetContent(n.Content).SetCreatedAt(n.CreatedAt)
	}, func(b []*ent.NotificationCreate) error { return c.Notification.CreateBulk(b...).Exec(ctx) })
	if err != nil {
		return err
	}
	err = insert(ctx, "oauth_clients", p.Clients, func(cl tClient) *ent.OAuthClientCreate {
		return c.OAuthClient.Create().SetID(cl.ID).SetUserID(cl.UserID).SetName(cl.Name).SetNillableSecretHash(cl.SecretHash).
			SetRedirectUris(cl.RedirectURIs).SetCreatedAt(cl.CreatedAt).SetUpdatedAt(now)
	}, func(b []*ent.OAuthClientCreate) error { return c.OAuthClient.CreateBulk(b...).Exec(ctx) })
	if err != nil {
		return err
	}
	if err := writeSettings(ctx, c, p.Settings, now); err != nil {
		return err
	}
	if p.SigningKey != "" {
		// A site started before the import made its own key already: BS's replaces it
		// (the value is immutable, so the row is replaced).
		_, err := c.SiteSecret.Delete().Where(sitesecret.ID(signingKeyName)).Exec(ctx)
		if err == nil {
			err = c.SiteSecret.Create().SetID(signingKeyName).SetValue(p.SigningKey).SetCreatedAt(now).Exec(ctx)
		}
		if err != nil {
			return fmt.Errorf("write the Yggdrasil signing key: %w", err)
		}
	}
	if p.Superadmins > 0 {
		// A site whose setup was completed and its accounts then deleted has the row
		// already; an upsert that does nothing reports that differently per dialect.
		done, err := c.SystemState.Query().Where(systemstate.ID(setupKey)).Exist(ctx)
		if err == nil && !done {
			err = c.SystemState.Create().SetID(setupKey).SetIntValue(1).Exec(ctx)
		}
		if err != nil {
			return fmt.Errorf("mark setup complete: %w", err)
		}
	}
	if dryRun {
		return tx.Rollback()
	}
	if err := resetSequences(ctx, tx, d.Dialect); err != nil {
		return err
	}
	if _, err := c.SystemState.Delete().Where(systemstate.ID(importLock)).Exec(ctx); err != nil {
		return fmt.Errorf("unlock: %w", err)
	}
	return tx.Commit()
}

func entDialect(d db.Dialect) string {
	switch d {
	case db.MySQL:
		return dialect.MySQL
	case db.Postgres:
		return dialect.Postgres
	}
	return dialect.SQLite
}

// writeSettings replaces the imported settings. The config version is taken before the
// setting rows, the order the settings service locks them in (ADR 0026), and is
// bumped so a running node would load the new values.
func writeSettings(ctx context.Context, c *ent.Client, values map[string]json.RawMessage, now time.Time) error {
	if len(values) == 0 {
		return nil
	}
	n, err := c.SystemState.Update().Where(systemstate.ID(settings.ConfigVersionKey)).AddIntValue(1).SetUpdatedAt(now).Save(ctx)
	if err == nil && n == 0 {
		err = c.SystemState.Create().SetID(settings.ConfigVersionKey).SetIntValue(1).SetUpdatedAt(now).Exec(ctx)
	}
	if err != nil {
		return fmt.Errorf("bump the config version: %w", err)
	}
	keys := sortedKeys(values)
	if _, err := c.Setting.Delete().Where(setting.IDIn(keys...)).Exec(ctx); err != nil {
		return fmt.Errorf("write settings: %w", err)
	}
	return insert(ctx, "settings", keys, func(k string) *ent.SettingCreate {
		return c.Setting.Create().SetID(k).SetValue(string(values[k])).SetUpdatedAt(now)
	}, func(b []*ent.SettingCreate) error { return c.Setting.CreateBulk(b...).Exec(ctx) })
}

// checkEmpty refuses a site that already has accounts or textures.
func checkEmpty(ctx context.Context, d *db.DB) error {
	if ok, err := d.Ent.User.Query().Exist(ctx); err != nil || ok {
		return cmp.Or(err, ErrNotEmpty)
	}
	if ok, err := d.Ent.Texture.Query().Exist(ctx); err != nil || ok {
		return cmp.Or(err, ErrNotEmpty)
	}
	return nil
}

// insert writes rows in batches; table names the error.
func insert[R any, C any](ctx context.Context, table string, rows []R, build func(R) C, exec func([]C) error) error {
	for start := 0; start < len(rows); start += batch {
		if err := ctx.Err(); err != nil {
			return err
		}
		end := min(start+batch, len(rows))
		creates := make([]C, 0, end-start)
		for _, r := range rows[start:end] {
			creates = append(creates, build(r))
		}
		if err := exec(creates); err != nil {
			return fmt.Errorf("write %s: %w", table, err)
		}
	}
	return nil
}

// resetSequences moves PostgreSQL's ID sequences past the imported IDs, which were
// given explicitly; the next user or texture would otherwise reuse ID 1. MySQL and
// SQLite advance their counters by themselves. setval is not undone by a rollback, but
// it only runs for an import that is about to commit.
func resetSequences(ctx context.Context, tx *sql.Tx, d db.Dialect) error {
	if d != db.Postgres {
		return nil
	}
	for _, t := range []string{user.Table, texture.Table, "players", report.Table, oauthclient.Table} {
		q := fmt.Sprintf(`SELECT setval(pg_get_serial_sequence('%[1]s', 'id'), (SELECT COALESCE(MAX(id), 0) + 1 FROM "%[1]s"), false)`, t)
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("move the ID sequence of %s on: %w", t, err)
		}
	}
	return nil
}
