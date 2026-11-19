package theme

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type contrastPair struct {
	fg, bg string
	min    float64
}

// contrastPairs are the readability requirements from web/design.md §2.4.
var contrastPairs = []contrastPair{
	{"foreground", "background", 7.0},
	{"muted-foreground", "card", 4.5},
	{"muted-foreground", "muted", 4.5},
	{"faint-foreground", "card", 4.5},
	{"faint-foreground", "background", 4.5},
	{"primary-foreground", "primary", 4.5},
	{"primary", "card", 4.5},
	{"highlight-foreground", "highlight", 4.5},
	{"success-foreground", "success", 4.5},
	{"destructive-foreground", "destructive", 4.5},
	{"ring", "background", 3.0},
}

// Check validates a theme package in files against the loading rules and returns any
// contrast or completeness warnings (web/design.md §2.4).
func Check(files fs.FS, base *Registry) (*Theme, []string, error) {
	t, err := load(files, "local")
	if err != nil {
		return nil, nil, err
	}
	if IsBuiltinID(t.ID) || (base != nil && base.IsBuiltin(t.ID)) {
		return nil, nil, fmt.Errorf("cannot override built-in theme %q", t.ID)
	}
	if t.Extends != "" {
		if base == nil {
			return nil, nil, fmt.Errorf("cannot resolve extends %q without a base theme registry", t.Extends)
		}
		if err := base.resolveExtends(t); err != nil {
			return nil, nil, err
		}
	} else {
		defaultWarning(t.Theme.Tokens.Light)
		defaultWarning(t.Theme.Tokens.Dark)
	}
	if t.Theme.ColorScheme == "" {
		t.Theme.ColorScheme = "both"
	}

	var warnings []string
	checkMode := func(mode string, tokens map[string]string) {
		for _, p := range contrastPairs {
			fg, okFg := tokens[p.fg]
			bg, okBg := tokens[p.bg]
			if !okFg {
				warnings = append(warnings, fmt.Sprintf("%s: missing token %q", mode, p.fg))
				continue
			}
			if !okBg {
				warnings = append(warnings, fmt.Sprintf("%s: missing token %q", mode, p.bg))
				continue
			}
			ratio, ok := ContrastRatio(fg, bg)
			if !ok {
				continue
			}
			if ratio < p.min {
				warnings = append(warnings, fmt.Sprintf("%s: %s on %s is %.2f:1 (want >= %.1f:1)", mode, p.fg, p.bg, ratio, p.min))
			}
		}
	}
	if t.Theme.ColorScheme != "dark" {
		checkMode("light", t.Theme.Tokens.Light)
	}
	if t.Theme.ColorScheme != "light" {
		checkMode("dark", t.Theme.Tokens.Dark)
	}
	return t, warnings, nil
}

var (
	oklchPat = regexp.MustCompile(`^oklch\(\s*([\d.]+%?)\s+([\d.]+)\s+([\d.]+)\s*\)$`)
	hexPat   = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)
)

// ContrastRatio returns the WCAG 2.1 contrast ratio between two opaque oklch() or #hex colors.
func ContrastRatio(fg, bg string) (float64, bool) {
	a, okA := linearRGB(fg)
	b, okB := linearRGB(bg)
	if !okA || !okB {
		return 0, false
	}
	lum := func(c [3]float64) float64 { return 0.2126*c[0] + 0.7152*c[1] + 0.0722*c[2] }
	la, lb := lum(a), lum(b)
	hi, lo := math.Max(la, lb), math.Min(la, lb)
	return (hi + 0.05) / (lo + 0.05), true
}

