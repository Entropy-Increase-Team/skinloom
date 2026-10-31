package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"math"
	"mime"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	tex "github.com/Entropy-Increase-Team/skinloom/ent/texture"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/account"
	"github.com/Entropy-Increase-Team/skinloom/internal/buildinfo"
	"github.com/Entropy-Increase-Team/skinloom/internal/notify"
	"github.com/Entropy-Increase-Team/skinloom/internal/oauth"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/validate"
	"github.com/Entropy-Increase-Team/skinloom/internal/player"
	"github.com/Entropy-Increase-Team/skinloom/internal/score"
	"github.com/Entropy-Increase-Team/skinloom/internal/settings"
	"github.com/Entropy-Increase-Team/skinloom/internal/texture"
)

// The Blessing Skin compatible resource API (/api/user, /api/players, /api/closet;
// contract docs/design/02 §2.6, L-B) for applications' OAuth tokens and personal
// access tokens. It answers in Blessing Skin's shapes, so applications written for it
// keep working: model objects with snake_case fields; {"code", "message", "data"}
// with code 0 on success and non-zero (still HTTP 200) for refusals; Laravel's
// {"message", "errors"} for invalid input (422); {"message"} for 401/403/404/429.

// BSCompatVersion is the Blessing Skin version whose API this one matches.
const BSCompatVersion = "6.0.2"

// bsRateLimit is Blessing Skin's throttle:60,1 (per user, per minute).
var bsRateLimit = account.NewLimiter(60, time.Minute)

const bsTime = "2006-01-02 15:04:05"

// bsMessages are the texts of answers, in the user's language (zh-CN or en).
var bsMessages = map[string][2]string{
	"unauthenticated":   {"Unauthenticated.", "Unauthenticated."},
	"scope":             {"Invalid scope(s) provided.", "Invalid scope(s) provided."},
	"banned":            {"你的账号已被封禁。", "Your account has been banned."},
	"unverified":        {"你必须验证邮箱后才能进行此操作。", "You should verify your email address first."},
	"throttled":         {"Too Many Attempts.", "Too Many Attempts."},
	"invalid":           {"提交的数据有误。", "The given data was invalid."},
	"notFound":          {"没有找到。", "Not found."},
	"player.added":      {"成功添加了角色 :name。", "Added player :name."},
	"player.lackScore":  {"积分不足，无法添加角色。", "You don't have enough score to add a player."},
	"player.notYours":   {"这不是你的角色。", "This player is not yours."},
	"player.deleted":    {"角色 :name 已被删除。", "Player :name was deleted."},
	"player.renamed":    {"角色 :old 已改名为 :new。", "Player :old was renamed to :new."},
	"player.set":        {"角色 :name 的材质已更新。", "Textures of :name were updated."},
	"player.cleared":    {"角色 :name 的材质已重置。", "Textures of :name were reset."},
	"closet.added":      {"已将材质 :name 添加到衣柜。", "Added :name to your closet."},
	"closet.lackScore":  {"积分不足，无法收藏材质。", "You don't have enough score to add this texture."},
	"closet.notFound":   {"材质不存在。", "The texture does not exist."},
	"closet.private":    {"这是私有材质。", "The texture is private."},
	"closet.repeated":   {"你已经收藏过这个材质了。", "This texture is already in your closet."},
	"closet.notInIt":    {"衣柜中不存在此材质。", "The texture is not in your closet."},
	"closet.renamed":    {"材质已改名为 :name。", "The item was renamed to :name."},
	"closet.removed":    {"已从衣柜中移除材质。", "Removed from your closet."},
	"notification.none": {"没有这条通知。", "No such notification."},
}

func bsText(locale, key string, args ...string) string {
	m, ok := bsMessages[key]
	if !ok {
		return key
	}
	s := m[0]
	if !strings.HasPrefix(locale, "zh") && locale != "" {
		s = m[1]
	}
	for i := 0; i+1 < len(args); i += 2 {
		s = strings.ReplaceAll(s, ":"+args[i], args[i+1])
	}
	return s
}

func bsWrite(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false) // as PHP's json_encode: notification content is HTML
	_ = enc.Encode(v)
}

// bsResult is Blessing Skin's json($message, $code[, $data]), always HTTP 200.
func bsResult(w http.ResponseWriter, code int, message string, data any) {
	out := map[string]any{"code": code, "message": message}
	if data != nil {
		out["data"] = data
	}
	bsWrite(w, http.StatusOK, out)
}

