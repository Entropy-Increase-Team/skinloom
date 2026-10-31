// Package settings holds runtime settings that admins edit: typed definitions, a
// database store, and an atomically swapped snapshot that changes are hot-applied from.
package settings

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"sync"
)

// Type is the value type of a setting.
type Type string

const (
	TypeString    Type = "string"
	TypeText      Type = "text"
	TypeBool      Type = "bool"
	TypeInt       Type = "int"
	TypeLocalized Type = "localized" // {"zh-CN": "…", "en": "…"}
	TypeJSON      Type = "json"      // any JSON value
)

// Def defines one setting.
type Def struct {
	Key     string
	Type    Type
	Default any
	// Public settings are sent to every visitor in the site bootstrap.
	Public bool
	// Secret settings (passwords, secret keys) are never shown again once saved: the
	// admin panel only learns whether one is set.
	Secret bool
	// SentTo names the settings that say where a Secret is sent (the SMTP server of the
	// SMTP password). The admin panel must give the secret again to change one of them:
	// a kept secret would otherwise go to a server of the editor's choosing.
	SentTo []string
	// Superadmin settings put code into every page (custom CSS): only a super
	// administrator, the admin token or the CLI may change them (BS S16).
	Superadmin bool
	Doc        string
	// Enum lists the allowed values; the admin panel offers them as a choice.
	Enum []any
	// Range bounds a TypeInt setting, both ends included.
	Range *Range
	// Validate runs after the type, Enum and Range checks; nil means none.
	Validate func(v any) error
	// Redact hides the secret parts of a value (an object with secret fields) from the
	// admin panel, and Restore puts the stored ones back into an edited value where the
	// panel left them redacted. Both or neither.
	Redact  func(v any) any
	Restore func(stored, edited any) any
}

// Range is an inclusive integer range.
type Range struct{ Min, Max int64 }

// Between is the range lo to hi.
func Between(lo, hi int64) *Range { return &Range{Min: lo, Max: hi} }

// AtLeast is the range from lo up, without an upper bound.
func AtLeast(lo int64) *Range { return &Range{Min: lo, Max: math.MaxInt64} }

// Registry is the set of known settings. Modules register theirs at startup.
type Registry struct {
	mu   sync.RWMutex
	defs map[string]Def
}

// NewRegistry returns a registry pre-filled with the core settings.
func NewRegistry() *Registry {
	r := &Registry{defs: map[string]Def{}}
	if err := r.Register(coreDefs()...); err != nil {
		panic(err)
	}
	return r
}

// Register adds definitions; duplicate keys and defaults that fail their own
// definition are errors.
func (r *Registry) Register(defs ...Def) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range defs {
		if _, ok := r.defs[d.Key]; ok {
			return fmt.Errorf("setting %q registered twice", d.Key)
		}
		switch {
		case d.Secret && d.Public:
			return fmt.Errorf("setting %q cannot be both secret and public", d.Key)
		case len(d.SentTo) > 0 && !d.Secret:
			return fmt.Errorf("setting %q: SentTo is for secret settings", d.Key)
		case (d.Redact == nil) != (d.Restore == nil):
			return fmt.Errorf("setting %q needs both Redact and Restore", d.Key)
		case d.Range != nil && d.Type != TypeInt:
			return fmt.Errorf("setting %q: Range is for int settings", d.Key)
		}
		if err := d.check(d.Default); err != nil {
			return fmt.Errorf("setting %q default: %w", d.Key, err)
		}
		r.defs[d.Key] = d
	}
	return nil
}

// SetValidator attaches a validator to an existing setting (used when the validator
// needs a dependency that is built after the registry, e.g. the theme registry).
func (r *Registry) SetValidator(key string, fn func(v any) error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d := r.defs[key]
	d.Validate = fn
	r.defs[key] = d
}

// Def returns the definition of key.
func (r *Registry) Def(key string) (Def, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.defs[key]
	return d, ok
}

// All returns every definition sorted by key.
func (r *Registry) All() []Def {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Def, 0, len(r.defs))
	for _, d := range r.defs {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// Redacted stands in for a saved secret inside a value shown to the admin panel
// (Def.Redact); sent back unchanged, it keeps the saved secret (Def.Restore).
const Redacted = "********"

// Shown is value v as the admin panel may see it: nil for a secret setting, the
// redacted value for one with secret parts.
func (d Def) Shown(v any) any {
	switch {
	case d.Secret:
		return nil
	case d.Redact != nil:
		return d.Redact(v)
	}
	return v
}

// validate decodes raw and checks it against the definition.
func (d Def) validate(raw json.RawMessage) (any, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("not valid JSON: %w", err)
	}
	if err := d.check(v); err != nil {
		return nil, err
	}
	if d.Validate != nil {
		if err := d.Validate(v); err != nil {
			return nil, err
		}
	}
	return v, nil
}

// check runs the declarative checks: type, Enum, Range.
func (d Def) check(v any) error {
	if err := checkType(d.Type, v); err != nil {
		return err
	}
	if len(d.Enum) > 0 && !inEnum(d.Enum, v) {
		return fmt.Errorf("want one of %v", d.Enum)
	}
	if r := d.Range; r != nil {
		if n, _ := asInt(v); n < r.Min || n > r.Max {
			if r.Max == math.MaxInt64 {
				return fmt.Errorf("must be %d or more", r.Min)
			}
			return fmt.Errorf("must be %d to %d", r.Min, r.Max)
		}
	}
	return nil
}

func checkType(t Type, v any) error {
	switch t {
	case TypeString, TypeText:
		if _, ok := v.(string); !ok {
			return fmt.Errorf("want a string")
		}
	case TypeBool:
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("want true or false")
		}
	case TypeInt:
		if _, ok := asInt(v); !ok {
			return fmt.Errorf("want an integer")
		}
	case TypeLocalized:
		m, ok := v.(map[string]any)
		if !ok {
			if _, isTyped := v.(map[string]string); isTyped {
				return nil
			}
			return fmt.Errorf(`want an object like {"zh-CN": "...", "en": "..."}`)
		}
		for k, s := range m {
			if _, ok := s.(string); !ok {
				return fmt.Errorf("value for locale %q must be a string", k)
			}
		}
	case TypeJSON:
	default:
		return fmt.Errorf("unknown setting type %q", t)
	}
	return nil
}

// asInt reads an integer from a decoded JSON number (float64) or a Go default.
func asInt(v any) (int64, bool) {
	switch n := v.(type) {
	case int:
		return int64(n), true
	case int64:
		return n, true
	case float64:
		if n != math.Trunc(n) || math.Abs(n) > 1<<53 {
			return 0, false
		}
		return int64(n), true
	}
	return 0, false
}

func inEnum(enum []any, v any) bool {
	for _, e := range enum {
		if a, ok := asInt(e); ok {
			if b, ok := asInt(v); ok && a == b {
				return true
			}
			continue
		}
		if e == v {
			return true
		}
	}
	return false
}
