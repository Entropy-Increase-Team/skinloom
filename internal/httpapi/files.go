package httpapi

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/Entropy-Increase-Team/skinloom/internal/modules/texture"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/storage"
)

var pngSignature = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

// mountFiles serves texture files: by hash for game clients (contract 02 §2.1), and
// the original by texture id (/raw/{tid}, honouring visibility and
// texture.allow_download).
func mountFiles(r chi.Router, d Deps) {
	byHash, raw := serveByHash(d), serveRaw(d)
	for _, p := range []string{"/textures/{hash}", "/csl/textures/{hash}", "/usm/textures/{hash}"} {
		r.Get(p, byHash)
		r.Head(p, byHash) // caches and launchers probe with HEAD; ServeContent sends no body
	}
	r.Get("/raw/{tid}", raw)
	r.Head("/raw/{tid}", raw)
}

// serveByHash: a hash names one immutable file, so it can be cached forever. With an
// S3 store that has a public URL (a CDN), clients are sent there instead.
func serveByHash(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hash := chi.URLParam(r, "hash")
		if url := d.Textures.FileURL(hash); url != "" {
			w.Header().Set("Cache-Control", "public, max-age=86400")
			http.Redirect(w, r, url, http.StatusFound)
			return
		}
		f, err := d.Textures.OpenHash(r.Context(), hash)
		if errors.Is(err, storage.ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		if err != nil {
			d.Log.Warn("open texture file", "hash", hash, "err", err)
			http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
			return
		}
		defer f.Close()
		// Defense-in-depth: texture endpoints serve PNG skin/cape blobs exclusively.
		// Reject any non-PNG object (such as a legacy v1.1 .slp ZIP archive) rather
		// than exposing it publicly with Content-Type: image/png.
		var sig [8]byte
		if _, err := io.ReadFull(f, sig[:]); err != nil || !bytes.Equal(sig[:], pngSignature) {
			http.NotFound(w, r)
			return
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			d.Log.Warn("seek texture file", "hash", hash, "err", err)
			http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
			return
		}
		h := w.Header()
		h.Set("Content-Type", "image/png")
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
		h.Set("ETag", `"`+hash+`"`)
		http.ServeContent(w, r, "", f.ModTime(), f)
	}
}

// cookieViewer is who sends a request outside /api (where withRequestInfo does not
// run): only private textures need to know.
func cookieViewer(r *http.Request, d Deps) texture.Viewer {
	if c, err := r.Cookie(SessionCookie); err == nil && d.Accounts != nil {
		if v, _ := d.Accounts.Authenticate(r.Context(), c.Value); v != nil {
			return texture.Viewer{UserID: v.User.ID, Admin: v.IsAdmin()}
		}
	}
	return texture.Viewer{}
}

func serveRaw(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(strings.TrimSuffix(chi.URLParam(r, "tid"), ".png"), 10, 64)
		if err != nil || id <= 0 {
			http.NotFound(w, r)
			return
		}
		p := texture.PolicyFrom(d.Settings.Current())
		if !p.AllowDownload {
			http.Error(w, "downloading textures is disabled on this site", http.StatusForbidden)
			return
		}
		t, f, err := d.Textures.Open(r.Context(), id, cookieViewer(r, d))
		switch {
		case errors.Is(err, texture.ErrNotFound):
			http.NotFound(w, r)
			return
		case errors.Is(err, texture.ErrPrivate):
			if p.PrivateStatus == http.StatusNotFound {
				http.NotFound(w, r)
			} else {
				http.Error(w, "this texture is private", http.StatusForbidden)
			}
			return
		case err != nil:
			d.Log.Warn("open texture", "texture", id, "err", err)
			http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
			return
		}
		defer f.Close()
		h := w.Header()
		h.Set("Content-Type", "image/png")
		h.Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": t.Name + ".png"}))
		h.Set("ETag", `"`+t.Hash+`"`)
		// The texture may change visibility or go away, unlike a hash URL.
		if t.Visibility == "public" {
			h.Set("Cache-Control", "public, max-age=300")
		} else {
			h.Set("Cache-Control", "private, no-cache")
		}
		http.ServeContent(w, r, "", f.ModTime(), f)
	}
}
