package httpapi

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/internal/player"
	"github.com/Entropy-Increase-Team/skinloom/internal/score"
)

// WornTexture is a texture a player wears.
type WornTexture struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Type string `json:"type" enum:"steve,alex,cape"`
	Hash string `json:"hash"`
}

// PlayerView is one of the signed-in user's players.
type PlayerView struct {
	ID        int64        `json:"id"`
	Name      string       `json:"name"`
	UUID      string       `json:"uuid" doc:"32 hex digits, no dashes"`
	Skin      *WornTexture `json:"skin,omitempty"`
	Cape      *WornTexture `json:"cape,omitempty"`
	UpdatedAt time.Time    `json:"updatedAt"`
}

func wornTexture(t *ent.Texture) *WornTexture {
	if t == nil {
		return nil
	}
	return &WornTexture{ID: t.ID, Name: t.Name, Type: string(t.Type), Hash: t.Hash}
}

func playerView(p *ent.Player) PlayerView {
	return PlayerView{ID: p.ID, Name: p.Name, UUID: p.UUID, Skin: wornTexture(p.Edges.Skin), Cape: wornTexture(p.Edges.Cape), UpdatedAt: p.UpdatedAt}
}

// PlayerRules are the player rules of the site.
type PlayerRules struct {
	NameRule  string `json:"nameRule" enum:"official,cjk,utf8,custom"`
	NameRegex string `json:"nameRegexp,omitempty" doc:"For nameRule custom"`
	MinLength int    `json:"minLength"`
	MaxLength int    `json:"maxLength"`
	Cost      int64  `json:"cost" doc:"Score to create a player"`
	Refund    bool   `json:"refund" doc:"Deleting a player gives back what it cost"`
}

type playerNameInput struct {
	Body struct {
		Name string `json:"name" maxLength:"64"`
	}
}

type playerIDInput struct {
	ID int64 `path:"id"`
}

type renamePlayerInput struct {
	ID   int64 `path:"id"`
	Body struct {
		Name string `json:"name" maxLength:"64"`
	}
}

type wearInput struct {
	ID   int64 `path:"id"`
	Body struct {
		Skin *int64 `json:"skin,omitempty" minimum:"0" doc:"Texture id of a skin in your closet; 0 takes the skin off; absent leaves it"`
		Cape *int64 `json:"cape,omitempty" minimum:"0" doc:"Texture id of a cape in your closet; 0 takes the cape off; absent leaves it"`
	}
}

// PlayerCreated is a new player and the score left.
type PlayerCreated struct {
	Player PlayerView `json:"player"`
	Score  int64      `json:"score"`
}

// PlayerDeleted reports what deleting a player gave back.
type PlayerDeleted struct {
	Refunded int64 `json:"refunded"`
}

func playerError(err error) error {
	switch {
	case errors.Is(err, player.ErrNotFound):
		return huma.Error404NotFound("no such player")
	case errors.Is(err, score.ErrInsufficient):
		return huma.NewError(http.StatusPaymentRequired, "not enough score")
	}
	return apiError(err)
}

func registerPlayers(api huma.API, d Deps) {
	op := func(id, method, path, summary string, status int) huma.Operation {
		return huma.Operation{OperationID: id, Method: method, Path: path, Summary: summary,
			Tags: []string{"players"}, Security: sessionSecurity, DefaultStatus: status}
	}

	huma.Register(api, huma.Operation{OperationID: "getPlayerRules", Method: http.MethodGet, Path: "/api/v1/players/rules",
		Summary: "Player name rules and costs", Tags: []string{"players"}},
		func(ctx context.Context, _ *struct{}) (*struct{ Body PlayerRules }, error) {
			snap := d.Settings.Current()
			p, r := player.PolicyFrom(snap), score.RatesFrom(snap)
			out := PlayerRules{NameRule: p.Rule, MinLength: p.Min, MaxLength: min(p.Max, player.MaxNameLen), Cost: r.PerPlayer, Refund: r.ReturnOnDelete}
			if p.Custom != nil {
				out.NameRegex = p.Custom.String()
			}
			return &struct{ Body PlayerRules }{out}, nil
		})

	huma.Register(api, op("listPlayers", http.MethodGet, "/api/v1/me/players", "Your players", 0),
		func(ctx context.Context, _ *struct{}) (*struct{ Body []PlayerView }, error) {
			v, err := signedIn(ctx)
			if err != nil {
				return nil, err
			}
			list, err := d.Players.List(ctx, v.User.ID)
			if err != nil {
				return nil, err
			}
			out := make([]PlayerView, 0, len(list))
			for _, p := range list {
				out = append(out, playerView(p))
			}
			return &struct{ Body []PlayerView }{out}, nil
		})

	huma.Register(api, op("createPlayer", http.MethodPost, "/api/v1/me/players", "Create a player", 0),
		func(ctx context.Context, in *playerNameInput) (*struct{ Body PlayerCreated }, error) {
			v, err := verifiedViewer(ctx, d)
			if err != nil {
				return nil, err
			}
			snap := d.Settings.Current()
			p, balance, err := d.Players.Create(ctx, v.User.ID, in.Body.Name, player.PolicyFrom(snap), score.RatesFrom(snap))
			if err != nil {
				return nil, playerError(err)
			}
			return &struct{ Body PlayerCreated }{PlayerCreated{Player: playerView(p), Score: balance}}, nil
		})

	huma.Register(api, op("renamePlayer", http.MethodPatch, "/api/v1/me/players/{id}", "Rename a player", 0),
		func(ctx context.Context, in *renamePlayerInput) (*struct{ Body PlayerView }, error) {
			v, err := verifiedViewer(ctx, d)
			if err != nil {
				return nil, err
			}
			p, err := d.Players.Rename(ctx, v.User.ID, in.ID, in.Body.Name, player.PolicyFrom(d.Settings.Current()))
			if err != nil {
				return nil, playerError(err)
			}
			return &struct{ Body PlayerView }{playerView(p)}, nil
		})

	huma.Register(api, op("wearTextures", http.MethodPut, "/api/v1/me/players/{id}/textures", "Choose what a player wears", 0),
		func(ctx context.Context, in *wearInput) (*struct{ Body PlayerView }, error) {
			v, err := verifiedViewer(ctx, d)
			if err != nil {
				return nil, err
			}
			p, err := d.Players.Wear(ctx, v.User.ID, in.ID, in.Body.Skin, in.Body.Cape, nil)
			if err != nil {
				return nil, playerError(err)
			}
			return &struct{ Body PlayerView }{playerView(p)}, nil
		})

	huma.Register(api, op("deletePlayer", http.MethodDelete, "/api/v1/me/players/{id}", "Delete a player", 0),
		func(ctx context.Context, in *playerIDInput) (*struct{ Body PlayerDeleted }, error) {
			v, err := verifiedViewer(ctx, d)
			if err != nil {
				return nil, err
			}
			refunded, err := d.Players.Delete(ctx, v.User.ID, in.ID, score.RatesFrom(d.Settings.Current()).ReturnOnDelete, nil)
			if err != nil {
				return nil, playerError(err)
			}
			return &struct{ Body PlayerDeleted }{PlayerDeleted{Refunded: refunded}}, nil
		})
}
