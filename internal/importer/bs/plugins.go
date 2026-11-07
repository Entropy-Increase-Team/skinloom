package bs

import (
	"encoding/json"
	"strings"
)

// pluginFate is what becomes of a BS plugin's work in SkinLoom, for the report.
type pluginFate struct {
	sev Severity
	msg string
}

// pluginFates covers the plugins of the BS plugin market that a site may run; any other
// is reported as unknown. Warnings are what users or game servers will notice.
var pluginFates = map[string]pluginFate{
	"yggdrasil-api":                 {Fixed, "SkinLoom serves the Yggdrasil API itself: player UUIDs and the signing key came along"},
	"usm-api":                       {Fixed, "SkinLoom serves the UniSkinMod API itself"},
	"legacy-api":                    {Fixed, "SkinLoom serves the legacy skin and cape URLs itself"},
	"password-algorithm-transition": {Fixed, "its password methods are recognised; each password moves to argon2id when its user next signs in"},
	"config-generator":              {Dropped, "SkinLoom shows launcher and authlib-injector settings on its own launcher page"},
	"gtag-js":                       {Dropped, "add the tag through custom JavaScript (Appearance)"},
	"refresh-ali-cdn":               {Dropped, "SkinLoom's CDN purge does this: set it up under Settings, CDN"},
	"refresh-qcloud-cdn":            {Dropped, "SkinLoom's CDN purge does this: set it up under Settings, CDN"},
	"textures-aliyun-oss":           {Dropped, "the files were read from --textures; SkinLoom keeps them in its own storage (local or S3)"},
	"textures-qcloud-cos":           {Dropped, "the files were read from --textures; SkinLoom keeps them in its own storage (local or S3)"},
	"textures-qiniu-storage":        {Dropped, "the files were read from --textures; SkinLoom keeps them in its own storage (local or S3)"},
	"hitokoto":                      {Dropped, "a page decoration: nothing to import"},
	"batch-import":                  {Dropped, "an administrator's tool: nothing to import"},
	"oauth":                         {Warning, "sign-in with GitHub, Microsoft and others is SkinLoom's social login: set the providers up in the site settings; users whose email is not verified link them again from their profile"},
	"yggdrasil-connect": {Warning, "its Yggdrasil API is served by SkinLoom (UUIDs and the signing key came along), but not its Yggdrasil Connect sign-in (OAuth through a Janus server): " +
		"launchers that sign in that way stop working after the switch, and a Janus server reading BS's database no longer sees new accounts"},
	"texture-description": {Warning, "texture descriptions are not imported: SkinLoom has no descriptions yet"},
	"mojang-verification": {Warning, "verified Minecraft accounts are not imported: SkinLoom has no premium-account verification yet"},
	"invitation-codes":    {Warning, "registration by invitation code is not in SkinLoom: codes are not imported and anyone may register unless registration is closed"},
}

// planPlugins reports each plugin enabled in BS: what SkinLoom does in its place, or
// that its tables and options stay behind.
func planPlugins(d *sourceData, r *Report) {
	var list []struct{ Name, Version string }
	if json.Unmarshal([]byte(d.Options["plugins_enabled"]), &list) != nil {
		return
	}
	for _, p := range list {
		name := strings.TrimSpace(p.Name)
		if name == "" {
			continue
		}
		f, ok := pluginFates[name]
		if !ok && strings.HasPrefix(name, "oauth-") {
			f, ok = pluginFate{Dropped, "a provider for the oauth plugin: set it up in SkinLoom's social login"}, true
		}
		if !ok {
			f = pluginFate{Warning, "SkinLoom does not know this plugin: its tables and options are not imported; redo by hand what users relied on"}
		}
		if v := strings.TrimSpace(p.Version); v != "" {
			name += " " + v
		}
		if f.sev == Warning {
			r.leave(f.sev, "plugins", "%s: %s", name, f.msg)
		} else {
			r.add(f.sev, "plugins", 0, "%s: %s", name, f.msg)
		}
	}
}
