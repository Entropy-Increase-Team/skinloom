package plugin

// SlotProp describes one React prop passed by the host to components mounted in a Slot.
type SlotProp struct {
	Name        string
	TypeScript  string // e.g. "number", "'steve' | 'alex' | 'cape'", "string | null"
	Optional    bool
	Description string
}

// SlotDef describes one versioned frontend extension slot rendered by the SkinLoom web app.
type SlotDef struct {
	Name        string
	Area        string // site | auth | user | skinlib | admin
	Description string
	Props       []SlotProp
}

// ThemeOverrideTargets lists the 7 built-in UI components that themes may replace
// client-side via theme.overrides (design 09 §0, §3.2).
var ThemeOverrideTargets = []string{
	"site.header",
	"site.footer",
	"site.logo",
	"auth.background",
	"home.hero",
	"texture.card",
	"player.card",
}

// FormScopes lists host forms where plugins may declaratively append input fields.
var FormScopes = []string{
	"auth.register",
	"auth.login",
	"user.profile",
	"texture.upload",
	"player.create",
}

// PluginSettingTypes lists all supported setting field types in plugin.json.
var PluginSettingTypes = []string{
	"string",
	"text",
	"markdown",
	"number",
	"boolean",
	"select",
	"multiselect",
	"color",
	"image",
	"secret",
	"json",
	"list",
	"group",
	"choice", // compatibility alias used by built-in theme manifests
}

