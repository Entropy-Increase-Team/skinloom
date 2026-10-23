// Package settings holds runtime settings that admins edit: typed definitions, a
// database store, and an atomically swapped snapshot that changes are hot-applied from.
package settings

import (
	"encoding/json"
	"fmt"
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
	Doc    string
	// Validate runs after the type check; nil means type check only.
	Validate func(v any) error
}

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

// Register adds definitions; duplicate keys are an error.
func (r *Registry) Register(defs ...Def) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range defs {
		if _, ok := r.defs[d.Key]; ok {
			return fmt.Errorf("setting %q registered twice", d.Key)
		}
		if err := checkType(d.Type, d.Default); err != nil {
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

// Validate type-checks raw and runs the definition's validator.
func (d Def) validate(raw json.RawMessage) (any, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("not valid JSON: %w", err)
	}
	if err := checkType(d.Type, v); err != nil {
		return nil, err
	}
	if d.Validate != nil {
		if err := d.Validate(v); err != nil {
			return nil, err
		}
	}
	return v, nil
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
		switch n := v.(type) {
		case int, int64:
		case float64:
			if n != float64(int64(n)) {
				return fmt.Errorf("want an integer")
			}
		default:
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