func bsFail(w http.ResponseWriter, status int, message string) {
	bsWrite(w, status, map[string]string{"message": message})
}

// bsInvalid answers like Laravel's validation: 422, the first message, and every
// message per field.
func bsInvalid(w http.ResponseWriter, locale string, fields []*validate.FieldError) {
	errs := map[string][]string{}
	for _, f := range fields {
		errs[f.Field] = append(errs[f.Field], f.Field+": "+f.Message)
	}
	msg := bsText(locale, "invalid")
	if len(fields) > 0 {
		msg = fields[0].Field + ": " + fields[0].Message
	}
	bsWrite(w, http.StatusUnprocessableEntity, map[string]any{"message": msg, "errors": errs})
}

// bsInput is a request body, JSON or a form, as Laravel reads either.
type bsInput map[string]any

func readBSInput(w http.ResponseWriter, r *http.Request) bsInput {
	in := bsInput{}
	for k, v := range r.URL.Query() {
		in[strings.TrimSuffix(k, "[]")] = toAny(v)
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, oauthMaxBody))
	if err != nil || len(body) == 0 {
		return in
	}
	ctype, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if ctype == "application/json" {
		var m map[string]any
		if json.Unmarshal(body, &m) == nil {
			for k, v := range m {
				in[k] = v
			}
		}
		return in
	}
	if form, err := url.ParseQuery(string(body)); err == nil {
		for k, v := range form {
			in[strings.TrimSuffix(k, "[]")] = toAny(v)
		}
	}
	return in
}

func toAny(v []string) any {
	if len(v) == 1 {
		return v[0]
	}
	out := make([]any, len(v))
	for i, s := range v {
		out[i] = s
	}
	return out
}

func (in bsInput) str(k string) string {
	switch v := in[k].(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return ""
}

// id reads a positive integer; ok is false when the value is there but not one.
func (in bsInput) id(k string) (int64, bool) {
	switch v := in[k].(type) {
	case nil:
		return 0, true
	case float64:
		if v >= 0 && v == math.Trunc(v) && v < 1<<53 {
			return int64(v), true
		}
	case string:
		if v == "" {
			return 0, true
		}
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
			return n, true
		}
	}
	return 0, false
}

func (in bsInput) has(k string) bool { _, ok := in[k]; return ok }

