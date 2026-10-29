package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Entropy-Increase-Team/skinloom/internal/render"
	"github.com/Entropy-Increase-Team/skinloom/internal/storage"
	"github.com/Entropy-Increase-Team/skinloom/internal/texture"
)

// A user may make their own private skin their avatar, and /avatar/user/{uid} shows
// its head to everyone. The ETag must not name the file: /textures/{hash} serves any
// stored file to whoever knows its hash, so a hash in a public header would hand out
// the whole private texture.
func TestRenderETagDoesNotNameTheFile(t *testing.T) {
	d := newTestDeps(t)
	setSettings(t, d, `{"captcha.provider": "none"}`)
	h, _ := New(d)
	setupSite(t, d, &browser{t: t, h: h})
	alice := registerUser(t, h, "alice@example.com")
	var me UserView
	_ = json.Unmarshal(alice.do("GET", "/api/v1/auth/me", "").Body.Bytes(), &me)
	var up UploadView
	_ = json.Unmarshal(upload(t, alice, "Secret", "steve", false, testPNG(t, 64, 64, 3)).Body.Bytes(), &up)
	secret := up.Texture
	if rec := alice.do("PUT", "/api/v1/me/avatar", fmt.Sprintf(`{"textureId":%d}`, secret.ID)); rec.Code != http.StatusNoContent {
		t.Fatalf("set avatar: %d %s", rec.Code, rec.Body)
	}
	for _, path := range []string{
		fmt.Sprintf("/avatar/user/%d", me.ID),
		fmt.Sprintf("/avatar/user/%d?3d&png&size=256", me.ID),
	} {
		rec := get(t, h, nil, path)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: %d", path, rec.Code)
		}
		for k, v := range rec.Header() {
			if strings.Contains(strings.Join(v, " "), secret.Hash) {
				t.Fatalf("%s: header %s names the private file: %q", path, k, v)
			}
		}
	}
	if rec := get(t, h, nil, "/textures/"+secret.Hash); rec.Code != http.StatusOK {
		t.Fatalf("precondition: a stored file is served by its hash (%d)", rec.Code)
	}
}

// The ETag is fixed by the request alone, so a client revalidating gets its 304
// without a render, even once the render has left the in-memory cache.
func TestRenderRevalidationNeedsNoRender(t *testing.T) {
	d := newTestDeps(t)
	dir := t.TempDir()
	store, err := storage.NewLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	d.Textures = texture.NewService(d.DB.Ent, store, d.Log)
	d.Renders = render.NewService(1, 0) // caches nothing
	setSettings(t, d, `{"captcha.provider": "none"}`)
	h, _ := New(d)
	setupSite(t, d, &browser{t: t, h: h})
	alice := registerUser(t, h, "alice@example.com")
	var up UploadView
	_ = json.Unmarshal(upload(t, alice, "Skin", "steve", true, testPNG(t, 64, 64, 1)).Body.Bytes(), &up)
	skin := up.Texture

	path := "/avatar/hash/" + skin.Hash + "?size=64"
	first := get(t, h, nil, path)
	etag := first.Header().Get("ETag")
	if first.Code != http.StatusOK || etag == "" {
		t.Fatalf("first: %d, ETag %q", first.Code, etag)
	}
	// Without the file a render fails, so a 304 proves none was attempted.
	if err := os.Remove(filepath.Join(dir, skin.Hash)); err != nil {
		t.Fatal(err)
	}
	rec := get(t, h, nil, path, "If-None-Match", etag)
	if rec.Code != http.StatusNotModified {
		t.Fatalf("revalidation: %d", rec.Code)
	}
	if rec.Header().Get("ETag") != etag || rec.Header().Get("Cache-Control") != first.Header().Get("Cache-Control") ||
		rec.Header().Get("Vary") != "Accept" {
		t.Fatalf("a 304 carries the same validators: %v", rec.Header())
	}
	if rec := get(t, h, nil, path, "If-None-Match", `"something-else"`); rec.Code == http.StatusNotModified {
		t.Fatal("another ETag is not a match")
	}
}