// Slots is the canonical catalog of the 21 frontend extension slots.
var Slots = []SlotDef{
	// Site-wide
	{
		Name:        "site.header.actions",
		Area:        "site",
		Description: "Action buttons or status badges rendered in the top site header bar.",
	},
	{
		Name:        "site.footer",
		Area:        "site",
		Description: "Custom links or widgets rendered inside the global footer.",
	},
	{
		Name:        "site.banner",
		Area:        "site",
		Description: "Dismissible or announcement banner rendered directly beneath the site header.",
	},
	// Auth
	{
		Name:        "auth.login.below",
		Area:        "auth",
		Description: "Content or notices rendered underneath the sign-in form.",
	},
	{
		Name:        "auth.login.providers",
		Area:        "auth",
		Description: "External identity provider sign-in buttons rendered on the login page.",
		Props: []SlotProp{
			{Name: "next", TypeScript: "string", Optional: true, Description: "Post-login redirect path."},
		},
	},
	{
		Name:        "auth.register.below",
		Area:        "auth",
		Description: "Content or notices rendered underneath the registration form.",
	},
	{
		Name:        "auth.register.providers",
		Area:        "auth",
		Description: "External identity provider sign-up buttons rendered on the registration page.",
		Props: []SlotProp{
			{Name: "next", TypeScript: "string", Optional: true, Description: "Post-registration redirect path."},
		},
	},
	{
		Name:        "auth.captcha.widget",
		Area:        "auth",
		Description: "Custom CAPTCHA widget rendered on auth forms when captcha.provider is set to a plugin-registered provider.",
		Props: []SlotProp{
			{Name: "provider", TypeScript: "string", Description: "Active captcha.provider ID."},
			{Name: "siteKey", TypeScript: "string", Optional: true, Description: "Configured captcha site key if any."},
			{Name: "resetKey", TypeScript: "number", Description: "Incremented whenever the host resets the captcha challenge."},
			{Name: "onChange", TypeScript: "(answer: { id?: string; value: string } | null) => void", Description: "Reports the solved captcha answer (or null when cleared/expired) to the host form."},
		},
	},
	// User Center
	{
		Name:        "user.dashboard.widgets",
		Area:        "user",
		Description: "Dashboard cards displayed on the signed-in user overview page.",
	},
	{
		Name:        "user.profile.sections",
		Area:        "user",
		Description: "Additional profile or security sections appended in user settings.",
	},
	{
		Name:        "user.player.actions",
		Area:        "user",
		Description: "Per-player action buttons rendered next to rename/texture controls.",
		Props: []SlotProp{
			{Name: "playerId", TypeScript: "number"},
			{Name: "name", TypeScript: "string"},
			{Name: "uuid", TypeScript: "string"},
		},
	},
	{
		Name:        "user.closet.item.actions",
		Area:        "user",
		Description: "Action controls rendered on each texture card in the user's closet.",
		Props: []SlotProp{
			{Name: "textureId", TypeScript: "number"},
			{Name: "name", TypeScript: "string"},
			{Name: "type", TypeScript: "'steve' | 'alex' | 'cape'"},
		},
	},
	// Skin Library
	{
		Name:        "skinlib.card.badges",
		Area:        "skinlib",
		Description: "Inline badges rendered on skin library texture cards.",
		Props: []SlotProp{
			{Name: "textureId", TypeScript: "number"},
			{Name: "name", TypeScript: "string"},
			{Name: "type", TypeScript: "'steve' | 'alex' | 'cape'"},
			{Name: "public", TypeScript: "boolean"},
			{Name: "likes", TypeScript: "number"},
		},
	},
	{
		Name:        "skinlib.detail.sidebar",
		Area:        "skinlib",
		Description: "Cards rendered inside the skin detail page sidebar.",
		Props: []SlotProp{
			{Name: "textureId", TypeScript: "number"},
			{Name: "name", TypeScript: "string"},
			{Name: "type", TypeScript: "'steve' | 'alex' | 'cape'"},
			{Name: "public", TypeScript: "boolean"},
			{Name: "uploaderId", TypeScript: "number | null", Optional: true},
		},
	},
	{
		Name:        "skinlib.detail.below",
		Area:        "skinlib",
		Description: "Full-width sections rendered below the skin detail viewer.",
		Props: []SlotProp{
			{Name: "textureId", TypeScript: "number"},
			{Name: "name", TypeScript: "string"},
			{Name: "type", TypeScript: "'steve' | 'alex' | 'cape'"},
			{Name: "public", TypeScript: "boolean"},
		},
	},
	{
		Name:        "skinlib.upload.below",
		Area:        "skinlib",
		Description: "Content or hints rendered below the texture upload form.",
	},
	// Admin Panel
	{
		Name:        "admin.dashboard.widgets",
		Area:        "admin",
		Description: "Metric or status widgets rendered on the admin overview dashboard.",
	},
	{
		Name:        "admin.user.actions",
		Area:        "admin",
		Description: "Row actions rendered for each account in the admin user management table.",
		Props: []SlotProp{
			{Name: "userId", TypeScript: "number"},
			{Name: "email", TypeScript: "string"},
			{Name: "nickname", TypeScript: "string"},
			{Name: "role", TypeScript: "'user' | 'admin' | 'superadmin'"},
			{Name: "status", TypeScript: "'active' | 'banned' | 'deleted'"},
		},
	},
	{
		Name:        "admin.user.tabs",
		Area:        "admin",
		Description: "Extra view tabs rendered alongside the admin user management workspace.",
	},
	{
		Name:        "admin.texture.actions",
		Area:        "admin",
		Description: "Moderator action menu entries on a texture page or moderation list.",
		Props: []SlotProp{
			{Name: "textureId", TypeScript: "number"},
			{Name: "name", TypeScript: "string"},
			{Name: "public", TypeScript: "boolean"},
			{Name: "uploaderId", TypeScript: "number | null", Optional: true},
		},
	},
	{
		Name:        "admin.status.cards",
		Area:        "admin",
		Description: "Diagnostic cards rendered on the admin system status page.",
	},
}

// LookupSlot looks up a canonical UI slot definition by name.
func LookupSlot(name string) (SlotDef, bool) {
	for _, s := range Slots {
		if s.Name == name {
			return s, true
		}
	}
	return SlotDef{}, false
}
