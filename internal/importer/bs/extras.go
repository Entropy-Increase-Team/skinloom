package bs

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Entropy-Increase-Team/skinloom/internal/modules/oauth"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/clip"
)

// Text overrides, OAuth applications and the Yggdrasil signing key: the parts of a BS
// site beyond its accounts and textures (ADR 0036).

// textTerms maps the BS translations an administrator rewords to give the site its own
// words (what the skin library or the score is called) to the SkinLoom texts that show
// the same term on their own. SkinLoom's texts are written anew, so the rest of
// language_lines has no counterpart: those lines are listed in the report, to be
// reworded on /admin/translations. A test checks every target exists in both message
// catalogs and has no placeholders.
var textTerms = map[string][]string{
	"general.index":                {"nav.home"},
	"general.skinlib":              {"nav.skinlib", "skinlib.title"},
	"front-end.general.skinlib":    {"nav.skinlib", "skinlib.title"},
	"general.user-center":          {"auth.dashboard", "dashboard.title"},
	"general.login":                {"nav.login", "auth.signIn"},
	"general.my-closet":            {"closet.title", "dashboard.links.closet"},
	"general.admin-panel":          {"admin.title"},
	"general.user.score":           {"dashboard.score", "admin.score", "admin.fields.score", "adminSettings.sections.score.title"},
	"front-end.general.user.score": {"dashboard.score", "admin.score", "admin.fields.score", "adminSettings.sections.score.title"},
	"general.skin":                 {"player.skin", "adminPlayers.skin"},
	"front-end.general.skin":       {"player.skin", "adminPlayers.skin"},
	"general.cape":                 {"player.cape", "adminPlayers.cape", "texture.types.cape"},
	"front-end.general.cape":       {"player.cape", "adminPlayers.cape", "texture.types.cape"},
}

// planOverrides turns language_lines into i18n.overrides ({locale: {message key: text}}).
func planOverrides(d *sourceData, locales []string, r *Report) map[string]map[string]string {
	out := map[string]map[string]string{}
	from := map[string]string{} // locale + key → the BS line that set it
	for _, l := range d.Lines {
		name := l.Group + "." + l.Key
		var texts map[string]string
		if err := json.Unmarshal([]byte(l.Text), &texts); err != nil {
			r.add(Dropped, "language_lines", l.ID, "%s: the text is not a JSON object of languages", name)
			continue
		}
		targets, ok := textTerms[name]
		if !ok {
			r.add(Dropped, "language_lines", l.ID, "%s = %s: SkinLoom's texts are new and this one has no counterpart; reword it on the translations page if it is still wanted", name, short(firstText(texts)))
			continue
		}
		for _, bsLoc := range sortedKeys(texts) {
			loc := localeOf(bsLoc)
			text := strings.TrimSpace(cleanText(texts[bsLoc]))
			switch {
			case text == "":
				continue
			case !containsLocale(locales, loc):
				r.add(Dropped, "language_lines", l.ID, "%s (%s): SkinLoom has no %s interface yet", name, bsLoc, bsLoc)
				continue
			// Braces would be placeholders and angle brackets tags (ICU messages): a
			// text that uses them could not be shown, and would fail i18n.overrides.
			case strings.ContainsAny(text, "{}<>"), utf8.RuneCountInString(text) > 200:
				r.add(Dropped, "language_lines", l.ID, "%s (%s) = %s: a term must be short plain text", name, bsLoc, short(text))
				continue
			}
			for _, key := range targets {
				if prev, taken := from[loc+key]; taken {
					if out[loc][key] != text {
						r.add(Warning, "language_lines", l.ID, "%s (%s) = %s: %s already set %s to %s", name, bsLoc, short(text), prev, key, short(out[loc][key]))
					}
					continue
				}
				if out[loc] == nil {
					out[loc] = map[string]string{}
				}
				out[loc][key] = text
				from[loc+key] = name
			}
			r.add(Fixed, "language_lines", l.ID, "%s (%s) = %s: used for %s; longer texts that mention the term keep the default wording", name, bsLoc, short(text), strings.Join(targets, ", "))
		}
	}
	return out
}

