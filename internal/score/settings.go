package score

import (
	"fmt"

	"github.com/Entropy-Increase-Team/skinloom/internal/settings"
)

// SettingDefs are the score rates (Blessing Skin equivalents in docs/design/01 §5.5).
// score.initial belongs to registration (internal/account).
func SettingDefs() []settings.Def {
	nonNegative := func(v any) error {
		var negative bool
		switch n := v.(type) {
		case float64:
			negative = n < 0
		case int:
			negative = n < 0
		case int64:
			negative = n < 0
		}
		if negative {
			return fmt.Errorf("must be 0 or more")
		}
		return nil
	}
	def := func(key string, value int, doc string) settings.Def {
		return settings.Def{Key: key, Type: settings.TypeInt, Public: true, Default: value, Validate: nonNegative, Doc: doc}
	}
	return []settings.Def{
		def("score.per_storage", 1, "Score per KB of a public upload. (BS score_per_storage)"),
		def("score.private_per_storage", 10, "Score per KB of a private upload. (BS private_score_per_storage)"),
		def("score.per_closet_item", 0, "Score to add a texture to the closet, uploads included. (BS score_per_closet_item)"),
		def("score.award_per_texture", 0, "Score awarded for uploading a public texture. (BS score_award_per_texture)"),
	}
}

// Rates are the score settings in effect.
type Rates struct {
	PerStorage, PrivatePerStorage, PerClosetItem, AwardPerTexture int64
}

// RatesFrom reads the rates from a settings snapshot.
func RatesFrom(s *settings.Snapshot) Rates {
	return Rates{
		PerStorage:        settings.Decode[int64](s, "score.per_storage"),
		PrivatePerStorage: settings.Decode[int64](s, "score.private_per_storage"),
		PerClosetItem:     settings.Decode[int64](s, "score.per_closet_item"),
		AwardPerTexture:   settings.Decode[int64](s, "score.award_per_texture"),
	}
}

// UploadCost is what an upload of size bytes costs: every started KB at the public
// or private rate, plus a closet item, minus the award, which only public textures
// earn (Blessing Skin also awarded private uploads, then took the award back when a
// texture went private and never returned it, C12). It can be negative: a gain.
func (r Rates) UploadCost(size int64, public bool) int64 {
	kb := (size + 1023) / 1024
	if public {
		return kb*r.PerStorage + r.PerClosetItem - r.AwardPerTexture
	}
	return kb*r.PrivatePerStorage + r.PerClosetItem
}
