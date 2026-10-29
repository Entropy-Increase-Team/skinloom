package httpapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/account"
	"github.com/Entropy-Increase-Team/skinloom/internal/score"
	"github.com/Entropy-Increase-Team/skinloom/internal/texture"
)

// UploaderView is who uploaded a texture.
type UploaderView struct {
	ID       int64  `json:"id"`
	Nickname string `json:"nickname"`
}

// TextureView is a texture in the skin library.
type TextureView struct {
	ID        int64         `json:"id"`
	Name      string        `json:"name"`
	Type      string        `json:"type" enum:"steve,alex,cape" doc:"steve and alex are skins (classic and slim arms)"`
	Hash      string        `json:"hash" doc:"SHA-256 of the file; the image is at /textures/{hash}"`
	Size      int64         `json:"size" doc:"Bytes"`
	Public    bool          `json:"public"`
	Likes     int           `json:"likes" doc:"Closets holding it"`
	Uploader  *UploaderView `json:"uploader,omitempty" doc:"Absent when the uploader's account is gone"`
	CreatedAt time.Time     `json:"createdAt"`
}

func textureView(t *ent.Texture) TextureView {
	v := TextureView{ID: t.ID, Name: t.Name, Type: string(t.Type), Hash: t.Hash, Size: t.Size,
		Public: t.Visibility == "public", Likes: t.Likes, CreatedAt: t.CreatedAt}
	if u := t.Edges.Uploader; u != nil && u.Status != user.StatusDeleted {
		v.Uploader = &UploaderView{ID: u.ID, Nickname: u.Nickname}
	}
	return v
}

// TexturePage is one page of the skin library.
type TexturePage struct {
	Textures []TextureView `json:"textures"`
	Next     string        `json:"next" doc:"Pass as cursor= for the next page; empty = last page"`
}

// TextureDetail is a texture as shown on its page.
type TextureDetail struct {
	TextureView
	InCloset bool `json:"inCloset" doc:"In the signed-in user's closet"`
	Mine     bool `json:"mine" doc:"Uploaded by the signed-in user"`
}

// UploadView is the result of an upload.
type UploadView struct {
	Texture   TextureView `json:"texture"`
	Duplicate bool        `json:"duplicate" doc:"The same image was already in the library: this is that texture, and nothing was charged"`
	Cost      int64       `json:"cost" doc:"Score spent; negative when the upload earned score"`
	Score     int64       `json:"score" doc:"The uploader's score afterwards"`
}

// ClosetItemView is a texture in the signed-in user's closet.
type ClosetItemView struct {
	ID      int64       `json:"id"`
	Name    string      `json:"name"`
	AddedAt time.Time   `json:"addedAt"`
	Texture TextureView `json:"texture"`
}

// ClosetPage is one page of a closet.
type ClosetPage struct {
	Items []ClosetItemView `json:"items"`
	Next  int64            `json:"next" doc:"Pass as before= for the next page; 0 = last page"`
}

// ScoreRates are the score settings an uploader needs to see the cost in advance.
type ScoreRates struct {
	PerStorage        int64 `json:"perStorage" doc:"Per KB of a public upload"`
	PrivatePerStorage int64 `json:"privatePerStorage" doc:"Per KB of a private upload"`
	PerClosetItem     int64 `json:"perClosetItem"`
	AwardPerTexture   int64 `json:"awardPerTexture" doc:"Earned for a public upload"`
	// Reports (score.reporter_*): what submitting one changes, and the reward when
	// it is upheld.
	ReporterModification int64 `json:"reporterModification" doc:"Negative: a report costs this much; positive: earns it"`
	ReporterReward       int64 `json:"reporterReward"`
}

// TextureRules are the upload rules of the site.
type TextureRules struct {
	MaxSizeKB     int64      `json:"maxSizeKb"`
	MaxWidth      int        `json:"maxWidth"`
	NameRule      string     `json:"nameRule,omitempty" doc:"Regular expression names must match (RE2)"`
	AllowDownload bool       `json:"allowDownload"`
	Rates         ScoreRates `json:"rates"`
	ContentPolicy string     `json:"contentPolicy,omitempty" doc:"Markdown, in the request's language"`
}

