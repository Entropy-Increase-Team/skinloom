package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/internal/account"
	"github.com/Entropy-Increase-Team/skinloom/internal/player"
	"github.com/Entropy-Increase-Team/skinloom/internal/score"
	"github.com/Entropy-Increase-Team/skinloom/internal/settings"
	"github.com/Entropy-Increase-Team/skinloom/internal/stats"
	"github.com/Entropy-Increase-Team/skinloom/internal/texture"
)

// AdminPlayerView is a player in the admin panel.
type AdminPlayerView struct {
	ID        int64         `json:"id"`
	Name      string        `json:"name"`
	UUID      string        `json:"uuid" doc:"32 hex digits, no dashes"`
	Owner     *AdminUserRef `json:"owner,omitempty"`
	Skin      *WornTexture  `json:"skin,omitempty"`
	Cape      *WornTexture  `json:"cape,omitempty"`
	CreatedAt time.Time     `json:"createdAt"`
	UpdatedAt time.Time     `json:"updatedAt"`
}

func adminPlayerView(p *ent.Player) AdminPlayerView {
	return AdminPlayerView{ID: p.ID, Name: p.Name, UUID: p.UUID, Owner: adminUserRef(p.Edges.Owner),
		Skin: wornTexture(p.Edges.Skin), Cape: wornTexture(p.Edges.Cape), CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt}
}

// AdminPlayerPage is one page of the player list.
type AdminPlayerPage struct {
	Players []AdminPlayerView `json:"players"`
	Next    int64             `json:"next" doc:"Pass as before= for the next page; 0 = last page"`
}

// OverviewDay is one day of the admin overview chart, in the site's time zone.
type OverviewDay struct {
	Date          string `json:"date" doc:"YYYY-MM-DD"`
	Registrations int    `json:"registrations"`
	Uploads       int    `json:"uploads"`
}

// AdminOverview is the admin overview.
type AdminOverview struct {
	Users          int           `json:"users"`
	Players        int           `json:"players"`
	Textures       int           `json:"textures"`
	PublicTextures int           `json:"publicTextures"`
	ClosetItems    int           `json:"closetItems"`
	Files          int           `json:"files" doc:"Stored files; equal images share one"`
	StorageBytes   int64         `json:"storageBytes"`
	PendingReports int           `json:"pendingReports"`
	Days           []OverviewDay `json:"days" doc:"The last 31 days, today last"`
}

type adminInput struct {
	Authorization string `header:"Authorization" doc:"Bearer <admin token>; not needed with an admin session"`
}

type listPlayersInput struct {
	Authorization string `header:"Authorization" doc:"Bearer <admin token>; not needed with an admin session"`
	Query         string `query:"q" maxLength:"64" doc:"Player id, UUID, or the start of a name"`
	User          int64  `query:"user" minimum:"0" doc:"One owner's players"`
	Before        int64  `query:"before" minimum:"0"`
	Limit         int    `query:"limit" minimum:"0" maximum:"100"`
}

type adminPlayerIDInput struct {
	Authorization string `header:"Authorization" doc:"Bearer <admin token>; not needed with an admin session"`
	ID            int64  `path:"id"`
}

type adminUpdatePlayerInput struct {
	Authorization string `header:"Authorization" doc:"Bearer <admin token>; not needed with an admin session"`
	ID            int64  `path:"id"`
	Body          struct {
		Name   *string `json:"name,omitempty" maxLength:"64"`
		UserID *int64  `json:"userId,omitempty" minimum:"1" doc:"Give the player to this user; what their closet lacks comes off"`
	}
}

type adminWearInput struct {
	Authorization string `header:"Authorization" doc:"Bearer <admin token>; not needed with an admin session"`
	ID            int64  `path:"id"`
	Body          struct {
		Skin *int64 `json:"skin,omitempty" minimum:"0" doc:"Texture id from the owner's closet; 0 takes it off"`
		Cape *int64 `json:"cape,omitempty" minimum:"0" doc:"Texture id from the owner's closet; 0 takes it off"`
	}
}

type adminClosetInput struct {
	Authorization string `header:"Authorization" doc:"Bearer <admin token>; not needed with an admin session"`
	ID            int64  `path:"id"`
	Category      string `query:"category" enum:"skin,cape,"`
	Query         string `query:"q" maxLength:"50"`
	Before        int64  `query:"before" minimum:"0"`
	Limit         int    `query:"limit" minimum:"0" maximum:"100"`
}

type adminGiveInput struct {
	Authorization string `header:"Authorization" doc:"Bearer <admin token>; not needed with an admin session"`
	ID            int64  `path:"id"`
	Body          struct {
		TextureID int64 `json:"textureId" minimum:"1"`
	}
}

type adminClosetItemInput struct {
	Authorization string `header:"Authorization" doc:"Bearer <admin token>; not needed with an admin session"`
	ID            int64  `path:"id"`
	TextureID     int64  `path:"textureId"`
}

func adminPlayerError(err error) error {
	if pe := playerError(err); pe != err {
		return pe
	}
	return adminError(err)
}

