package httpapi

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/go-chi/chi/v5"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	entreport "github.com/Entropy-Increase-Team/skinloom/ent/report"
	"github.com/Entropy-Increase-Team/skinloom/internal/account"
	"github.com/Entropy-Increase-Team/skinloom/internal/oauth"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/validate"
	"github.com/Entropy-Increase-Team/skinloom/internal/player"
	"github.com/Entropy-Increase-Team/skinloom/internal/report"
	"github.com/Entropy-Increase-Team/skinloom/internal/score"
	"github.com/Entropy-Increase-Team/skinloom/internal/texture"
)

// The administration part of the Blessing Skin compatible resource API (/api/admin/*;
// docs/design/02 §2.6, L-B). Tokens need an administration scope, which only an
// administrator can grant and which works only while that user is still one; the
// same rules as the admin pages decide whom they may manage, through the same
// services, and the same audit entries are written.

// Page sizes of Blessing Skin's lists.
const (
	bsUsersPerPage   = 10
	bsPlayersPerPage = 10
	bsReportsPerPage = 9
	bsClosetMax      = 1000 // Blessing Skin listed a whole closet; this bounds it
)

func init() {
	for k, v := range map[string][2]string{
		"admin.noPermission":  {"你没有权限这么做。", "You have no permission to do this."},
		"admin.userNotFound":  {"用户不存在。", "The user does not exist."},
		"admin.done":          {"操作成功。", "Done."},
		"admin.emailSet":      {"邮箱已修改。", "The email was changed."},
		"admin.verification":  {"邮箱验证状态已修改。", "The verification status was changed."},
		"admin.nicknameSet":   {"昵称已改为 :new。", "The nickname is now :new."},
		"admin.passwordSet":   {"密码已修改。", "The password was changed."},
		"admin.scoreSet":      {"积分已修改。", "The score was changed."},
		"admin.permissionSet": {"权限已修改。", "The permission was changed."},
		"admin.userDeleted":   {"用户已删除。", "The user was deleted."},
		"admin.lastSuper":     {"这是最后一位超级管理员。", "This is the last super administrator."},
		"admin.playerRenamed": {"角色已改名为 :player。", "The player is now :player."},
		"admin.ownerSet":      {"角色 :player 已转给 :user。", "Player :player now belongs to :user."},
		"admin.texturesSet":   {"角色 :player 的材质已修改。", "Textures of :player were changed."},
		"admin.playerDeleted": {"角色已删除。", "The player was deleted."},
		"admin.textureNone":   {"材质 :tid 不存在。", "Texture :tid does not exist."},
		"admin.reviewed":      {"举报已处理。", "The report was handled."},
		"admin.reportDone":    {"这条举报已经处理过了。", "The report has been handled already."},
		"admin.noUploader":    {"上传者的账号已不存在。", "The uploader's account no longer exists."},
		"admin.sent":          {"通知已发送。", "The notification was sent."},
	} {
		bsMessages[k] = v
	}
}

// bsSearch reads Blessing Skin's search string ("uid:3", "email:a@b", or plain
// text) as a field and a value; only the first term counts.
func bsSearch(q string) (field, value string) {
	terms := strings.Fields(q)
	if len(terms) == 0 {
		return "", ""
	}
	if f, v, ok := strings.Cut(terms[0], ":"); ok && f != "" {
		return strings.ToLower(f), strings.Trim(v, `"'`)
	}
	return "", strings.TrimSpace(q)
}

// bsAdminFailed answers an administration error in Blessing Skin's way.
func bsAdminFailed(w http.ResponseWriter, d Deps, locale string, err error) {
	switch {
	case errors.Is(err, account.ErrForbidden):
		bsWrite(w, http.StatusForbidden, map[string]any{"code": 1, "message": bsText(locale, "admin.noPermission")})
	case errors.Is(err, account.ErrLastSuperadmin):
		bsWrite(w, http.StatusForbidden, map[string]any{"code": 1, "message": bsText(locale, "admin.lastSuper")})
	case errors.Is(err, account.ErrNotFound), errors.Is(err, player.ErrNotFound), errors.Is(err, report.ErrNotFound):
		bsFail(w, http.StatusNotFound, bsText(locale, "notFound"))
	case len(validate.FieldErrors(err)) > 0:
		bsInvalid(w, locale, validate.FieldErrors(err))
	default:
		var he huma.StatusError
		if errors.As(err, &he) && he.GetStatus() < 500 {
			bsFromHuma(w, locale, err)
			return
		}
		bsServerError(w, d, err)
	}
}

