package contract

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"image"
	"image/png"
	"net/http"
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	pemKey := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	if k, err := ParsePublicKey(pemKey); err != nil || !k.Equal(&key.PublicKey) {
		t.Fatalf("PEM key: %v", err)
	}
	if _, err := ParsePublicKey(base64.StdEncoding.EncodeToString(der)); err != nil {
		t.Fatalf("base64 DER key: %v", err)
	}

	value := base64.StdEncoding.EncodeToString([]byte(`{"timestamp":1700000000000,"profileName":"Steve","textures":{}}`))
	h := sha1.Sum([]byte(value))
	sig, _ := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA1, h[:])
	body, _ := json.Marshal(map[string]any{"id": "abc", "properties": []any{
		map[string]any{"name": "textures", "value": value, "signature": base64.StdEncoding.EncodeToString(sig)},
		map[string]any{"name": "uploadableTextures", "value": "skin,cape", "signature": base64.StdEncoding.EncodeToString(sig)},
	}, "accessToken": "secret", "signaturePublickey": pemKey})
	hdr := http.Header{"Last-Modified": {"Mon, 02 Jan 2006 15:04:05 GMT"}, "Content-Type": {"application/json; charset=utf-8"}, "Set-Cookie": {"x=1"}}
	got := Normalize(Response{Status: 200, Header: hdr, Body: body}, Options{PublicKey: &key.PublicKey})
	for _, want := range []string{
		"200 OK\nContent-Type: application/json;charset=utf-8\nLast-Modified: <date>\n",
		`"accessToken": "<token>"`, `"signaturePublickey": "<RSA public key, 1024 bits>"`,
		`"profileName": "Steve"`, `"timestamp": "<time>"`, `"signature": "<signature valid>"`,
		`"signature": "<signature INVALID>"`, // the second property's signature is for another value
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s in\n%s", want, got)
		}
	}
	if strings.Contains(got, "Set-Cookie") || strings.Contains(got, "secret") {
		t.Errorf("not part of the contract:\n%s", got)
	}
	if got := Normalize(Response{Status: 200, Body: body}, Options{}); !strings.Contains(got, "<signature, not checked>") {
		t.Errorf("no key:\n%s", got)
	}

	var img bytes.Buffer
	_ = png.Encode(&img, image.NewNRGBA(image.Rect(0, 0, 64, 32)))
	if got := Body(img.Bytes(), Options{}); !strings.HasPrefix(got, "<PNG 64x32, sha256 ") {
		t.Errorf("png: %s", got)
	}
	if got := Body([]byte(strings.Repeat("x", 300)), Options{}); len(got) > 210 {
		t.Errorf("long text not shortened: %d", len(got))
	}
}