func (in bsInput) list(k string) []string {
	switch v := in[k].(type) {
	case string:
		return []string{v}
	case []any:
		out := []string{}
		for _, x := range v {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

type bsHandler func(w http.ResponseWriter, r *http.Request, p *oauth.Principal, locale string)

// bsRoute authenticates a resource API request by its bearer token and checks one
// of the scopes and the rate limit.
func bsRoute(d Deps, scopes []string, fn bsHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, err := d.OAuth.Authenticate(r.Context(), oauthBearer(r))
		switch {
		case errors.Is(err, oauth.ErrBanned):
			bsFail(w, http.StatusForbidden, bsText("", "banned"))
			return
		case errors.Is(err, oauth.ErrInvalidToken):
			w.Header().Set("WWW-Authenticate", `Bearer realm="api"`)
			bsFail(w, http.StatusUnauthorized, bsText("", "unauthenticated"))
			return
		case err != nil:
			d.Log.Error("resource api auth", "err", err)
			bsFail(w, http.StatusInternalServerError, "Server Error")
			return
		}
		locale := p.User.Locale
		key := "u" + strconv.FormatInt(p.User.ID, 10)
		if wait, blocked := bsRateLimit.Take(key); blocked {
			w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
			bsFail(w, http.StatusTooManyRequests, bsText(locale, "throttled"))
			return
		}
		allowed := false
		for _, s := range scopes {
			allowed = allowed || p.Can(s)
		}
		if !allowed {
			bsFail(w, http.StatusForbidden, bsText(locale, "scope"))
			return
		}
		fn(w, r, p, locale)
	}
}

// bsVerified refuses with 403 when the site requires a verified email address and the
// token's user has not verified theirs (account.CheckVerified).
func bsVerified(d Deps, fn bsHandler) bsHandler {
	return func(w http.ResponseWriter, r *http.Request, p *oauth.Principal, locale string) {
		if err := account.CheckVerified(d.Settings.Current(), p.User); err != nil {
			bsFail(w, http.StatusForbidden, bsText(locale, "unverified"))
			return
		}
		fn(w, r, p, locale)
	}
}

func bsServerError(w http.ResponseWriter, d Deps, err error) {
	d.Log.Error("resource api", "err", err)
	bsFail(w, http.StatusInternalServerError, "Server Error")
}

// ---- Views in Blessing Skin's shapes ----

func bsFormat(t *time.Time, loc *time.Location) any {
	if t == nil {
		return nil
	}
	return t.In(loc).Format(bsTime)
}

func bsUser(u *ent.User, loc *time.Location) map[string]any {
	perm := 0
	switch {
	case u.Status == user.StatusBanned:
		perm = -1
	case u.Role == user.RoleAdmin:
		perm = 1
	case u.Role == user.RoleSuperadmin:
		perm = 2
	}
	avatar := int64(0)
	if u.AvatarTextureID != nil {
		avatar = *u.AvatarTextureID
	}
	return map[string]any{"uid": u.ID, "email": u.Email, "nickname": u.Nickname, "locale": u.Locale, "score": u.Score,
		"avatar": avatar, "permission": perm, "last_sign_at": bsFormat(u.LastSignAt, loc),
		"register_at": bsFormat(&u.CreatedAt, loc), "verified": u.EmailVerifiedAt != nil, "is_dark_mode": false}
}

func bsPlayer(p *ent.Player, loc *time.Location) map[string]any {
	skin, cape := int64(0), int64(0)
	if p.SkinID != nil {
		skin = *p.SkinID
	}
	if p.CapeID != nil {
		cape = *p.CapeID
	}
	return map[string]any{"pid": p.ID, "uid": p.UserID, "name": p.Name, "tid_skin": skin, "tid_cape": cape,
		"last_modified": bsFormat(&p.UpdatedAt, loc)}
}

func bsTexture(t *ent.Texture, loc *time.Location) map[string]any {
	uploader := int64(0)
	if t.UploaderID != nil {
		uploader = *t.UploaderID
	}
	return map[string]any{"tid": t.ID, "name": t.Name, "type": string(t.Type), "hash": t.Hash,
		"size": (t.Size + 1023) / 1024, "uploader": uploader, "public": t.Visibility == tex.VisibilityPublic,
		"upload_at": bsFormat(&t.CreatedAt, loc), "likes": t.Likes}
}

func bsClosetItem(it *ent.ClosetItem, loc *time.Location) map[string]any {
	out := bsTexture(it.Edges.Texture, loc)
	out["pivot"] = map[string]any{"user_uid": it.UserID, "texture_tid": it.TextureID, "item_name": it.Name}
	return out
}

// ---- Routes ----

func mountBSAPI(r chi.Router, d Deps) {
	loc := func() *time.Location { return settings.Location(d.Settings.Current()) }
	oneOf := func(names ...string) []string { return names }

	r.With(openCORS).HandleFunc("/api", func(w http.ResponseWriter, req *http.Request) {
		snap := d.Settings.Current()
		bsWrite(w, http.StatusOK, map[string]any{"blessing_skin": BSCompatVersion, "spec": 0,
			"copyright": "Powered by SkinLoom.", "site_name": snap.Localized("site.name", requestLocale(req)),
			"skinloom": buildinfo.Version})
	})

	r.Group(func(r chi.Router) {
		r.Use(openCORS)
		mountBSAdminAPI(r, d, loc)
		// Preflights for these paths only: a catch-all under /api would also answer
		// for /api/v1, whose CSRF defense is that no other origin is ever approved.
		for _, p := range []string{"/api/user", "/api/players", "/api/closet", "/api/admin"} {
			r.Options(p, func(http.ResponseWriter, *http.Request) {})
			r.Options(p+"/*", func(http.ResponseWriter, *http.Request) {})
		}

		r.Get("/api/user", bsRoute(d, oneOf("User.Read"), func(w http.ResponseWriter, _ *http.Request, p *oauth.Principal, _ string) {
			bsWrite(w, http.StatusOK, bsUser(p.User, loc()))
		}))

		r.Get("/api/user/notifications", bsRoute(d, oneOf("Notification.Read"), func(w http.ResponseWriter, req *http.Request, p *oauth.Principal, _ string) {
			list, _, err := d.Notify.List(req.Context(), p.User.ID, notify.Filter{Unread: true, Limit: 100})
			if err != nil {
				bsServerError(w, d, err)
				return
			}
			out := make([]map[string]any, len(list))
			for i, n := range list {
				out[i] = map[string]any{"id": strconv.FormatInt(n.ID, 10), "title": n.Title}
			}
			bsWrite(w, http.StatusOK, out)
		}))

		r.Post("/api/user/notifications/{id}", bsRoute(d, oneOf("Notification.Read"), func(w http.ResponseWriter, req *http.Request, p *oauth.Principal, locale string) {
			id, err := strconv.ParseInt(chi.URLParam(req, "id"), 10, 64)
			var n *ent.Notification
			if err == nil {
				n, err = d.DB.Ent.Notification.Get(req.Context(), id)
			}
			if err != nil || n.UserID != p.User.ID {
				if err != nil && !ent.IsNotFound(err) && !errors.Is(err, strconv.ErrSyntax) && !errors.Is(err, strconv.ErrRange) {
					bsServerError(w, d, err)
					return
				}
				bsFail(w, http.StatusNotFound, bsText(locale, "notification.none")) // Blessing Skin: 500 (C15)
				return
			}
			if err := d.Notify.MarkRead(req.Context(), p.User.ID, id); err != nil && !errors.Is(err, notify.ErrNotFound) {
				bsServerError(w, d, err)
				return
			}
			bsWrite(w, http.StatusOK, map[string]any{"title": n.Title, "content": notify.Markdown(n.Content),
				"time": n.CreatedAt.In(loc()).Format(bsTime)})
		}))

		r.Get("/api/players", bsRoute(d, oneOf("Player.Read"), func(w http.ResponseWriter, req *http.Request, p *oauth.Principal, _ string) {
			list, err := d.Players.List(req.Context(), p.User.ID)
			if err != nil {
				bsServerError(w, d, err)
				return
			}
			out := make([]map[string]any, len(list))
			for i, pl := range list {
				out[i] = bsPlayer(pl, loc())
			}
			bsWrite(w, http.StatusOK, out)
		}))

		r.Post("/api/players", bsRoute(d, oneOf("Player.ReadWrite"), bsVerified(d, func(w http.ResponseWriter, req *http.Request, p *oauth.Principal, locale string) {
			in := readBSInput(w, req)
			name := in.str("name")
			if name == "" {
				bsInvalid(w, locale, []*validate.FieldError{{Field: "name", Message: "required"}})
				return
			}
			snap := d.Settings.Current()
			pl, _, err := d.Players.Create(req.Context(), p.User.ID, name, player.PolicyFrom(snap), score.RatesFrom(snap))
			switch {
			case errors.Is(err, score.ErrInsufficient):
				bsResult(w, 7, bsText(locale, "player.lackScore"), nil)
			case len(validate.FieldErrors(err)) > 0:
				bsInvalid(w, locale, validate.FieldErrors(err))
			case err != nil:
				bsServerError(w, d, err)
			default:
				bsResult(w, 0, bsText(locale, "player.added", "name", pl.Name), bsPlayer(pl, loc()))
			}
		})))

		// ownPlayer loads {pid}: 404 if there is none, 403 if it is someone else's.
		ownPlayer := func(w http.ResponseWriter, req *http.Request, p *oauth.Principal, locale string) *ent.Player {
			id, err := strconv.ParseInt(chi.URLParam(req, "pid"), 10, 64)
			var pl *ent.Player
			if err == nil {
				pl, err = d.Players.Get(req.Context(), id)
			}
			switch {
			case err != nil && !errors.Is(err, player.ErrNotFound) && !errors.Is(err, strconv.ErrSyntax) && !errors.Is(err, strconv.ErrRange):
				bsServerError(w, d, err)
			case err != nil:
				bsFail(w, http.StatusNotFound, bsText(locale, "notFound"))
			case pl.UserID != p.User.ID:
				bsWrite(w, http.StatusForbidden, map[string]any{"code": 1, "message": bsText(locale, "player.notYours")})
			default:
				return pl
			}
			return nil
		}

		r.Delete("/api/players/{pid}", bsRoute(d, oneOf("Player.ReadWrite"), bsVerified(d, func(w http.ResponseWriter, req *http.Request, p *oauth.Principal, locale string) {
			pl := ownPlayer(w, req, p, locale)
			if pl == nil {
				return
			}
			if _, err := d.Players.Delete(req.Context(), p.User.ID, pl.ID, score.RatesFrom(d.Settings.Current()).ReturnOnDelete, nil); err != nil {
				bsServerError(w, d, err)
				return
			}
			bsResult(w, 0, bsText(locale, "player.deleted", "name", pl.Name), nil)
		})))

		r.Put("/api/players/{pid}/name", bsRoute(d, oneOf("Player.ReadWrite"), bsVerified(d, func(w http.ResponseWriter, req *http.Request, p *oauth.Principal, locale string) {
			pl := ownPlayer(w, req, p, locale)
			if pl == nil {
				return
			}
			name := readBSInput(w, req).str("name")
			if name == "" {
				bsInvalid(w, locale, []*validate.FieldError{{Field: "name", Message: "required"}})
				return
			}
			renamed, err := d.Players.Rename(req.Context(), p.User.ID, pl.ID, name, player.PolicyFrom(d.Settings.Current()))
			switch {
			case len(validate.FieldErrors(err)) > 0:
				bsInvalid(w, locale, validate.FieldErrors(err))
			case err != nil:
				bsServerError(w, d, err)
			default:
				bsResult(w, 0, bsText(locale, "player.renamed", "old", pl.Name, "new", renamed.Name), bsPlayer(renamed, loc()))
			}
		})))

		r.Put("/api/players/{pid}/textures", bsRoute(d, oneOf("Player.ReadWrite"), bsVerified(d, func(w http.ResponseWriter, req *http.Request, p *oauth.Principal, locale string) {
			pl := ownPlayer(w, req, p, locale)
			if pl == nil {
				return
			}
			in := readBSInput(w, req)
			var slots [2]*int64
			for i, k := range []string{"skin", "cape"} {
				id, ok := in.id(k)
				if !ok {
					bsInvalid(w, locale, []*validate.FieldError{{Field: k, Message: "must be a texture id"}})
					return
				}
				if id > 0 { // Blessing Skin: an empty or 0 value leaves the slot alone
					slots[i] = &id
				}
			}
			worn, err := d.Players.Wear(req.Context(), p.User.ID, pl.ID, slots[0], slots[1], nil)
			if fields := validate.FieldErrors(err); len(fields) > 0 {
				bsResult(w, 1, fields[0].Field+": "+fields[0].Message, nil)
				return
			}
			if err != nil {
				bsServerError(w, d, err)
				return
			}
			bsResult(w, 0, bsText(locale, "player.set", "name", worn.Name), bsPlayer(worn, loc()))
		})))

		r.Delete("/api/players/{pid}/textures", bsRoute(d, oneOf("Player.ReadWrite"), bsVerified(d, func(w http.ResponseWriter, req *http.Request, p *oauth.Principal, locale string) {
			pl := ownPlayer(w, req, p, locale)
			if pl == nil {
				return
			}
			in := readBSInput(w, req)
			types := in.list("type")
			zero := int64(0)
			var slots [2]*int64
			for i, k := range []string{"skin", "cape"} {
				if in.has(k) || slices.Contains(types, k) {
					slots[i] = &zero
				}
			}
			cleared, err := d.Players.Wear(req.Context(), p.User.ID, pl.ID, slots[0], slots[1], nil)
			if err != nil {
				bsServerError(w, d, err)
				return
			}
			bsResult(w, 0, bsText(locale, "player.cleared", "name", cleared.Name), bsPlayer(cleared, loc()))
		})))

		r.Get("/api/closet", bsRoute(d, oneOf("Closet.Read"), func(w http.ResponseWriter, req *http.Request, p *oauth.Principal, _ string) {
			q := req.URL.Query()
			category := "skin"
			if q.Get("category") == "cape" {
				category = "cape"
			}
			page := bsPage(req)
			perPage, err := strconv.Atoi(q.Get("perPage"))
			if err != nil || perPage < 1 {
				perPage = 6
			}
			perPage = min(perPage, 100) // Blessing Skin took any perPage (03 §7.4)
			list, total, err := d.Textures.ClosetPage(req.Context(), p.User.ID, texture.ClosetFilter{Category: category, Query: q.Get("q")}, page, perPage)
			if err != nil {
				bsServerError(w, d, err)
				return
			}
			data := make([]map[string]any, len(list))
			for i, it := range list {
				data[i] = bsClosetItem(it, loc())
			}
			bsWrite(w, http.StatusOK, bsPaginator(siteBase(req, d)+"/api/closet", data, total, page, perPage))
		}))

		r.Post("/api/closet", bsRoute(d, oneOf("Closet.ReadWrite"), func(w http.ResponseWriter, req *http.Request, p *oauth.Principal, locale string) {
			in := readBSInput(w, req)
			tid, ok := in.id("tid")
			if !ok || tid == 0 {
				bsInvalid(w, locale, []*validate.FieldError{{Field: "tid", Message: "required, an integer"}})
				return
			}
			v := texture.Viewer{UserID: p.User.ID, Admin: p.User.Role == user.RoleAdmin || p.User.Role == user.RoleSuperadmin}
			item, _, err := d.Textures.Favorite(req.Context(), v, tid, in.str("name"), score.RatesFrom(d.Settings.Current()))
			switch {
			case errors.Is(err, texture.ErrNotFound):
				bsResult(w, 1, bsText(locale, "closet.notFound"), nil)
			case errors.Is(err, texture.ErrPrivate):
				bsResult(w, 1, bsText(locale, "closet.private"), nil)
			case errors.Is(err, texture.ErrInCloset):
				bsResult(w, 1, bsText(locale, "closet.repeated"), nil)
			case errors.Is(err, score.ErrInsufficient):
				bsResult(w, 1, bsText(locale, "closet.lackScore"), nil)
			case len(validate.FieldErrors(err)) > 0:
				bsInvalid(w, locale, validate.FieldErrors(err))
			case err != nil:
				bsServerError(w, d, err)
			default:
				bsResult(w, 0, bsText(locale, "closet.added", "name", item.Name), nil)
			}
		}))

		r.Put("/api/closet/{tid}", bsRoute(d, oneOf("Closet.ReadWrite"), func(w http.ResponseWriter, req *http.Request, p *oauth.Principal, locale string) {
			tid, err := strconv.ParseInt(chi.URLParam(req, "tid"), 10, 64)
			if err != nil {
				bsResult(w, 1, bsText(locale, "closet.notInIt"), nil)
				return
			}
			name := readBSInput(w, req).str("name")
			if name == "" {
				bsInvalid(w, locale, []*validate.FieldError{{Field: "name", Message: "required"}})
				return
			}
			item, err := d.Textures.RenameClosetItem(req.Context(), p.User.ID, tid, name)
			switch {
			case errors.Is(err, texture.ErrNotInCloset):
				bsResult(w, 1, bsText(locale, "closet.notInIt"), nil)
			case len(validate.FieldErrors(err)) > 0:
				bsInvalid(w, locale, validate.FieldErrors(err))
			case err != nil:
				bsServerError(w, d, err)
			default:
				bsResult(w, 0, bsText(locale, "closet.renamed", "name", item.Name), nil)
			}
		}))

		r.Delete("/api/closet/{tid}", bsRoute(d, oneOf("Closet.ReadWrite"), func(w http.ResponseWriter, req *http.Request, p *oauth.Principal, locale string) {
			tid, err := strconv.ParseInt(chi.URLParam(req, "tid"), 10, 64)
			if err == nil {
				_, err = d.Textures.Unfavorite(req.Context(), p.User.ID, tid, score.RatesFrom(d.Settings.Current()).ReturnOnDelete, nil)
			}
			switch {
			case err == nil:
				bsResult(w, 0, bsText(locale, "closet.removed"), nil)
			case errors.Is(err, texture.ErrNotInCloset), errors.Is(err, texture.ErrNotFound), errors.Is(err, strconv.ErrSyntax), errors.Is(err, strconv.ErrRange):
				bsResult(w, 1, bsText(locale, "closet.notInIt"), nil)
			default:
				bsServerError(w, d, err)
			}
		}))
	})
}

// bsPaginator is Laravel's length-aware paginator JSON.
func bsPaginator(path string, data []map[string]any, total, page, perPage int) map[string]any {
	last := max((total+perPage-1)/perPage, 1)
	pageURL := func(n int) any {
		if n < 1 || n > last {
			return nil
		}
		return path + "?page=" + strconv.Itoa(n)
	}
	from, to := any(nil), any(nil)
	if len(data) > 0 {
		from, to = (page-1)*perPage+1, (page-1)*perPage+len(data)
	}
	return map[string]any{"current_page": page, "data": data, "first_page_url": pageURL(1), "from": from,
		"last_page": last, "last_page_url": pageURL(last), "next_page_url": pageURL(page + 1), "path": path,
		"per_page": perPage, "prev_page_url": pageURL(page - 1), "to": to, "total": total}
}

// bsPage reads ?page= (from 1). It is capped so that (page-1)*perPage cannot overflow
// into a negative offset (the first page again, or a database error).
func bsPage(r *http.Request) int {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	return min(max(page, 1), math.MaxInt32)
}
