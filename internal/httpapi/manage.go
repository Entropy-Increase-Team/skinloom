package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Entropy-Increase-Team/skinloom/internal/audit"
	"github.com/Entropy-Increase-Team/skinloom/internal/notify"
	"github.com/Entropy-Increase-Team/skinloom/internal/score"
	"github.com/Entropy-Increase-Team/skinloom/internal/texture"
)

type favoriteInput struct {
	Body struct {
		TextureID int64  `json:"textureId" minimum:"1"`
		Name      string `json:"name,omitempty" maxLength:"128" doc:"Name in your closet; default: the texture's name"`
	}
}

// Favorited is a new closet item and the score left.
type Favorited struct {
	Item  ClosetItemView `json:"item"`
	Score int64          `json:"score"`
}

type closetItemInput struct {
	TextureID int64 `path:"textureId"`
}

type renameClosetItemInput struct {
	TextureID int64 `path:"textureId"`
	Body      struct {
		Name string `json:"name" maxLength:"128"`
	}
}

// Unfavorited reports what removing a closet item gave back.
type Unfavorited struct {
	Refunded int64 `json:"refunded"`
}

type updateTextureInput struct {
	ID   int64 `path:"id"`
	Body struct {
		Name   *string `json:"name,omitempty" maxLength:"128"`
		Type   *string `json:"type,omitempty" enum:"steve,alex,cape" doc:"Must fit the image; players wearing it in a slot it no longer fits take it off"`
		Public *bool   `json:"public,omitempty" doc:"Going private costs the uploader the price difference (and the upload award); going public gives it back"`
	}
}

// SignResult is a check-in.
type SignResult struct {
	Gained int64     `json:"gained"`
	Score  int64     `json:"score"`
	NextAt time.Time `json:"nextAt" doc:"When the next check-in is possible"`
}

// SignInfo is the check-in state of the signed-in user.
type SignInfo struct {
	NextAt *time.Time `json:"nextAt,omitempty" doc:"When the next check-in is possible; absent = now"`
	Min    int64      `json:"min"`
	Max    int64      `json:"max"`
	Daily  bool       `json:"daily" doc:"Once per calendar day instead of every gapHours"`
	Gap    int64      `json:"gapHours"`
}

// Dashboard is the signed-in user's overview (/user).
type Dashboard struct {
	Score            int64    `json:"score"`
	Players          int      `json:"players"`
	ClosetItems      int      `json:"closetItems"`
	Uploads          int      `json:"uploads"`
	StorageBytes     int64    `json:"storageBytes"`
	Sign             SignInfo `json:"sign"`
	Announcement     string   `json:"announcement,omitempty" doc:"Markdown, in the request's language"`
	AnnouncementHTML string   `json:"announcementHtml,omitempty" doc:"The announcement rendered: no raw HTML, no script links"`
}

// manageError maps the errors of closet and texture changes.
func manageError(d Deps, err error) error {
	var dup *texture.PublicDuplicateError
	var early *score.TooEarlyError
	switch {
	case errors.Is(err, texture.ErrForbidden):
		return huma.Error403Forbidden(err.Error())
	case errors.Is(err, texture.ErrInCloset):
		return huma.Error409Conflict("already in your closet")
	case errors.Is(err, texture.ErrNotInCloset):
		return huma.Error404NotFound("not in your closet")
	case errors.As(err, &dup):
		return huma.Error409Conflict(fmt.Sprintf("the same image is public already as texture %d", dup.ID))
	case errors.As(err, &early):
		return huma.ErrorWithHeaders(huma.Error409Conflict("already checked in", &huma.ErrorDetail{Location: "nextAt", Value: early.Next}),
			http.Header{"Retry-After": {strconv.Itoa(max(1, int(time.Until(early.Next).Seconds())))}})
	}
	return textureError(d, err)
}

