package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Entropy-Increase-Team/skinloom/internal/plugin"
)

func TestPlugingenDeterminismAndCoverage(t *testing.T) {
	a, err := buildSchemaJSON()
	if err != nil {
		t.Fatalf("buildSchemaJSON: %v", err)
	}
	b, err := buildSchemaJSON()
	if err != nil {
		t.Fatalf("second buildSchemaJSON: %v", err)
	}
	if string(a) != string(b) {
		t.Fatalf("schema JSON generation is non-deterministic")
	}

	var parsed map[string]any
	if err := json.Unmarshal(a, &parsed); err != nil {
		t.Fatalf("invalid generated schema JSON: %v", err)
	}
	props := parsed["properties"].(map[string]any)
	for _, key := range []string{"permissions", "hooks", "storage", "slots", "forms", "theme", "services", "menus", "blocks"} {
		if _, ok := props[key]; !ok {
			t.Fatalf("generated schema missing top-level property %q", key)
		}
	}

	ts1 := buildTSCatalog()
	ts2 := buildTSCatalog()
	if ts1 != ts2 {
		t.Fatalf("TS catalog generation is non-deterministic")
	}
	for _, needle := range []string{
		"export interface AuthRegisterGuard",
		"export interface UserBadge",
		"export type HookGuardName =",
		"export type HookFilterName =",
		"export type HookEventName =",
		"export type PluginPermission =",
		"export type PluginSlotName =",
		"'admin.status.cards':",
		"'yggdrasil.joined':",
		"'score.changed':",
		"export type BuiltinBlockType =",
		"export type ThemeOverrideTarget =",
	} {
		if !strings.Contains(ts1, needle) {
			t.Fatalf("generated catalog.ts missing %q", needle)
		}
	}
}

func TestBuiltinThemesMatchSchemaAndManifest(t *testing.T) {
	schemaBytes, err := buildSchemaJSON()
	if err != nil {
		t.Fatalf("buildSchemaJSON: %v", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(schemaBytes, &schema); err != nil {
		t.Fatalf("decode schema: %v", err)
	}
	topProps := schema["properties"].(map[string]any)
	themeProps := topProps["theme"].(map[string]any)["properties"].(map[string]any)

	for _, name := range []string{"default", "brocade"} {
		path := filepath.Join("..", "..", "..", "themes", name, "plugin.json")
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read theme %s: %v", name, err)
		}
		if _, err := plugin.ParseManifest(raw); err != nil {
			t.Fatalf("ParseManifest(%s): %v", name, err)
		}
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("unmarshal theme %s: %v", name, err)
		}
		for key := range doc {
			if _, ok := topProps[key]; !ok {
				t.Fatalf("theme %s uses top-level key %q absent from plugin.v1.json", name, key)
			}
		}
		if themeObj, ok := doc["theme"].(map[string]any); ok {
			for key := range themeObj {
				if _, ok := themeProps[key]; !ok {
					t.Fatalf("theme %s uses theme.%s absent from plugin.v1.json", name, key)
				}
			}
		}
	}
}
