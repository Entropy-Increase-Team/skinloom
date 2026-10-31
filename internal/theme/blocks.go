package theme

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/Entropy-Increase-Team/skinloom/internal/settings"
)

// PropType is the kind of value a block property holds.
type PropType string

const (
	// PropText is a short line: a string, or one per language {"zh-CN": …, "en": …}.
	PropText PropType = "text"
	// PropMarkdown is longer text in Markdown, plain or per language. Pages receive it
	// rendered, as <name>Html.
	PropMarkdown PropType = "markdown"
	// PropLink is a path on this site (/skinlib) or an http(s) URL.
	PropLink PropType = "link"
	// PropImage is an image URL or a path on this site.
	PropImage PropType = "image"
	// PropNumber is an integer from Min to Max.
	PropNumber PropType = "number"
	// PropChoice is one of Options.
	PropChoice PropType = "choice"
	// PropButtons is a list of {"label": text, "href": link}.
	PropButtons PropType = "buttons"
)

// Prop describes one property of a block type.
type Prop struct {
	Name    string   `json:"name"`
	Type    PropType `json:"type" enum:"text,markdown,link,image,number,choice,buttons"`
	Min     int      `json:"min,omitempty"`
	Max     int      `json:"max,omitempty"`
	Options []string `json:"options,omitempty"`
}

// BlockType is a kind of block pages are composed of (docs/design/05 §5.4).
type BlockType struct {
	Type  string `json:"type"`
	Props []Prop `json:"props"`
}

// Limits of a page composition.
const (
	MaxBlocks      = 30
	maxText        = 200
	maxMarkdown    = 10000
	maxLink        = 500
	maxButtons     = 6
	textureListMin = 4
	textureListMax = 24
)

// BlockTypes are the built-in blocks, rendered on the server by the frontend
// (web/apps/web/src/components/blocks). Plugins declare theirs in phase 2.
var BlockTypes = []BlockType{
	{Type: "hero", Props: []Prop{
		{Name: "title", Type: PropText}, {Name: "subtitle", Type: PropText}, {Name: "background", Type: PropImage},
		{Name: "primaryAction", Type: PropLink}, {Name: "secondaryAction", Type: PropLink},
	}},
	{Type: "announcement", Props: []Prop{}},
	{Type: "features", Props: []Prop{}},
	{Type: "stats", Props: []Prop{{Name: "title", Type: PropText}}},
	{Type: "latest-textures", Props: textureListProps()},
	{Type: "popular-textures", Props: textureListProps()},
	{Type: "markdown", Props: []Prop{{Name: "content", Type: PropMarkdown}}},
	{Type: "image", Props: []Prop{{Name: "src", Type: PropImage}, {Name: "alt", Type: PropText}, {Name: "href", Type: PropLink}}},
	{Type: "buttons", Props: []Prop{{Name: "items", Type: PropButtons}}},
	{Type: "launcher-guide", Props: []Prop{}},
}

func textureListProps() []Prop {
	return []Prop{
		{Name: "title", Type: PropText},
		{Name: "limit", Type: PropNumber, Min: textureListMin, Max: textureListMax},
		{Name: "category", Type: PropChoice, Options: []string{"skin", "cape"}},
	}
}

func blockType(name string) (BlockType, bool) {
	i := slices.IndexFunc(BlockTypes, func(b BlockType) bool { return b.Type == name })
	if i < 0 {
		return BlockType{}, false
	}
	return BlockTypes[i], true
}

// ValidateHome is the validator of the home.blocks setting: null, or blocks that
// ValidateBlocks accepts.
func ValidateHome(v any) error {
	if v == nil {
		return nil
	}
	list, ok := v.([]any)
	if !ok {
		return errors.New("want a list of blocks or null")
	}
	blocks := make([]Block, 0, len(list))
	for i, item := range list { // messages count from 1, as the administrator sees the list
		m, ok := item.(map[string]any)
		if !ok {
			return fmt.Errorf("block %d: want an object", i+1)
		}
		for k := range m {
			if k != "type" && k != "props" {
				return fmt.Errorf("block %d: unknown field %q", i+1, k)
			}
		}
		b := Block{}
		b.Type, _ = m["type"].(string)
		if p, ok := m["props"]; ok && p != nil {
			if b.Props, ok = p.(map[string]any); !ok {
				return fmt.Errorf("block %d: props must be an object", i+1)
			}
		}
		blocks = append(blocks, b)
	}
	return ValidateBlocks(blocks)
}

// ValidateBlocks checks a page composition against BlockTypes: known types, known
// properties of the right kind, and the size limits.
func ValidateBlocks(blocks []Block) error {
	if len(blocks) > MaxBlocks {
		return fmt.Errorf("at most %d blocks", MaxBlocks)
	}
	for i, b := range blocks {
		bt, ok := blockType(b.Type)
		if !ok {
			return fmt.Errorf("block %d: unknown type %q", i+1, b.Type)
		}
		for name, v := range b.Props {
			j := slices.IndexFunc(bt.Props, func(p Prop) bool { return p.Name == name })
			if j < 0 {
				return fmt.Errorf("block %d (%s): unknown property %q", i+1, b.Type, name)
			}
			if err := checkProp(bt.Props[j], v); err != nil {
				return fmt.Errorf("block %d (%s): %s %w", i+1, b.Type, name, err)
			}
		}
	}
	return nil
}