func registerAdminPlayers(api huma.API, d Deps) {
	op := func(id, method, path, summary string, status int) huma.Operation {
		return huma.Operation{OperationID: id, Method: method, Path: path, Summary: summary,
			Tags: []string{"admin"}, Security: adminSecurity, DefaultStatus: status}
	}
	audit := func(ctx context.Context, a account.Actor, action, targetType string, id int64, meta map[string]any) {
		if err := d.Accounts.Audit(ctx, actorIDOf(a), action, targetType, strconv.FormatInt(id, 10), requestInfo(ctx).client.ClientIP, meta); err != nil {
			d.Log.Warn("audit", "action", action, "err", err)
		}
	}
	// managed loads a player whose owner the actor may manage.
	managed := func(ctx context.Context, a account.Actor, id int64) (*ent.Player, error) {
		p, err := d.Players.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		if _, err := d.Accounts.MayManageOwned(ctx, a, p.UserID); err != nil {
			return nil, err
		}
		return p, nil
	}

	huma.Register(api, op("getOverview", http.MethodGet, "/api/v1/admin/overview", "Site totals and the last 31 days", 0),
		func(ctx context.Context, in *adminInput) (*struct{ Body AdminOverview }, error) {
			if _, err := adminActor(ctx, d, in.Authorization); err != nil {
				return nil, err
			}
			o, err := stats.Compute(ctx, d.DB.Ent, time.Now(), settings.Location(d.Settings.Current()))
			if err != nil {
				return nil, err
			}
			out := AdminOverview{Users: o.Users, Players: o.Players, Textures: o.Textures, PublicTextures: o.PublicTextures,
				ClosetItems: o.ClosetItems, Files: o.Files, StorageBytes: o.StorageBytes, PendingReports: o.PendingReports,
				Days: make([]OverviewDay, len(o.Days))}
			for i, day := range o.Days {
				out.Days[i] = OverviewDay{Date: day.Date, Registrations: day.Registrations, Uploads: day.Uploads}
			}
			return &struct{ Body AdminOverview }{out}, nil
		})

	huma.Register(api, op("adminListPlayers", http.MethodGet, "/api/v1/admin/players", "List players", 0),
		func(ctx context.Context, in *listPlayersInput) (*struct{ Body AdminPlayerPage }, error) {
			if _, err := adminActor(ctx, d, in.Authorization); err != nil {
				return nil, err
			}
			list, next, err := d.Players.Search(ctx, player.Filter{Query: in.Query, UserID: in.User, Before: in.Before, Limit: in.Limit})
			if err != nil {
				return nil, err
			}
			page := AdminPlayerPage{Players: make([]AdminPlayerView, 0, len(list)), Next: next}
			for _, p := range list {
				page.Players = append(page.Players, adminPlayerView(p))
			}
			return &struct{ Body AdminPlayerPage }{page}, nil
		})

	huma.Register(api, op("adminUpdatePlayer", http.MethodPatch, "/api/v1/admin/players/{id}", "Rename a player or give it to another user", 0),
		func(ctx context.Context, in *adminUpdatePlayerInput) (*struct{ Body AdminPlayerView }, error) {
			a, err := adminActor(ctx, d, in.Authorization)
			if err != nil {
				return nil, err
			}
			p, err := managed(ctx, a, in.ID)
			if err != nil {
				return nil, adminPlayerError(err)
			}
			meta := map[string]any{}
			if in.Body.UserID != nil && *in.Body.UserID != p.UserID {
				if _, err := d.Accounts.MayManageOwned(ctx, a, *in.Body.UserID); err != nil {
					if errors.Is(err, account.ErrNotFound) {
						return nil, apiError(&account.FieldError{Field: "userId", Message: "no such user"})
					}
					return nil, adminError(err)
				}
			}
			if in.Body.Name != nil && *in.Body.Name != p.Name {
				renamed, err := d.Players.Rename(ctx, p.UserID, p.ID, *in.Body.Name, player.PolicyFrom(d.Settings.Current()))
				if err != nil {
					return nil, adminPlayerError(err)
				}
				meta["name"] = []string{p.Name, renamed.Name}
			}
			if in.Body.UserID != nil && *in.Body.UserID != p.UserID {
				if _, err := d.Players.Transfer(ctx, p.ID, *in.Body.UserID); err != nil {
					return nil, adminPlayerError(err)
				}
				meta["owner"] = []int64{p.UserID, *in.Body.UserID}
			}
			if len(meta) > 0 {
				audit(ctx, a, "admin.player_updated", "player", p.ID, meta)
			}
			if p, err = d.Players.Get(ctx, p.ID); err != nil {
				return nil, adminPlayerError(err)
			}
			return &struct{ Body AdminPlayerView }{adminPlayerView(p)}, nil
		})

	huma.Register(api, op("adminWearPlayer", http.MethodPut, "/api/v1/admin/players/{id}/textures", "Set what a player wears", 0),
		func(ctx context.Context, in *adminWearInput) (*struct{ Body AdminPlayerView }, error) {
			a, err := adminActor(ctx, d, in.Authorization)
			if err != nil {
				return nil, err
			}
			p, err := managed(ctx, a, in.ID)
			if err != nil {
				return nil, adminPlayerError(err)
			}
			if _, err := d.Players.Wear(ctx, p.UserID, p.ID, in.Body.Skin, in.Body.Cape); err != nil {
				return nil, adminPlayerError(err)
			}
			audit(ctx, a, "admin.player_textures", "player", p.ID, map[string]any{"skin": in.Body.Skin, "cape": in.Body.Cape})
			if p, err = d.Players.Get(ctx, p.ID); err != nil {
				return nil, adminPlayerError(err)
			}
			return &struct{ Body AdminPlayerView }{adminPlayerView(p)}, nil
		})

	huma.Register(api, op("adminDeletePlayer", http.MethodDelete, "/api/v1/admin/players/{id}", "Delete a player", http.StatusNoContent),
		func(ctx context.Context, in *adminPlayerIDInput) (*struct{}, error) {
			a, err := adminActor(ctx, d, in.Authorization)
			if err != nil {
				return nil, err
			}
			p, err := managed(ctx, a, in.ID)
			if err != nil {
				return nil, adminPlayerError(err)
			}
			refund := score.RatesFrom(d.Settings.Current()).ReturnOnDelete
			if _, err := d.Players.Delete(ctx, p.UserID, p.ID, refund); err != nil {
				return nil, adminPlayerError(err)
			}
			audit(ctx, a, "admin.player_deleted", "player", p.ID, map[string]any{"name": p.Name, "owner": p.UserID})
			return nil, nil
		})

	huma.Register(api, op("adminListCloset", http.MethodGet, "/api/v1/admin/users/{id}/closet", "A user's closet", 0),
		func(ctx context.Context, in *adminClosetInput) (*struct{ Body ClosetPage }, error) {
			if _, err := adminActor(ctx, d, in.Authorization); err != nil {
				return nil, err
			}
			if _, err := d.Accounts.GetUser(ctx, in.ID); err != nil {
				return nil, adminError(err)
			}
			items, next, err := d.Textures.Closet(ctx, in.ID, texture.ClosetFilter{Category: in.Category, Query: in.Query, Before: in.Before, Limit: in.Limit})
			if err != nil {
				return nil, err
			}
			page := ClosetPage{Items: make([]ClosetItemView, 0, len(items)), Next: next}
			for _, it := range items {
				page.Items = append(page.Items, ClosetItemView{ID: it.ID, Name: it.Name, AddedAt: it.CreatedAt, Texture: textureView(it.Edges.Texture)})
			}
			return &struct{ Body ClosetPage }{page}, nil
		})

	huma.Register(api, op("adminAddToCloset", http.MethodPost, "/api/v1/admin/users/{id}/closet", "Put a texture in a user's closet, free", 0),
		func(ctx context.Context, in *adminGiveInput) (*struct{ Body ClosetItemView }, error) {
			a, err := adminActor(ctx, d, in.Authorization)
			if err != nil {
				return nil, err
			}
			if _, err := d.Accounts.MayManageOwned(ctx, a, in.ID); err != nil {
				return nil, adminError(err)
			}
			item, err := d.Textures.GiveToCloset(ctx, in.ID, in.Body.TextureID)
			if err != nil {
				return nil, manageError(d, err)
			}
			t, err := d.Textures.Get(ctx, item.TextureID, texture.Viewer{Admin: true})
			if err != nil {
				return nil, manageError(d, err)
			}
			audit(ctx, a, "admin.closet_added", "user", in.ID, map[string]any{"texture": in.Body.TextureID})
			return &struct{ Body ClosetItemView }{ClosetItemView{ID: item.ID, Name: item.Name, AddedAt: item.CreatedAt, Texture: textureView(t)}}, nil
		})

	huma.Register(api, op("adminRemoveFromCloset", http.MethodDelete, "/api/v1/admin/users/{id}/closet/{textureId}", "Take a texture out of a user's closet", 0),
		func(ctx context.Context, in *adminClosetItemInput) (*struct{ Body Unfavorited }, error) {
			a, err := adminActor(ctx, d, in.Authorization)
			if err != nil {
				return nil, err
			}
			if _, err := d.Accounts.MayManageOwned(ctx, a, in.ID); err != nil {
				return nil, adminError(err)
			}
			refund := score.RatesFrom(d.Settings.Current()).ReturnOnDelete
			n, err := d.Textures.Unfavorite(ctx, in.ID, in.TextureID, refund)
			if err != nil {
				return nil, manageError(d, err)
			}
			audit(ctx, a, "admin.closet_removed", "user", in.ID, map[string]any{"texture": in.TextureID, "refunded": n})
			return &struct{ Body Unfavorited }{Unfavorited{Refunded: n}}, nil
		})
}

// actorIDOf is who acts, for the audit log: nil for the admin token.
func actorIDOf(a account.Actor) *int64 {
	if a == nil {
		return nil
	}
	return &a.User.ID
}
