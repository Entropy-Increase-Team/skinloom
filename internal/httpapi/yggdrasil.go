package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"

	"github.com/Entropy-Increase-Team/skinloom/internal/buildinfo"
	"github.com/Entropy-Increase-Team/skinloom/internal/score"
	"github.com/Entropy-Increase-Team/skinloom/internal/texture"
	"github.com/Entropy-Increase-Team/skinloom/internal/yggdrasil"
)

// YggdrasilPrefix is where the Yggdrasil API lives (authlib-injector's API root).
const YggdrasilPrefix = "/api/yggdrasil"

// mountYggdrasil serves the Yggdrasil API for authlib-injector (contract 02 §2.4).
// It is a game protocol, not part of the REST API: not in the OpenAPI document, no
// sessions or cookies, errors in Yggdrasil's own format.
func mountYggdrasil(r chi.Router, d Deps) {
	r.Route(YggdrasilPrefix, func(r chi.Router) {
		r.Get("/", yggMeta(d))
		r.Post("/authserver/authenticate", yggJSON(d, func(ctx context.Context, w http.ResponseWriter, r *http.Request, body []byte) (any, error) {
			var in yggdrasil.AuthRequest
			if err := json.Unmarshal(body, &in); err != nil {
				return nil, yggBadBody
			}
			return d.Yggdrasil.Authenticate(ctx, in, yggPolicy(d), requestInfo(ctx).meta(r.UserAgent()))
		}))
		r.Post("/authserver/refresh", yggJSON(d, func(ctx context.Context, _ http.ResponseWriter, _ *http.Request, body []byte) (any, error) {
			var in yggdrasil.RefreshRequest
			if err := json.Unmarshal(body, &in); err != nil {
				return nil, yggBadBody
			}
			return d.Yggdrasil.Refresh(ctx, in, yggPolicy(d))
		}))
		r.Post("/authserver/validate", yggJSON(d, func(ctx context.Context, _ http.ResponseWriter, _ *http.Request, body []byte) (any, error) {
			var in struct{ AccessToken, ClientToken string }
			if err := json.Unmarshal(body, &in); err != nil {
				return nil, yggBadBody
			}
			return nil, d.Yggdrasil.Validate(ctx, in.AccessToken, in.ClientToken)
		}))
		r.Post("/authserver/invalidate", yggJSON(d, func(ctx context.Context, _ http.ResponseWriter, _ *http.Request, body []byte) (any, error) {
			var in struct{ AccessToken string }
			_ = json.Unmarshal(body, &in) // always 204
			return nil, d.Yggdrasil.Invalidate(ctx, in.AccessToken)
		}))
		r.Post("/authserver/signout", yggJSON(d, func(ctx context.Context, _ http.ResponseWriter, r *http.Request, body []byte) (any, error) {
			var in struct{ Username, Password string }
			if err := json.Unmarshal(body, &in); err != nil {
				return nil, yggBadBody
			}
			return nil, d.Yggdrasil.Signout(ctx, in.Username, in.Password, requestInfo(ctx).meta(r.UserAgent()))
		}))
		r.Post("/sessionserver/session/minecraft/join", yggJSON(d, func(ctx context.Context, _ http.ResponseWriter, _ *http.Request, body []byte) (any, error) {
			var in struct{ AccessToken, SelectedProfile, ServerID string }
			if err := json.Unmarshal(body, &in); err != nil {
				return nil, yggBadBody
			}
			return nil, d.Yggdrasil.Join(ctx, in.AccessToken, in.SelectedProfile, in.ServerID, requestInfo(ctx).client.ClientIP)
		}))
		r.Get("/sessionserver/session/minecraft/hasJoined", func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			p, err := d.Yggdrasil.HasJoined(r.Context(), q.Get("username"), q.Get("serverId"), q.Get("ip"), siteBase(r, d))
			yggAnswer(w, d, p, err)
		})
		r.Get("/sessionserver/session/minecraft/profile/{uuid}", func(w http.ResponseWriter, r *http.Request) {
			// Signed only on ?unsigned=false, as Mojang's API.
			signed := r.URL.Query().Get("unsigned") == "false"
			p, err := d.Yggdrasil.ProfileByUUID(r.Context(), chi.URLParam(r, "uuid"), siteBase(r, d), signed)
			yggAnswer(w, d, p, err)
		})
		r.Post("/api/profiles/minecraft", yggJSON(d, func(ctx context.Context, _ http.ResponseWriter, _ *http.Request, body []byte) (any, error) {
			var names []string
			if err := json.Unmarshal(body, &names); err != nil {
				return nil, yggBadBody
			}
			return d.Yggdrasil.LookupNames(ctx, names, yggPolicy(d))
		}))
		r.Get("/api/users/profiles/minecraft/{name}", func(w http.ResponseWriter, r *http.Request) {
			name, _ := url.PathUnescape(chi.URLParam(r, "name"))
			p, err := d.Yggdrasil.LookupName(r.Context(), name)
			yggAnswer(w, d, p, err)
		})
		r.Put("/api/user/profile/{uuid}/{kind}", yggTextureUpload(d))
		r.Delete("/api/user/profile/{uuid}/{kind}", func(w http.ResponseWriter, r *http.Request) {
			kind := chi.URLParam(r, "kind")
			if kind != "skin" && kind != "cape" {
				http.NotFound(w, r)
				return
			}
			yggAnswer(w, d, nil, d.Yggdrasil.ResetTexture(r.Context(), bearer(r), chi.URLParam(r, "uuid"), kind, yggPolicy(d)))
		})
		r.Get("/minecraftservices/publickeys", func(w http.ResponseWriter, r *http.Request) {
			_, der, err := d.Yggdrasil.PublicKey(r.Context())
			if err != nil {
				yggAnswer(w, d, nil, err)
				return
			}
			key := []map[string]string{{"publicKey": base64.StdEncoding.EncodeToString(der)}}
			yggAnswer(w, d, map[string]any{"profilePropertyKeys": key, "playerCertificateKeys": key}, nil)
		})
	})
}