func registerManage(api huma.API, d Deps) {
	op := func(id, method, path, summary string, tags string, status int) huma.Operation {
		return huma.Operation{OperationID: id, Method: method, Path: path, Summary: summary,
			Tags: []string{tags}, Security: sessionSecurity, DefaultStatus: status}
	}

	huma.Register(api, op("addToCloset", http.MethodPost, "/api/v1/me/closet", "Add a texture to your closet", "me", 0),
		func(ctx context.Context, in *favoriteInput) (*struct{ Body Favorited }, error) {
			v, err := signedIn(ctx)
			if err != nil {
				return nil, err
			}
			item, balance, err := d.Textures.Favorite(ctx, textureViewer(ctx), in.Body.TextureID, in.Body.Name, score.RatesFrom(d.Settings.Current()))
			if err != nil {
				return nil, manageError(d, err)
			}
			t, err := d.Textures.Get(ctx, item.TextureID, texture.Viewer{UserID: v.User.ID, Admin: v.IsAdmin()})
			if err != nil {
				return nil, err
			}
			return &struct{ Body Favorited }{Favorited{Item: ClosetItemView{ID: item.ID, Name: item.Name, AddedAt: item.CreatedAt, Texture: textureView(t)}, Score: balance}}, nil
		})

	huma.Register(api, op("renameClosetItem", http.MethodPatch, "/api/v1/me/closet/{textureId}", "Rename a texture in your closet", "me", 0),
		func(ctx context.Context, in *renameClosetItemInput) (*struct{ Body ClosetItemView }, error) {
			v, err := signedIn(ctx)
			if err != nil {
				return nil, err
			}
			it, err := d.Textures.RenameClosetItem(ctx, v.User.ID, in.TextureID, in.Body.Name)
			if err != nil {
				return nil, manageError(d, err)
			}
			return &struct{ Body ClosetItemView }{ClosetItemView{ID: it.ID, Name: it.Name, AddedAt: it.CreatedAt, Texture: textureView(it.Edges.Texture)}}, nil
		})

	huma.Register(api, op("removeFromCloset", http.MethodDelete, "/api/v1/me/closet/{textureId}", "Remove a texture from your closet", "me", 0),
		func(ctx context.Context, in *closetItemInput) (*struct{ Body Unfavorited }, error) {
			v, err := signedIn(ctx)
			if err != nil {
				return nil, err
			}
			refunded, err := d.Textures.Unfavorite(ctx, v.User.ID, in.TextureID, score.RatesFrom(d.Settings.Current()).ReturnOnDelete, nil)
			if err != nil {
				return nil, manageError(d, err)
			}
			return &struct{ Body Unfavorited }{Unfavorited{Refunded: refunded}}, nil
		})

	huma.Register(api, op("updateTexture", http.MethodPatch, "/api/v1/textures/{id}", "Change a texture (its uploader or an administrator)", "textures", 0),
		func(ctx context.Context, in *updateTextureInput) (*struct{ Body TextureView }, error) {
			v, err := verifiedViewer(ctx, d)
			if err != nil {
				return nil, err
			}
			snap := d.Settings.Current()
			b := in.Body
			// Written by the service only when this is someone else's texture (moderation).
			rec := textureRecord(ctx, v.User.ID, "admin.texture_updated", in.ID, map[string]any{"name": b.Name, "type": b.Type, "public": b.Public})
			t, err := d.Textures.Update(ctx, textureViewer(ctx), in.ID, texture.Edit{Name: b.Name, Type: b.Type, Public: b.Public},
				texture.PolicyFrom(snap), score.RatesFrom(snap), rec)
			if err != nil {
				return nil, manageError(d, err)
			}
			return &struct{ Body TextureView }{textureView(t)}, nil
		})

	huma.Register(api, op("deleteTexture", http.MethodDelete, "/api/v1/textures/{id}", "Delete a texture (its uploader or an administrator)", "textures", http.StatusNoContent),
		func(ctx context.Context, in *textureIDInput) (*struct{}, error) {
			v, err := verifiedViewer(ctx, d)
			if err != nil {
				return nil, err
			}
			// Written by the service only when this is someone else's texture (moderation).
			rec := textureRecord(ctx, v.User.ID, "admin.texture_deleted", in.ID, nil)
			if err := d.Textures.Delete(ctx, textureViewer(ctx), in.ID, score.RatesFrom(d.Settings.Current()).ReturnOnDelete, rec); err != nil {
				return nil, manageError(d, err)
			}
			return nil, nil
		})

	huma.Register(api, op("signIn", http.MethodPost, "/api/v1/me/sign", "Daily check-in", "me", 0),
		func(ctx context.Context, _ *struct{}) (*struct{ Body SignResult }, error) {
			v, err := signedIn(ctx)
			if err != nil {
				return nil, err
			}
			gained, balance, next, err := score.SignIn(ctx, d.DB.Ent, v.User.ID, score.SignPolicyFrom(d.Settings.Current()), time.Now())
			if err != nil {
				return nil, manageError(d, err)
			}
			return &struct{ Body SignResult }{SignResult{Gained: gained, Score: balance, NextAt: next}}, nil
		})

	huma.Register(api, op("getDashboard", http.MethodGet, "/api/v1/me/dashboard", "Your overview", "me", 0),
		func(ctx context.Context, _ *struct{}) (*struct{ Body Dashboard }, error) {
			v, err := signedIn(ctx)
			if err != nil {
				return nil, err
			}
			snap := d.Settings.Current()
			players, err := d.Players.List(ctx, v.User.ID)
			if err != nil {
				return nil, err
			}
			closet, err := d.Textures.ClosetSize(ctx, v.User.ID)
			if err != nil {
				return nil, err
			}
			uploads, bytes, err := d.Textures.Usage(ctx, v.User.ID)
			if err != nil {
				return nil, err
			}
			p := score.SignPolicyFrom(snap)
			sign := SignInfo{Min: p.Min, Max: p.Max, Daily: p.Daily, Gap: int64(p.Gap / time.Hour)}
			if next := p.Next(v.User.LastSignAt); next.After(time.Now()) {
				sign.NextAt = &next
			}
			return &struct{ Body Dashboard }{Dashboard{Score: v.User.Score, Players: len(players), ClosetItems: closet, Uploads: uploads,
				StorageBytes: bytes, Sign: sign, Announcement: snap.Localized("site.announcement", requestInfo(ctx).locale),
				AnnouncementHTML: notify.Markdown(snap.Localized("site.announcement", requestInfo(ctx).locale))}}, nil
		})
}

// textureRecord is the audit record of an administrator's change to someone else's
// texture.
func textureRecord(ctx context.Context, actor int64, action string, id int64, meta map[string]any) *audit.Entry {
	return &audit.Entry{Actor: &actor, Action: action, TargetType: "texture", TargetID: strconv.FormatInt(id, 10),
		IP: requestInfo(ctx).client.ClientIP, Meta: meta}
}
