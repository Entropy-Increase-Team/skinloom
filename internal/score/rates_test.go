package score

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/Entropy-Increase-Team/skinloom/internal/settings"
)

// The score rates had no upper bound, so the registry accepted any integer up to 2^53,
// and a large private rate times the KB of an upload overflowed int64: the "cost"
// turned into a large gain of score. Every rate is now at most MaxRate, and the most
// expensive upload at that rate still costs what it should.
func TestRatesCannotOverflowUploadCost(t *testing.T) {
	reg := settings.NewRegistry()
	if err := reg.Register(SettingDefs()...); err != nil {
		t.Fatal(err)
	}
	for _, d := range reg.All() {
		if d.Type == settings.TypeInt && (d.Range == nil || d.Range.Max > MaxRate) {
			t.Errorf("%s has no upper bound of at most %d", d.Key, MaxRate)
		}
	}
	snap := settings.NewSnapshot(reg, 1, map[string]json.RawMessage{
		"score.private_per_storage": json.RawMessage(strconv.Itoa(MaxRate)),
	})
	if cost := RatesFrom(snap).UploadCost(8192*1024, false); cost != 8192*MaxRate {
		t.Fatalf("an 8 MB private upload at the highest rate costs %d, want %d", cost, 8192*MaxRate)
	}
}
