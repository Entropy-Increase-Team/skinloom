package hook

import (
	"context"
	"encoding/json"
	"testing"
)

func TestCatalogCompleteness(t *testing.T) {
	defs := Catalog()
	counts := map[Kind]int{}
	byName := make(map[string]Def, len(defs))
	for _, d := range defs {
		if len(d.Name) >= 5 && d.Name[:5] == "test." {
			continue
		}
		counts[d.Kind]++
		byName[d.Name] = d
	}

	if counts[KindGuard] != 21 {
		t.Fatalf("expected 21 guards, got %d", counts[KindGuard])
	}
	if counts[KindFilter] != 7 {
		t.Fatalf("expected 7 filters, got %d", counts[KindFilter])
	}
	if counts[KindEvent] != 19 {
		t.Fatalf("expected 19 events, got %d", counts[KindEvent])
	}

	expectedGuards := map[string]FailPolicy{
		"auth.login":             FailClosed,
		"auth.register":          FailClosed,
		"player.create":          FailClosed,
		"player.rename":          FailClosed,
		"player.delete":          FailClosed,
		"player.textures.set":    FailClosed,
		"player.textures.clear":  FailClosed,
		"texture.upload":         FailClosed,
		"texture.rename":         FailClosed,
		"texture.visibility":     FailClosed,
		"texture.type":           FailClosed,
		"texture.delete":         FailClosed,
		"closet.add":             FailClosed,
		"closet.rename":          FailClosed,
		"closet.remove":          FailClosed,
		"score.checkin":          FailClosed,
		"user.profile.update":    FailClosed,
		"user.avatar.update":     FailClosed,
		"report.submit":          FailClosed,
		"yggdrasil.authenticate": FailOpen,
		"yggdrasil.join":         FailOpen,
	}
	for name, wantFail := range expectedGuards {
		d, ok := byName[name]
		if !ok || d.Kind != KindGuard {
			t.Fatalf("missing guard %q", name)
		}
		if d.Fail != wantFail {
			t.Errorf("guard %q: got fail=%v, want %v", name, d.Fail, wantFail)
		}
	}

	for _, name := range []string{
		"player.name",
		"texture.name",
		"closet.itemName",
		"score.checkinAmount",
		"user.badges",
		"profile.textures",
		"yggdrasil.profileProperties",
	} {
		if d, ok := byName[name]; !ok || d.Kind != KindFilter {
			t.Fatalf("missing filter %q", name)
		}
	}

	for _, name := range []string{
		"user.loggedIn",
		"user.updated",
		"score.changed",
		"yggdrasil.joined",
	} {
		if d, ok := byName[name]; !ok || d.Kind != KindEvent {
			t.Fatalf("missing event %q", name)
		}
	}
}

func TestNilBusNoop(t *testing.T) {
	ctx := context.Background()
	if err := GuardAuthLogin.Check(ctx, nil, AuthLoginGuard{Identifier: "a"}); err != nil {
		t.Fatalf("nil bus guard check: %v", err)
	}
	if got := FilterPlayerName.Apply(ctx, nil, "Steve", PlayerNameFilterContext{UserID: 1}); got != "Steve" {
		t.Fatalf("nil bus filter apply: got %q", got)
	}
	OnUserLoggedIn.Emit(nil, UserLoginEvent{UserID: 1, Method: "password"})
	if err := OnUserLoggedIn.Publish(ctx, nil, nil, UserLoginEvent{UserID: 1, Method: "password"}); err != nil {
		t.Fatalf("nil bus event publish: %v", err)
	}
}

func TestRedactForm(t *testing.T) {
	in := map[string]any{
		"invite_code":           "ABC-123",
		"password":              "hunter2",
		"password_confirmation": "hunter2",
		"clientSecret":          "top-secret",
		"terms":                 true,
	}
	out := RedactForm(in)
	if out["invite_code"] != "ABC-123" || out["terms"] != true {
		t.Fatalf("unexpected non-secret fields kept: %#v", out)
	}
	for _, k := range []string{"password", "password_confirmation", "clientSecret"} {
		if _, exists := out[k]; exists {
			t.Fatalf("expected sensitive field %q stripped", k)
		}
	}

	raw, err := json.Marshal(UserEvent{UserID: 42, Method: "password", Form: out})
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) == 0 {
		t.Fatal("empty json")
	}
}