type uploadForm struct {
	// application/octet-stream too: browsers send it for files whose type the system
	// does not know. The upload checks the bytes, whatever the declared type.
	File   huma.FormFile `form:"file" contentType:"image/png,application/octet-stream" required:"true" doc:"PNG image"`
	Name   string        `form:"name" doc:"1 to 50 characters"`
	Type   string        `form:"type" doc:"steve, alex or cape"`
	Public bool          `form:"public" doc:"Shown in the skin library; private textures cost more"`
}

type uploadInput struct {
	RawBody huma.MultipartFormFiles[uploadForm]
}

type listTexturesInput struct {
	Category string `query:"category" enum:"skin,steve,alex,cape," doc:"skin = steve and alex"`
	Query    string `query:"q" maxLength:"50" doc:"Name fragment"`
	Uploader int64  `query:"uploader" minimum:"0"`
	Sort     string `query:"sort" enum:"time,likes," doc:"time (default, newest first) or likes"`
	Cursor   string `query:"cursor" maxLength:"40"`
	Limit    int    `query:"limit" minimum:"0" maximum:"100"`
}

type textureIDInput struct {
	ID int64 `path:"id"`
}

type closetInput struct {
	Category string `query:"category" enum:"skin,cape,"`
	Query    string `query:"q" maxLength:"50" doc:"Item name fragment"`
	Before   int64  `query:"before" minimum:"0"`
	Limit    int    `query:"limit" minimum:"0" maximum:"100"`
}

// textureViewer describes the signed-in user for visibility checks.
func textureViewer(ctx context.Context) texture.Viewer {
	if v := requestInfo(ctx).viewer(); v != nil {
		return texture.Viewer{UserID: v.User.ID, Admin: v.IsAdmin()}
	}
	return texture.Viewer{}
}

// textureError maps texture errors; a private texture answers with the site's
// texture.private_status.
func textureError(d Deps, err error) error {
	switch {
	case errors.Is(err, texture.ErrNotFound):
		return huma.Error404NotFound("no such texture")
	case errors.Is(err, texture.ErrPrivate):
		if texture.PolicyFrom(d.Settings.Current()).PrivateStatus == http.StatusNotFound {
			return huma.Error404NotFound("no such texture")
		}
		return huma.Error403Forbidden("this texture is private")
	case errors.Is(err, score.ErrInsufficient):
		return huma.NewError(http.StatusPaymentRequired, "not enough score")
	}
	return apiError(err)
}

// uploadsPerUser bounds upload attempts per user in ten minutes. Each one decodes and
// re-encodes an image (a duplicate costs no score but the same work), and only a
// few decode at a time, so one user must not be able to keep them all busy.
const uploadsPerUser = 30

