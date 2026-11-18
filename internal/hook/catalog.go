package hook

import "strings"

// The hook catalog (docs/design/05 §4.4, docs/design/09 §2.2). Payloads are public
// API for plugins: add fields freely, but renaming or removing one, or changing
// what an event means, takes a new version and an ADR (CONTRIBUTING).
//
// Domain events carry ids and the few values a listener needs to decide whether it
// cares (a CDN purge needs the player's name); anything else it reads through the
// services. Persistent events are published in the transaction of the change
// (Publish) and delivered after commit, at least once; Listening(b) short-circuits
// when no handler is registered so hot paths pay zero serialization/outbox cost.

// RedactForm clones form while dropping sensitive credential fields (passwords,
// passphrases, secrets) so extension fields like invite_code remain available to
// user.registered listeners without leaking credentials to the outbox or plugins.
func RedactForm(form map[string]any) map[string]any {
	if len(form) == 0 {
		return nil
	}
	out := make(map[string]any, len(form))
	for k, v := range form {
		lower := strings.ToLower(k)
		if strings.Contains(lower, "password") || strings.Contains(lower, "secret") ||
			lower == "pass" || lower == "pwd" {
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// --- Events (19 total) ---

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
	// Form carries non-sensitive plugin extension fields from registration.
	Form map[string]any `json:"form,omitempty"`
}

// UserLoginEvent is emitted when an account signs in.
type UserLoginEvent struct {
	UserID int64  `json:"userId"`
	IP     string `json:"ip,omitempty"`
	Method string `json:"method"` // password | social:<id> | ...
}

// UserUpdatedEvent is emitted when account fields change.
type UserUpdatedEvent struct {
	UserID int64    `json:"userId"`
	Fields []string `json:"fields"`
}

// ScoreChangedEvent is emitted whenever a user's score ledger changes.
type ScoreChangedEvent struct {
	UserID       int64  `json:"userId"`
	Delta        int64  `json:"delta"`
	BalanceAfter int64  `json:"balanceAfter"`
	Reason       string `json:"reason"`
}

// YggdrasilJoinedEvent is emitted after a client finishes a Yggdrasil session join.
type YggdrasilJoinedEvent struct {
	UserID   int64  `json:"userId"`
	PlayerID int64  `json:"playerId"`
	UUID     string `json:"uuid"`
	ServerID string `json:"serverId"`
	IP       string `json:"ip,omitempty"`
}

var (
	OnUserRegistered = NewEvent[UserEvent]("user.registered", 1, "An account was created.")
	OnUserLoggedIn   = NewEvent[UserLoginEvent]("user.loggedIn", 1, "A user logged in.")
	OnUserUpdated    = NewEvent[UserUpdatedEvent]("user.updated", 1, "An account's fields were updated.")
	OnUserBanned     = NewEvent[UserEvent]("user.banned", 1, "An administrator banned an account.")
	OnUserDeleted    = NewEvent[UserEvent]("user.deleted", 1,
		"An account was removed for good (an administrator's delete, or the end of the grace period after the user deleted it). Its players (each announced as player.deleted first), closet and private textures went with it; its public textures stay without an uploader.")
	OnScoreChanged    = NewEvent[ScoreChangedEvent]("score.changed", 1, "A user's score changed via the ledger.")
	OnYggdrasilJoined = NewEvent[YggdrasilJoinedEvent]("yggdrasil.joined", 1, "A player joined a game server via Yggdrasil sessionserver.")
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
		"What a player wears changed (put on, taken off, or removed with a texture or a closet item), or its owner's account was banned, deleted or restored: what game clients get for it is different now.")
	OnPlayerDeleted = NewEvent[PlayerEvent]("player.deleted", 1, "A player was deleted by its owner or an administrator, or with its account.")
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

// --- Guards (21 total) ---

type AuthLoginGuard struct {
	Identifier string         `json:"identifier"`
	UserID     int64          `json:"userId"`
	IP         string         `json:"ip,omitempty"`
	Form       map[string]any `json:"form,omitempty"`
}

type AuthRegisterGuard struct {
	Email      string         `json:"email"`
	Nickname   string         `json:"nickname"`
	PlayerName string         `json:"playerName,omitempty"`
	IP         string         `json:"ip,omitempty"`
	Form       map[string]any `json:"form,omitempty"`
}

type PlayerCreateGuard struct {
	UserID int64          `json:"userId"`
	Name   string         `json:"name"`
	Form   map[string]any `json:"form,omitempty"`
}

type PlayerRenameGuard struct {
	UserID   int64  `json:"userId"`
	PlayerID int64  `json:"playerId"`
	OldName  string `json:"oldName"`
	NewName  string `json:"newName"`
}

type PlayerDeleteGuard struct {
	UserID   int64  `json:"userId"`
	PlayerID int64  `json:"playerId"`
	Name     string `json:"name"`
}

type PlayerTexturesSetGuard struct {
	UserID   int64  `json:"userId"`
	PlayerID int64  `json:"playerId"`
	SkinID   *int64 `json:"skinId,omitempty"`
	CapeID   *int64 `json:"capeId,omitempty"`
}

type PlayerTexturesClearGuard struct {
	UserID   int64    `json:"userId"`
	PlayerID int64    `json:"playerId"`
	Types    []string `json:"types"`
}

type TextureUploadGuard struct {
	UserID    int64          `json:"userId"`
	Name      string         `json:"name"`
	Type      string         `json:"type"`
	Public    bool           `json:"public"`
	Width     int            `json:"width"`
	Height    int            `json:"height"`
	SizeBytes int64          `json:"sizeBytes"`
	Hash      string         `json:"hash"`
	Form      map[string]any `json:"form,omitempty"`
}

type TextureRenameGuard struct {
	ActorID   int64  `json:"actorId"`
	TextureID int64  `json:"textureId"`
	OldName   string `json:"oldName"`
	NewName   string `json:"newName"`
}

type TextureVisibilityGuard struct {
	ActorID   int64 `json:"actorId"`
	TextureID int64 `json:"textureId"`
	Public    bool  `json:"public"`
}

type TextureTypeGuard struct {
	ActorID   int64  `json:"actorId"`
	TextureID int64  `json:"textureId"`
	OldType   string `json:"oldType"`
	NewType   string `json:"newType"`
}

type TextureDeleteGuard struct {
	ActorID   int64  `json:"actorId"`
	TextureID int64  `json:"textureId"`
	Hash      string `json:"hash"`
}

type ClosetAddGuard struct {
	UserID    int64  `json:"userId"`
	TextureID int64  `json:"textureId"`
	Name      string `json:"name"`
}

type ClosetRenameGuard struct {
	UserID    int64  `json:"userId"`
	TextureID int64  `json:"textureId"`
	OldName   string `json:"oldName"`
	NewName   string `json:"newName"`
}

type ClosetRemoveGuard struct {
	UserID    int64 `json:"userId"`
	TextureID int64 `json:"textureId"`
}

type ScoreCheckinGuard struct {
	UserID int64 `json:"userId"`
}

type UserProfileUpdateGuard struct {
	UserID int64          `json:"userId"`
	Field  string         `json:"field"`
	Value  string         `json:"value"`
	Form   map[string]any `json:"form,omitempty"`
}

type UserAvatarUpdateGuard struct {
	UserID    int64  `json:"userId"`
	TextureID *int64 `json:"textureId,omitempty"`
}

type ReportSubmitGuard struct {
	ReporterID int64  `json:"reporterId"`
	TextureID  int64  `json:"textureId"`
	Reason     string `json:"reason"`
}

type YggdrasilAuthGuard struct {
	UserID     int64  `json:"userId"`
	Identifier string `json:"identifier"`
	IP         string `json:"ip,omitempty"`
}

type YggdrasilJoinGuard struct {
	UserID   int64  `json:"userId"`
	PlayerID int64  `json:"playerId"`
	UUID     string `json:"uuid"`
	ServerID string `json:"serverId"`
	IP       string `json:"ip,omitempty"`
}

var (
	GuardAuthLogin             = NewGuard[AuthLoginGuard]("auth.login", 1, FailClosed, "Guard checked before completing password login.")
	GuardAuthRegister          = NewGuard[AuthRegisterGuard]("auth.register", 1, FailClosed, "Guard checked before registering an account.")
	GuardPlayerCreate          = NewGuard[PlayerCreateGuard]("player.create", 1, FailClosed, "Guard checked before creating a player.")
	GuardPlayerRename          = NewGuard[PlayerRenameGuard]("player.rename", 1, FailClosed, "Guard checked before renaming a player.")
	GuardPlayerDelete          = NewGuard[PlayerDeleteGuard]("player.delete", 1, FailClosed, "Guard checked before deleting a player.")
	GuardPlayerTexturesSet     = NewGuard[PlayerTexturesSetGuard]("player.textures.set", 1, FailClosed, "Guard checked before setting player textures.")
	GuardPlayerTexturesClear   = NewGuard[PlayerTexturesClearGuard]("player.textures.clear", 1, FailClosed, "Guard checked before clearing player textures.")
	GuardTextureUpload         = NewGuard[TextureUploadGuard]("texture.upload", 1, FailClosed, "Guard checked before persisting an uploaded texture.")
	GuardTextureRename         = NewGuard[TextureRenameGuard]("texture.rename", 1, FailClosed, "Guard checked before renaming a texture.")
	GuardTextureVisibility     = NewGuard[TextureVisibilityGuard]("texture.visibility", 1, FailClosed, "Guard checked before changing texture visibility.")
	GuardTextureType           = NewGuard[TextureTypeGuard]("texture.type", 1, FailClosed, "Guard checked before changing a texture's model type.")
	GuardTextureDelete         = NewGuard[TextureDeleteGuard]("texture.delete", 1, FailClosed, "Guard checked before deleting a texture.")
	GuardClosetAdd             = NewGuard[ClosetAddGuard]("closet.add", 1, FailClosed, "Guard checked before adding a texture to a closet.")
	GuardClosetRename          = NewGuard[ClosetRenameGuard]("closet.rename", 1, FailClosed, "Guard checked before renaming a closet item.")
	GuardClosetRemove          = NewGuard[ClosetRemoveGuard]("closet.remove", 1, FailClosed, "Guard checked before removing a closet item.")
	GuardScoreCheckin          = NewGuard[ScoreCheckinGuard]("score.checkin", 1, FailClosed, "Guard checked before awarding a daily check-in.")
	GuardUserProfileUpdate     = NewGuard[UserProfileUpdateGuard]("user.profile.update", 1, FailClosed, "Guard checked before updating profile fields.")
	GuardUserAvatarUpdate      = NewGuard[UserAvatarUpdateGuard]("user.avatar.update", 1, FailClosed, "Guard checked before updating a user's avatar.")
	GuardReportSubmit          = NewGuard[ReportSubmitGuard]("report.submit", 1, FailClosed, "Guard checked before recording a texture report.")
	GuardYggdrasilAuthenticate = NewGuard[YggdrasilAuthGuard]("yggdrasil.authenticate", 1, FailOpen, "Guard checked before issuing a Yggdrasil token.")
	GuardYggdrasilJoin         = NewGuard[YggdrasilJoinGuard]("yggdrasil.join", 1, FailOpen, "Guard checked before recording a Yggdrasil server join.")
)

// --- Filters (7 total) ---

type PlayerNameFilterContext struct {
	UserID   int64  `json:"userId"`
	PlayerID *int64 `json:"playerId,omitempty"`
}

type TextureNameFilterContext struct {
	UserID    int64  `json:"userId"`
	TextureID *int64 `json:"textureId,omitempty"`
	Type      string `json:"type"`
}

type ClosetNameFilterContext struct {
	UserID    int64 `json:"userId"`
	TextureID int64 `json:"textureId"`
}

type CheckinFilterContext struct {
	UserID          int64 `json:"userId"`
	ConsecutiveDays int   `json:"consecutiveDays"`
}

type UserBadge struct {
	Text  string `json:"text"`
	Color string `json:"color,omitempty"`
	Icon  string `json:"icon,omitempty"`
}

type UserBadgeFilterContext struct {
	UserID int64  `json:"userId"`
	Role   string `json:"role"`
}

type ProfileTextures struct {
	SkinHash  string `json:"skinHash,omitempty"`
	SkinModel string `json:"skinModel,omitempty"` // default | slim
	CapeHash  string `json:"capeHash,omitempty"`
}

type ProfileTexturesFilterContext struct {
	PlayerID int64  `json:"playerId"`
	Name     string `json:"name"`
	UUID     string `json:"uuid"`
	Protocol string `json:"protocol"` // csl | usm | yggdrasil
}

type YggProperty struct {
	Name      string `json:"name"`
	Value     string `json:"value"`
	Signature string `json:"signature,omitempty"`
}

type YggPropertiesFilterContext struct {
	PlayerID int64  `json:"playerId"`
	UUID     string `json:"uuid"`
	Name     string `json:"name"`
	Signed   bool   `json:"signed"`
}

var (
	FilterPlayerName                 = NewFilter[string, PlayerNameFilterContext]("player.name", 1, "Filters a player name before validation on create or rename.")
	FilterTextureName                = NewFilter[string, TextureNameFilterContext]("texture.name", 1, "Filters a texture name on upload or rename.")
	FilterClosetItemName             = NewFilter[string, ClosetNameFilterContext]("closet.itemName", 1, "Filters a closet item name on add or rename.")
	FilterScoreCheckinAmount         = NewFilter[int64, CheckinFilterContext]("score.checkinAmount", 1, "Filters the daily check-in score reward before granting.")
	FilterUserBadges                 = NewFilter[[]UserBadge, UserBadgeFilterContext]("user.badges", 1, "Filters custom user badges shown in profile, texture uploader and admin views.")
	FilterProfileTextures            = NewFilter[ProfileTextures, ProfileTexturesFilterContext]("profile.textures", 1, "Filters resolved textures for CSL, USM, or Yggdrasil profiles.")
	FilterYggdrasilProfileProperties = NewFilter[[]YggProperty, YggPropertiesFilterContext]("yggdrasil.profileProperties", 1, "Filters Yggdrasil sessionserver profile properties before returning.")
)
