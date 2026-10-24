// Package texture owns uploaded skins and capes: the upload pipeline, the files
// (blobs, reference counted), the skin library and closets.
package texture

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/blob"
	"github.com/Entropy-Increase-Team/skinloom/ent/closetitem"
	"github.com/Entropy-Increase-Team/skinloom/ent/predicate"
	tex "github.com/Entropy-Increase-Team/skinloom/ent/texture"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/validate"
	"github.com/Entropy-Increase-Team/skinloom/internal/score"
	"github.com/Entropy-Increase-Team/skinloom/internal/storage"
)

// MaxNameLen is counted in characters (Blessing Skin's column was 50 wide).
const MaxNameLen = 50

var (
	ErrNotFound = errors.New("texture not found")
	// ErrPrivate means the texture exists but the viewer may not see it.
	ErrPrivate = errors.New("texture is private")
)

// Service implements textures.
type Service struct {
	db       *ent.Client
	store    storage.Store
	log      *slog.Logger
	decoding chan struct{}
	notify   func() // see SetJobNotifier
}

// NewService creates the service.
func NewService(db *ent.Client, store storage.Store, log *slog.Logger) *Service {
	return &Service{db: db, store: store, log: log, decoding: make(chan struct{}, 4)}
}

// Viewer is who is looking: 0 for a guest.
type Viewer struct {
	UserID int64
	Admin  bool
}

func (v Viewer) mayView(t *ent.Texture) bool {
	return t.Visibility == tex.VisibilityPublic || v.Admin || (v.UserID != 0 && t.UploaderID != nil && *t.UploaderID == v.UserID)
}

// UploadInput is one upload.
type UploadInput struct {
	Name   string
	Type   string
	Public bool
	Data   []byte
}

// UploadResult is the uploaded texture, or the existing one when the same image is
// already in the library (Duplicate; nothing is charged).
type UploadResult struct {
	Texture   *ent.Texture
	Duplicate bool
	Cost      int64 // score spent (negative: earned)
	Balance   int64
}

// ValidateName checks a texture name: 1–50 printable characters after trimming, and
// the site's name rule if it has one.
func ValidateName(name string, rule *regexp.Regexp) error {
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n == 0 || n > MaxNameLen {
		return &validate.FieldError{Field: "name", Message: fmt.Sprintf("must be 1 to %d characters", MaxNameLen)}
	}
	for _, r := range name {
		if !unicode.IsPrint(r) {
			return &validate.FieldError{Field: "name", Message: "contains invisible or control characters"}
		}
	}
	if rule != nil && !rule.MatchString(name) {
		return &validate.FieldError{Field: "name", Message: "does not match the site's naming rule"}
	}
	return nil
}

