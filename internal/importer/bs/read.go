package bs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
)

// The Blessing Skin rows the import reads. IDs are BS's own; 0 means none.
type (
	srcUser struct {
		ID                        int64
		Email, Nickname, Password string
		IP, Locale                string
		Score, Avatar, Permission int64
		Verified                  bool
		LastSignAt, RegisteredAt  wall
	}
	srcPlayer struct {
		ID, UserID, Skin, Cape int64
		Name                   string
		ModifiedAt             wall
	}
	srcTexture struct {
		ID, SizeKB, Uploader int64
		Name, Type, Hash     string
		Public               bool
		UploadedAt           wall
	}
	srcCloset struct {
		UserID, TextureID int64
		Name              string
	}
	srcReport struct {
		ID, TextureID, Uploader, Reporter, Status int64
		Reason                                    string
		ReportedAt                                wall
	}
	srcNotification struct {
		UserID    int64
		Data      string
		CreatedAt wall
	}
	srcUUID struct {
		ID         int64
		Name, UUID string
	}
)

// sourceData is everything read from BS, in primary-key order.
type sourceData struct {
	Users         []srcUser
	Players       []srcPlayer
	Textures      []srcTexture
	Closet        []srcCloset
	Reports       []srcReport
	Notifications []srcNotification
	UUIDs         []srcUUID
	Options       map[string]string
	// Tables the site does not have (older BS versions, plugins not installed).
	Missing []string
	// UUIDTable: the Yggdrasil plugin's uuid table exists.
	UUIDTable bool
}