// bsFromHuma turns an error of a helper shared with /api/v1 into Blessing Skin's shape.
func bsFromHuma(w http.ResponseWriter, locale string, err error) {
	var em *huma.ErrorModel
	if !errors.As(err, &em) {
		bsFail(w, http.StatusBadRequest, err.Error())
		return
	}
	if em.Status == http.StatusUnprocessableEntity {
		fields := []*validate.FieldError{}
		for _, e := range em.Errors {
			fields = append(fields, &validate.FieldError{Field: strings.TrimPrefix(e.Location, "body."), Message: e.Message})
		}
		bsInvalid(w, locale, fields)
		return
	}
	bsFail(w, em.Status, em.Detail)
}

func bsAdminUser(u *ent.User, loc *time.Location) map[string]any {
	out := bsUser(u, loc)
	out["ip"] = u.RegisterIP
	return out
}

func bsReportStatus(s entreport.Status) int {
	switch s {
	case entreport.StatusResolved:
		return 1
	case entreport.StatusRejected:
		return 2
	}
	return 0
}

func bsReport(r *ent.Report, loc *time.Location) map[string]any {
	id := func(p *int64) int64 {
		if p == nil {
			return 0
		}
		return *p
	}
	out := map[string]any{"id": r.ID, "tid": id(r.TextureID), "uploader": id(r.UploaderID), "reporter": id(r.ReporterID),
		"reason": r.Reason, "status": bsReportStatus(r.Status), "report_at": bsFormat(&r.CreatedAt, loc),
		"texture": nil, "texture_uploader": nil, "informer": nil}
	if t := r.Edges.Texture; t != nil {
		out["texture"] = bsTexture(t, loc)
	}
	if u := r.Edges.Uploader; u != nil {
		out["texture_uploader"] = bsAdminUser(u, loc)
	}
	if u := r.Edges.Reporter; u != nil {
		out["informer"] = bsAdminUser(u, loc)
	}
	return out
}

