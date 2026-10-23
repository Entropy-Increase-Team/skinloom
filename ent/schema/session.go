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

// Session is a browser login. The id is the SHA-256 of the cookie token, so a leaked
// database cannot be replayed as cookies. Revoking = setting revoked_at.
type Session struct {
	ent.Schema
}

func (Session) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "sessions"}}
}

func (Session) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").MaxLen(64).NotEmpty().Immutable(),
		field.Int64("user_id").Immutable(),
		field.String("ip").MaxLen(45).Default(""),
		field.String("user_agent").MaxLen(512).Default(""),
		field.Time("created_at").SchemaType(datetimeType).Default(time.Now).Immutable(),
		field.Time("last_seen_at").SchemaType(datetimeType).Default(time.Now),
		field.Time("expires_at").SchemaType(datetimeType),
		field.Time("revoked_at").SchemaType(datetimeType).Optional().Nillable(),
	}
}

func (Session) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).Ref("sessions").Field("user_id").Unique().Required().Immutable(),
	}
}

func (Session) Indexes() []ent.Index {
	return []ent.Index{index.Fields("user_id")}
}
