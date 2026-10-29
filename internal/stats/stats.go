// Package stats computes the figures of the admin overview. Everything is counted in
// the database (Blessing Skin loaded a month of rows into PHP to draw its chart).
package stats

import (
	"context"
	"database/sql"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/blob"
	entreport "github.com/Entropy-Increase-Team/skinloom/ent/report"
	tex "github.com/Entropy-Increase-Team/skinloom/ent/texture"
	"github.com/Entropy-Increase-Team/skinloom/ent/user"
)

// Days is how many days the overview chart covers, today included (as Blessing Skin).
const Days = 31

// Day is one day of the chart, in the site's time zone.
type Day struct {
	Date          string // YYYY-MM-DD
	Registrations int
	Uploads       int
}

// Overview is the admin overview.
type Overview struct {
	Users, Players, Textures, PublicTextures, ClosetItems int
	Files                                                 int   // stored files (shared by equal images)
	StorageBytes                                          int64 // their total size
	PendingReports                                        int
	Days                                                  []Day // oldest first
}

// Compute counts everything for the overview. Each day of the chart is two range
// counts on indexed created_at columns, with day boundaries taken in loc, so the
// queries are the same on every database and daylight saving time is handled. The
// bounds are passed in the server's zone, the one created_at is written in (time.Now):
// SQLite compares times as the stored text, which orders correctly only within one
// zone.
func Compute(ctx context.Context, db *ent.Client, now time.Time, loc *time.Location) (*Overview, error) {
	var (
		o   Overview
		err error
	)
	counts := []struct {
		dst *int
		fn  func() (int, error)
	}{
		{&o.Users, func() (int, error) { return db.User.Query().Count(ctx) }},
		{&o.Players, func() (int, error) { return db.Player.Query().Count(ctx) }},
		{&o.Textures, func() (int, error) { return db.Texture.Query().Count(ctx) }},
		{&o.PublicTextures, func() (int, error) {
			return db.Texture.Query().Where(tex.VisibilityEQ(tex.VisibilityPublic)).Count(ctx)
		}},
		{&o.ClosetItems, func() (int, error) { return db.ClosetItem.Query().Count(ctx) }},
		{&o.PendingReports, func() (int, error) {
			return db.Report.Query().Where(entreport.StatusEQ(entreport.StatusPending)).Count(ctx)
		}},
	}
	for _, c := range counts {
		if *c.dst, err = c.fn(); err != nil {
			return nil, err
		}
	}
	var files []struct {
		Count int           `json:"count"`
		Sum   sql.NullInt64 `json:"sum"`
	}
	if err := db.Blob.Query().Aggregate(ent.Count(), ent.Sum(blob.FieldSize)).Scan(ctx, &files); err != nil {
		return nil, err
	}
	if len(files) == 1 {
		o.Files, o.StorageBytes = files[0].Count, files[0].Sum.Int64
	}

	local := now.In(loc)
	o.Days = make([]Day, Days)
	for i := range Days {
		start := time.Date(local.Year(), local.Month(), local.Day()-(Days-1-i), 0, 0, 0, 0, loc)
		end := time.Date(start.Year(), start.Month(), start.Day()+1, 0, 0, 0, 0, loc)
		d := Day{Date: start.Format(time.DateOnly)}
		from, to := start.In(time.Local), end.In(time.Local)
		if d.Registrations, err = db.User.Query().Where(user.CreatedAtGTE(from), user.CreatedAtLT(to)).Count(ctx); err != nil {
			return nil, err
		}
		if d.Uploads, err = db.Texture.Query().Where(tex.CreatedAtGTE(from), tex.CreatedAtLT(to)).Count(ctx); err != nil {
			return nil, err
		}
		o.Days[i] = d
	}
	return &o, nil
}
