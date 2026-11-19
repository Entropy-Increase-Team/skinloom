package texture

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/Entropy-Increase-Team/skinloom/internal/modules/score"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/dbtest"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/storage"
)

type stubSearchProvider struct {
	mu      sync.Mutex
	indexed []SearchDoc
	deleted []int64
	ids     []int64
	err     error
}

func (s *stubSearchProvider) Index(_ context.Context, doc SearchDoc) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.indexed = append(s.indexed, doc)
	return nil
}

func (s *stubSearchProvider) Delete(_ context.Context, textureID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleted = append(s.deleted, textureID)
	return nil
}

func (s *stubSearchProvider) Search(_ context.Context, _ SearchQuery) (SearchResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return SearchResult{}, s.err
	}
	return SearchResult{IDs: append([]int64(nil), s.ids...), Total: int64(len(s.ids))}, nil
}

func TestSearchProviderHotSwapVisibilityAndFallback(t *testing.T) {
	dbtest.ForEach(t, func(t *testing.T, d *db.DB) {
		ctx := context.Background()
		store, err := storage.NewLocal(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		svc := NewService(d.Ent, store, quiet)

		u, err := d.Ent.User.Create().SetEmail("s@example.com").SetEmailNormalized("s@example.com").SetNickname("s").SetPasswordHash("x").SetScore(1000).Save(ctx)
		if err != nil {
			t.Fatal(err)
		}

		sp := &stubSearchProvider{}
		svc.SetSearchProvider("plugin:search-a", sp)
		if svc.SearchProviderOwner() != "plugin:search-a" {
			t.Fatalf("owner = %q", svc.SearchProviderOwner())
		}

		pol := Policy{MaxBytes: 1 << 20, MaxWidth: 1024}
		r1, err := svc.Upload(ctx, u.ID, UploadInput{Name: "Knight Public", Type: "steve", Public: true, Data: pngOf(t, 64, 64, 1)}, pol, score.Rates{})
		if err != nil {
			t.Fatal(err)
		}
		r2, err := svc.Upload(ctx, u.ID, UploadInput{Name: "Secret Private", Type: "steve", Public: false, Data: pngOf(t, 64, 64, 2)}, pol, score.Rates{})
		if err != nil {
			t.Fatal(err)
		}
		if len(sp.indexed) != 2 {
			t.Fatalf("expected 2 indexed docs, got %d", len(sp.indexed))
		}

		// Return private + public IDs; guest viewer must only receive the public texture!
		sp.ids = []int64{r2.Texture.ID, r1.Texture.ID}
		list, _, err := svc.List(ctx, Filter{Query: "anything"}, Viewer{})
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 1 || list[0].ID != r1.Texture.ID {
			t.Fatalf("expected only public texture %d, got %v", r1.Texture.ID, list)
		}

		// Provider error falls back transparently to built-in SQL LIKE query.
		sp.err = errors.New("engine offline")
		list, _, err = svc.List(ctx, Filter{Query: "Knight"}, Viewer{})
		if err != nil || len(list) != 1 || list[0].ID != r1.Texture.ID {
			t.Fatalf("expected SQL fallback match, got len=%d err=%v", len(list), err)
		}

		// Delete triggers SearchProvider.Delete.
		if err := svc.Delete(ctx, Viewer{UserID: u.ID}, r1.Texture.ID, false, nil); err != nil {
			t.Fatal(err)
		}
		if len(sp.deleted) != 1 || sp.deleted[0] != r1.Texture.ID {
			t.Fatalf("expected deleted=[%d], got %v", r1.Texture.ID, sp.deleted)
		}

		// Owner-guarded clear.
		svc.ClearSearchProvider("plugin:other")
		if svc.SearchProviderOwner() != "plugin:search-a" {
			t.Fatalf("stale clear removed active provider")
		}
		svc.ClearSearchProvider("plugin:search-a")
		if svc.SearchProviderOwner() != "" {
			t.Fatalf("expected empty owner after clear")
		}
	})
}
