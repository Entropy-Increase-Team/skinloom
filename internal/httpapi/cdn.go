package httpapi

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Entropy-Increase-Team/skinloom/internal/audit"
	"github.com/Entropy-Increase-Team/skinloom/internal/cdn"
)

type purgeInput struct {
	Authorization string `header:"Authorization" doc:"Bearer <admin token>"`
	Body          struct {
		Players []string `json:"players" minItems:"1" maxItems:"50" doc:"Player names whose cached profiles and avatars to purge"`
	}
}

// PurgeResult is what a manual purge did.
type PurgeResult struct {
	URLs int `json:"urls" doc:"URLs the CDN was asked to drop"`
}

func registerCDN(api huma.API, d Deps) {
	huma.Register(api, huma.Operation{
		OperationID: "purgeCDN",
		Method:      http.MethodPost,
		Path:        "/api/v1/admin/cdn/purge",
		Summary:     "Purge players from the CDN",
		Description: "Asks the configured CDN to drop the cached profiles and avatars of these players, as a player change does on its own. Also a way to check the CDN settings: 409 when none is configured, 502 with the provider's answer when it refuses.",
		Tags:        []string{"admin"},
		Security:    adminSecurity,
	}, func(ctx context.Context, in *purgeInput) (*struct{ Body PurgeResult }, error) {
		actor, err := checkAdmin(ctx, d, in.Authorization)
		if err != nil {
			return nil, err
		}
		names := in.Body.Players
		for _, n := range names {
			if strings.TrimSpace(n) == "" {
				return nil, huma.Error422UnprocessableEntity("a player name is empty")
			}
		}
		err = d.CDN.Purge(ctx, names)
		switch {
		case errors.Is(err, cdn.ErrOff), errors.Is(err, cdn.ErrIncomplete):
			return nil, huma.Error409Conflict(err.Error())
		case err != nil:
			return nil, huma.Error502BadGateway(err.Error())
		}
		rec := &audit.Entry{Actor: actor, Action: "admin.cdn_purge", TargetType: "cdn", IP: requestInfo(ctx).client.ClientIP}
		if err := audit.Write(ctx, d.DB.Ent, rec.With("players", names)); err != nil {
			d.Log.Warn("audit", "action", "admin.cdn_purge", "err", err)
		}
		return &struct{ Body PurgeResult }{PurgeResult{URLs: len(names) * len(cdn.ProfileURLs("", ""))}}, nil
	})
}
