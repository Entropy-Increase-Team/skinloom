package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	tex "github.com/Entropy-Increase-Team/skinloom/ent/texture"
	"github.com/Entropy-Increase-Team/skinloom/internal/account"
	"github.com/Entropy-Increase-Team/skinloom/internal/report"
	"github.com/Entropy-Increase-Team/skinloom/internal/score"
)

// ReportView is a report as its reporter sees it.
type ReportView struct {
	ID        int64        `json:"id"`
	Texture   *TextureView `json:"texture,omitempty" doc:"Absent once the texture is deleted or no longer public"`
	Reason    string       `json:"reason"`
	Status    string       `json:"status" enum:"pending,resolved,rejected"`
	Action    string       `json:"action,omitempty" enum:"delete,ban,reject," doc:"What the reviewer did"`
	CreatedAt time.Time    `json:"createdAt"`
	HandledAt *time.Time   `json:"handledAt,omitempty"`
}

func reportView(r *ent.Report) ReportView {
	v := ReportView{ID: r.ID, Reason: r.Reason, Status: string(r.Status), CreatedAt: r.CreatedAt, HandledAt: r.HandledAt}
	if r.Action != nil {
		v.Action = string(*r.Action)
	}
	if t := r.Edges.Texture; t != nil && t.Visibility == tex.VisibilityPublic {
		tv := textureView(t)
		v.Texture = &tv
	}
	return v
}

// ReportPage is one page of the signed-in user's reports.
type ReportPage struct {
	Reports []ReportView `json:"reports"`
	Next    int64        `json:"next" doc:"Pass as before= for the next page; 0 = last page"`
}

// Reported answers a new report.
type Reported struct {
	Report ReportView `json:"report"`
	Score  int64      `json:"score" doc:"Your score after score.reporter_modification"`
}

// AdminUserRef names a user in the admin panel.
type AdminUserRef struct {
	ID       int64  `json:"id"`
	Nickname string `json:"nickname"`
	Email    string `json:"email"`
	Status   string `json:"status" enum:"active,banned,deleted"`
}

func adminUserRef(u *ent.User) *AdminUserRef {
	if u == nil {
		return nil
	}
	return &AdminUserRef{ID: u.ID, Nickname: u.Nickname, Email: u.Email, Status: string(u.Status)}
}

// AdminReportView is a report in the review queue.
type AdminReportView struct {
	ID        int64         `json:"id"`
	Texture   *TextureView  `json:"texture,omitempty" doc:"Absent once the texture is deleted"`
	Reason    string        `json:"reason"`
	Status    string        `json:"status" enum:"pending,resolved,rejected"`
	Action    string        `json:"action,omitempty" enum:"delete,ban,reject,"`
	Reporter  *AdminUserRef `json:"reporter,omitempty" doc:"Absent once the account is gone"`
	Uploader  *AdminUserRef `json:"uploader,omitempty" doc:"The texture's uploader when it was reported"`
	Handler   *AdminUserRef `json:"handler,omitempty" doc:"Absent for the admin token"`
	CreatedAt time.Time     `json:"createdAt"`
	HandledAt *time.Time    `json:"handledAt,omitempty"`
}

func adminReportView(r *ent.Report) AdminReportView {
	v := AdminReportView{ID: r.ID, Reason: r.Reason, Status: string(r.Status), CreatedAt: r.CreatedAt, HandledAt: r.HandledAt,
		Reporter: adminUserRef(r.Edges.Reporter), Uploader: adminUserRef(r.Edges.Uploader), Handler: adminUserRef(r.Edges.Handler)}
	if r.Action != nil {
		v.Action = string(*r.Action)
	}
	if t := r.Edges.Texture; t != nil {
		tv := textureView(t)
		v.Texture = &tv
	}
	return v
}

// AdminReportPage is one page of the review queue.
type AdminReportPage struct {
	Reports []AdminReportView `json:"reports"`
	Next    int64             `json:"next" doc:"Pass as before= for the next page; 0 = last page"`
}

type reportTextureInput struct {
	ID   int64 `path:"id"`
	Body struct {
		Reason string `json:"reason" maxLength:"2000" doc:"Up to 500 characters"`
	}
}

type pageInput struct {
	Before int64 `query:"before" minimum:"0"`
	Limit  int   `query:"limit" minimum:"0" maximum:"100"`
}

type listReportsInput struct {
	Authorization string `header:"Authorization" doc:"Bearer <admin token>; not needed with an admin session"`
	Status        string `query:"status" enum:"pending,resolved,rejected,"`
	Before        int64  `query:"before" minimum:"0"`
	Limit         int    `query:"limit" minimum:"0" maximum:"100"`
}

type reviewReportInput struct {
	Authorization string `header:"Authorization" doc:"Bearer <admin token>; not needed with an admin session"`
	ID            int64  `path:"id"`
	Body          struct {
		Action string `json:"action" enum:"delete,ban,reject" doc:"delete the texture, ban its uploader, or reject the report"`
	}
}

func reportError(d Deps, err error) error {
	switch {
	case errors.Is(err, report.ErrNotFound):
		return huma.Error404NotFound("no such report")
	case errors.Is(err, report.ErrDuplicate):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, report.ErrOwnTexture):
		return huma.Error403Forbidden(err.Error())
	case errors.Is(err, report.ErrReviewed), errors.Is(err, report.ErrNoUploader):
		return huma.Error409Conflict(err.Error())
	case errors.Is(err, account.ErrForbidden):
		return huma.Error403Forbidden(err.Error())
	}
	return textureError(d, err)
}

