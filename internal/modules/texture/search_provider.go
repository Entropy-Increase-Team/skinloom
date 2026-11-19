package texture

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	tex "github.com/Entropy-Increase-Team/skinloom/ent/texture"
)

const searchProviderCursorPrefix = "sp:"

// SearchDoc describes a texture document pushed to an external SearchProvider.
type SearchDoc struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	Type       string    `json:"type"`
	Hash       string    `json:"hash"`
	UploaderID int64     `json:"uploaderId,omitempty"`
	Public     bool      `json:"public"`
	Likes      int       `json:"likes"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// SearchQuery carries full-text search parameters delegated to a SearchProvider.
type SearchQuery struct {
	Query          string
	Category       string
	UploaderID     int64
	IncludePrivate bool
	Sort           string
	Cursor         string
	Limit          int
}

// SearchResult holds ranked texture IDs and pagination cursor from a SearchProvider.
type SearchResult struct {
	IDs        []int64
	NextCursor string
	Total      int64
}

// SearchProvider is a hot-swappable full-text search backend registered by an
// authorized native plugin with the "provider.search" permission.
type SearchProvider interface {
	Index(ctx context.Context, doc SearchDoc) error
	Delete(ctx context.Context, textureID int64) error
	Search(ctx context.Context, q SearchQuery) (SearchResult, error)
}

type searchProviderEntry struct {
	owner    string
	provider SearchProvider
}

type searchProviderSlot struct {
	entry atomic.Pointer[searchProviderEntry]
}

// SetSearchProvider atomically installs p under owner (e.g. "plugin:<id>").
// Passing nil removes any custom search provider.
func (s *Service) SetSearchProvider(owner string, p SearchProvider) {
	if p == nil {
		s.search.entry.Store(nil)
		return
	}
	s.search.entry.Store(&searchProviderEntry{owner: owner, provider: p})
}

// ClearSearchProvider removes the active SearchProvider only if it is owned by owner.
func (s *Service) ClearSearchProvider(owner string) {
	for {
		cur := s.search.entry.Load()
		if cur == nil || cur.owner != owner {
			return
		}
		if s.search.entry.CompareAndSwap(cur, nil) {
			return
		}
	}
}

// SearchProviderOwner returns the owner of the currently registered SearchProvider, or "".
func (s *Service) SearchProviderOwner() string {
	if cur := s.search.entry.Load(); cur != nil {
		return cur.owner
	}
	return ""
}

func (s *Service) activeSearchProvider() SearchProvider {
	if cur := s.search.entry.Load(); cur != nil {
		return cur.provider
	}
	return nil
}

func searchDocFromTexture(t *ent.Texture) SearchDoc {
	var uid int64
	if t.UploaderID != nil {
		uid = *t.UploaderID
	}
	return SearchDoc{
		ID:         t.ID,
		Name:       t.Name,
		Type:       string(t.Type),
		Hash:       t.Hash,
		UploaderID: uid,
		Public:     t.Visibility == tex.VisibilityPublic,
		Likes:      t.Likes,
		CreatedAt:  t.CreatedAt,
		UpdatedAt:  t.UpdatedAt,
	}
}

func (s *Service) notifySearchIndex(ctx context.Context, t *ent.Texture) {
	sp := s.activeSearchProvider()
	if sp == nil || t == nil {
		return
	}
	doc := searchDocFromTexture(t)
	defer func() {
		if r := recover(); r != nil && s.log != nil {
			s.log.Warn("search provider index panic recovered", "texture", doc.ID, "panic", r)
		}
	}()
	callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := sp.Index(callCtx, doc); err != nil && s.log != nil {
		s.log.Warn("search provider index failed", "texture", doc.ID, "err", err)
	}
}

func (s *Service) notifySearchDelete(ctx context.Context, textureID int64) {
	sp := s.activeSearchProvider()
	if sp == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil && s.log != nil {
			s.log.Warn("search provider delete panic recovered", "texture", textureID, "panic", r)
		}
	}()
	callCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := sp.Delete(callCtx, textureID); err != nil && s.log != nil {
		s.log.Warn("search provider delete failed", "texture", textureID, "err", err)
	}
}

func matchesTextureCategory(category string, tType tex.Type) bool {
	switch category {
	case "", "all":
		return true
	case "skin":
		return tType == tex.TypeSteve || tType == tex.TypeAlex
	case "steve":
		return tType == tex.TypeSteve
	case "alex":
		return tType == tex.TypeAlex
	case "cape":
		return tType == tex.TypeCape
	default:
		return false
	}
}

func safeSearchProviderQuery(ctx context.Context, sp SearchProvider, q SearchQuery) (res SearchResult, err error) {
	defer func() {
		if r := recover(); r != nil {
			res = SearchResult{}
			err = fmt.Errorf("search provider panic: %v", r)
		}
	}()
	return sp.Search(ctx, q)
}

// listViaSearchProvider executes f against the active SearchProvider and hydrates
// matching ent.Texture rows while strictly re-checking visibility and category invariants.
func (s *Service) listViaSearchProvider(ctx context.Context, sp SearchProvider, f Filter, v Viewer, limit int) ([]*ent.Texture, string, bool) {
	includePrivate := f.UploaderID > 0 && (f.UploaderID == v.UserID || v.Admin)
	providerCursor := strings.TrimPrefix(f.Cursor, searchProviderCursorPrefix)
	res, err := safeSearchProviderQuery(ctx, sp, SearchQuery{
		Query:          strings.TrimSpace(f.Query),
		Category:       f.Category,
		UploaderID:     f.UploaderID,
		IncludePrivate: includePrivate,
		Sort:           f.Sort,
		Cursor:         providerCursor,
		Limit:          limit,
	})
	if err != nil {
		if s.log != nil {
			s.log.Warn("search provider query failed; falling back to SQL", "err", err)
		}
		return nil, "", false
	}
	if len(res.IDs) == 0 {
		return []*ent.Texture{}, "", true
	}

	// Deduplicate and bound candidate IDs so a buggy provider cannot exceed SQL parameter limits.
	maxCandidates := max(limit*2, 200)
	seen := make(map[int64]bool, min(len(res.IDs), maxCandidates))
	dedupIDs := make([]int64, 0, min(len(res.IDs), maxCandidates))
	for _, id := range res.IDs {
		if id <= 0 || seen[id] {
			continue
		}
		seen[id] = true
		dedupIDs = append(dedupIDs, id)
		if len(dedupIDs) >= maxCandidates {
			break
		}
	}
	if len(dedupIDs) == 0 {
		return []*ent.Texture{}, "", true
	}

	rows, err := s.db.Texture.Query().
		Where(tex.IDIn(dedupIDs...)).
		WithUploader(selectUploader).
		All(ctx)
	if err != nil {
		return nil, "", false
	}
	byID := make(map[int64]*ent.Texture, len(rows))
	for _, t := range rows {
		byID[t.ID] = t
	}
	ordered := make([]*ent.Texture, 0, min(len(dedupIDs), limit))
	for _, id := range dedupIDs {
		t, ok := byID[id]
		if !ok {
			continue
		}
		if !matchesTextureCategory(f.Category, t.Type) {
			continue
		}
		if f.UploaderID > 0 && (t.UploaderID == nil || *t.UploaderID != f.UploaderID) {
			continue
		}
		if !includePrivate && t.Visibility != tex.VisibilityPublic {
			continue
		}
		if !v.MayView(t) {
			continue
		}
		ordered = append(ordered, t)
		if len(ordered) >= limit {
			break
		}
	}
	nextCursor := res.NextCursor
	if nextCursor != "" && !strings.HasPrefix(nextCursor, searchProviderCursorPrefix) {
		nextCursor = searchProviderCursorPrefix + nextCursor
	}
	return ordered, nextCursor, true
}