func registerTextures(api huma.API, d Deps) {
	uploads := account.NewLimiter(uploadsPerUser, 10*time.Minute)
	huma.Register(api, huma.Operation{
		OperationID: "getTextureRules",
		Method:      http.MethodGet,
		Path:        "/api/v1/textures/rules",
		Summary:     "Upload rules and costs",
		Tags:        []string{"textures"},
	}, func(ctx context.Context, _ *struct{}) (*struct{ Body TextureRules }, error) {
		snap := d.Settings.Current()
		p, r := texture.PolicyFrom(snap), score.RatesFrom(snap)
		out := TextureRules{MaxSizeKB: p.MaxBytes / 1024, MaxWidth: p.MaxWidth, AllowDownload: p.AllowDownload,
			Rates: ScoreRates{PerStorage: r.PerStorage, PrivatePerStorage: r.PrivatePerStorage, PerClosetItem: r.PerClosetItem,
				AwardPerTexture: r.AwardPerTexture, ReporterModification: r.ReporterModification, ReporterReward: r.ReporterReward},
			ContentPolicy: snap.Localized("texture.content_policy", requestInfo(ctx).locale)}
		if p.NameRule != nil {
			out.NameRule = p.NameRule.String()
		}
		return &struct{ Body TextureRules }{out}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:  "uploadTexture",
		Method:       http.MethodPost,
		Path:         "/api/v1/textures",
		Summary:      "Upload a skin or cape",
		Description:  "The image is checked against texture.* settings, re-encoded and charged for; it goes into the uploader's closet. If the same image is already in the library (public, or the uploader's own), that texture is returned instead and nothing is charged. 402 when the score does not cover the cost.",
		Tags:         []string{"textures"},
		Security:     sessionSecurity,
		MaxBodyBytes: (texture.MaxUploadKB + 64) * 1024,
		// Huma's default of 5 seconds is too short for a few MB on a slow connection.
		BodyReadTimeout: 2 * time.Minute,
	}, func(ctx context.Context, in *uploadInput) (*struct{ Body UploadView }, error) {
		v, err := signedIn(ctx)
		if err != nil {
			return nil, err
		}
		key := strconv.FormatInt(v.User.ID, 10)
		if wait, blocked := uploads.Blocked(key); blocked {
			return nil, rateLimitError(&account.RateLimitError{RetryAfter: wait}, "too many uploads, try again later")
		}
		uploads.Fail(key)
		form := in.RawBody.Data()
		snap := d.Settings.Current()
		p := texture.PolicyFrom(snap)
		data, err := io.ReadAll(io.LimitReader(form.File, p.MaxBytes+1))
		if err != nil {
			return nil, err
		}
		res, err := d.Textures.Upload(ctx, v.User.ID, texture.UploadInput{Name: form.Name, Type: form.Type, Public: form.Public, Data: data},
			p, score.RatesFrom(snap))
		if err != nil {
			return nil, textureError(d, err)
		}
		out := UploadView{Texture: textureView(res.Texture), Duplicate: res.Duplicate, Cost: res.Cost, Score: res.Balance}
		if res.Duplicate {
			out.Score = v.User.Score
		}
		return &struct{ Body UploadView }{out}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "listTextures",
		Method:      http.MethodGet,
		Path:        "/api/v1/textures",
		Summary:     "The skin library",
		Description: "Public textures. With uploader set to yourself (or as an administrator), that user's private textures too. Cursor paging: pass next as cursor.",
		Tags:        []string{"textures"},
	}, func(ctx context.Context, in *listTexturesInput) (*struct{ Body TexturePage }, error) {
		list, next, err := d.Textures.List(ctx, texture.Filter{Category: in.Category, Query: in.Query, UploaderID: in.Uploader,
			Sort: in.Sort, Cursor: in.Cursor, Limit: in.Limit}, textureViewer(ctx))
		if err != nil {
			return nil, apiError(err)
		}
		page := TexturePage{Textures: make([]TextureView, 0, len(list)), Next: next}
		for _, t := range list {
			page.Textures = append(page.Textures, textureView(t))
		}
		return &struct{ Body TexturePage }{page}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "getTexture",
		Method:      http.MethodGet,
		Path:        "/api/v1/textures/{id}",
		Summary:     "A texture",
		Description: "Private textures only for their uploader and administrators; others get texture.private_status (403 or 404).",
		Tags:        []string{"textures"},
	}, func(ctx context.Context, in *textureIDInput) (*struct{ Body TextureDetail }, error) {
		tv := textureViewer(ctx)
		t, err := d.Textures.Get(ctx, in.ID, tv)
		if err != nil {
			return nil, textureError(d, err)
		}
		out := TextureDetail{TextureView: textureView(t)}
		if tv.UserID != 0 {
			out.Mine = t.UploaderID != nil && *t.UploaderID == tv.UserID
			if out.InCloset, err = d.Textures.InCloset(ctx, tv.UserID, t.ID); err != nil {
				return nil, err
			}
		}
		return &struct{ Body TextureDetail }{out}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "listCloset",
		Method:      http.MethodGet,
		Path:        "/api/v1/me/closet",
		Summary:     "Your closet",
		Tags:        []string{"me"},
		Security:    sessionSecurity,
	}, func(ctx context.Context, in *closetInput) (*struct{ Body ClosetPage }, error) {
		v, err := signedIn(ctx)
		if err != nil {
			return nil, err
		}
		items, next, err := d.Textures.Closet(ctx, v.User.ID, texture.ClosetFilter{Category: in.Category, Query: in.Query,
			Before: in.Before, Limit: in.Limit})
		if err != nil {
			return nil, err
		}
		page := ClosetPage{Items: make([]ClosetItemView, 0, len(items)), Next: next}
		for _, it := range items {
			page.Items = append(page.Items, ClosetItemView{ID: it.ID, Name: it.Name, AddedAt: it.CreatedAt, Texture: textureView(it.Edges.Texture)})
		}
		return &struct{ Body ClosetPage }{page}, nil
	})
}
