package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/png" // textures are PNG
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	tex "github.com/Entropy-Increase-Team/skinloom/ent/texture"
	"github.com/Entropy-Increase-Team/skinloom/internal/player"
	"github.com/Entropy-Increase-Team/skinloom/internal/render"
	"github.com/Entropy-Increase-Team/skinloom/internal/texture"
)

// mountRender serves avatars and previews (contract 02 §2.5): a skin's head, flat or
// as a cube (?3d), and a texture seen from the front and back. Sizes snap to a fixed
// list, the output is WebP unless ?png or the client cannot take it, and every result
// is cached by file hash and parameters.
func mountRender(r chi.Router, d Deps) {
	routes := map[string]http.HandlerFunc{
		"/avatar/player/{name}": func(w http.ResponseWriter, r *http.Request) {
			name, err := url.PathUnescape(chi.URLParam(r, "name"))
			if err != nil {
				http.NotFound(w, r)
				return
			}
			p, err := d.Players.Profile(r.Context(), name)
			switch {
			case errors.Is(err, player.ErrNotFound):
				http.NotFound(w, r) // as Blessing Skin: an unknown player is a 404
			case errors.Is(err, player.ErrBanned):
				serveAvatar(w, r, d, "", shortCache(d))
			case err != nil:
				renderFailed(w, d, err)
			default:
				serveAvatar(w, r, d, p.Skin, shortCache(d))
			}
		},
		"/avatar/user/{uid}": func(w http.ResponseWriter, r *http.Request) {
			id, _ := strconv.ParseInt(chi.URLParam(r, "uid"), 10, 64)
			t, err := d.Textures.Avatar(r.Context(), id)
			if err != nil {
				renderFailed(w, d, err)
				return
			}
			serveAvatar(w, r, d, hashOf(t), shortCache(d))
		},
		"/avatar/hash/{hash}": func(w http.ResponseWriter, r *http.Request) {
			t, err := d.Textures.ByHash(r.Context(), chi.URLParam(r, "hash"))
			if err != nil && !errors.Is(err, texture.ErrNotFound) {
				renderFailed(w, d, err)
				return
			}
			if t != nil && t.Type == tex.TypeCape {
				http.Error(w, "a cape has no head", http.StatusUnprocessableEntity)
				return
			}
			cache := shortCache(d) // a texture with this file may yet be uploaded
			if t != nil {
				cache = foreverCache
			}
			serveAvatar(w, r, d, hashOf(t), cache)
		},
		"/avatar/{tid}": func(w http.ResponseWriter, r *http.Request) {
			t, err := textureParam(r, d)
			if err != nil && !errors.Is(err, texture.ErrNotFound) && !errors.Is(err, texture.ErrPrivate) {
				renderFailed(w, d, err)
				return
			}
			if t != nil && t.Type == tex.TypeCape {
				http.Error(w, "a cape has no head", http.StatusUnprocessableEntity)
				return
			}
			cache := shortCache(d)
			if errors.Is(err, texture.ErrPrivate) || (t != nil && t.Visibility != tex.VisibilityPublic) {
				cache = privateCache // the answer depends on who asks
			}
			serveAvatar(w, r, d, hashOf(t), cache) // missing or not visible: the placeholder
		},
		"/preview/hash/{hash}": func(w http.ResponseWriter, r *http.Request) {
			t, err := d.Textures.ByHash(r.Context(), chi.URLParam(r, "hash"))
			if errors.Is(err, texture.ErrNotFound) {
				http.NotFound(w, r)
				return
			}
			if err != nil {
				renderFailed(w, d, err)
				return
			}
			servePreview(w, r, d, t, foreverCache)
		},
		"/preview/{tid}": func(w http.ResponseWriter, r *http.Request) {
			t, err := textureParam(r, d)
			switch {
			case errors.Is(err, texture.ErrNotFound):
				http.NotFound(w, r)
			case errors.Is(err, texture.ErrPrivate):
				status := texture.PolicyFrom(d.Settings.Current()).PrivateStatus
				w.Header().Set("Cache-Control", privateCache)
				http.Error(w, http.StatusText(status), status)
			case err != nil:
				renderFailed(w, d, err)
			case t.Visibility != tex.VisibilityPublic:
				servePreview(w, r, d, t, privateCache)
			default:
				servePreview(w, r, d, t, shortCache(d))
			}
		},
	}
	for p, h := range routes {
		r.Get(p, h)
		r.Head(p, h)
	}
}

func hashOf(t *ent.Texture) string {
	if t == nil {
		return ""
	}
	return t.Hash
}

// textureParam loads {tid} (".png" tolerated) as the requester may see it.
func textureParam(r *http.Request, d Deps) (*ent.Texture, error) {
	id, err := strconv.ParseInt(strings.TrimSuffix(chi.URLParam(r, "tid"), ".png"), 10, 64)
	if err != nil || id <= 0 {
		return nil, texture.ErrNotFound
	}
	return d.Textures.Get(r.Context(), id, cookieViewer(r, d))
}