var yggBadBody = &yggdrasil.Error{Status: http.StatusBadRequest, Kind: "IllegalArgumentException", Message: "Invalid request body."}

// yggMaxBody bounds JSON bodies (credentials, tokens, a few names).
const yggMaxBody = 64 << 10

func yggPolicy(d Deps) yggdrasil.Policy { return yggdrasil.PolicyFrom(d.Settings.Current()) }

func bearer(r *http.Request) string {
	token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	return strings.TrimSpace(token)
}

// siteBase is the URL textures are served under: site.url, or the address the
// request came to (hasJoined is called by game servers, which use that address).
func siteBase(r *http.Request, d Deps) string {
	if u := strings.TrimRight(d.Settings.Current().String("site.url"), "/"); u != "" {
		return u
	}
	c := requestInfo(r.Context()).client
	return c.Proto + "://" + c.Host
}

func yggJSON(d Deps, fn func(ctx context.Context, w http.ResponseWriter, r *http.Request, body []byte) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, yggMaxBody))
		if err != nil {
			yggAnswer(w, d, nil, yggBadBody)
			return
		}
		out, err := fn(r.Context(), w, r, body)
		yggAnswer(w, d, out, err)
	}
}

// yggAnswer writes a result: JSON, 204 for none, or a Yggdrasil error.
func yggAnswer(w http.ResponseWriter, d Deps, out any, err error) {
	h := w.Header()
	h.Set("Cache-Control", "no-store")
	var ye *yggdrasil.Error
	switch {
	case errors.As(err, &ye):
		h.Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(ye.Status)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": ye.Kind, "errorMessage": ye.Message})
		return
	case err != nil:
		d.Log.Warn("yggdrasil", "err", err)
		h.Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "InternalServerError", "errorMessage": "Something went wrong on the server."})
		return
	}
	if isNil(out) {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	h.Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(out)
}

// isNil reports whether out is nil, including a typed nil pointer.
func isNil(out any) bool {
	switch v := out.(type) {
	case nil:
		return true
	case *yggdrasil.Profile:
		return v == nil
	case *yggdrasil.ProfileRef:
		return v == nil
	}
	return false
}

