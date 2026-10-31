package player

import (
	"fmt"
	"regexp"
	"sync"

	"github.com/Entropy-Increase-Team/skinloom/internal/settings"
)

// Name rules (Blessing Skin player_name_rule).
const (
	RuleOfficial = "official" // Mojang's: letters, digits, underscore
	RuleCJK      = "cjk"      // also Chinese characters (and §, as Blessing Skin)
	RuleUTF8     = "utf8"     // any printable characters except spaces
	RuleCustom   = "custom"   // player.name_regexp
)

// MaxNameLen is the longest name the database holds, in characters.
const MaxNameLen = 32

// SettingDefs are the player and profile delivery settings (Blessing Skin equivalents
// in docs/design/02 §4).
func SettingDefs() []settings.Def {
	return []settings.Def{
		{
			Key: "player.name_rule", Type: settings.TypeString, Public: true, Default: RuleOfficial,
			Enum: []any{RuleOfficial, RuleCJK, RuleUTF8, RuleCustom},
			Doc:  "official: letters, digits and _ (Mojang's rule); cjk: also Chinese characters; utf8: any printable characters except spaces; custom: player.name_regexp. (BS player_name_rule)",
		},
		{
			Key: "player.name_regexp", Type: settings.TypeString, Default: "",
			Doc: "Regular expression for player.name_rule = custom (Go RE2 syntax). (BS custom_player_name_regexp)",
			Validate: func(v any) error {
				_, err := regexp.Compile(v.(string))
				return err
			},
		},
		{Key: "player.name_length_min", Type: settings.TypeInt, Public: true, Default: 3, Range: settings.Between(1, MaxNameLen),
			Doc: "Shortest player name, in characters. (BS player_name_length_min)"},
		{Key: "player.name_length_max", Type: settings.TypeInt, Public: true, Default: 16, Range: settings.Between(1, MaxNameLen),
			Doc: fmt.Sprintf("Longest player name, in characters, at most %d. (BS player_name_length_max)", MaxNameLen)},
		{
			Key: "yggdrasil.uuid_algorithm", Type: settings.TypeString, Default: "v3", Enum: []any{"v3", "v4"},
			Doc: "UUIDs of new players. v3: derived from the name like offline-mode servers (a rename gives a new UUID); v4: random, kept on rename. (BS ygg_uuid_algorithm)",
		},
		{
			Key: "delivery.profile_cache_seconds", Type: settings.TypeInt, Default: 60, Range: settings.Between(0, 31536000),
			Doc: "How long game clients and CDNs may cache /{player}.json and the other profile APIs. Blessing Skin's cache_expire_time defaulted to a year, which kept old skins in caches.",
		},
		{
			Key: "delivery.missing_player_status", Type: settings.TypeInt, Default: 404, Enum: []any{404, 204},
			Doc: "Status of /{player}.json for a name nobody uses: 404, or 204 for clients that try the next skin site on it. (BS plugin 204-for-unexisted-players)",
		},
	}
}

// Policy is what player management needs from the settings.
type Policy struct {
	Rule     string
	Custom   *regexp.Regexp // RuleCustom; nil = any name
	Min, Max int
	UUIDv4   bool
}

var customRules sync.Map // pattern → *regexp.Regexp

// PolicyFrom reads the policy from a settings snapshot.
func PolicyFrom(s *settings.Snapshot) Policy {
	p := Policy{
		Rule:   s.String("player.name_rule"),
		Min:    settings.Decode[int](s, "player.name_length_min"),
		Max:    settings.Decode[int](s, "player.name_length_max"),
		UUIDv4: s.String("yggdrasil.uuid_algorithm") == "v4",
	}
	p.Max = max(p.Max, p.Min) // a misconfigured range still lets names of length min through
	if pattern := s.String("player.name_regexp"); p.Rule == RuleCustom && pattern != "" {
		if re, ok := customRules.Load(pattern); ok {
			p.Custom = re.(*regexp.Regexp)
		} else if re, err := regexp.Compile(pattern); err == nil { // validated on save
			customRules.Store(pattern, re)
			p.Custom = re
		}
	}
	return p
}

// Delivery is how the game APIs answer.
type Delivery struct {
	CacheSeconds  int
	MissingStatus int // 404 or 204
}

// DeliveryFrom reads the delivery settings from a snapshot.
func DeliveryFrom(s *settings.Snapshot) Delivery {
	return Delivery{
		CacheSeconds:  settings.Decode[int](s, "delivery.profile_cache_seconds"),
		MissingStatus: settings.Decode[int](s, "delivery.missing_player_status"),
	}
}
