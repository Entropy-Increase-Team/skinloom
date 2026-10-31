package score

import (
	"github.com/Entropy-Increase-Team/skinloom/internal/settings"
)

// MaxRate bounds every score rate: a rate times the KB of the largest upload, or a
// sum of awards, then stays far from int64 overflow (which would turn a cost into a
// gain).
const MaxRate = 1_000_000

// SettingDefs are the score rates (Blessing Skin equivalents in docs/design/01 §5.5).
// score.initial belongs to registration (internal/account).
func SettingDefs() []settings.Def {
	def := func(key string, value int, doc string) settings.Def {
		return settings.Def{Key: key, Type: settings.TypeInt, Public: true, Default: value, Range: settings.Between(0, MaxRate), Doc: doc}
	}
	return []settings.Def{
		def("score.per_storage", 1, "Score per KB of a public upload. (BS score_per_storage)"),
		def("score.private_per_storage", 10, "Score per KB of a private upload. (BS private_score_per_storage)"),
		def("score.per_closet_item", 0, "Score to add a texture to the closet, uploads included. (BS score_per_closet_item)"),
		def("score.award_per_texture", 0, "Score awarded for uploading a public texture. (BS score_award_per_texture)"),
		def("score.per_player", 100, "Score to create a player. (BS score_per_player)"),
		def("score.award_per_like", 0, "Score the uploader earns when someone else adds their texture to a closet. (BS score_award_per_like)"),
		def("score.sign_min", 10, "Smallest score a daily check-in gives. (BS sign_score)"),
		def("score.sign_max", 100, "Largest score a daily check-in gives. (BS sign_score)"),
		{
			Key: "score.sign_gap_hours", Type: settings.TypeInt, Public: true, Default: 24, Range: settings.Between(1, 8760),
			Doc: "Hours between check-ins, when score.sign_daily is off. (BS sign_gap_time)",
		},
		{
			Key: "score.sign_daily", Type: settings.TypeBool, Public: true, Default: false,
			Doc: "Once per calendar day (from midnight in site.timezone) instead of every score.sign_gap_hours. (BS sign_after_zero)",
		},
		{
			Key: "score.return_on_delete", Type: settings.TypeBool, Public: true, Default: true,
			Doc: "Deleting a player, taking a texture out of a closet or deleting a texture gives back what it cost (the amount actually paid). (BS return_score)",
		},
		{
			Key: "score.reporter_modification", Type: settings.TypeInt, Public: true, Default: 0, Range: settings.Between(-MaxRate, MaxRate),
			Doc: "Score change for submitting a report: negative charges it (given back when the report is upheld), positive rewards it (taken back when it is rejected). (BS reporter_score_modification)",
		},
		def("score.reporter_reward", 0, "Score awarded when a report is upheld (the texture deleted or its uploader banned). (BS reporter_reward_score)"),
	}
}

// Rates are the score settings in effect.
type Rates struct {
	PerStorage, PrivatePerStorage, PerClosetItem, AwardPerTexture int64
	PerPlayer, AwardPerLike                                       int64
	ReporterModification, ReporterReward                          int64
	ReturnOnDelete                                                bool
}

// RatesFrom reads the rates from a settings snapshot.
func RatesFrom(s *settings.Snapshot) Rates {
	return Rates{
		PerStorage:           settings.Decode[int64](s, "score.per_storage"),
		PrivatePerStorage:    settings.Decode[int64](s, "score.private_per_storage"),
		PerClosetItem:        settings.Decode[int64](s, "score.per_closet_item"),
		AwardPerTexture:      settings.Decode[int64](s, "score.award_per_texture"),
		PerPlayer:            settings.Decode[int64](s, "score.per_player"),
		AwardPerLike:         settings.Decode[int64](s, "score.award_per_like"),
		ReporterModification: settings.Decode[int64](s, "score.reporter_modification"),
		ReporterReward:       settings.Decode[int64](s, "score.reporter_reward"),
		ReturnOnDelete:       settings.Decode[bool](s, "score.return_on_delete"),
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
