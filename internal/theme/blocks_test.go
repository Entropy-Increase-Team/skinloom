package theme

import (
	"reflect"
	"strings"
	"testing"
)

func TestValidateHome(t *testing.T) {
	good := `[
		{"type": "hero", "props": {"title": {"zh-CN": "欢迎", "en": "Welcome"}, "background": "https://cdn.example.com/bg.webp", "primaryAction": "/skinlib"}},
		{"type": "latest-textures", "props": {"limit": 8, "category": "skin"}},
		{"type": "markdown", "props": {"content": "**hello**"}},
		{"type": "buttons", "props": {"items": [{"label": "QQ 群", "href": "https://qm.qq.com/x"}, {"label": {"en": "Docs"}, "href": "/about"}]}},
		{"type": "image", "props": {"src": "/static/banner.png", "alt": "banner"}},
		{"type": "stats"},
		{"type": "launcher-guide", "props": {}}
	]`
	if err := ValidateHome(decode(t, good)); err != nil {
		t.Fatal(err)
	}
	if err := ValidateHome(nil); err != nil {
		t.Fatal("null is the theme's own home page")
	}
	cases := map[string]string{
		"unknown type":       `[{"type": "carousel"}]`,
		"no type":            `[{"props": {}}]`,
		"unknown property":   `[{"type": "hero", "props": {"colour": "red"}}]`,
		"unknown field":      `[{"type": "hero", "style": "x"}]`,
		"script link":        `[{"type": "hero", "props": {"primaryAction": "javascript:alert(1)"}}]`,
		"other site":         `[{"type": "image", "props": {"src": "//evil.example/x.png"}}]`,
		"limit too large":    `[{"type": "popular-textures", "props": {"limit": 1000}}]`,
		"limit fraction":     `[{"type": "popular-textures", "props": {"limit": 8.5}}]`,
		"bad choice":         `[{"type": "latest-textures", "props": {"category": "steve"}}]`,
		"title not text":     `[{"type": "stats", "props": {"title": 3}}]`,
		"bad locale":         `[{"type": "stats", "props": {"title": {"<b>": "x"}}}]`,
		"button without url": `[{"type": "buttons", "props": {"items": [{"label": "x"}]}}]`,
		"not a list":         `{"type": "hero"}`,
		"too long":           `[{"type": "stats", "props": {"title": "` + strings.Repeat("长", 201) + `"}}]`,
	}
	for name, v := range cases {
		if err := ValidateHome(decode(t, v)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	many := "[" + strings.Repeat(`{"type": "stats"},`, MaxBlocks) + `{"type": "stats"}]`
	if err := ValidateHome(decode(t, many)); err == nil {
		t.Errorf("%d blocks accepted", MaxBlocks+1)
	}
}

func TestLocalizeBlocks(t *testing.T) {
	blocks := []Block{
		{Type: "hero", Props: map[string]any{"title": map[string]any{"zh-CN": "欢迎", "en": "Welcome"}, "primaryAction": "/skinlib"}},
		{Type: "markdown", Props: map[string]any{"content": map[string]any{"zh-CN": "**你好**", "en": "**hi**"}}},
		{Type: "buttons", Props: map[string]any{"items": []any{map[string]any{"label": map[string]any{"en": "Docs"}, "href": "/about"}}}},
		{Type: "stats"},
	}
	md := func(s string) string { return "<p>" + s + "</p>" }
	got := LocalizeBlocks(blocks, "en", md)
	want := []Block{
		{Type: "hero", Props: map[string]any{"title": "Welcome", "primaryAction": "/skinlib"}},
		{Type: "markdown", Props: map[string]any{"contentHtml": "<p>**hi**</p>"}},
		{Type: "buttons", Props: map[string]any{"items": []map[string]string{{"label": "Docs", "href": "/about"}}}},
		{Type: "stats"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v", got)
	}
	if zh := LocalizeBlocks(blocks, "zh-CN", md); zh[0].Props["title"] != "欢迎" {
		t.Fatalf("zh-CN title %v", zh[0].Props["title"])
	}
	if blocks[0].Props["title"].(map[string]any)["en"] != "Welcome" {
		t.Fatal("LocalizeBlocks must not change its input")
	}
}
