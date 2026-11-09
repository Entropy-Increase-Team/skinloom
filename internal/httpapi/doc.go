// Package httpapi is the backend's HTTP surface. New (api.go) builds one chi router
// that serves:
//
//   - /api/v1: the REST API used by the frontend and scripts, declared with Huma, which
//     also generates the OpenAPI document (api/openapi.json);
//   - the game and launcher protocols, compatible with Blessing Skin;
//   - health probes, the /events change stream and theme assets.
//
// Handlers translate HTTP to calls on the services in internal/modules and back; the
// rules live in the services. Where to find an endpoint:
//
//	Site and accounts (/api/v1)
//	  site.go            bootstrap (settings, theme, viewer for every page), site stats
//	  auth.go            first-run setup, login, logout, the signed-in user; CSRF guard
//	  register.go        registration, image captcha
//	  mail.go            email verification, password reset, test mail
//	  me.go              profile, password, email, devices, account deletion
//	  social.go          sign-in with GitHub, Microsoft, OpenID Connect
//	Textures and players (/api/v1)
//	  textures.go        upload, skin library, texture pages, closet listing
//	  manage.go          closet changes, texture edits and deletion, check-in, dashboard
//	  players.go         players and what they wear
//	  reports.go         reporting textures and reviewing reports
//	  notifications.go   inbox, avatar choice, sending notifications
//	Administration (/api/v1/admin)
//	  admin_settings.go  settings, themes, home page blocks
//	  admin_users.go     users and the audit log
//	  admin_players.go   overview, players, closets of users
//	  admin_status.go    system status
//	  admin_cdn.go       CDN purge
//	Open platform
//	  oauth.go           OAuth 2.0 server (/oauth/*), consent, applications, tokens
//	  bsapi.go           Blessing Skin compatible user API (/api/user, /api/players...)
//	  bsadmin.go         Blessing Skin compatible admin API (/api/admin/*)
//	Game clients
//	  game.go            CustomSkinLoader, UniSkinMod and legacy profile URLs
//	  files.go           texture files (/textures/{hash}, /raw/{tid})
//	  render.go          avatars and previews (/avatar, /preview)
//	  yggdrasil.go       authlib-injector (/api/yggdrasil) and launcher sign-outs
//	Plumbing
//	  api.go             router, middleware, health probes, admin check
//	  events.go          /events (Server-Sent Events)
package httpapi