// reportsPerUser bounds reports per user in an hour: each one waits for a person.
const reportsPerUser = 20

func registerReports(api huma.API, d Deps) {
	submits := account.NewLimiter(reportsPerUser, time.Hour)

	huma.Register(api, huma.Operation{OperationID: "reportTexture", Method: http.MethodPost, Path: "/api/v1/textures/{id}/reports",
		Summary: "Report a texture", Tags: []string{"reports"}, Security: sessionSecurity},
		func(ctx context.Context, in *reportTextureInput) (*struct{ Body Reported }, error) {
			v, err := signedIn(ctx)
			if err != nil {
				return nil, err
			}
			key := strconv.FormatInt(v.User.ID, 10)
			if wait, blocked := submits.Blocked(key); blocked {
				return nil, rateLimitError(&account.RateLimitError{RetryAfter: wait}, "too many reports, try again later")
			}
			submits.Fail(key)
			rep, balance, err := d.Reports.Submit(ctx, textureViewer(ctx), in.ID, in.Body.Reason, score.RatesFrom(d.Settings.Current()))
			if err != nil {
				return nil, reportError(d, err)
			}
			if rep, err = d.Reports.Get(ctx, rep.ID); err != nil {
				return nil, err
			}
			return &struct{ Body Reported }{Reported{Report: reportView(rep), Score: balance}}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "listMyReports", Method: http.MethodGet, Path: "/api/v1/me/reports",
		Summary: "Your reports, newest first", Tags: []string{"reports"}, Security: sessionSecurity},
		func(ctx context.Context, in *pageInput) (*struct{ Body ReportPage }, error) {
			v, err := signedIn(ctx)
			if err != nil {
				return nil, err
			}
			list, next, err := d.Reports.List(ctx, report.Filter{ReporterID: v.User.ID, Before: in.Before, Limit: in.Limit})
			if err != nil {
				return nil, err
			}
			page := ReportPage{Reports: make([]ReportView, 0, len(list)), Next: next}
			for _, r := range list {
				page.Reports = append(page.Reports, reportView(r))
			}
			return &struct{ Body ReportPage }{page}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "listReports", Method: http.MethodGet, Path: "/api/v1/admin/reports",
		Summary: "The review queue, newest first", Tags: []string{"admin"}, Security: adminSecurity},
		func(ctx context.Context, in *listReportsInput) (*struct{ Body AdminReportPage }, error) {
			if _, err := adminActor(ctx, d, in.Authorization); err != nil {
				return nil, err
			}
			list, next, err := d.Reports.List(ctx, report.Filter{Status: in.Status, Before: in.Before, Limit: in.Limit})
			if err != nil {
				return nil, err
			}
			page := AdminReportPage{Reports: make([]AdminReportView, 0, len(list)), Next: next}
			for _, r := range list {
				page.Reports = append(page.Reports, adminReportView(r))
			}
			return &struct{ Body AdminReportPage }{page}, nil
		})

	huma.Register(api, huma.Operation{OperationID: "reviewReport", Method: http.MethodPost, Path: "/api/v1/admin/reports/{id}/review",
		Summary: "Uphold or reject a report", Tags: []string{"admin"}, Security: adminSecurity,
		Description: "delete deletes the texture (closets refunded as score.return_on_delete says); ban bans the uploader, " +
			"if you may manage them. Both give the reporter back a charge and score.reporter_reward; reject takes a submit reward back."},
		func(ctx context.Context, in *reviewReportInput) (*struct{ Body AdminReportView }, error) {
			a, err := adminActor(ctx, d, in.Authorization)
			if err != nil {
				return nil, err
			}
			rep, err := reviewReport(ctx, d, a, in.ID, in.Body.Action)
			if err != nil {
				return nil, reportError(d, err)
			}
			return &struct{ Body AdminReportView }{adminReportView(rep)}, nil
		})
}

// reviewReport upholds or rejects a report for an administrator (both APIs), and
// records it in the audit log.
func reviewReport(ctx context.Context, d Deps, a account.Actor, id int64, action string) (*ent.Report, error) {
	info := requestInfo(ctx)
	ban := func(ctx context.Context, uploader int64) error {
		banned := "banned"
		_, err := d.Accounts.AdminUpdateUser(ctx, a, uploader, account.AdminUpdate{Status: &banned}, info.meta(""))
		if errors.Is(err, account.ErrNotFound) {
			return report.ErrNoUploader
		}
		return err
	}
	r := score.RatesFrom(d.Settings.Current())
	handler := actorIDOf(a)
	rep, err := d.Reports.Review(ctx, handler, id, action, r, r.ReturnOnDelete, ban)
	if err != nil {
		return nil, err
	}
	meta := map[string]any{"action": action}
	if rep.UploaderID != nil {
		meta["uploader"] = *rep.UploaderID
	}
	if err := d.Accounts.Audit(ctx, handler, "admin.report_reviewed", "report", strconv.FormatInt(rep.ID, 10), info.client.ClientIP, meta); err != nil {
		d.Log.Warn("audit", "action", "admin.report_reviewed", "err", err)
	}
	return rep, nil
}