// Cache-Control of render answers: by hash for good; by name or id for a short while
// (what they show can change); private when the answer depends on who asks.
const (
	foreverCache = "public, max-age=31536000, immutable"
	privateCache = "private, no-cache"
)

func shortCache(d Deps) string {
	return fmt.Sprintf("public, max-age=%d", player.DeliveryFrom(d.Settings.Current()).CacheSeconds)
}

// imageFormat is WebP, as Blessing Skin served, unless ?png is given or the client
// lists the image types it takes and WebP is not among them.
func imageFormat(r *http.Request) string {
	if r.URL.Query().Has("png") {
		return render.PNG
	}
	a := r.Header.Get("Accept")
	if a == "" || strings.Contains(a, "image/webp") || strings.Contains(a, "image/*") || strings.Contains(a, "*/*") {
		return render.WebP
	}
	return render.PNG
}

// serveAvatar renders the head of the skin with this hash ("" = the placeholder).
func serveAvatar(w http.ResponseWriter, r *http.Request, d Deps, hash, cache string) {
	q := r.URL.Query()
	size := 100
	if s := q.Get("size"); s != "" {
		size, _ = strconv.Atoi(s)
	}
	size = render.Snap(size, render.AvatarSizes, 100)
	cube := q.Has("3d")
	format := imageFormat(r)
	key := fmt.Sprintf("avatar:%s:%t:%d:%s", hash, cube, size, format)
	serveImage(w, r, d, key, format, cache, func() (image.Image, error) {
		skin := render.Placeholder()
		if hash != "" {
			img, err := decodeFile(r, d, hash)
			if err != nil {
				return nil, err
			}
			skin = render.Normalize(img)
		}
		if cube {
			return render.Avatar3D(skin, size), nil
		}
		return render.Avatar2D(skin, size), nil
	})
}

// servePreview renders a texture: a skin from the front and the back, or a cape.
func servePreview(w http.ResponseWriter, r *http.Request, d Deps, t *ent.Texture, cache string) {
	height := 200
	if s := r.URL.Query().Get("height"); s != "" {
		height, _ = strconv.Atoi(s)
	}
	height = render.Snap(height, render.PreviewHeights, 200)
	format := imageFormat(r)
	// The key holds the type: a texture's image never changes, but its type can (an
	// Alex skin is drawn with slim arms), so the hash alone is not enough.
	key := fmt.Sprintf("preview:%s:%s:%d:%s", t.Hash, t.Type, height, format)
	serveImage(w, r, d, key, format, cache, func() (image.Image, error) {
		img, err := decodeFile(r, d, t.Hash)
		if err != nil {
			return nil, err
		}
		if t.Type == tex.TypeCape {
			return render.CapePreview(img, height), nil
		}
		return render.SkinPreview(render.Normalize(img), t.Type == tex.TypeAlex, height), nil
	})
}

func decodeFile(r *http.Request, d Deps, hash string) (image.Image, error) {
	data, err := d.Textures.ReadFile(r.Context(), hash)
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	return img, err
}

// serveImage answers with the cached render for key, rendering it on a miss. The same
// key is the same bytes, so the ETag follows from the key and a revalidation is
// answered before anything is rendered. It is a digest of the key: the key names the
// file, and a private texture's hash must not appear in a public answer (an avatar
// may show the head of its owner's private skin; /textures/{hash} serves any file).
func serveImage(w http.ResponseWriter, r *http.Request, d Deps, key, format, cache string, draw func() (image.Image, error)) {
	sum := sha256.Sum256([]byte(key))
	etag := `"` + hex.EncodeToString(sum[:16]) + `"`
	validators := func() {
		h := w.Header()
		h.Set("Vary", "Accept")
		h.Set("ETag", etag)
		h.Set("Cache-Control", cache)
	}
	if etagMatches(r.Header.Get("If-None-Match"), etag) {
		validators()
		w.WriteHeader(http.StatusNotModified)
		return
	}
	data, err := d.Renders.Get(r.Context(), key, func() ([]byte, error) {
		img, err := draw()
		if err != nil {
			return nil, err
		}
		return render.Encode(img, format)
	})
	if err != nil {
		renderFailed(w, d, err) // without the cache headers: a failure is not kept
		return
	}
	validators()
	w.Header().Set("Content-Type", "image/"+format)
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
}

// etagMatches reports whether an If-None-Match header lists etag (weakly, as
// RFC 9110 says for If-None-Match), or is "*".
func etagMatches(header, etag string) bool {
	for _, t := range strings.Split(header, ",") {
		t = strings.TrimPrefix(strings.TrimSpace(t), "W/")
		if t == etag || t == "*" {
			return true
		}
	}
	return false
}

func renderFailed(w http.ResponseWriter, d Deps, err error) {
	d.Log.Warn("render", "err", err)
	http.Error(w, "could not render", http.StatusServiceUnavailable)
}