func mountBSAdminAPI(r chi.Router, d Deps, loc func() *time.Location) {
	oneOf := func(names ...string) []string { return names }
	actor := func(p *oauth.Principal) account.Actor { return &account.Viewer{User: p.User} }
	pathID := func(req *http.Request, name string) (int64, bool) {
		id, err := strconv.ParseInt(chi.URLParam(req, name), 10, 64)
		return id, err == nil && id > 0
	}
	// target loads the {uid} user, answering 404 itself when there is none.
	target := func(w http.ResponseWriter, req *http.Request, locale string) *ent.User {
		id, ok := pathID(req, "uid")
		var u *ent.User
		var err error = account.ErrNotFound
		if ok {
			u, err = d.Accounts.GetUser(req.Context(), id)
		}
		if err != nil {
			bsAdminFailed(w, d, locale, err)
			return nil
		}
		return u
	}

	// ---- Users ----

	r.Get("/api/admin/users", bsRoute(d, oneOf("UsersManagement.Read"), func(w http.ResponseWriter, req *http.Request, _ *oauth.Principal, _ string) {
		page := bsPage(req)
		f := account.UserFilter{Offset: (page - 1) * bsUsersPerPage, Limit: bsUsersPerPage}
		_, f.Query = bsSearch(req.URL.Query().Get("q"))
		list, _, err := d.Accounts.ListUsers(req.Context(), f)
		var total int
		if err == nil {
			total, err = d.Accounts.CountUsers(req.Context(), f)
		}
		if err != nil {
			bsServerError(w, d, err)
			return
		}
		data := make([]map[string]any, len(list))
		for i, u := range list {
			data[i] = bsAdminUser(u, loc())
		}
		bsWrite(w, http.StatusOK, bsPaginator(siteBase(req, d)+"/api/admin/users", data, total, page, bsUsersPerPage))
	}))

	// update applies one change to {uid} and answers with msg.
	update := func(name string, build func(in bsInput, u *ent.User) (account.AdminUpdate, []*validate.FieldError), msg func(locale string, u *ent.User) string) {
		r.Put("/api/admin/users/{uid}/"+name, bsRoute(d, oneOf("UsersManagement.ReadWrite"), func(w http.ResponseWriter, req *http.Request, p *oauth.Principal, locale string) {
			u := target(w, req, locale)
			if u == nil {
				return
			}
			up, invalid := build(readBSInput(w, req), u)
			if len(invalid) > 0 {
				bsInvalid(w, locale, invalid)
				return
			}
			changed, err := d.Accounts.AdminUpdateUser(req.Context(), actor(p), u.ID, up, requestInfo(req.Context()).meta(""))
			if err != nil {
				bsAdminFailed(w, d, locale, err)
				return
			}
			bsResult(w, 0, msg(locale, changed), nil)
		}))
	}
	required := func(field string) []*validate.FieldError {
		return []*validate.FieldError{{Field: field, Message: "required"}}
	}
	text := func(key string) func(string, *ent.User) string {
		return func(locale string, _ *ent.User) string { return bsText(locale, key) }
	}
	update("email", func(in bsInput, _ *ent.User) (account.AdminUpdate, []*validate.FieldError) {
		email := in.str("email")
		if email == "" {
			return account.AdminUpdate{}, required("email")
		}
		return account.AdminUpdate{Email: &email}, nil
	}, text("admin.emailSet"))
	update("verification", func(_ bsInput, u *ent.User) (account.AdminUpdate, []*validate.FieldError) {
		flip := u.EmailVerifiedAt == nil // Blessing Skin toggles it
		return account.AdminUpdate{EmailVerified: &flip}, nil
	}, text("admin.verification"))
	update("nickname", func(in bsInput, _ *ent.User) (account.AdminUpdate, []*validate.FieldError) {
		nick := in.str("nickname")
		if nick == "" {
			return account.AdminUpdate{}, required("nickname")
		}
		return account.AdminUpdate{Nickname: &nick}, nil
	}, func(locale string, u *ent.User) string { return bsText(locale, "admin.nicknameSet", "new", u.Nickname) })
	update("password", func(in bsInput, _ *ent.User) (account.AdminUpdate, []*validate.FieldError) {
		pw := in.str("password")
		if pw == "" {
			return account.AdminUpdate{}, required("password")
		}
		return account.AdminUpdate{Password: &pw}, nil
	}, text("admin.passwordSet"))
	update("score", func(in bsInput, _ *ent.User) (account.AdminUpdate, []*validate.FieldError) {
		n, ok := in.id("score")
		if !ok || !in.has("score") {
			return account.AdminUpdate{}, []*validate.FieldError{{Field: "score", Message: "required, a whole number of at least 0"}}
		}
		return account.AdminUpdate{Score: &n}, nil
	}, text("admin.scoreSet"))
	update("permission", func(in bsInput, u *ent.User) (account.AdminUpdate, []*validate.FieldError) {
		// Blessing Skin's permission: -1 banned, 0 user, 1 administrator (2, super
		// administrator, cannot be given here).
		var role, status string
		switch strings.TrimSpace(in.str("permission")) {
		case "-1":
			status = "banned"
		case "0":
			role, status = "user", "active"
		case "1":
			role, status = "admin", "active"
		default:
			return account.AdminUpdate{}, []*validate.FieldError{{Field: "permission", Message: "must be -1, 0 or 1"}}
		}
		var up account.AdminUpdate
		if role != "" && role != string(u.Role) {
			up.Role = &role
		}
		if status != string(u.Status) {
			up.Status = &status
		}
		return up, nil
	}, text("admin.permissionSet"))

	r.Delete("/api/admin/users/{uid}", bsRoute(d, oneOf("UsersManagement.ReadWrite"), func(w http.ResponseWriter, req *http.Request, p *oauth.Principal, locale string) {
		u := target(w, req, locale)
		if u == nil {
			return
		}
		if err := d.Accounts.AdminDeleteUser(req.Context(), actor(p), u.ID, requestInfo(req.Context()).meta("")); err != nil {
			bsAdminFailed(w, d, locale, err)
			return
		}
		bsResult(w, 0, bsText(locale, "admin.userDeleted"), nil)
	}))

	// ---- Players ----

	r.Get("/api/admin/players", bsRoute(d, oneOf("PlayersManagement.Read"), func(w http.ResponseWriter, req *http.Request, _ *oauth.Principal, _ string) {
		page := bsPage(req)
		f := player.Filter{Offset: (page - 1) * bsPlayersPerPage, Limit: bsPlayersPerPage}
		switch field, value := bsSearch(req.URL.Query().Get("q")); field {
		case "uid":
			f.UserID, _ = strconv.ParseInt(value, 10, 64)
			if f.UserID == 0 {
				f.UserID = -1 // matches nobody, like an unknown uid
			}
		default:
			f.Query = value
		}
		var list []*ent.Player
		var total int
		var err error
		if f.UserID >= 0 {
			if list, _, err = d.Players.Search(req.Context(), f); err == nil {
				total, err = d.Players.Count(req.Context(), f)
			}
		}
		if err != nil {
			bsServerError(w, d, err)
			return
		}
		data := make([]map[string]any, len(list))
		for i, pl := range list {
			data[i] = bsPlayer(pl, loc())
		}
		bsWrite(w, http.StatusOK, bsPaginator(siteBase(req, d)+"/api/admin/players", data, total, page, bsPlayersPerPage))
	}))

	// managed loads {pid}, a player whose owner the token's user may manage.
	managed := func(w http.ResponseWriter, req *http.Request, p *oauth.Principal, locale string) *ent.Player {
		id, ok := pathID(req, "pid")
		var pl *ent.Player
		var err error = player.ErrNotFound
		if ok {
			pl, err = managedPlayer(req.Context(), d, actor(p), id)
		}
		if err != nil {
			bsAdminFailed(w, d, locale, err)
			return nil
		}
		return pl
	}

	r.Put("/api/admin/players/{pid}/name", bsRoute(d, oneOf("PlayersManagement.ReadWrite"), func(w http.ResponseWriter, req *http.Request, p *oauth.Principal, locale string) {
		pl := managed(w, req, p, locale)
		if pl == nil {
			return
		}
		name := readBSInput(w, req).str("player_name")
		if name == "" {
			bsInvalid(w, locale, required("player_name"))
			return
		}
		if err := updatePlayerAsAdmin(req.Context(), d, actor(p), pl, &name, nil); err != nil {
			bsAdminFailed(w, d, locale, err)
			return
		}
		bsResult(w, 0, bsText(locale, "admin.playerRenamed", "player", name), nil)
	}))

	r.Put("/api/admin/players/{pid}/owner", bsRoute(d, oneOf("PlayersManagement.ReadWrite"), func(w http.ResponseWriter, req *http.Request, p *oauth.Principal, locale string) {
		pl := managed(w, req, p, locale)
		if pl == nil {
			return
		}
		uid, ok := readBSInput(w, req).id("uid")
		if !ok || uid == 0 {
			bsInvalid(w, locale, []*validate.FieldError{{Field: "uid", Message: "required, an integer"}})
			return
		}
		err := updatePlayerAsAdmin(req.Context(), d, actor(p), pl, nil, &uid)
		if errors.Is(err, account.ErrNotFound) {
			bsResult(w, 1, bsText(locale, "admin.userNotFound"), nil)
			return
		}
		if err != nil {
			bsAdminFailed(w, d, locale, err)
			return
		}
		owner, err := d.Accounts.GetUser(req.Context(), uid)
		if err != nil {
			bsServerError(w, d, err)
			return
		}
		bsResult(w, 0, bsText(locale, "admin.ownerSet", "player", pl.Name, "user", owner.Nickname), nil)
	}))

	r.Put("/api/admin/players/{pid}/textures", bsRoute(d, oneOf("PlayersManagement.ReadWrite"), func(w http.ResponseWriter, req *http.Request, p *oauth.Principal, locale string) {
		pl := managed(w, req, p, locale)
		if pl == nil {
			return
		}
		in := readBSInput(w, req)
		tid, ok := in.id("tid")
		kind := in.str("type")
		if !ok || !in.has("tid") || (kind != "skin" && kind != "cape") {
			bsInvalid(w, locale, []*validate.FieldError{{Field: "tid", Message: "required, an integer"}, {Field: "type", Message: "skin or cape"}})
			return
		}
		var skin, cape *int64
		if kind == "skin" {
			skin = &tid
		} else {
			cape = &tid
		}
		// Unlike Blessing Skin, the texture must be in the owner's closet and of the
		// right kind (C07), as on the admin pages.
		if _, err := d.Players.Wear(req.Context(), pl.UserID, pl.ID, skin, cape); err != nil {
			if fields := validate.FieldErrors(err); len(fields) > 0 {
				msg := fields[0].Field + ": " + fields[0].Message
				if fields[0].Message == "no such texture" {
					msg = bsText(locale, "admin.textureNone", "tid", strconv.FormatInt(tid, 10))
				}
				bsResult(w, 1, msg, nil)
				return
			}
			bsAdminFailed(w, d, locale, err)
			return
		}
		adminAudit(req.Context(), d, actor(p), "admin.player_textures", "player", pl.ID, map[string]any{"skin": skin, "cape": cape})
		bsResult(w, 0, bsText(locale, "admin.texturesSet", "player", pl.Name), nil)
	}))

	r.Delete("/api/admin/players/{pid}", bsRoute(d, oneOf("PlayersManagement.ReadWrite"), func(w http.ResponseWriter, req *http.Request, p *oauth.Principal, locale string) {
		pl := managed(w, req, p, locale)
		if pl == nil {
			return
		}
		if _, err := d.Players.Delete(req.Context(), pl.UserID, pl.ID, score.RatesFrom(d.Settings.Current()).ReturnOnDelete); err != nil {
			bsAdminFailed(w, d, locale, err)
			return
		}
		adminAudit(req.Context(), d, actor(p), "admin.player_deleted", "player", pl.ID, map[string]any{"name": pl.Name, "owner": pl.UserID})
		bsResult(w, 0, bsText(locale, "admin.playerDeleted"), nil)
	}))

	// ---- Closets ----

	r.Get("/api/admin/closet/{uid}", bsRoute(d, oneOf("ClosetManagement.Read"), func(w http.ResponseWriter, req *http.Request, _ *oauth.Principal, locale string) {
		u := target(w, req, locale)
		if u == nil {
			return
		}
		items, _, err := d.Textures.ClosetPage(req.Context(), u.ID, texture.ClosetFilter{}, 1, bsClosetMax)
		if err != nil {
			bsServerError(w, d, err)
			return
		}
		data := make([]map[string]any, len(items))
		for i, it := range items {
			data[i] = bsClosetItem(it, loc())
		}
		bsWrite(w, http.StatusOK, data)
	}))

	// closetChange adds a texture to {uid}'s closet or takes it out.
	closetChange := func(add bool) http.HandlerFunc {
		return bsRoute(d, oneOf("ClosetManagement.ReadWrite"), func(w http.ResponseWriter, req *http.Request, p *oauth.Principal, locale string) {
			u := target(w, req, locale)
			if u == nil {
				return
			}
			if _, err := d.Accounts.MayManageOwned(req.Context(), actor(p), u.ID); err != nil {
				bsAdminFailed(w, d, locale, err)
				return
			}
			tid, ok := readBSInput(w, req).id("tid")
			if !ok || tid == 0 {
				bsInvalid(w, locale, []*validate.FieldError{{Field: "tid", Message: "required, an integer"}})
				return
			}
			t, err := d.Textures.Get(req.Context(), tid, texture.Viewer{Admin: true})
			if err == nil {
				if add {
					_, err = d.Textures.GiveToCloset(req.Context(), u.ID, tid)
				} else {
					_, err = d.Textures.Unfavorite(req.Context(), u.ID, tid, score.RatesFrom(d.Settings.Current()).ReturnOnDelete)
				}
			}
			switch {
			case errors.Is(err, texture.ErrNotFound):
				bsResult(w, 1, bsText(locale, "closet.notFound"), nil)
			case errors.Is(err, texture.ErrInCloset):
				bsResult(w, 1, bsText(locale, "closet.repeated"), nil)
			case errors.Is(err, texture.ErrNotInCloset):
				bsResult(w, 1, bsText(locale, "closet.notInIt"), nil)
			case err != nil:
				bsAdminFailed(w, d, locale, err)
			default:
				action := "admin.closet_removed"
				if add {
					action = "admin.closet_added"
				}
				adminAudit(req.Context(), d, actor(p), action, "user", u.ID, map[string]any{"texture": tid})
				bsResult(w, 0, "", map[string]any{"user": bsAdminUser(u, loc()), "texture": bsTexture(t, loc())})
			}
		})
	}
	r.Post("/api/admin/closet/{uid}", closetChange(true))
	r.Delete("/api/admin/closet/{uid}", closetChange(false))

	// ---- Reports ----

	r.Get("/api/admin/reports", bsRoute(d, oneOf("ReportsManagement.Read"), func(w http.ResponseWriter, req *http.Request, _ *oauth.Principal, _ string) {
		page := bsPage(req)
		f := report.Filter{Offset: (page - 1) * bsReportsPerPage, Limit: bsReportsPerPage}
		field, value := bsSearch(req.URL.Query().Get("q"))
		n, _ := strconv.ParseInt(value, 10, 64)
		// A condition no report can meet matches none (as in Blessing Skin), rather
		// than being dropped and matching all.
		impossible := false
		switch field {
		case "status":
			f.Status = map[string]string{"0": "pending", "1": "resolved", "2": "rejected"}[value]
			impossible = f.Status == ""
		case "reporter":
			f.ReporterID, impossible = n, n <= 0
		case "uploader":
			f.UploaderID, impossible = n, n <= 0
		case "tid":
			f.TextureID, impossible = n, n <= 0
		}
		var (
			list  []*ent.Report
			total int
			err   error
		)
		if !impossible {
			list, _, err = d.Reports.List(req.Context(), f)
			if err == nil {
				total, err = d.Reports.Count(req.Context(), f)
			}
		}
		if err != nil {
			bsServerError(w, d, err)
			return
		}
		data := make([]map[string]any, len(list))
		for i, rep := range list {
			data[i] = bsReport(rep, loc())
		}
		bsWrite(w, http.StatusOK, bsPaginator(siteBase(req, d)+"/api/admin/reports", data, total, page, bsReportsPerPage))
	}))

	r.Put("/api/admin/reports/{id}", bsRoute(d, oneOf("ReportsManagement.ReadWrite"), func(w http.ResponseWriter, req *http.Request, p *oauth.Principal, locale string) {
		id, ok := pathID(req, "id")
		if !ok {
			bsFail(w, http.StatusNotFound, bsText(locale, "notFound"))
			return
		}
		action := readBSInput(w, req).str("action")
		if action != "delete" && action != "ban" && action != "reject" {
			bsInvalid(w, locale, []*validate.FieldError{{Field: "action", Message: "delete, ban or reject"}})
			return
		}
		rep, err := reviewReport(req.Context(), d, actor(p), id, action)
		switch {
		case errors.Is(err, report.ErrReviewed):
			bsResult(w, 1, bsText(locale, "admin.reportDone"), nil)
		case errors.Is(err, report.ErrNoUploader):
			bsResult(w, 1, bsText(locale, "admin.noUploader"), nil)
		case errors.Is(err, account.ErrForbidden):
			bsResult(w, 1, bsText(locale, "admin.noPermission"), nil) // Blessing Skin: HTTP 200 here
		case err != nil:
			bsAdminFailed(w, d, locale, err)
		default:
			bsResult(w, 0, bsText(locale, "admin.reviewed"), map[string]any{"status": bsReportStatus(rep.Status)})
		}
	}))

	// ---- Notifications ----

	r.Post("/api/admin/notifications", bsRoute(d, oneOf("Notification.ReadWrite"), func(w http.ResponseWriter, req *http.Request, p *oauth.Principal, locale string) {
		in := readBSInput(w, req)
		title := in.str("title")
		if title == "" {
			bsInvalid(w, locale, required("title"))
			return
		}
		uid, _ := in.id("uid")
		var to string
		switch in.str("receiver") {
		case "all":
			to = "all"
		case "normal":
			to = "users"
		case "uid", "email":
			to = "user"
		default:
			bsInvalid(w, locale, []*validate.FieldError{{Field: "receiver", Message: "all, normal, uid or email"}})
			return
		}
		email := ""
		if in.str("receiver") == "email" {
			email, uid = in.str("email"), 0
		}
		if _, err := sendNotification(req.Context(), d, actor(p), to, uid, email, title, in.str("content")); err != nil {
			bsAdminFailed(w, d, locale, err)
			return
		}
		// Blessing Skin answered with a redirect to /admin (C15).
		bsResult(w, 0, bsText(locale, "admin.sent"), nil)
	}))
}
