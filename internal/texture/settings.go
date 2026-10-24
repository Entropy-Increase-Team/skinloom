package texture

import (
	"fmt"
	"regexp"
	"sync"

	"github.com/Entropy-Increase-Team/skinloom/internal/settings"
)

// SettingDefs are the texture settings (Blessing Skin equivalents in docs/design/02 §4).
func SettingDefs() []settings.Def {
	return []settings.Def{
		{
			Key: "texture.max_size_kb", Type: settings.TypeInt, Public: true, Default: 1024, Validate: between(1, MaxUploadKB),
			Doc: fmt.Sprintf("Largest upload in KB, at most %d. (BS max_upload_file_size)", MaxUploadKB),
		},
		{
			Key: "texture.max_width", Type: settings.TypeInt, Public: true, Default: MaxWidth, Validate: between(64, MaxWidth),
			Doc: fmt.Sprintf("Widest texture in pixels, at most %d. (BS max_texture_width)", MaxWidth),
		},
		{
			Key: "texture.name_regexp", Type: settings.TypeString, Default: "",
			Doc: "Texture names must match this regular expression (Go RE2 syntax); empty = any name. (BS texture_name_regexp)",
			Validate: func(v any) error {
				_, err := regexp.Compile(v.(string))
				return err
			},
		},
		{
			Key: "texture.allow_download", Type: settings.TypeBool, Public: true, Default: true,
			Doc: "Whether /raw/{tid} serves the original file. (BS allow_downloading_texture)",
		},
		{
			Key: "texture.private_status", Type: settings.TypeInt, Default: 403,
			Doc: "Status for a private texture requested by someone else: 403, or 404 to hide that it exists. (BS status_code_for_private)",
			Validate: func(v any) error {
				if n := toInt(v); n != 403 && n != 404 {
					return fmt.Errorf("want 403 or 404")
				}
				return nil
			},
		},
		{
			Key: "texture.content_policy", Type: settings.TypeLocalized, Public: true, Default: map[string]string{},
			Doc: "Content policy shown on the upload page (Markdown). (BS content_policy)",
		},
	}
}

func between(lo, hi int64) func(any) error {
	return func(v any) error {
		if n := toInt(v); n < lo || n > hi {
			return fmt.Errorf("must be %d to %d", lo, hi)
		}
		return nil
	}
}

func toInt(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int:
		return int64(n)
	case int64:
		return n
	}
	return 0
}

// Policy is what uploads and downloads need from the settings.
type Policy struct {
	MaxBytes      int64
	MaxWidth      int
	NameRule      *regexp.Regexp // nil = any name
	AllowDownload bool
	PrivateStatus int // 403 or 404
}

var rules sync.Map // pattern → *regexp.Regexp, so a snapshot read does not recompile

// PolicyFrom reads the policy from a settings snapshot.
func PolicyFrom(s *settings.Snapshot) Policy {
	p := Policy{
		MaxBytes:      settings.Decode[int64](s, "texture.max_size_kb") * 1024,
		MaxWidth:      settings.Decode[int](s, "texture.max_width"),
		AllowDownload: settings.Decode[bool](s, "texture.allow_download"),
		PrivateStatus: settings.Decode[int](s, "texture.private_status"),
	}
	if pattern := s.String("texture.name_regexp"); pattern != "" {
		if re, ok := rules.Load(pattern); ok {
			p.NameRule = re.(*regexp.Regexp)
		} else if re, err := regexp.Compile(pattern); err == nil { // validated on save
			rules.Store(pattern, re)
			p.NameRule = re
		}
	}
	return p
}