func firstText(m map[string]string) string {
	if v := m[bsLocale]; v != "" {
		return v
	}
	for _, k := range sortedKeys(m) {
		if m[k] != "" {
			return m[k]
		}
	}
	return ""
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// tClient is an OAuth application (oauth_clients). Its ID is the BS client_id, so the
// applications keep working with their configuration.
type tClient struct {
	ID, UserID   int64
	Name         string
	SecretHash   *string
	RedirectURIs []string
	CreatedAt    time.Time
}

// planClients brings over BS's Passport clients: the client_id stays, a plaintext
// secret is hashed (02 §2.6), tokens are not imported (users authorize again).
func planClients(d *sourceData, userExists func(int64) bool, at func(wall) time.Time, r *Report) []tClient {
	var out []tClient
	for _, c := range d.Clients {
		switch {
		case c.Revoked:
			r.add(Dropped, "oauth_clients", c.ID, "revoked")
			continue
		case c.Personal:
			r.add(Dropped, "oauth_clients", c.ID, "Passport's personal access client: SkinLoom makes personal tokens without one")
			continue
		case c.Password:
			r.add(Dropped, "oauth_clients", c.ID, "a password grant client: SkinLoom does not offer the password grant")
			continue
		case !userExists(c.UserID):
			r.add(Dropped, "oauth_clients", c.ID, "owner %d no longer exists", c.UserID)
			continue
		}
		name := clip.Runes(strings.TrimSpace(cleanText(c.Name)), 64)
		if name == "" {
			name = fmt.Sprintf("Application %d", c.ID)
			r.add(Fixed, "oauth_clients", c.ID, "no name: named %q", name)
		}
		var redirects []string
		for u := range strings.SplitSeq(c.Redirect, ",") {
			if u = strings.TrimSpace(u); u == "" {
				continue
			}
			if _, err := oauth.CheckRedirects([]string{u}); err != nil {
				r.add(Warning, "oauth_clients", c.ID, "redirect URI %s left out: %v", short(u), err)
				continue
			}
			if !slices.Contains(redirects, u) {
				redirects = append(redirects, u)
			}
		}
		if len(redirects) > oauth.MaxRedirectURIs {
			r.add(Warning, "oauth_clients", c.ID, "%d redirect URIs: the first %d kept", len(redirects), oauth.MaxRedirectURIs)
			redirects = redirects[:oauth.MaxRedirectURIs]
		}
		if len(redirects) == 0 {
			r.add(Dropped, "oauth_clients", c.ID, "%q has no usable redirect URI", name)
			continue
		}
		t := tClient{ID: c.ID, UserID: c.UserID, Name: name, RedirectURIs: redirects, CreatedAt: at(c.CreatedAt)}
		switch s := c.Secret; {
		case s == "":
			// A public client: it already used PKCE, which SkinLoom requires of it.
		case strings.HasPrefix(s, "$2y$") || strings.HasPrefix(s, "$argon2"):
			// Passport::hashClientSecrets(): the secret cannot be recovered. A random one
			// keeps the application confidential until its owner makes a new one.
			h := oauth.HashSecret(randomHex())
			t.SecretHash = &h
			r.add(Warning, "oauth_clients", c.ID, "%q: BS stored only a hash of the secret, so it cannot be kept; the owner makes a new one in the user center", name)
		default:
			h := oauth.HashSecret(s)
			t.SecretHash = &h
		}
		out = append(out, t)
	}
	return out
}

func randomHex() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b) // never fails (Go 1.24+)
	return hex.EncodeToString(b)
}

// signingKeyName is where the Yggdrasil service keeps its signing key (site_secrets).
const signingKeyName = "yggdrasil.signing_key"

// planSigningKey brings the Yggdrasil plugin's private key over as PKCS #8 PEM, so game
// servers that cached the site's public key keep verifying its signatures.
func planSigningKey(d *sourceData, r *Report) string {
	raw := strings.TrimSpace(d.Options["ygg_private_key"])
	if raw == "" {
		return ""
	}
	fail := func(why string) string {
		r.add(Warning, "options", 0, "ygg_private_key: %s; SkinLoom makes a new key, and game servers using authlib-injector must restart once after the switch", why)
		return ""
	}
	block, _ := pem.Decode([]byte(raw))
	if block == nil {
		return fail("not a PEM key")
	}
	var key any
	var err error
	switch block.Type {
	case "RSA PRIVATE KEY":
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
	case "PRIVATE KEY":
		key, err = x509.ParsePKCS8PrivateKey(block.Bytes)
	case "ENCRYPTED PRIVATE KEY":
		return fail("the key is encrypted with a passphrase")
	default:
		return fail("a " + block.Type + " block, not a private key")
	}
	if err != nil {
		return fail(err.Error())
	}
	rk, ok := key.(*rsa.PrivateKey)
	if !ok {
		return fail("not an RSA key")
	}
	if err := rk.Validate(); err != nil {
		return fail(err.Error())
	}
	if bits := rk.N.BitLen(); bits < 2048 {
		return fail(fmt.Sprintf("%d bits is too weak", bits))
	}
	der, err := x509.MarshalPKCS8PrivateKey(rk)
	if err != nil {
		return fail(err.Error())
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}
