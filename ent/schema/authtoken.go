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

// AuthToken is a one-time token sent by mail or printed on the console (setup). Only
// the SHA-256 is stored; a token is spent by setting used_at in the same transaction
// that performs its action (fixes Blessing Skin S02/S03/S11).
type AuthToken struct {
	ent.Schema
}

func (AuthToken) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "auth_tokens"}}
}

func (AuthToken) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Int64("user_id").Optional().Nillable().Immutable().Comment("NULL for setup tokens"),
		field.Enum("purpose").Values("setup", "verify_email", "reset_password").Immutable(),
		field.String("token_hash").MaxLen(64).NotEmpty().Unique().Immutable(),
		field.Time("expires_at").SchemaType(datetimeType).Immutable(),
		field.Time("used_at").SchemaType(datetimeType).Optional().Nillable(),
		field.Time("created_at").SchemaType(datetimeType).Default(time.Now).Immutable(),
	}
}

func (AuthToken) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).Ref("auth_tokens").Field("user_id").Unique().Immutable(),
	}
}

func (AuthToken) Indexes() []ent.Index {
	return []ent.Index{index.Fields("user_id", "purpose")}
}
