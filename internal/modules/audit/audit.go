// Package audit writes the audit log (audit_logs): who did what to what, from where.
//
// A change and its record belong in one transaction: a record written afterwards is
// lost when its write fails, and one written before records changes that then roll
// back. Services that make administrative changes therefore take an *Entry and write
// it inside their transaction (nil: nothing to record, e.g. a user's own change).
package audit

import (
	"context"

	"github.com/Entropy-Increase-Team/skinloom/ent"
)

// Entry is one audit record.
type Entry struct {
	Actor      *int64 // the user acting; nil for the system or the admin token
	Action     string // e.g. "admin.player_updated"
	TargetType string
	TargetID   string
	IP         string
	Meta       map[string]any
}

// With returns a copy of e with key set in Meta (e may be nil: then nil).
func (e *Entry) With(key string, value any) *Entry {
	if e == nil {
		return nil
	}
	out := *e
	out.Meta = make(map[string]any, len(e.Meta)+1)
	for k, v := range e.Meta {
		out.Meta[k] = v
	}
	out.Meta[key] = value
	return &out
}

// Target returns a copy of e naming the target (e may be nil: then nil), for a
// service that learns the target's id inside the transaction (a new row).
func (e *Entry) Target(typ, id string) *Entry {
	if e == nil {
		return nil
	}
	out := *e
	out.TargetType, out.TargetID = typ, id
	return &out
}

// Write records e through c, normally the transaction of the change. A nil e writes
// nothing.
func Write(ctx context.Context, c *ent.Client, e *Entry) error {
	if e == nil {
		return nil
	}
	q := c.AuditLog.Create().
		SetNillableActorID(e.Actor).
		SetAction(e.Action).
		SetTargetType(e.TargetType).
		SetTargetID(e.TargetID).
		SetIP(e.IP)
	if e.Meta != nil {
		q.SetMeta(e.Meta)
	}
	return q.Exec(ctx)
}
