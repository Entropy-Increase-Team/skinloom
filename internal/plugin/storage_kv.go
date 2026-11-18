package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/pluginkv"
	"github.com/Entropy-Increase-Team/skinloom/ent/schema"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
)

// KVEntry is one item returned by ctx.kv.list.
type KVEntry struct {
	Key   string `json:"key"`
	Value any    `json:"value"`
}

// KVStore implements plugin-isolated key/value storage backed by plugin_kv.
type KVStore struct {
	pluginID string
	db       *ent.Client
	maxKeys  int
	perms    *PermissionSet
}

// NewKVStore returns a plugin-scoped KV store guarded by storage.kv permission.
func NewKVStore(pluginID string, client *ent.Client, maxKeys int, perms *PermissionSet) *KVStore {
	if maxKeys <= 0 {
		maxKeys = MaxPluginKVKeys
	}
	return &KVStore{pluginID: pluginID, db: client, maxKeys: maxKeys, perms: perms}
}

func (s *KVStore) checkPerm() error {
	return s.perms.Check(PermStorageKV)
}

func validateKVKey(key string) error {
	if key == "" || len(key) > 255 {
		return fmt.Errorf("kv key must be 1-255 bytes")
	}
	return nil
}

// Get returns the decoded JSON value for key, or nil if missing or expired.
func (s *KVStore) Get(ctx context.Context, key string) (any, error) {
	if err := s.checkPerm(); err != nil {
		return nil, err
	}
	if err := validateKVKey(key); err != nil {
		return nil, err
	}
	row, err := s.db.PluginKV.Query().
		Where(pluginkv.Plugin(s.pluginID), pluginkv.Key(key)).
		Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if row.ExpiresAt != nil && !row.ExpiresAt.After(time.Now()) {
		_, _ = s.db.PluginKV.Delete().Where(pluginkv.ID(row.ID)).Exec(ctx)
		return nil, nil
	}
	var out any
	if len(row.Value) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(row.Value, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Set stores a JSON value under key with optional TTL (seconds <= 0 means no expiry).
func (s *KVStore) Set(ctx context.Context, key string, val any, ttlSeconds float64) error {
	if err := s.checkPerm(); err != nil {
		return err
	}
	if err := validateKVKey(key); err != nil {
		return err
	}
	raw, err := json.Marshal(val)
	if err != nil {
		return fmt.Errorf("kv value must be JSON serializable: %w", err)
	}
	if len(raw) > MaxPluginKVValBytes {
		return fmt.Errorf("%w: kv value exceeds %d bytes", ErrQuotaExceeded, MaxPluginKVValBytes)
	}
	var exp *time.Time
	now := time.Now().UTC()
	if ttlSeconds > 0 && !math.IsNaN(ttlSeconds) && !math.IsInf(ttlSeconds, 0) {
		t := now.Add(time.Duration(ttlSeconds * float64(time.Second)))
		exp = &t
	}

	return db.WithTx(ctx, s.db, func(tx *ent.Client) error {
		exists, err := tx.PluginKV.Query().Where(pluginkv.Plugin(s.pluginID), pluginkv.Key(key)).Exist(ctx)
		if err != nil {
			return err
		}
		if !exists {
			// Purge expired keys for this plugin before rejecting on quota.
			_, _ = tx.PluginKV.Delete().Where(pluginkv.Plugin(s.pluginID), pluginkv.ExpiresAtLTE(now)).Exec(ctx)
			cnt, err := tx.PluginKV.Query().Where(pluginkv.Plugin(s.pluginID)).Count(ctx)
			if err != nil {
				return err
			}
			if cnt >= s.maxKeys {
				return fmt.Errorf("%w: max kv keys (%d) reached", ErrQuotaExceeded, s.maxKeys)
			}
		}
		c := tx.PluginKV.Create().
			SetPlugin(s.pluginID).
			SetKey(key).
			SetValue(schema.KVValue(raw)).
			SetUpdatedAt(now)
		if exp != nil {
			c.SetExpiresAt(*exp)
		}
		return c.OnConflictColumns(pluginkv.FieldPlugin, pluginkv.FieldKey).
			Update(func(u *ent.PluginKVUpsert) {
				u.SetValue(schema.KVValue(raw))
				u.SetUpdatedAt(now)
				if exp != nil {
					u.SetExpiresAt(*exp)
				} else {
					u.ClearExpiresAt()
				}
			}).Exec(ctx)
	})
}

// Delete removes key for this plugin.
func (s *KVStore) Delete(ctx context.Context, key string) (bool, error) {
	if err := s.checkPerm(); err != nil {
		return false, err
	}
	if err := validateKVKey(key); err != nil {
		return false, err
	}
	n, err := s.db.PluginKV.Delete().Where(pluginkv.Plugin(s.pluginID), pluginkv.Key(key)).Exec(ctx)
	return n > 0, err
}

// List returns non-expired keys matching prefix.
func (s *KVStore) List(ctx context.Context, prefix string, limit int) ([]KVEntry, error) {
	if err := s.checkPerm(); err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	now := time.Now().UTC()
	q := s.db.PluginKV.Query().Where(
		pluginkv.Plugin(s.pluginID),
		pluginkv.Or(pluginkv.ExpiresAtIsNil(), pluginkv.ExpiresAtGT(now)),
	)
	if prefix != "" {
		q = q.Where(pluginkv.KeyHasPrefix(prefix))
	}
	rows, err := q.Order(ent.Asc(pluginkv.FieldKey)).Limit(limit).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]KVEntry, 0, len(rows))
	for _, r := range rows {
		var v any
		if len(r.Value) > 0 {
			_ = json.Unmarshal(r.Value, &v)
		}
		out = append(out, KVEntry{Key: r.Key, Value: v})
	}
	return out, nil
}

