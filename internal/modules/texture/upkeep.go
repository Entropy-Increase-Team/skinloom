package texture

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/blob"
	"github.com/Entropy-Increase-Team/skinloom/ent/closetitem"
	tex "github.com/Entropy-Increase-Team/skinloom/ent/texture"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/jobs"
)

// SweepJobKind deletes a file that no texture uses any more, once it is safe (see
// SweepFile).
const SweepJobKind = "texture.sweep"

// SweepGrace is how long an unreferenced file is kept: an upload writes its file
// before its transaction, and a file just written again by an upload of the same
// image must not be deleted under it.
const SweepGrace = time.Hour

// unref drops one reference to a blob; the last one deletes the row and schedules the
// file for deletion.
func (s *Service) unref(ctx context.Context, tx *ent.Client, hash string) error {
	if err := tx.Blob.UpdateOneID(hash).AddRefCount(-1).Exec(ctx); ent.IsNotFound(err) {
		return nil // no row to count down (imported data); the orphan sweep handles the file
	} else if err != nil {
		return err
	}
	n, err := tx.Blob.Delete().Where(blob.ID(hash), blob.RefCountLTE(0)).Exec(ctx)
	if err != nil || n == 0 {
		return err
	}
	return jobs.EnqueueAt(ctx, tx, SweepJobKind, sweepPayload{Hash: hash}, time.Now().Add(SweepGrace))
}

type sweepPayload struct {
	Hash string `json:"hash"`
}

// SweepFile deletes a stored file that no blob row names and that nothing has written
// for SweepGrace. Both conditions matter: the row is created only after an upload's
// file is written, and writing the same image again refreshes the file's time.
func (s *Service) SweepFile(ctx context.Context, hash string, now time.Time) (bool, error) {
	used, err := s.db.Blob.Query().Where(blob.ID(hash)).Exist(ctx)
	if err != nil || used {
		return false, err
	}
	written, err := s.store.Stat(ctx, hash)
	if err != nil {
		return false, nil // gone already
	}
	if now.Sub(written) < SweepGrace {
		return false, nil
	}
	return true, s.store.Delete(ctx, hash)
}

// SweepOrphans deletes every stored file without a blob row, older than SweepGrace:
// files of uploads whose transaction failed, or whose sweep job was lost.
func (s *Service) SweepOrphans(ctx context.Context, now time.Time) (int, error) {
	var old []string
	err := s.store.Walk(ctx, func(key string, mod time.Time) error {
		if now.Sub(mod) >= SweepGrace {
			old = append(old, key)
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	deleted := 0
	for len(old) > 0 {
		chunk := old[:min(len(old), 500)]
		old = old[len(chunk):]
		known, err := s.db.Blob.Query().Where(blob.IDIn(chunk...)).IDs(ctx)
		if err != nil {
			return deleted, err
		}
		keep := make(map[string]bool, len(known))
		for _, k := range known {
			keep[k] = true
		}
		for _, k := range chunk {
			if keep[k] {
				continue
			}
			if ok, err := s.SweepFile(ctx, k, now); err != nil {
				return deleted, err
			} else if ok {
				deleted++
			}
		}
	}
	return deleted, nil
}

// HandleSweepJob is the job handler for SweepJobKind.
func (s *Service) HandleSweepJob(ctx context.Context, payload json.RawMessage) error {
	var p sweepPayload
	if err := json.Unmarshal(payload, &p); err != nil || p.Hash == "" {
		return jobs.Permanent(fmt.Errorf("bad sweep payload: %v", err))
	}
	_, err := s.SweepFile(ctx, p.Hash, time.Now())
	return err
}

// SetJobNotifier sets a function called after a transaction that queued a job
// commits, so the worker picks it up without waiting for its next poll.
func (s *Service) SetJobNotifier(f func()) { s.notify = f }

// Kick wakes the job worker after a transaction that called DeleteIn has committed.
func (s *Service) Kick() { s.kick() }

func (s *Service) kick() {
	if s.notify != nil {
		s.notify()
	}
}

// CalibrateLikes sets every texture's likes back to the number of closets holding it
// and reports how many it corrected. Likes change with every closet add and removal,
// in the same transaction; this catches what slips past: an account deleted at the
// very moment its owner added a texture, data imported from Blessing Skin (design 04
// §5.2). It reads in pages and corrects one texture per transaction, under that
// texture's lock, so it never holds two locks and cannot deadlock with anything.
func (s *Service) CalibrateLikes(ctx context.Context) (int, error) {
	const pageSize = 500
	fixed := 0
	var after int64
	for {
		page, err := s.db.Texture.Query().Where(tex.IDGT(after)).Order(ent.Asc(tex.FieldID)).Limit(pageSize).
			Select(tex.FieldID, tex.FieldLikes).All(ctx)
		if err != nil || len(page) == 0 {
			return fixed, err
		}
		after = page[len(page)-1].ID
		ids := make([]int64, len(page))
		for i, t := range page {
			ids[i] = t.ID
		}
		var counts []struct {
			TextureID int64 `json:"texture_id"`
			Count     int   `json:"count"`
		}
		if err := s.db.ClosetItem.Query().Where(closetitem.TextureIDIn(ids...)).
			GroupBy(closetitem.FieldTextureID).Aggregate(ent.Count()).Scan(ctx, &counts); err != nil {
			return fixed, err
		}
		have := make(map[int64]int, len(counts))
		for _, c := range counts {
			have[c.TextureID] = c.Count
		}
		for _, t := range page {
			if t.Likes == have[t.ID] {
				continue
			}
			changed, err := s.calibrate(ctx, t.ID)
			if err != nil {
				return fixed, err
			}
			if changed {
				fixed++
			}
		}
		if len(page) < pageSize {
			return fixed, nil
		}
	}
}

// calibrate recounts one texture's likes under its lock (the count read outside may
// be out of date by then).
func (s *Service) calibrate(ctx context.Context, id int64) (bool, error) {
	changed := false
	err := db.WithTx(ctx, s.db, func(tx *ent.Client) error {
		t, err := lockAndGet(ctx, tx, id)
		if errors.Is(err, ErrNotFound) {
			return nil // deleted meanwhile
		}
		if err != nil {
			return err
		}
		n, err := tx.ClosetItem.Query().Where(closetitem.TextureID(id)).Count(ctx)
		if err != nil || t.Likes == n {
			return err
		}
		changed = true
		return tx.Texture.UpdateOneID(id).SetLikes(n).Exec(ctx)
	})
	return changed, err
}
