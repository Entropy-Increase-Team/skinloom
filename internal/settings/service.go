package settings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/setting"
	"github.com/Entropy-Increase-Team/skinloom/ent/systemstate"
	"github.com/Entropy-Increase-Team/skinloom/internal/audit"
	"github.com/Entropy-Increase-Team/skinloom/internal/hook"
	"github.com/Entropy-Increase-Team/skinloom/internal/pubsub"
)

// ConfigVersionKey is the system_state row every node polls. It is bumped by any change
// that should reach browsers and other nodes: settings, theme, plugins.
const ConfigVersionKey = "config_version"

// ValidationError lists per-key problems of a rejected update.
type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string {
	keys := make([]string, 0, len(e.Fields))
	for k := range e.Fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+": "+e.Fields[k])
	}
	return "invalid settings: " + strings.Join(parts, "; ")
}

// Snapshot is an immutable view of all settings at one config version.
type Snapshot struct {
	Version int64
	values  map[string]json.RawMessage
	reg     *Registry
}

// NewSnapshot builds a snapshot from raw JSON values (tests and tooling; the service
// builds its own from the database).
func NewSnapshot(reg *Registry, version int64, values map[string]json.RawMessage) *Snapshot {
	if values == nil {
		values = map[string]json.RawMessage{}
	}
	return &Snapshot{Version: version, values: values, reg: reg}
}

// Raw returns the stored value of key, or its default.
func (s *Snapshot) Raw(key string) json.RawMessage {
	if v, ok := s.values[key]; ok {
		return v
	}
	if d, ok := s.reg.Def(key); ok {
		b, _ := json.Marshal(d.Default)
		return b
	}
	return json.RawMessage("null")
}

// IsSet reports whether key has a stored (non-default) value.
func (s *Snapshot) IsSet(key string) bool { _, ok := s.values[key]; return ok }

// Decode unmarshals the value of key into T.
func Decode[T any](s *Snapshot, key string) T {
	var v T
	_ = json.Unmarshal(s.Raw(key), &v)
	return v
}

// String returns a string setting.
func (s *Snapshot) String(key string) string { return Decode[string](s, key) }

// Localized picks the best translation of a localized setting for locale.
func (s *Snapshot) Localized(key, locale string) string {
	return PickLocalized(Decode[map[string]string](s, key), locale)
}

