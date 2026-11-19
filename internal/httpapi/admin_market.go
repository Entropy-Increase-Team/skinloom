package httpapi

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/Entropy-Increase-Team/skinloom/internal/plugin"
)

type listMarketPackagesInput struct {
	Authorization  string `header:"Authorization" doc:"Bearer <admin token>"`
	Q              string `query:"q" doc:"Search query across package ID, localized name, description, category, and authors"`
	Kind           string `query:"kind" doc:"Filter by kind: all | plugin | theme"`
	Runtime        string `query:"runtime" doc:"Filter by runtime: all | js | native"`
	Category       string `query:"category" doc:"Filter by category: all | recommended | official | security | score | migration | theme"`
	CompatibleOnly bool   `query:"compatibleOnly" doc:"When true, only return packages compatible with the current host environment"`
}

type marketInspectInput struct {
	Authorization string                      `header:"Authorization" doc:"Bearer <admin token>"`
	Body          plugin.MarketInspectRequest `doc:"Market package identifier and optional version to inspect"`
}

type marketInstallInput struct {
	Authorization string                      `header:"Authorization" doc:"Bearer <admin token>"`
	Body          plugin.MarketInstallRequest `doc:"Market package installation options and approved permissions"`
}

// AdminMarketInstallResult summarizes the installed market package and any cascade-installed dependencies.
type AdminMarketInstallResult struct {
	Plugin                AdminPluginView   `json:"plugin"`
	InstalledDependencies []AdminPluginView `json:"installedDependencies,omitempty"`
}

func registerAdminMarketRoutes(api huma.API, d Deps) {
	op := func(id, method, p, summary string, status int) huma.Operation {
		return huma.Operation{
			OperationID:   id,
			Method:        method,
			Path:          p,
			Summary:       summary,
			Tags:          []string{"admin"},
			Security:      adminSecurity,
			DefaultStatus: status,
		}
	}

	huma.Register(api, op("listMarketPackages", http.MethodGet, "/api/v1/admin/market/packages", "List and filter official/configured plugin and theme market packages", 0),
		func(ctx context.Context, in *listMarketPackagesInput) (*struct{ Body *plugin.MarketListResult }, error) {
			if _, err := requirePluginAdmin(ctx, d, in.Authorization); err != nil {
				return nil, err
			}
			res, err := d.Plugins.Registry().ListPackages(ctx, plugin.MarketFilter{
				Query:          in.Q,
				Kind:           in.Kind,
				Runtime:        in.Runtime,
				Category:       in.Category,
				CompatibleOnly: in.CompatibleOnly,
				Locale:         requestInfo(ctx).locale,
			})
			if err != nil {
				return nil, pluginAPIError(err)
			}
			return &struct{ Body *plugin.MarketListResult }{res}, nil
		})

	huma.Register(api, op("refreshMarketPackages", http.MethodPost, "/api/v1/admin/market/refresh", "Force refresh market indexes and enforce security blocklists", 0),
		func(ctx context.Context, in *adminAuth) (*struct{ Body *plugin.MarketRefreshResult }, error) {
			if _, err := requirePluginAdmin(ctx, d, in.Authorization); err != nil {
				return nil, err
			}
			res, err := d.Plugins.Registry().RefreshAndEnforce(ctx, requestInfo(ctx).locale)
			if err != nil {
				return nil, pluginAPIError(err)
			}
			if len(res.BlockedActions) > 0 {
				notifyPluginConfigFallback(d)
			}
			return &struct{ Body *plugin.MarketRefreshResult }{res}, nil
		})

	huma.Register(api, op("inspectMarketPackage", http.MethodPost, "/api/v1/admin/market/inspect", "Download and pre-inspect a market package for the current host platform", 0),
		func(ctx context.Context, in *marketInspectInput) (*struct{ Body *plugin.MarketInspectResult }, error) {
			if _, err := requirePluginAdmin(ctx, d, in.Authorization); err != nil {
				return nil, err
			}
			res, err := d.Plugins.Registry().InspectFromMarket(ctx, in.Body, requestInfo(ctx).locale)
			if err != nil {
				return nil, pluginAPIError(err)
			}
			return &struct{ Body *plugin.MarketInspectResult }{res}, nil
		})

	huma.Register(api, op("installMarketPackage", http.MethodPost, "/api/v1/admin/market/install", "Install or upgrade a plugin or theme from the market with optional cascade dependencies", http.StatusCreated),
		func(ctx context.Context, in *marketInstallInput) (*struct{ Body AdminMarketInstallResult }, error) {
			actorID, err := requirePluginSuperadmin(ctx, d, in.Authorization)
			if err != nil {
				return nil, err
			}
			res, err := d.Plugins.Registry().InstallFromMarket(ctx, in.Body)
			if err != nil {
				return nil, pluginAPIError(err)
			}
			locale := requestInfo(ctx).locale
			var depViews []AdminPluginView
			var depIDs []string
			for _, dep := range res.InstalledDependencies {
				depViews = append(depViews, toAdminPluginView(dep, locale))
				depIDs = append(depIDs, dep.ID+"@"+dep.Version)
				writePluginAudit(ctx, d, actorID, "plugin.market_installed", dep.ID, map[string]any{
					"version":    dep.Version,
					"kind":       dep.Kind,
					"runtime":    dep.Runtime,
					"registryId": dep.RegistrySource,
					"cascade":    true,
					"enabled":    !in.Body.Disabled,
				})
			}
			writePluginAudit(ctx, d, actorID, "plugin.market_installed", res.Plugin.ID, map[string]any{
				"version":      res.Plugin.Version,
				"kind":         res.Plugin.Kind,
				"runtime":      res.Plugin.Runtime,
				"registryId":   res.Plugin.RegistrySource,
				"dependencies": depIDs,
				"enabled":      !in.Body.Disabled,
			})
			notifyPluginConfigFallback(d)
			return &struct{ Body AdminMarketInstallResult }{
				Body: AdminMarketInstallResult{
					Plugin:                toAdminPluginView(res.Plugin, locale),
					InstalledDependencies: depViews,
				},
			}, nil
		})
}
