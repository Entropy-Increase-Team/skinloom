package hook

// The hook catalog (docs/design/05 §4.4). Payloads are public API for plugins: add
// fields freely, but renaming or removing one, or changing what an event means, takes
// a new version and an ADR (CONTRIBUTING).
//
// Domain events carry ids and the few values a listener needs to decide whether it
// cares (a CDN purge needs the player's name); anything else it reads through the
// services. They are published in the transaction of the change (Publish) and
// delivered after it commits, at least once. Not published yet: user.loggedIn,
// user.updated, score.changed and yggdrasil.joined (hot paths: they wait for a plugin
// that needs them). Guards and filters arrive with the plugin runtime (phase 2), with
// their call sites.

// SettingsChanged is emitted after runtime settings were saved.
type SettingsChanged struct {
	Version int64    `json:"version"`
	Keys    []string `json:"keys"`
}

// OnSettingsChanged fires after a settings write is committed and the new snapshot is live.
var OnSettingsChanged = NewEvent[SettingsChanged]("settings.changed", 1,
	"Runtime settings were saved; payload lists the changed keys.")

// UserEvent is about one account.
type UserEvent struct {
	UserID int64 `json:"userId"`
	// Method is how a new account was created (user.registered): "password",
	// "setup" (the first administrator) or "social:<provider id>".
	Method string `json:"method,omitempty"`
}

var (
	OnUserRegistered = NewEvent[UserEvent]("user.registered", 1, "An account was created.")
	OnUserBanned     = NewEvent[UserEvent]("user.banned", 1, "An administrator banned an account.")
	OnUserDeleted    = NewEvent[UserEvent]("user.deleted", 1,
		"An account was removed for good (an administrator's delete, or the end of the grace period after the user deleted it). Its players, closet and private textures went with it; its public textures stay without an uploader.")
)

// PlayerEvent is about one player, as it is after the change.
type PlayerEvent struct {
	PlayerID int64  `json:"playerId"`
	UserID   int64  `json:"userId"` // the owner
	Name     string `json:"name"`
	UUID     string `json:"uuid"`
	OldName  string `json:"oldName,omitempty"` // player.renamed
}

var (
	OnPlayerCreated = NewEvent[PlayerEvent]("player.created", 1, "A player was created.")
	OnPlayerRenamed = NewEvent[PlayerEvent]("player.renamed", 1,
		"A player was renamed (with v3 UUIDs its UUID changed with the name).")
	OnPlayerUpdated = NewEvent[PlayerEvent]("player.updated", 1,
		"What a player wears changed (put on, taken off, or removed with a texture or a closet item), or its owner did: what game clients get for it is different now.")
	OnPlayerDeleted = NewEvent[PlayerEvent]("player.deleted", 1, "A player was deleted by its owner or an administrator.")
)

// TextureEvent is about one texture, as it is after the change (texture.deleted: as
// it was).
type TextureEvent struct {
	TextureID  int64  `json:"textureId"`
	UploaderID *int64 `json:"uploaderId,omitempty"`
	Hash       string `json:"hash"`
	Type       string `json:"type"` // steve | alex | cape
	Public     bool   `json:"public"`
}

var (
	OnTextureUploaded = NewEvent[TextureEvent]("texture.uploaded", 1, "A texture was uploaded (not sent for a duplicate upload).")
	OnTextureUpdated  = NewEvent[TextureEvent]("texture.updated", 1, "A texture's name, type or visibility changed.")
	OnTextureDeleted  = NewEvent[TextureEvent]("texture.deleted", 1,
		"A texture was deleted (by its uploader, an administrator, a report review, or with its uploader's account).")
)

// ClosetEvent is about one closet item.
type ClosetEvent struct {
	UserID    int64 `json:"userId"`
	TextureID int64 `json:"textureId"`
}

var (
	OnClosetAdded   = NewEvent[ClosetEvent]("closet.added", 1, "A texture was added to a closet (by its owner, an upload, or an administrator).")
	OnClosetRemoved = NewEvent[ClosetEvent]("closet.removed", 1,
		"A texture left a closet (removed, or taken out when it went private or was deleted). Not sent for closets deleted with their account.")
)

// ReportEvent is about one report.
type ReportEvent struct {
	ReportID  int64  `json:"reportId"`
	TextureID *int64 `json:"textureId,omitempty"`
	Status    string `json:"status"`           // pending | resolved | rejected
	Action    string `json:"action,omitempty"` // report.resolved: delete | ban | reject
}

var (
	OnReportSubmitted = NewEvent[ReportEvent]("report.submitted", 1, "A user reported a texture.")
	OnReportResolved  = NewEvent[ReportEvent]("report.resolved", 1,
		"An administrator reviewed a report (resolved or rejected); other pending reports a texture deletion settled are sent too.")
)
