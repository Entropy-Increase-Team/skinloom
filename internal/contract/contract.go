// Package contract turns responses of the game-facing APIs (CustomSkinLoader, USM,
// Legacy, texture files, Yggdrasil) into stable text: the status, the headers that are
// part of the contract, and the body with what changes from run to run (times, tokens,
// signatures) replaced by markers. Golden-file tests compare this text with a reviewed
// copy, and skinloom import compare puts a Blessing Skin site next to its import (02 §6
// item 1).
package contract

import (
	"bytes"
	"crypto"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"image/png"
	"net/http"
	"strings"
)

// Response is what Normalize reads.
type Response struct {
	Status int
	Header http.Header
	Body   []byte
}

// Options holds what normalizing needs to know about the site.
type Options struct {
	// PublicKey checks Yggdrasil signatures; nil marks every signature unchecked.
	PublicKey *rsa.PublicKey
}

// Headers are the response headers clients depend on.
var Headers = []string{"Content-Type", "Cache-Control", "ETag", "Last-Modified", "Location", "X-Authlib-Injector-API-Location"}

// Normalize renders r as text.
func Normalize(r Response, o Options) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d %s\n", r.Status, http.StatusText(r.Status))
	for _, h := range Headers {
		v := r.Header.Get(h)
		switch {
		case v == "":
			continue
		case h == "Last-Modified":
			v = "<date>"
		case h == "Content-Type":
			v = strings.ToLower(strings.ReplaceAll(v, " ", ""))
		}
		fmt.Fprintf(&b, "%s: %s\n", h, v)
	}
	if body := Body(r.Body, o); body != "" {
		b.WriteString(body)
		b.WriteByte('\n')
	}
	return b.String()
}

// Body renders a body: JSON indented with sorted keys and markers, a PNG by its size
// and SHA-256, other text as it is (shortened).
func Body(body []byte, o Options) string {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return ""
	}
	if bytes.HasPrefix(body, []byte("\x89PNG")) {
		sum := sha256.Sum256(body)
		cfg, err := png.DecodeConfig(bytes.NewReader(body))
		if err != nil {
			return fmt.Sprintf("<PNG, unreadable: %v, sha256 %s>", err, hex.EncodeToString(sum[:]))
		}
		return fmt.Sprintf("<PNG %dx%d, sha256 %s>", cfg.Width, cfg.Height, hex.EncodeToString(sum[:]))
	}
	if trimmed[0] == '{' || trimmed[0] == '[' {
		d := json.NewDecoder(bytes.NewReader(trimmed))
		d.UseNumber()
		var v any
		if err := d.Decode(&v); err == nil {
			var out bytes.Buffer
			enc := json.NewEncoder(&out)
			enc.SetEscapeHTML(false) // keep the <markers> readable
			enc.SetIndent("", "  ")
			_ = enc.Encode(walk(v, "", o))
			return strings.TrimSuffix(out.String(), "\n")
		}
	}
	s := string(trimmed)
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

// walk replaces what differs between runs. Maps marshal with sorted keys, so the
// order a site writes keys in does not matter.
func walk(v any, key string, o Options) any {
	switch t := v.(type) {
	case map[string]any:
		if name, _ := t["name"].(string); name != "" {
			if value, ok := t["value"].(string); ok {
				return property(t, name, value, o)
			}
		}
		out := make(map[string]any, len(t))
		for k, x := range t {
			out[k] = walk(x, k, o)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, x := range t {
			out[i] = walk(x, key, o)
		}
		return out
	case json.Number:
		switch key {
		case "timestamp", "last_update":
			return "<time>"
		}
		return t
	case string:
		switch key {
		case "accessToken":
			return "<token>"
		case "signaturePublickey":
			return describeKey(t)
		case "publicKey":
			return describeKey(t)
		case "implementationVersion":
			return "<version>"
		}
		return t
	}
	return v
}

// property renders a Yggdrasil profile property: a textures value decoded (it is
// base64 JSON with a timestamp), a signature checked.
func property(p map[string]any, name, value string, o Options) map[string]any {
	out := map[string]any{"name": name, "value": value}
	if name == "textures" {
		if raw, err := base64.StdEncoding.DecodeString(value); err == nil {
			d := json.NewDecoder(bytes.NewReader(raw))
			d.UseNumber()
			var v any
			if d.Decode(&v) == nil {
				out["value"] = map[string]any{"base64 JSON": walk(v, "", o)}
			}
		}
	}
	if sig, ok := p["signature"].(string); ok {
		out["signature"] = checkSignature(value, sig, o.PublicKey)
	}
	for k, x := range p {
		if _, done := out[k]; !done {
			out[k] = walk(x, k, o)
		}
	}
	return out
}

func checkSignature(value, sig string, key *rsa.PublicKey) string {
	if key == nil {
		return "<signature, not checked>"
	}
	raw, err := base64.StdEncoding.DecodeString(sig)
	if err != nil {
		return "<signature, not base64>"
	}
	h := sha1.Sum([]byte(value))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA1, h[:], raw); err != nil {
		return "<signature INVALID>"
	}
	return "<signature valid>"
}

// describeKey shows a public key (PEM or base64 DER) as its kind and size.
func describeKey(s string) string {
	k, err := ParsePublicKey(s)
	if err != nil {
		return "<public key, unreadable: " + err.Error() + ">"
	}
	return fmt.Sprintf("<RSA public key, %d bits>", k.N.BitLen())
}

// ParsePublicKey reads an RSA public key as Yggdrasil metadata gives it (PEM) or as
// /minecraftservices/publickeys does (base64 DER).
func ParsePublicKey(s string) (*rsa.PublicKey, error) {
	der := []byte(nil)
	if blk, _ := pem.Decode([]byte(s)); blk != nil {
		der = blk.Bytes
	} else if raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s)); err == nil {
		der = raw
	} else {
		return nil, fmt.Errorf("neither PEM nor base64")
	}
	k, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, err
	}
	rk, ok := k.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("not RSA")
	}
	return rk, nil
}