// read loads the whole site inside one read-only transaction.
func (s Source) read(ctx context.Context, pool *sql.DB) (*sourceData, error) {
	// has reports whether a table has the columns. Only "no such table or column" means
	// no: any other error (a read-only account missing a GRANT) stops the import rather
	// than silently leaving the table out.
	var probeErr error
	has := func(table, cols string) bool {
		if probeErr != nil {
			return false
		}
		rows, err := pool.QueryContext(ctx, "SELECT "+cols+" FROM "+s.table(table)+" WHERE 1=0")
		if err != nil {
			if !undefined(err) {
				probeErr = fmt.Errorf("read %s: %w", s.table(table), err)
			}
			return false
		}
		rows.Close()
		return true
	}
	players := has("players", "pid, uid, name, tid_skin, tid_cape")
	if probeErr != nil {
		return nil, probeErr
	}
	if !players {
		return nil, fmt.Errorf("no %s table with the columns of Blessing Skin 4 or later: check the prefix (--prefix), or upgrade the BS site first", s.table("players"))
	}
	var (
		withVerified = has("users", "verified")
		withLocale   = has("users", "locale")
		optional     = map[string]bool{}
		d            = &sourceData{Options: map[string]string{}}
	)
	for _, t := range []struct{ table, cols string }{
		{"user_closet", "user_uid, texture_tid, item_name"},
		{"reports", "id, tid, uploader, reporter, reason, status, report_at"},
		{"notifications", "notifiable_type, notifiable_id, data, read_at, created_at"},
		{"uuid", "id, name, uuid"}, // the Yggdrasil plugin's
	} {
		if optional[t.table] = has(t.table, t.cols); !optional[t.table] {
			d.Missing = append(d.Missing, s.Prefix+t.table)
		}
	}
	if probeErr != nil {
		return nil, probeErr
	}
	d.UUIDTable = optional["uuid"]

	tx, err := s.begin(ctx, pool)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	userCols := "uid, email, nickname, score, avatar, password, ip, permission, last_sign_at, register_at"
	if withVerified {
		userCols += ", verified"
	}
	if withLocale {
		userCols += ", locale"
	}
	err = each(ctx, tx, "SELECT "+userCols+" FROM "+s.table("users")+" ORDER BY uid", func(sc scanner) error {
		var (
			u                           srcUser
			email, nick, pw, ip, locale sql.NullString
			score, avatar, perm         sql.NullInt64
			verified                    flexBool
		)
		dest := []any{&u.ID, &email, &nick, &score, &avatar, &pw, &ip, &perm, &u.LastSignAt, &u.RegisteredAt}
		if withVerified {
			dest = append(dest, &verified)
		}
		if withLocale {
			dest = append(dest, &locale)
		}
		if err := sc.Scan(dest...); err != nil {
			return err
		}
		u.Email, u.Nickname, u.Password, u.IP, u.Locale = email.String, nick.String, pw.String, ip.String, locale.String
		u.Score, u.Avatar, u.Permission, u.Verified = score.Int64, avatar.Int64, perm.Int64, bool(verified)
		d.Users = append(d.Users, u)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read users: %w", err)
	}

	err = each(ctx, tx, "SELECT pid, uid, name, tid_skin, tid_cape, last_modified FROM "+s.table("players")+" ORDER BY pid", func(sc scanner) error {
		var (
			p               srcPlayer
			name            sql.NullString
			uid, skin, cape sql.NullInt64
		)
		if err := sc.Scan(&p.ID, &uid, &name, &skin, &cape, &p.ModifiedAt); err != nil {
			return err
		}
		p.UserID, p.Name, p.Skin, p.Cape = uid.Int64, name.String, skin.Int64, cape.Int64
		d.Players = append(d.Players, p)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read players: %w", err)
	}

	err = each(ctx, tx, "SELECT tid, name, type, hash, size, uploader, public, upload_at FROM "+s.table("textures")+" ORDER BY tid", func(sc scanner) error {
		var (
			t               srcTexture
			name, typ, hash sql.NullString
			size, uploader  sql.NullInt64
			public          flexBool
		)
		if err := sc.Scan(&t.ID, &name, &typ, &hash, &size, &uploader, &public, &t.UploadedAt); err != nil {
			return err
		}
		t.Name, t.Type, t.Hash, t.SizeKB, t.Uploader, t.Public = name.String, typ.String, hash.String, size.Int64, uploader.Int64, bool(public)
		d.Textures = append(d.Textures, t)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read textures: %w", err)
	}

	if optional["user_closet"] {
		// The table has no key, so the order of one user's items is whatever the
		// database returns.
		err = each(ctx, tx, "SELECT user_uid, texture_tid, item_name FROM "+s.table("user_closet")+" ORDER BY user_uid", func(sc scanner) error {
			var (
				c        srcCloset
				uid, tid sql.NullInt64
				name     sql.NullString
			)
			if err := sc.Scan(&uid, &tid, &name); err != nil {
				return err
			}
			c.UserID, c.TextureID, c.Name = uid.Int64, tid.Int64, name.String
			d.Closet = append(d.Closet, c)
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("read user_closet: %w", err)
		}
	}

	if optional["reports"] {
		err = each(ctx, tx, "SELECT id, tid, uploader, reporter, reason, status, report_at FROM "+s.table("reports")+" ORDER BY id", func(sc scanner) error {
			var (
				r                               srcReport
				tid, uploader, reporter, status sql.NullInt64
				reason                          sql.NullString
			)
			if err := sc.Scan(&r.ID, &tid, &uploader, &reporter, &reason, &status, &r.ReportedAt); err != nil {
				return err
			}
			r.TextureID, r.Uploader, r.Reporter, r.Reason, r.Status = tid.Int64, uploader.Int64, reporter.Int64, reason.String, status.Int64
			d.Reports = append(d.Reports, r)
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("read reports: %w", err)
		}
	}

	if optional["notifications"] {
		// Only unread ones (02 §5): read notifications have served their purpose.
		err = each(ctx, tx, "SELECT notifiable_type, notifiable_id, data, created_at FROM "+s.table("notifications")+" WHERE read_at IS NULL ORDER BY created_at, id", func(sc scanner) error {
			var (
				n         srcNotification
				typ, data sql.NullString
				uid       sql.NullInt64
			)
			if err := sc.Scan(&typ, &uid, &data, &n.CreatedAt); err != nil {
				return err
			}
			if !strings.HasSuffix(typ.String, "User") { // App\Models\User
				return nil
			}
			n.UserID, n.Data = uid.Int64, data.String
			d.Notifications = append(d.Notifications, n)
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("read notifications: %w", err)
		}
	}

	if optional["uuid"] {
		err = each(ctx, tx, "SELECT id, name, uuid FROM "+s.table("uuid")+" ORDER BY id", func(sc scanner) error {
			var (
				u          srcUUID
				name, uuid sql.NullString
			)
			if err := sc.Scan(&u.ID, &name, &uuid); err != nil {
				return err
			}
			u.Name, u.UUID = name.String, uuid.String
			d.UUIDs = append(d.UUIDs, u)
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("read uuid: %w", err)
		}
	}

	err = each(ctx, tx, "SELECT option_name, option_value FROM "+s.table("options"), func(sc scanner) error {
		var k, v sql.NullString
		if err := sc.Scan(&k, &v); err != nil {
			return err
		}
		d.Options[k.String] = v.String
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read options: %w", err)
	}
	return d, tx.Commit()
}

// undefined reports whether err says a table or column does not exist.
func undefined(err error) bool {
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		return pg.Code == "42P01" || pg.Code == "42703" // undefined_table, undefined_column
	}
	var my *mysql.MySQLError
	if errors.As(err, &my) {
		return my.Number == 1146 || my.Number == 1054 // ER_NO_SUCH_TABLE, ER_BAD_FIELD_ERROR
	}
	msg := err.Error() // SQLite
	return strings.Contains(msg, "no such table") || strings.Contains(msg, "no such column")
}

type scanner interface{ Scan(dest ...any) error }

func each(ctx context.Context, tx *sql.Tx, query string, fn func(scanner) error) error {
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := fn(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

// wall is a BS datetime: a wall-clock time without a zone, as MySQL (read as text),
// PostgreSQL and SQLite return it. Zero dates ("0000-00-00") and NULL are invalid.
type wall struct {
	t     time.Time // the wall clock, labelled UTC
	Valid bool
}

func (w *wall) Scan(v any) error {
	*w = wall{}
	var s string
	switch v := v.(type) {
	case nil:
		return nil
	case time.Time:
		w.t, w.Valid = time.Date(v.Year(), v.Month(), v.Day(), v.Hour(), v.Minute(), v.Second(), v.Nanosecond(), time.UTC), true
		return nil
	case []byte:
		s = string(v)
	case string:
		s = v
	default:
		return fmt.Errorf("unexpected datetime %T", v)
	}
	s = strings.TrimSpace(s)
	if s == "" || strings.HasPrefix(s, "0000-00-00") {
		return nil
	}
	for _, layout := range []string{"2006-01-02 15:04:05.999999999", time.RFC3339Nano, "2006-01-02T15:04:05.999999999", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			w.t, w.Valid = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), time.UTC), true
			return nil
		}
	}
	return fmt.Errorf("unrecognised datetime %q", s)
}

// in returns the instant the wall clock means in loc.
func (w wall) in(loc *time.Location) (time.Time, bool) {
	if !w.Valid {
		return time.Time{}, false
	}
	t := w.t
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), t.Nanosecond(), loc), true
}

// flexBool reads MySQL/SQLite integers and PostgreSQL booleans alike.
type flexBool bool

func (b *flexBool) Scan(v any) error {
	var s string
	switch v := v.(type) {
	case nil:
		*b = false
		return nil
	case bool:
		*b = flexBool(v)
		return nil
	case int64:
		*b = v != 0
		return nil
	case []byte:
		s = string(v)
	case string:
		s = v
	default:
		return fmt.Errorf("unexpected boolean %T", v)
	}
	s = strings.ToLower(strings.TrimSpace(s))
	n, err := strconv.ParseInt(s, 10, 64)
	*b = flexBool((err == nil && n != 0) || s == "t" || s == "true")
	return nil
}
