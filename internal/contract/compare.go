package contract

import (
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

// YggdrasilPrefix is where both Blessing Skin's yggdrasil-api and SkinLoom serve the
// Yggdrasil API.
const YggdrasilPrefix = "/api/yggdrasil"

// Site is one side of a comparison.
type Site struct {
	Base string // https://skins.example.com, without a trailing slash
	key  *rsa.PublicKey
	home string // the site URL its metadata gives, replaced with <site> in bodies
}

// Result is one request asked of both sites.
type Result struct {
	Request  string
	Same     bool
	Old, New string // the normalized responses, when they differ
}

// KeyRequest is the first Result: whether both sites sign with the same Yggdrasil key.
// Each site's signatures are checked with its own key, so a key the import did not
// carry over shows only here; game servers would then need a restart to pick it up.
const KeyRequest = "Yggdrasil signing key (metadata signaturePublickey)"

// Compare asks a Blessing Skin site and SkinLoom the same game-API requests about
// the given players and reports where the answers differ (design 02 §6 item 1, for
// the real-site beta: run it against BS and a SkinLoom imported from it). The
// comparison is of what launchers and game servers read: status, content type, and
// the body with each site's own address, times and signatures masked. Error texts,
// cache times and validators differ on purpose (ADR 0037) and are left out.
func Compare(ctx context.Context, c *http.Client, old, neu *Site, names []string) ([]Result, error) {
	for _, s := range []*Site{old, neu} {
		s.Base = strings.TrimRight(s.Base, "/")
		if err := s.meta(ctx, c); err != nil {
			return nil, fmt.Errorf("%s: %w", s.Base, err)
		}
	}
	out := []Result{{Request: KeyRequest, Same: old.key.Equal(neu.key)}}
	if !out[0].Same {
		out[0].Old, out[0].New = fingerprint(old.key), fingerprint(neu.key)
	}
	ask := func(method, path, body string) (Result, []byte, error) {
		r := Result{Request: strings.TrimSpace(method + " " + path + " " + body)}
		a, aBody, err := old.fetch(ctx, c, method, path, body)
		if err != nil {
			return r, nil, err
		}
		b, _, err := neu.fetch(ctx, c, method, path, body)
		if err != nil {
			return r, nil, err
		}
		r.Same = a == b
		if !r.Same {
			r.Old, r.New = a, b
		}
		out = append(out, r)
		return r, aBody, nil
	}

	hashes := map[string]bool{}
	for _, name := range names {
		p := url.PathEscape(name)
		_, body, err := ask("GET", "/"+p+".json", "")
		if err != nil {
			return out, err
		}
		var csl struct {
			Skins map[string]*string `json:"skins"`
			Cape  *string            `json:"cape"`
		}
		if json.Unmarshal(body, &csl) == nil {
			for _, h := range csl.Skins {
				if h != nil {
					hashes[*h] = true
				}
			}
			if csl.Cape != nil {
				hashes[*csl.Cape] = true
			}
		}
		for _, path := range []string{"/csl/" + p + ".json", "/usm/" + p + ".json", "/skin/" + p + ".png", "/cape/" + p + ".png"} {
			if _, _, err := ask("GET", path, ""); err != nil {
				return out, err
			}
		}
		_, body, err = ask("GET", YggdrasilPrefix+"/api/users/profiles/minecraft/"+p, "")
		if err != nil {
			return out, err
		}
		var prof struct{ ID string }
		if json.Unmarshal(body, &prof) == nil && prof.ID != "" {
			for _, q := range []string{"", "?unsigned=false"} {
				if _, _, err := ask("GET", YggdrasilPrefix+"/sessionserver/session/minecraft/profile/"+url.PathEscape(prof.ID)+q, ""); err != nil {
					return out, err
				}
			}
		}
	}
	if len(names) > 0 {
		list, _ := json.Marshal(names)
		if _, _, err := ask("POST", YggdrasilPrefix+"/api/profiles/minecraft", string(list)); err != nil {
			return out, err
		}
	}
	for _, h := range slices.Sorted(maps.Keys(hashes)) {
		if _, _, err := ask("GET", "/textures/"+url.PathEscape(h), ""); err != nil {
			return out, err
		}
	}
	return out, nil
}

// meta reads the site's signing key and address from its Yggdrasil metadata.
func (s *Site) meta(ctx context.Context, c *http.Client) error {
	_, body, err := s.do(ctx, c, "GET", YggdrasilPrefix+"/", "")
	if err != nil {
		return err
	}
	var m struct {
		Meta struct {
			Links struct{ Homepage string } `json:"links"`
		} `json:"meta"`
		Key string `json:"signaturePublickey"`
	}
	if err := json.Unmarshal(body, &m); err != nil {
		return fmt.Errorf("no Yggdrasil metadata at %s/: %w", YggdrasilPrefix, err)
	}
	if s.key, err = ParsePublicKey(m.Key); err != nil {
		return fmt.Errorf("signaturePublickey: %w", err)
	}
	s.home = strings.TrimRight(cmpOr(m.Meta.Links.Homepage, s.Base), "/")
	return nil
}

func fingerprint(k *rsa.PublicKey) string {
	der, err := x509.MarshalPKIXPublicKey(k)
	if err != nil {
		return err.Error()
	}
	return fmt.Sprintf("RSA %d bits, SHA-256 %x\n", k.N.BitLen(), sha256.Sum256(der))
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// fetch returns the comparable form of a response, and its raw body.
func (s *Site) fetch(ctx context.Context, c *http.Client, method, path, body string) (string, []byte, error) {
	resp, raw, err := s.do(ctx, c, method, path, body)
	if err != nil {
		return "", nil, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d %s\n", resp.StatusCode, http.StatusText(resp.StatusCode))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if ct, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type")); err == nil {
			fmt.Fprintf(&b, "Content-Type: %s\n", ct)
		}
		cmpBody := raw
		if strings.HasSuffix(path, "/api/profiles/minecraft") {
			cmpBody = byName(raw) // the order of the answer is not part of the contract
		}
		b.WriteString(Body(cmpBody, Options{PublicKey: s.key}))
		// Each site's own address, also inside the decoded textures property.
		return strings.ReplaceAll(b.String(), s.home, "<site>"), raw, nil
	}
	return b.String(), raw, nil
}

func (s *Site) do(ctx context.Context, c *http.Client, method, path, body string) (*http.Response, []byte, error) {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.Base+path, rd)
	if err != nil {
		return nil, nil, err
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json, image/png")
	resp, err := c.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	return resp, raw, err
}

// byName sorts a profile list ([{id, name}]) by name.
func byName(raw []byte) []byte {
	var list []map[string]any
	if json.Unmarshal(raw, &list) != nil {
		return raw
	}
	slices.SortFunc(list, func(a, b map[string]any) int {
		x, _ := a["name"].(string)
		y, _ := b["name"].(string)
		return strings.Compare(strings.ToLower(x), strings.ToLower(y))
	})
	out, err := json.Marshal(list)
	if err != nil {
		return raw
	}
	return out
}
