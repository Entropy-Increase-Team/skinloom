package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// AuditLog records security-relevant actions (setup, logins, role and setting
// changes). actor_id has no foreign key on purpose: the log outlives deleted users.
type AuditLog struct {
	ent.Schema
}

func (AuditLog) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "audit_logs"}}
}

func (AuditLog) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Int64("actor_id").Optional().Nillable().Immutable(),
		field.String("action").MaxLen(64).NotEmpty().Immutable(),
		field.String("target_type").MaxLen(32).Default("").Immutable(),
		field.String("target_id").MaxLen(64).Default("").Immutable(),
		field.String("ip").MaxLen(45).Default("").Immutable(),
		field.JSON("meta", map[string]any{}).Optional().Immutable(),
		field.Time("created_at").SchemaType(datetimeType).Default(time.Now).Immutable(),
	}
}

func (AuditLog) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("actor_id", "created_at"),
		index.Fields("action", "created_at"),
	}
}
