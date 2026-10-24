package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/Entropy-Increase-Team/skinloom/internal/player"
	"github.com/Entropy-Increase-Team/skinloom/internal/storage"
)

// mountGame serves the profile APIs game clients use to fetch skins (contract
// docs/design/02 §2.1–2.3): CustomSkinLoader, UniSkinMod and the legacy PNG URLs.
// They read no session and set no cookie.
//
// A route parameter covers the whole path segment and the suffix is checked by hand:
// a pattern such as /{player}.json would stop at the first dot of a name.
func mountGame(r chi.Router, d Deps) {
	r.Get("/{file}", serveCSL(d))
	r.Get("/csl/{file}", serveCSL(d))
	r.Get("/usm/{file}", serveUSM(d))
	r.Get("/skin/{file}", serveLegacy(d, false))
	r.Get("/cape/{file}", serveLegacy(d, true))
}

// playerParam returns the player name of a /…/{name}<suffix> request, or false.
// chi gives the parameter as it appears in the raw path, so it is unescaped here:
// game clients percent-encode names that are not ASCII.
func playerParam(r *http.Request, suffix string) (string, bool) {
	file, err := url.PathUnescape(chi.URLParam(r, "file"))
	if err != nil {
		return "", false
	}
	name, ok := strings.CutSuffix(file, suffix)
	return name, ok && name != ""
}

// lookupProfile answers the lookup errors every profile API shares and returns the
// profile when there is one.
func lookupProfile(w http.ResponseWriter, r *http.Request, d Deps, suffix string, notFound func(http.ResponseWriter)) *player.Profile {
	name, ok := playerParam(r, suffix)
	if !ok {
		http.NotFound(w, r)
		return nil
	}
	p, err := d.Players.Profile(r.Context(), name)
	switch {
	case errors.Is(err, player.ErrNotFound):
		notFound(w)
		return nil
	case errors.Is(err, player.ErrBanned):
		http.Error(w, "the owner of this player is banned", http.StatusForbidden)
		return nil
	case err != nil:
		d.Log.Warn("profile lookup", "name", name, "err", err)
		http.Error(w, "profile unavailable", http.StatusServiceUnavailable)
		return nil
	}
	return p
}

// writeProfile sends a profile body that may be cached for the configured time and
// revalidated with If-Modified-Since.
func writeProfile(w http.ResponseWriter, r *http.Request, d Deps, p *player.Profile, body any) {
	b, err := json.Marshal(body)
	if err != nil {
		http.Error(w, "profile unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age="+strconv.Itoa(player.DeliveryFrom(d.Settings.Current()).CacheSeconds))
	http.ServeContent(w, r, "", p.Modified, bytes.NewReader(b))
}

// nullable is a hash, or JSON null for none.
func nullable(hash string) *string {
	if hash == "" {
		return nil
	}
	return &hash
}

// cslProfile is CustomSkinAPI R1: one key in skins, named after the skin's model.
type cslProfile struct {
	Username string             `json:"username"`
	Skins    map[string]*string `json:"skins"`
	Cape     *string            `json:"cape"`
}

func serveCSL(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := lookupProfile(w, r, d, ".json", func(w http.ResponseWriter) {
			if player.DeliveryFrom(d.Settings.Current()).MissingStatus == http.StatusNoContent {
				w.WriteHeader(http.StatusNoContent) // clients then try their next skin site
				return
			}
			http.Error(w, "player not found", http.StatusNotFound)
		})
		if p == nil {
			return
		}
		writeProfile(w, r, d, p, cslProfile{Username: p.Name, Skins: map[string]*string{p.Model(): nullable(p.Skin)}, Cape: nullable(p.Cape)})
	}
}

// usmProfile is UniSkinMod's profile format.
type usmProfile struct {
	PlayerName      string             `json:"player_name"`
	LastUpdate      int64              `json:"last_update"`
	ModelPreference []string           `json:"model_preference"`
	Skins           map[string]*string `json:"skins"`
	Cape            *string            `json:"cape"`
}

func serveUSM(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := lookupProfile(w, r, d, ".json", func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errno":1,"msg":"Player not found."}`))
		})
		if p == nil {
			return
		}
		writeProfile(w, r, d, p, usmProfile{PlayerName: p.Name, LastUpdate: p.Modified.Unix(), ModelPreference: []string{p.Model()},
			Skins: map[string]*string{p.Model(): nullable(p.Skin)}, Cape: nullable(p.Cape)})
	}
}

// serveLegacy answers /skin/{name}.png and /cape/{name}.png with the image itself.
func serveLegacy(d Deps, cape bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := lookupProfile(w, r, d, ".png", func(w http.ResponseWriter) { http.Error(w, "player not found", http.StatusNotFound) })
		if p == nil {
			return
		}
		hash := p.Skin
		if cape {
			hash = p.Cape
		}
		if hash == "" {
			http.Error(w, "texture not found", http.StatusNotFound)
			return
		}
		f, err := d.Textures.OpenHash(r.Context(), hash)
		if errors.Is(err, storage.ErrNotFound) {
			http.Error(w, "texture not found", http.StatusNotFound)
			return
		}
		if err != nil {
			d.Log.Warn("open texture file", "hash", hash, "err", err)
			http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age="+strconv.Itoa(player.DeliveryFrom(d.Settings.Current()).CacheSeconds))
		// The same URL serves another image once the player changes clothes, so the
		// player's change time is the validator, not the file's.
		http.ServeContent(w, r, "", p.Modified, f)
	}
}
