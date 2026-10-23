package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Job is a unit of background work (sending mail now; cleanups and renders later).
// Enqueue it in the same transaction as the change that needs it (transactional
// outbox): either both happen or neither does. Workers on every node claim due jobs
// with a short lease, so a job runs on one node at a time.
type Job struct {
	ent.Schema
}

func (Job) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "jobs"}}
}

func (Job) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.String("kind").MaxLen(64).NotEmpty().Immutable(),
		// Wiped when the job finishes or gives up: a mail job carries one-time links.
		field.Text("payload"),
		field.Enum("status").Values("pending", "done", "failed").Default("pending"),
		field.Int("attempts").Default(0),
		field.Time("run_at").SchemaType(datetimeType).Default(time.Now),
		field.Time("locked_until").SchemaType(datetimeType).Optional().Nillable(),
		field.String("last_error").MaxLen(1024).Default(""),
		field.Time("created_at").SchemaType(datetimeType).Default(time.Now).Immutable(),
		field.Time("finished_at").SchemaType(datetimeType).Optional().Nillable(),
	}
}

func (Job) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("status", "run_at"), // the worker's "what is due" query
	}
}