func linearRGB(value string) ([3]float64, bool) {
	v := strings.TrimSpace(value)
	if m := oklchPat.FindStringSubmatch(v); len(m) == 4 {
		lStr := m[1]
		pct := strings.HasSuffix(lStr, "%")
		L, err1 := strconv.ParseFloat(strings.TrimSuffix(lStr, "%"), 64)
		if pct {
			L /= 100
		}
		C, err2 := strconv.ParseFloat(m[2], 64)
		h, err3 := strconv.ParseFloat(m[3], 64)
		if err1 != nil || err2 != nil || err3 != nil {
			return [3]float64{}, false
		}
		rad := h * math.Pi / 180
		a := C * math.Cos(rad)
		b := C * math.Sin(rad)
		l := math.Pow(L+0.3963377774*a+0.2158037573*b, 3)
		mm := math.Pow(L-0.1055613458*a-0.0638541728*b, 3)
		s := math.Pow(L-0.0894841775*a-1.2914855480*b, 3)
		clamp := func(x float64) float64 { return math.Min(1, math.Max(0, x)) }
		return [3]float64{
			clamp(4.0767416621*l - 3.3077115913*mm + 0.2309699292*s),
			clamp(-1.2684380046*l + 2.6097574011*mm - 0.3413193965*s),
			clamp(-0.0041960863*l - 0.7034186147*mm + 1.7076147010*s),
		}, true
	}
	if m := hexPat.FindStringSubmatch(v); len(m) == 2 {
		hex := m[1]
		if len(hex) == 3 {
			hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
		}
		n, err := strconv.ParseUint(hex, 16, 32)
		if err != nil {
			return [3]float64{}, false
		}
		toLinear := func(c uint8) float64 {
			s := float64(c) / 255.0
			if s <= 0.04045 {
				return s / 12.92
			}
			return math.Pow((s+0.055)/1.055, 2.4)
		}
		return [3]float64{
			toLinear(uint8(n >> 16)),
			toLinear(uint8(n >> 8)),
			toLinear(uint8(n)),
		}, true
	}
	return [3]float64{}, false
}

// Scaffold creates a new local theme skeleton under <themesDir>/<id>/.
func Scaffold(themesDir, id, extends string) (string, error) {
	if !idPattern.MatchString(id) {
		return "", fmt.Errorf("invalid theme id %q: use 1 to 32 lowercase letters, digits or dashes", id)
	}
	if IsBuiltinID(id) {
		return "", fmt.Errorf("theme id %q is reserved by a built-in theme", id)
	}
	target := filepath.Join(themesDir, id)
	if _, err := os.Stat(target); err == nil {
		return "", fmt.Errorf("directory %s already exists", target)
	}
	if err := os.MkdirAll(filepath.Join(target, "assets"), 0o755); err != nil {
		return "", err
	}

	manifest := map[string]any{
		"$schema":     "https://skinloom.cn/schema/plugin.v1.json",
		"id":          id,
		"kind":        "theme",
		"version":     "0.1.0",
		"name":        map[string]string{"zh-CN": id, "en": id},
		"description": map[string]string{"zh-CN": "自定义主题", "en": "A custom SkinLoom theme"},
		"license":     "MIT",
		"apiVersion":  1,
	}
	if extends != "" {
		manifest["extends"] = extends
	}
	manifest["theme"] = map[string]any{
		"colorScheme": "both",
		"tokens": map[string]any{
			"light": map[string]string{
				"primary":            "oklch(0.47 0.13 262)",
				"primary-foreground": "oklch(0.985 0.002 262)",
			},
			"dark": map[string]string{
				"primary":            "oklch(0.74 0.11 262)",
				"primary-foreground": "oklch(0.18 0.03 262)",
			},
		},
		"css": "theme.css",
		"layout": map[string]string{
			"auth":    "card",
			"skinlib": "comfortable",
			"user":    "sidebar",
			"texture": "split",
		},
		"settings": map[string]any{
			"accent": map[string]any{
				"type":  "color",
				"label": map[string]string{"zh-CN": "主色", "en": "Primary color"},
				"token": "primary",
			},
		},
	}

	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", err
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(filepath.Join(target, "plugin.json"), raw, 0o644); err != nil {
		return "", err
	}

	css := "/* " + id + ": custom rules on top of token variables.\n" +
		"   Unlayered rules win over host utilities without !important (ADR 0043). */\n"
	if err := os.WriteFile(filepath.Join(target, "theme.css"), []byte(css), 0o644); err != nil {
		return "", err
	}

	readme := "# " + id + "\n\n" +
		"Local SkinLoom theme package.\n\n" +
		"- Edit `plugin.json` and `theme.css`.\n" +
		"- Place optional preview image at `screenshot.webp` (16:10) and reference `" + `"screenshot": "screenshot.webp"` + "` in `plugin.json`.\n" +
		"- Validate with `skinloom theme check " + target + "`.\n" +
		"- Reload in a running server with `skinloom theme reload` or the Rescan button in Admin -> Appearance.\n"
	if err := os.WriteFile(filepath.Join(target, "README.md"), []byte(readme), 0o644); err != nil {
		return "", err
	}
	return target, nil
}