// Upload checks, stores and charges for a new texture, and puts it in the uploader's
// closet. The same image already in the library (public, or the uploader's own)
// is returned instead of stored twice.
func (s *Service) Upload(ctx context.Context, uploader int64, in UploadInput, p Policy, r score.Rates) (*UploadResult, error) {
	errs := []error{ValidateName(in.Name, p.NameRule)}
	typ := tex.Type(in.Type)
	if tex.TypeValidator(typ) != nil {
		errs = append(errs, &validate.FieldError{Field: "type", Message: "want steve, alex or cape"})
	}
	if int64(len(in.Data)) > p.MaxBytes {
		errs = append(errs, &validate.FieldError{Field: "file", Message: fmt.Sprintf("at most %d KB", p.MaxBytes/1024)})
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	img, err := s.checkImage(ctx, in.Data, typ, p.MaxWidth)
	if err != nil {
		return nil, err
	}

	if dup, err := s.duplicate(ctx, uploader, img.hash); err != nil {
		return nil, err
	} else if dup != nil {
		return &UploadResult{Texture: dup, Duplicate: true}, nil
	}

	cost := r.UploadCost(int64(len(img.png)), in.Public)
	// Checked again atomically when charging; this only avoids writing a file for an
	// upload that cannot be paid for.
	if u, err := s.db.User.Query().Where(user.ID(uploader)).Only(ctx); err != nil {
		return nil, err
	} else if u.Score < cost {
		return nil, score.ErrInsufficient
	}
	// The file goes first: a texture row must never point at a missing file. If the
	// transaction then fails, the file stays unreferenced (an orphan sweep removes it).
	if err := s.store.Put(ctx, img.hash, img.png); err != nil {
		return nil, fmt.Errorf("store texture: %w", err)
	}
	visibility := tex.VisibilityPrivate
	if in.Public {
		visibility = tex.VisibilityPublic
	}
	name := strings.TrimSpace(in.Name)
	res := &UploadResult{Cost: cost}
	err = withTx(ctx, s.db, func(tx *ent.Client) error {
		err := tx.Blob.Create().
			SetID(img.hash).SetSize(int64(len(img.png))).SetWidth(img.width).SetHeight(img.height).SetRefCount(1).
			OnConflictColumns(blob.FieldID).
			Update(func(u *ent.BlobUpsert) { u.AddRefCount(1) }).
			Exec(ctx)
		if err != nil {
			return err
		}
		t, err := tx.Texture.Create().
			SetName(name).SetType(typ).SetHash(img.hash).SetSize(int64(len(img.png))).
			SetUploaderID(uploader).SetVisibility(visibility).SetLikes(1). // in the uploader's closet
			Save(ctx)
		if ent.IsConstraintError(err) {
			return errRaced // the same image, uploaded concurrently (unique uploader_id, hash)
		}
		if err != nil {
			return err
		}
		if err := tx.ClosetItem.Create().SetUserID(uploader).SetTextureID(t.ID).SetName(name).Exec(ctx); err != nil {
			return err
		}
		res.Texture = t
		res.Balance, err = score.Apply(ctx, tx, score.Change{UserID: uploader, Delta: -cost,
			Reason: score.ReasonTextureUpload, RefType: "texture", RefID: strconv.FormatInt(t.ID, 10)})
		return err
	})
	if errors.Is(err, errRaced) {
		dup, err := s.duplicate(ctx, uploader, img.hash)
		if err != nil {
			return nil, err
		}
		if dup == nil {
			return nil, errors.New("texture upload conflicted, but no duplicate is visible")
		}
		return &UploadResult{Texture: dup, Duplicate: true}, nil
	}
	if err != nil {
		return nil, err
	}
	// Read back with the uploader, like every texture this service returns.
	if res.Texture, err = s.db.Texture.Query().Where(tex.ID(res.Texture.ID)).WithUploader(selectUploader).Only(ctx); err != nil {
		return nil, err
	}
	return res, nil
}

var errRaced = errors.New("concurrent upload of the same image")

// duplicate finds the texture an upload of hash by uploader would duplicate: a public
// one, or the uploader's own (nil when there is none).
func (s *Service) duplicate(ctx context.Context, uploader int64, hash string) (*ent.Texture, error) {
	t, err := s.db.Texture.Query().
		Where(tex.Hash(hash), tex.Or(tex.VisibilityEQ(tex.VisibilityPublic), tex.UploaderID(uploader))).
		WithUploader(selectUploader).Order(ent.Asc(tex.FieldID)).First(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	return t, err
}

// Get returns a texture the viewer may see, with its uploader.
func (s *Service) Get(ctx context.Context, id int64, v Viewer) (*ent.Texture, error) {
	t, err := s.db.Texture.Query().Where(tex.ID(id)).WithUploader(selectUploader).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if !v.mayView(t) {
		return nil, ErrPrivate
	}
	return t, nil
}

func selectUploader(q *ent.UserQuery) { q.Select(user.FieldID, user.FieldNickname, user.FieldStatus) }

// InCloset reports whether the texture is in the user's closet.
func (s *Service) InCloset(ctx context.Context, userID, textureID int64) (bool, error) {
	return s.db.ClosetItem.Query().Where(closetitem.UserID(userID), closetitem.TextureID(textureID)).Exist(ctx)
}

// Sort orders of the skin library (a whitelist; Blessing Skin sorted by any column
// the client named, including users' emails and password hashes, S07).
const (
	SortTime  = "time"
	SortLikes = "likes"
)

// Filter selects textures for the skin library.
type Filter struct {
	Category   string // "", skin (steve and alex), steve, alex or cape
	Query      string // name fragment
	UploaderID int64
	Sort       string // SortTime (default) or SortLikes
	Cursor     string // from the previous page
	Limit      int
}

// List returns a page of the skin library and the cursor of the next page ("" at the
// end). The library is public textures; private ones are listed only among the
// uploads of one user, to that user and to administrators. (Mixing "or my own private
// ones" into every page would keep the query from walking one index in order.)
func (s *Service) List(ctx context.Context, f Filter, v Viewer) ([]*ent.Texture, string, error) {
	limit := pageSize(f.Limit)
	var where []predicate.Texture
	if c := categoryOf(f.Category); c != nil {
		where = append(where, c)
	}
	switch {
	case f.UploaderID > 0 && (f.UploaderID == v.UserID || v.Admin):
		where = append(where, tex.UploaderID(f.UploaderID))
	case f.UploaderID > 0:
		where = append(where, tex.UploaderID(f.UploaderID), tex.VisibilityEQ(tex.VisibilityPublic))
	default:
		where = append(where, tex.VisibilityEQ(tex.VisibilityPublic))
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		where = append(where, tex.NameContainsFold(q))
	}
	q := s.db.Texture.Query().WithUploader(selectUploader)
	switch f.Sort {
	case "", SortTime:
		if f.Cursor != "" {
			id, err := strconv.ParseInt(f.Cursor, 10, 64)
			if err != nil || id <= 0 {
				return nil, "", badCursor
			}
			where = append(where, tex.IDLT(id))
		}
		q = q.Order(ent.Desc(tex.FieldID))
	case SortLikes:
		if f.Cursor != "" {
			likes, id, ok := splitCursor(f.Cursor)
			if !ok {
				return nil, "", badCursor
			}
			where = append(where, tex.Or(tex.LikesLT(likes), tex.And(tex.Likes(likes), tex.IDLT(id))))
		}
		q = q.Order(ent.Desc(tex.FieldLikes), ent.Desc(tex.FieldID))
	default:
		return nil, "", &validate.FieldError{Field: "sort", Message: "want time or likes"}
	}
	list, err := q.Where(where...).Limit(limit + 1).All(ctx)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(list) > limit {
		list = list[:limit]
		last := list[limit-1]
		if f.Sort == SortLikes {
			next = fmt.Sprintf("%d.%d", last.Likes, last.ID)
		} else {
			next = strconv.FormatInt(last.ID, 10)
		}
	}
	return list, next, nil
}

var badCursor = &validate.FieldError{Field: "cursor", Message: "invalid cursor"}

func splitCursor(c string) (int, int64, bool) {
	a, b, ok := strings.Cut(c, ".")
	likes, err1 := strconv.Atoi(a)
	id, err2 := strconv.ParseInt(b, 10, 64)
	return likes, id, ok && err1 == nil && err2 == nil && likes >= 0 && id > 0
}

func categoryOf(c string) predicate.Texture {
	switch c {
	case "skin":
		return tex.TypeIn(tex.TypeSteve, tex.TypeAlex)
	case "steve", "alex", "cape":
		return tex.TypeEQ(tex.Type(c))
	}
	return nil // any type
}

// ClosetFilter selects items of a closet.
type ClosetFilter struct {
	Category string // "", skin or cape
	Query    string // item name fragment
	Before   int64  // cursor: items added before this one
	Limit    int
}

// Closet returns a page of a user's closet, newest first, with each item's texture.
func (s *Service) Closet(ctx context.Context, userID int64, f ClosetFilter) ([]*ent.ClosetItem, int64, error) {
	limit := pageSize(f.Limit)
	where := []predicate.ClosetItem{closetitem.UserID(userID)}
	switch f.Category {
	case "skin":
		where = append(where, closetitem.HasTextureWith(tex.TypeIn(tex.TypeSteve, tex.TypeAlex)))
	case "cape":
		where = append(where, closetitem.HasTextureWith(tex.TypeEQ(tex.TypeCape)))
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		where = append(where, closetitem.NameContainsFold(q))
	}
	if f.Before > 0 {
		where = append(where, closetitem.IDLT(f.Before))
	}
	list, err := s.db.ClosetItem.Query().Where(where...).
		WithTexture(func(q *ent.TextureQuery) { q.WithUploader(selectUploader) }).
		Order(ent.Desc(closetitem.FieldID)).Limit(limit + 1).All(ctx)
	if err != nil {
		return nil, 0, err
	}
	var next int64
	if len(list) > limit {
		list = list[:limit]
		next = list[limit-1].ID
	}
	return list, next, nil
}

// Open returns the stored file of a texture the viewer may see (/raw/{tid}).
func (s *Service) Open(ctx context.Context, id int64, v Viewer) (*ent.Texture, storage.File, error) {
	t, err := s.Get(ctx, id, v)
	if err != nil {
		return nil, nil, err
	}
	f, err := s.store.Open(ctx, t.Hash)
	if errors.Is(err, storage.ErrNotFound) {
		s.log.Warn("texture file is missing", "texture", t.ID, "hash", t.Hash)
		return nil, nil, ErrNotFound
	}
	return t, f, err
}

// OpenHash returns a stored file by hash (/textures/{hash}). Hashes are the SHA-256
// of the image, so knowing one means having seen the texture.
func (s *Service) OpenHash(ctx context.Context, hash string) (storage.File, error) {
	if !storage.ValidKey(hash) {
		return nil, storage.ErrNotFound
	}
	return s.store.Open(ctx, hash)
}

// pageSize is the page size for list endpoints: 20 when not given (Blessing Skin's
// library page), at most 100.
func pageSize(n int) int {
	if n <= 0 {
		return 20
	}
	return min(n, 100)
}

func withTx(ctx context.Context, db *ent.Client, fn func(tx *ent.Client) error) error {
	tx, err := db.Tx(ctx)
	if err != nil {
		return err
	}
	if err := fn(tx.Client()); err != nil {
		if rerr := tx.Rollback(); rerr != nil {
			return errors.Join(err, rerr)
		}
		return err
	}
	return tx.Commit()
}
