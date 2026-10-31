package settings_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"sync"
	"testing"

	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/settings"
)

func TestRegisterChecksDefinitions(t *testing.T) {
	keep := func(_, v any) any { return v }
	cases := []struct {
		name string
		def  settings.Def
	}{
		{"secret and public", settings.Def{Key: "a", Type: settings.TypeString, Default: "", Secret: true, Public: true}},
		{"redact without restore", settings.Def{Key: "a", Type: settings.TypeJSON, Redact: func(v any) any { return v }}},
		{"restore without redact", settings.Def{Key: "a", Type: settings.TypeJSON, Restore: keep}},
		{"range on a string", settings.Def{Key: "a", Type: settings.TypeString, Default: "", Range: settings.Between(1, 2)}},
		{"default outside the enum", settings.Def{Key: "a", Type: settings.TypeString, Default: "c", Enum: []any{"a", "b"}}},
		{"default outside the range", settings.Def{Key: "a", Type: settings.TypeInt, Default: 0, Range: settings.Between(1, 2)}},
	}
	for _, c := range cases {
		if err := settings.NewRegistry().Register(c.def); err == nil {
			t.Errorf("%s: registered", c.name)
		}
	}
}

func TestEnumAndRange(t *testing.T) {
	svc, _ := newService(t, openDB(t, "sqlite", "test.db"))
	if err := svc.Registry().Register(
		settings.Def{Key: "t.mode", Type: settings.TypeString, Default: "a", Enum: []any{"a", "b"}},
		settings.Def{Key: "t.status", Type: settings.TypeInt, Default: 404, Enum: []any{404, 204}},
		settings.Def{Key: "t.count", Type: settings.TypeInt, Default: 1, Range: settings.Between(1, 10)},
		settings.Def{Key: "t.floor", Type: settings.TypeInt, Default: 0, Range: settings.AtLeast(0)},
	); err != nil {
		t.Fatal(err)
	}
	set := func(body string) map[string]string {
		t.Helper()
		var changes map[string]json.RawMessage
		if err := json.Unmarshal([]byte(body), &changes); err != nil {
			t.Fatal(err)
		}
		_, err := svc.Set(context.Background(), nil, changes, nil)
		var verr *settings.ValidationError
		if errors.As(err, &verr) {
			return verr.Fields
		}
		if err != nil {
			t.Fatal(err)
		}
		return nil
	}
	if fields := set(`{"t.mode":"b","t.status":204,"t.count":10,"t.floor":1000000}`); fields != nil {
		t.Fatalf("valid values refused: %v", fields)
	}
	fields := set(`{"t.mode":"c","t.status":200,"t.count":11,"t.floor":-1}`)
	want := map[string]string{
		"t.mode": "want one of [a b]", "t.status": "want one of [404 204]",
		"t.count": "must be 1 to 10", "t.floor": "must be 0 or more",
	}
	if !maps.Equal(fields, want) {
		t.Fatalf("errors %v, want %v", fields, want)
	}
	if fields := set(`{"t.count":2.5}`); fields["t.count"] != "want an integer" {
		t.Fatalf("fraction: %v", fields)
	}
}

// credentials is a setting with a secret part, like auth.social.providers.
func credentials() settings.Def {
	return settings.Def{
		Key: "t.credentials", Type: settings.TypeJSON, Default: map[string]any{},
		Validate: func(v any) error {
			if v.(map[string]any)["secret"] == settings.Redacted {
				return errors.New("the placeholder would be saved as the secret")
			}
			return nil
		},
		Redact: func(v any) any {
			m := maps.Clone(v.(map[string]any))
			if _, ok := m["secret"]; ok {
				m["secret"] = settings.Redacted
			}
			return m
		},
		Restore: func(stored, edited any) any {
			m := maps.Clone(edited.(map[string]any))
			if m["secret"] == settings.Redacted {
				if s, ok := stored.(map[string]any)["secret"]; ok {
					m["secret"] = s
				} else {
					delete(m, "secret")
				}
			}
			return m
		},
	}
}

func TestRestoreSecrets(t *testing.T) { forEachDB(t, testRestoreSecrets) }

func testRestoreSecrets(t *testing.T, database *db.DB) {
	ctx := context.Background()
	svc, _ := newService(t, database)
	def := credentials()
	if err := svc.Registry().Register(def); err != nil {
		t.Fatal(err)
	}
	save := func(v string) error {
		_, err := svc.Set(ctx, nil, map[string]json.RawMessage{def.Key: json.RawMessage(v)}, nil)
		return err
	}
	stored := func() map[string]any { return settings.Decode[map[string]any](svc.Current(), def.Key) }

	// Nothing saved yet: a redacted secret has nothing to keep.
	if err := save(`{"user":"a","secret":"` + settings.Redacted + `"}`); err != nil || stored()["secret"] != nil {
		t.Fatalf("first save: %v, %v", err, stored())
	}
	if err := save(`{"user":"a","secret":"s0"}`); err != nil {
		t.Fatal(err)
	}
	if shown := def.Shown(stored()).(map[string]any); shown["secret"] != settings.Redacted || shown["user"] != "a" {
		t.Fatalf("shown %v", shown)
	}
	if err := save(`{"user":"b","secret":"` + settings.Redacted + `"}`); err != nil {
		t.Fatal(err)
	}
	if got := stored(); got["user"] != "b" || got["secret"] != "s0" {
		t.Fatalf("after editing the user: %v", got)
	}

	// One admin stores a new secret while another saves an edit with the old one
	// redacted. The stored row is read under its lock, so the edit never writes the
	// old secret back over the new one.
	for round := 1; round <= 20; round++ {
		next := fmt.Sprintf("s%d", round)
		var wg sync.WaitGroup
		errs := make([]error, 2)
		wg.Add(2)
		go func() { defer wg.Done(); errs[0] = save(`{"user":"c","secret":"` + next + `"}`) }()
		go func() { defer wg.Done(); errs[1] = save(`{"user":"d","secret":"` + settings.Redacted + `"}`) }()
		wg.Wait()
		if err := errors.Join(errs...); err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		if got := stored()["secret"]; got != next {
			t.Fatalf("round %d: secret %v, want %s", round, got, next)
		}
	}
}