// Incr atomically increments (or creates) a numeric KV value by delta.
func (s *KVStore) Incr(ctx context.Context, key string, delta float64) (float64, error) {
	if err := s.checkPerm(); err != nil {
		return 0, err
	}
	if err := validateKVKey(key); err != nil {
		return 0, err
	}
	if math.IsNaN(delta) || math.IsInf(delta, 0) {
		return 0, errors.New("kv incr delta must be a finite number")
	}
	var next float64
	now := time.Now().UTC()
	err := db.WithTx(ctx, s.db, func(tx *ent.Client) error {
		row, err := tx.PluginKV.Query().Where(pluginkv.Plugin(s.pluginID), pluginkv.Key(key)).Only(ctx)
		if err != nil && !ent.IsNotFound(err) {
			return err
		}
		var cur float64
		var exp *time.Time
		if err == nil && (row.ExpiresAt == nil || row.ExpiresAt.After(now)) {
			exp = row.ExpiresAt
			var existing any
			if len(row.Value) > 0 {
				if err := json.Unmarshal(row.Value, &existing); err != nil {
					return err
				}
			}
			switch n := existing.(type) {
			case nil:
			case float64:
				cur = n
			default:
				return errors.New("kv incr target is not a number")
			}
		} else if ent.IsNotFound(err) {
			_, _ = tx.PluginKV.Delete().Where(pluginkv.Plugin(s.pluginID), pluginkv.ExpiresAtLTE(now)).Exec(ctx)
			cnt, err := tx.PluginKV.Query().Where(pluginkv.Plugin(s.pluginID)).Count(ctx)
			if err != nil {
				return err
			}
			if cnt >= s.maxKeys {
				return fmt.Errorf("%w: max kv keys (%d) reached", ErrQuotaExceeded, s.maxKeys)
			}
		}
		next = cur + delta
		raw, err := json.Marshal(next)
		if err != nil {
			return err
		}
		c := tx.PluginKV.Create().SetPlugin(s.pluginID).SetKey(key).SetValue(schema.KVValue(raw)).SetUpdatedAt(now)
		if exp != nil {
			c.SetExpiresAt(*exp)
		}
		return c.OnConflictColumns(pluginkv.FieldPlugin, pluginkv.FieldKey).
			Update(func(u *ent.PluginKVUpsert) {
				u.SetValue(schema.KVValue(raw))
				u.SetUpdatedAt(now)
				if exp != nil {
					u.SetExpiresAt(*exp)
				} else {
					u.ClearExpiresAt()
				}
			}).Exec(ctx)
	})
	return next, err
}