var localeKey = regexp.MustCompile(`^[a-z]{2,3}(-[A-Za-z0-9]{2,8})*$`)

func checkProp(p Prop, v any) error {
	switch p.Type {
	case PropText:
		return checkLocalized(v, maxText)
	case PropMarkdown:
		return checkLocalized(v, maxMarkdown)
	case PropLink, PropImage:
		s, ok := v.(string)
		if !ok {
			return errors.New("must be a string")
		}
		return checkLink(s)
	case PropNumber:
		n, ok := v.(float64)
		if !ok || n != float64(int(n)) || int(n) < p.Min || int(n) > p.Max {
			return fmt.Errorf("must be a whole number from %d to %d", p.Min, p.Max)
		}
	case PropChoice:
		if s, ok := v.(string); !ok || !slices.Contains(p.Options, s) {
			return fmt.Errorf("must be one of %v", p.Options)
		}
	case PropButtons:
		list, ok := v.([]any)
		if !ok || len(list) > maxButtons {
			return fmt.Errorf("must be a list of at most %d buttons", maxButtons)
		}
		for k, item := range list {
			m, _ := item.(map[string]any)
			href, _ := m["href"].(string)
			if len(m) != 2 || m["label"] == nil || href == "" {
				return fmt.Errorf("button %d: want {\"label\", \"href\"}", k+1)
			}
			if err := checkLocalized(m["label"], maxText); err != nil {
				return fmt.Errorf("button %d label %w", k+1, err)
			}
			if err := checkLink(href); err != nil {
				return fmt.Errorf("button %d href %w", k+1, err)
			}
		}
	}
	return nil
}

// checkLocalized accepts a string or {locale: string}, each at most limit characters.
func checkLocalized(v any, limit int) error {
	check := func(s string) error {
		if utf8.RuneCountInString(s) > limit {
			return fmt.Errorf("is longer than %d characters", limit)
		}
		return nil
	}
	switch x := v.(type) {
	case string:
		return check(x)
	case map[string]any:
		for k, s := range x {
			str, ok := s.(string)
			if !ok || !localeKey.MatchString(k) {
				return errors.New(`must be a string or {"zh-CN": "...", "en": "..."}`)
			}
			if err := check(str); err != nil {
				return err
			}
		}
		return nil
	}
	return errors.New(`must be a string or {"zh-CN": "...", "en": "..."}`)
}

// checkLink accepts a path on this site or an http(s) URL, the same as the frontend's
// safeHref and safeImageUrl, so a saved value is never dropped on render.
func checkLink(s string) error {
	if len(s) > maxLink {
		return fmt.Errorf("is longer than %d characters", maxLink)
	}
	if strings.HasPrefix(s, "/") && !strings.HasPrefix(s, "//") && !strings.ContainsAny(s, "\\ ") {
		return nil
	}
	if u, err := url.Parse(s); err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" {
		return nil
	}
	return errors.New("must be a path on this site (/…) or an http(s) URL")
}

// LocalizeBlocks prepares blocks for one request: text properties become the text of
// locale, Markdown ones are rendered by markdown into <name>Html. Pages then only read
// strings; the admin panel edits the stored values (home.blocks) instead.
func LocalizeBlocks(blocks []Block, locale string, markdown func(string) string) []Block {
	out := make([]Block, len(blocks))
	for i, b := range blocks {
		out[i] = Block{Type: b.Type}
		bt, ok := blockType(b.Type)
		if !ok || len(b.Props) == 0 {
			continue
		}
		props := make(map[string]any, len(b.Props))
		for _, p := range bt.Props {
			v, ok := b.Props[p.Name]
			if !ok {
				continue
			}
			switch p.Type {
			case PropText:
				props[p.Name] = pick(v, locale)
			case PropMarkdown:
				props[p.Name+"Html"] = markdown(pick(v, locale))
			case PropButtons:
				list, _ := v.([]any)
				items := make([]map[string]string, 0, len(list))
				for _, item := range list {
					m, _ := item.(map[string]any)
					href, _ := m["href"].(string)
					items = append(items, map[string]string{"label": pick(m["label"], locale), "href": href})
				}
				props[p.Name] = items
			default:
				props[p.Name] = v
			}
		}
		out[i].Props = props
	}
	return out
}

func pick(v any, locale string) string {
	switch x := v.(type) {
	case string:
		return x
	case map[string]any:
		m := make(map[string]string, len(x))
		for k, s := range x {
			m[k], _ = s.(string)
		}
		return settings.PickLocalized(m, locale)
	}
	return ""
}
