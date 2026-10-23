package hook

// Core hooks available from M0. Domain hooks (auth.register, texture.upload, …) are
// declared by their modules as those modules land; see docs/design/05 §4.4 for the plan.

// SettingsChanged is emitted after runtime settings were saved.
type SettingsChanged struct {
	Version int64    `json:"version"`
	Keys    []string `json:"keys"`
}

// OnSettingsChanged fires after a settings write is committed and the new snapshot is live.
var OnSettingsChanged = NewEvent[SettingsChanged]("settings.changed", 1,
	"Runtime settings were saved; payload lists the changed keys.")
