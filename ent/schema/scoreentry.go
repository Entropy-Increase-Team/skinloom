package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// ScoreEntry is one line of the score ledger: every change of users.score writes one
// in the same transaction (internal/score), with the balance it left. Refunds reverse
// the original entry instead of recomputing from current settings (Blessing Skin
// C03, C05, C12).
type ScoreEntry struct {
	ent.Schema
}

func (ScoreEntry) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "score_ledger"}}
}

func (ScoreEntry) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Int64("user_id").Immutable(),
		field.Int64("delta").Immutable(),
		field.Int64("balance").Immutable().Comment("users.score after this entry"),
		field.String("reason").MaxLen(64).NotEmpty().Immutable(),
		// What the change is about, e.g. ("texture", "42").
		field.String("ref_type").MaxLen(32).Default("").Immutable(),
		field.String("ref_id").MaxLen(64).Default("").Immutable(),
		// The administrator who made the change; nil for the user or the system.
		field.Int64("actor_id").Optional().Nillable().Immutable(),
		field.Time("created_at").SchemaType(datetimeType).Default(time.Now).Immutable(),
	}
}

func (ScoreEntry) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).Ref("score_entries").Field("user_id").Unique().Required().Immutable(),
	}
}

func (ScoreEntry) Indexes() []ent.Index {
	return []ent.Index{index.Fields("user_id", "id")}
}
