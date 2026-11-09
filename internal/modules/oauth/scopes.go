package oauth

import (
	"slices"
	"strings"
)

// Scope is a permission an application or a personal access token can be given,
// named as in Blessing Skin (docs/design 02 §2.6). X.ReadWrite includes X.Read.
type Scope struct {
	Name  string `json:"name"`
	Admin bool   `json:"admin" doc:"Only administrators can grant it; others' grants leave it out"`
}

// Scopes are the scopes this site knows, in display order.
var Scopes = []Scope{
	{Name: "User.Read"},
	{Name: "Notification.Read"},
	{Name: "Player.Read"},
	{Name: "Player.ReadWrite"},
	{Name: "Closet.Read"},
	{Name: "Closet.ReadWrite"},
	{Name: "Notification.ReadWrite", Admin: true},
	{Name: "UsersManagement.Read", Admin: true},
	{Name: "UsersManagement.ReadWrite", Admin: true},
	{Name: "PlayersManagement.Read", Admin: true},
	{Name: "PlayersManagement.ReadWrite", Admin: true},
	{Name: "ClosetManagement.Read", Admin: true},
	{Name: "ClosetManagement.ReadWrite", Admin: true},
	{Name: "ReportsManagement.Read", Admin: true},
	{Name: "ReportsManagement.ReadWrite", Admin: true},
}

// DefaultScope is granted when a request names none (as Blessing Skin did).
const DefaultScope = "User.Read"

// aliases are other spellings applications may ask for: Blessing Skin registered the
// closet write scope as "Closet.ReadWrtie" while its routes required
// "Closet.ReadWrite", so applications could never get it (03 C13).
var aliases = map[string]string{"Closet.ReadWrtie": "Closet.ReadWrite"}

func lookupScope(name string) (Scope, bool) {
	if a, ok := aliases[name]; ok {
		name = a
	}
	for _, s := range Scopes {
		if s.Name == name {
			return s, true
		}
	}
	return Scope{}, false
}

// ParseScopes reads a space-separated scope parameter (commas are accepted too) into
// known scope names, in display order without duplicates. The second result is the
// first unknown name, if any.
func ParseScopes(raw string) ([]string, string) {
	fields := strings.FieldsFunc(raw, func(r rune) bool { return r == ' ' || r == ',' || r == '+' })
	if len(fields) == 0 {
		fields = []string{DefaultScope}
	}
	seen := map[string]bool{}
	for _, f := range fields {
		s, ok := lookupScope(f)
		if !ok {
			return nil, f
		}
		seen[s.Name] = true
	}
	return ordered(seen), ""
}

func ordered(set map[string]bool) []string {
	out := []string{}
	for _, s := range Scopes {
		if set[s.Name] {
			out = append(out, s.Name)
		}
	}
	return out
}

// allowedFor drops the scopes a user cannot grant (administration for non-admins).
func allowedFor(scopes []string, admin bool) []string {
	out := []string{}
	for _, name := range scopes {
		if s, ok := lookupScope(name); ok && (admin || !s.Admin) {
			out = append(out, s.Name)
		}
	}
	return out
}

// covers reports whether granted includes every scope of asked.
func covers(granted, asked []string) bool {
	for _, a := range asked {
		if !HasScope(granted, a) {
			return false
		}
	}
	return true
}

// HasScope reports whether scopes allow need; a ReadWrite scope also allows its Read.
func HasScope(scopes []string, need string) bool {
	if slices.Contains(scopes, need) {
		return true
	}
	if base, ok := strings.CutSuffix(need, ".Read"); ok {
		return slices.Contains(scopes, base+".ReadWrite")
	}
	return false
}
