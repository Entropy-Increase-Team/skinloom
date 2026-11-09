package settings_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
)

// ADR 0034: the first save of a setting with secret parts has no row for
// restore's "UPDATE settings SET updated_at" to lock. On MySQL (REPEATABLE READ) that
// UPDATE takes a gap lock instead; two first saves at once both hold one and then both
// INSERT into the gap, so one fails with a deadlock (1213). SQLite (single writer) and
// PostgreSQL (no gap locks) pass; run with SKINLOOM_TEST_MYSQL to see it.
func TestConcurrentFirstSaveOfRestoreSetting(t *testing.T) {
	forEachDB(t, func(t *testing.T, database *db.DB) {
		ctx := context.Background()
		svc, _ := newService(t, database)
		for round := 1; round <= 20; round++ {
			def := credentials()
			def.Key = fmt.Sprintf("t.first%d", round)
			if err := svc.Registry().Register(def); err != nil {
				t.Fatal(err)
			}
			var wg sync.WaitGroup
			errs := make([]error, 2)
			for i := range errs {
				wg.Add(1)
				go func() {
					defer wg.Done()
					_, errs[i] = svc.Set(ctx, nil, map[string]json.RawMessage{
						def.Key: json.RawMessage(fmt.Sprintf(`{"user":"u%d","secret":"s%d"}`, i, i)),
					}, nil)
				}()
			}
			wg.Wait()
			if err := errors.Join(errs...); err != nil {
				t.Fatalf("round %d: concurrent first saves: %v", round, err)
			}
		}
	})
}