// yggMeta is the API root: what authlib-injector reads first.
func yggMeta(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		snap := d.Settings.Current()
		base := siteBase(r, d)
		pemText, _, err := d.Yggdrasil.PublicKey(r.Context())
		if err != nil {
			yggAnswer(w, d, nil, err)
			return
		}
		links := map[string]string{"homepage": base + "/"}
		if snap.String("auth.registration") == "open" {
			links["register"] = base + "/auth/register"
		}
		domains := []string{}
		seen := map[string]bool{}
		add := func(host string) {
			if host = strings.TrimSpace(host); host != "" && !seen[host] {
				seen[host] = true
				domains = append(domains, host)
			}
		}
		if u, err := url.Parse(base); err == nil {
			add(u.Hostname()) // site.url, or without it the address the request came to
		}
		if pub := d.Textures.FileURL(strings.Repeat("0", 64)); pub != "" {
			if u, err := url.Parse(pub); err == nil {
				add(u.Hostname()) // files redirect to the storage's public URL
			}
		}
		for _, x := range yggPolicy(d).SkinDomains {
			add(x)
		}
		locale := Locales[0].Code
		yggAnswer(w, d, map[string]any{
			"meta": map[string]any{
				"serverName":              snap.Localized("site.name", locale),
				"implementationName":      "SkinLoom",
				"implementationVersion":   buildinfo.Version,
				"links":                   links,
				"feature.non_email_login": true,
			},
			"skinDomains":        domains,
			"signaturePublickey": pemText,
		}, nil)
	}
}

// yggTextureUpload is PUT /api/user/profile/{uuid}/{skin|cape}: multipart with
// "file" and, for skins, "model" ("slim" or empty).
func yggTextureUpload(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		kind := chi.URLParam(r, "kind")
		if kind != "skin" && kind != "cape" {
			http.NotFound(w, r)
			return
		}
		snap := d.Settings.Current()
		tp := texture.PolicyFrom(snap)
		r.Body = http.MaxBytesReader(w, r.Body, tp.MaxBytes+64<<10)
		if ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); ct != "multipart/form-data" {
			yggAnswer(w, d, nil, yggBadBody)
			return
		}
		if err := r.ParseMultipartForm(tp.MaxBytes + 64<<10); err != nil {
			yggAnswer(w, d, nil, &yggdrasil.Error{Status: http.StatusBadRequest, Kind: "IllegalArgumentException", Message: "The file is missing or too large."})
			return
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			yggAnswer(w, d, nil, yggBadBody)
			return
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, tp.MaxBytes+1))
		if err != nil {
			yggAnswer(w, d, nil, yggBadBody)
			return
		}
		in := yggdrasil.Upload{Kind: kind, Slim: r.FormValue("model") == "slim",
			Name: strings.TrimSuffix(header.Filename, ".png"), Data: data}
		err = d.Yggdrasil.UploadTexture(r.Context(), bearer(r), chi.URLParam(r, "uuid"), in, yggdrasil.PolicyFrom(snap), tp, score.RatesFrom(snap))
		yggAnswer(w, d, nil, err)
	}
}

// YggdrasilTokens is how many game launchers are signed in to the account.
type YggdrasilTokens struct {
	Tokens int    `json:"tokens"`
	APIURL string `json:"apiUrl" doc:"The address to give authlib-injector"`
}

func registerYggdrasilAccount(api huma.API, d Deps) {
	huma.Register(api, huma.Operation{OperationID: "getYggdrasil", Method: http.MethodGet, Path: "/api/v1/me/yggdrasil",
		Summary: "Game launchers signed in to your account", Tags: []string{"me"}, Security: sessionSecurity},
		func(ctx context.Context, _ *struct{}) (*struct{ Body YggdrasilTokens }, error) {
			v, err := signedIn(ctx)
			if err != nil {
				return nil, err
			}
			n, err := d.Yggdrasil.Tokens(ctx, v.User.ID)
			if err != nil {
				return nil, err
			}
			base := strings.TrimRight(d.Settings.Current().String("site.url"), "/")
			if base == "" {
				c := requestInfo(ctx).client
				base = c.Proto + "://" + c.Host
			}
			return &struct{ Body YggdrasilTokens }{YggdrasilTokens{Tokens: n, APIURL: base + YggdrasilPrefix}}, nil
		})
	huma.Register(api, huma.Operation{OperationID: "signOutLaunchers", Method: http.MethodPost, Path: "/api/v1/me/yggdrasil/signout",
		Summary: "Sign every game launcher out", Tags: []string{"me"}, Security: sessionSecurity},
		func(ctx context.Context, _ *struct{}) (*struct{ Body YggdrasilTokens }, error) {
			v, err := signedIn(ctx)
			if err != nil {
				return nil, err
			}
			if _, err := d.Yggdrasil.RevokeAll(ctx, v.User.ID); err != nil {
				return nil, err
			}
			return &struct{ Body YggdrasilTokens }{YggdrasilTokens{}}, nil
		})
}