// PickLocalized chooses exact locale, then same language, then en, then zh-CN, then any.
func PickLocalized(m map[string]string, locale string) string {
	if len(m) == 0 {
		return ""
	}
	if v, ok := m[locale]; ok && v != "" {
		return v
	}
	lang, _, _ := strings.Cut(locale, "-")
	for k, v := range m {
		if l, _, _ := strings.Cut(k, "-"); strings.EqualFold(l, lang) && v != "" {
			return v
		}
	}
	for _, fallback := range []string{"en", "zh-CN"} {
		if v := m[fallback]; v != "" {
			return v
		}
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return m[keys[0]]
}

// Service loads, stores and broadcasts settings.
type Service struct {
	reg    *Registry
	client *ent.Client
	broker *pubsub.Broker
	bus    *hook.Bus
	log    *slog.Logger
	cur    atomic.Pointer[Snapshot]
}

// NewService builds the service; call Load before use.
func NewService(reg *Registry, client *ent.Client, broker *pubsub.Broker, bus *hook.Bus, log *slog.Logger) *Service {
	s := &Service{reg: reg, client: client, broker: broker, bus: bus, log: log}
	s.cur.Store(&Snapshot{values: map[string]json.RawMessage{}, reg: reg})
	return s
}

// Registry returns the definitions.
func (s *Service) Registry() *Registry { return s.reg }

// Current returns the live snapshot. Hold on to it for the duration of one request.
func (s *Service) Current() *Snapshot { return s.cur.Load() }

// Load reads every setting and the config version and makes them current.
func (s *Service) Load(ctx context.Context) error {
	version, err := readVersion(ctx, s.client)
	if err != nil {
		return err
	}
	rows, err := s.client.Setting.Query().All(ctx)
	if err != nil {
		return fmt.Errorf("load settings: %w", err)
	}
	values := make(map[string]json.RawMessage, len(rows))
	for _, r := range rows {
		if _, known := s.reg.Def(r.ID); !known {
			continue // left by a removed module or plugin; keep the row, ignore the value
		}
		values[r.ID] = json.RawMessage(r.Value)
	}
	s.publish(&Snapshot{Version: version, values: values, reg: s.reg})
	return nil
}

// publish makes next current unless a newer snapshot is already live. Set and Watch
// both reload concurrently; without this a slow reload could roll settings back.
// The version is read before the rows, so a snapshot may carry newer values than its
// version says — never older — and the next poll reconciles the version.
func (s *Service) publish(next *Snapshot) bool {
	for {
		cur := s.cur.Load()
		if cur != nil && cur.Version > next.Version {
			return false
		}
		if s.cur.CompareAndSwap(cur, next) {
			return true
		}
	}
}

// Set validates and stores changes in one transaction, bumps the config version and
// publishes the new snapshot. A JSON null resets a key to its default. A setting with
// secret parts (Def.Restore) gets the stored ones back where the edited value left
// them redacted. rec, if not nil, is written to the audit log in the same transaction,
// with the keys changed.
func (s *Service) Set(ctx context.Context, actor *int64, changes map[string]json.RawMessage, rec *audit.Entry) (*Snapshot, error) {
	verr := &ValidationError{Fields: map[string]string{}}
	type change struct {
		def   Def
		value json.RawMessage
		reset bool
	}
	var list []change
	for key, raw := range changes {
		d, ok := s.reg.Def(key)
		if !ok {
			verr.Fields[key] = "unknown setting"
			continue
		}
		if strings.TrimSpace(string(raw)) == "null" {
			list = append(list, change{def: d, reset: true})
			continue
		}
		if d.Restore != nil { // checked in the transaction, once the stored secrets are back
			if !json.Valid(raw) {
				verr.Fields[key] = "not valid JSON"
				continue
			}
			list = append(list, change{def: d, value: raw})
			continue
		}
		v, err := d.validate(raw)
		if err != nil {
			verr.Fields[key] = err.Error()
			continue
		}
		canonical, _ := json.Marshal(v)
		list = append(list, change{def: d, value: canonical})
	}
	// A kept secret follows the server it was given for (Def.SentTo).
	cur := s.Current()
	for _, d := range s.reg.All() {
		if _, given := changes[d.Key]; len(d.SentTo) == 0 || given || !cur.IsSet(d.Key) {
			continue
		}
		for _, k := range d.SentTo {
			if raw, ok := changes[k]; ok && !sameJSON(raw, cur.Raw(k)) {
				verr.Fields[d.Key] = "give it again: " + k + " changes where it is sent"
				break
			}
		}
	}
	if len(verr.Fields) > 0 {
		return nil, verr
	}
	if len(list) == 0 {
		return s.Current(), nil
	}
	// Rows are written in key order: in map order, two saves of overlapping keys locked
	// them in opposite orders and deadlocked (PostgreSQL, 8 saves at once: 130 of 200).
	slices.SortFunc(list, func(a, b change) int { return strings.Compare(a.def.Key, b.def.Key) })
	keys := make([]string, 0, len(list))
	for _, c := range list {
		keys = append(keys, c.def.Key)
	}

	tx, err := s.client.Tx(ctx)
	if err != nil {
		return nil, err
	}
	// The config version row is taken first, so saves run one at a time: their row
	// locks cannot cross, and the first save of a key cannot leave restore's lock on a
	// missing row as a gap lock that a concurrent first save deadlocks on (MySQL).
	version, err := bumpVersion(ctx, tx.Client())
	if err != nil {
		return nil, rollback(tx, err)
	}
	for _, c := range list {
		if c.reset {
			if err := tx.Setting.DeleteOneID(c.def.Key).Exec(ctx); err != nil && !ent.IsNotFound(err) {
				return nil, rollback(tx, err)
			}
			continue
		}
		if c.def.Restore != nil {
			if c.value, err = restore(ctx, tx.Client(), c.def, c.value); err != nil {
				return nil, rollback(tx, err)
			}
		}
		err := tx.Setting.Create().
			SetID(c.def.Key).
			SetValue(string(c.value)).
			SetNillableUpdatedBy(actor).
			SetUpdatedAt(time.Now()).
			OnConflictColumns(setting.FieldID).
			UpdateNewValues().
			Exec(ctx)
		if err != nil {
			return nil, rollback(tx, fmt.Errorf("save %s: %w", c.def.Key, err))
		}
	}
	if err := audit.Write(ctx, tx.Client(), rec.With("keys", keys)); err != nil {
		return nil, rollback(tx, err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	if err := s.Load(ctx); err != nil {
		return nil, err
	}
	s.log.Info("settings updated", "keys", keys, "version", version)
	s.announce(version, keys)
	return s.Current(), nil
}

// restore completes an edited value of d with the stored secrets (Def.Restore) and
// validates the result. The stored row is locked first: read before the lock, the
// secret a concurrent save just stored could be written back over with the old one.
func restore(ctx context.Context, tx *ent.Client, d Def, raw json.RawMessage) (json.RawMessage, error) {
	if err := tx.Setting.Update().Where(setting.ID(d.Key)).SetUpdatedAt(time.Now()).Exec(ctx); err != nil {
		return nil, fmt.Errorf("lock %s: %w", d.Key, err)
	}
	storedRaw, _ := json.Marshal(d.Default)
	switch row, err := tx.Setting.Get(ctx, d.Key); {
	case err == nil:
		storedRaw = []byte(row.Value)
	case !ent.IsNotFound(err):
		return nil, fmt.Errorf("read %s: %w", d.Key, err)
	}
	var stored, edited any
	if err := json.Unmarshal(storedRaw, &stored); err != nil {
		return nil, fmt.Errorf("read %s: %w", d.Key, err)
	}
	if err := json.Unmarshal(raw, &edited); err != nil {
		return nil, err
	}
	full, err := json.Marshal(d.Restore(stored, edited))
	if err != nil {
		return nil, err
	}
	v, err := d.validate(full)
	if err != nil {
		return nil, &ValidationError{Fields: map[string]string{d.Key: err.Error()}}
	}
	return json.Marshal(v)
}

// Touch bumps the config version without changing settings (themes and plugins use it
// when their own state changes) and broadcasts it.
func (s *Service) Touch(ctx context.Context) (int64, error) {
	v, err := bumpVersion(ctx, s.client)
	if err != nil {
		return 0, err
	}
	if err := s.Load(ctx); err != nil {
		return 0, err
	}
	s.announce(v, nil)
	return v, nil
}

func (s *Service) announce(version int64, keys []string) {
	s.broker.Publish(pubsub.TopicConfigChanged, version)
	hook.OnSettingsChanged.Emit(s.bus, hook.SettingsChanged{Version: version, Keys: keys})
}

// Watch polls the config version so changes made by other nodes or by the CLI become
// live here too. With Redis configured (later milestone) this becomes a subscription.
func (s *Service) Watch(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			v, err := readVersion(ctx, s.client)
			if err != nil {
				if ctx.Err() == nil {
					s.log.Warn("poll config version", "err", err)
				}
				continue
			}
			if v == s.Current().Version {
				continue
			}
			if err := s.Load(ctx); err != nil {
				s.log.Warn("reload settings", "err", err)
				continue
			}
			s.log.Info("settings changed elsewhere; reloaded", "version", v)
			s.announce(v, nil)
		}
	}
}

func readVersion(ctx context.Context, c *ent.Client) (int64, error) {
	row, err := c.SystemState.Get(ctx, ConfigVersionKey)
	if ent.IsNotFound(err) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read config version: %w", err)
	}
	return row.IntValue, nil
}

func bumpVersion(ctx context.Context, c *ent.Client) (int64, error) {
	err := c.SystemState.Create().
		SetID(ConfigVersionKey).
		SetIntValue(1).
		SetUpdatedAt(time.Now()).
		OnConflictColumns(systemstate.FieldID).
		Update(func(u *ent.SystemStateUpsert) {
			u.AddIntValue(1)
			u.UpdateUpdatedAt()
		}).
		Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("bump config version: %w", err)
	}
	return readVersion(ctx, c)
}

func rollback(tx *ent.Tx, err error) error {
	if rerr := tx.Rollback(); rerr != nil {
		return errors.Join(err, rerr)
	}
	return err
}

// sameJSON: a and b decode to the same value.
func sameJSON(a, b json.RawMessage) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}
