package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// PluginDoc stores dialect-neutral structured plugin documents using
// pre-allocated unique and indexed slot columns (ADR 0007 / 0045) so zero
// runtime DDL is required on SQLite, MySQL, or PostgreSQL.
type PluginDoc struct {
	ent.Schema
}

func (PluginDoc) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "plugin_docs"}}
}

func (PluginDoc) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.String("plugin").MaxLen(40).NotEmpty(),
		field.String("collection").MaxLen(64).NotEmpty(),
		field.String("doc_id").MaxLen(64).NotEmpty(),
		field.JSON("data", map[string]any{}),
		// Pre-allocated unique string slots ("string:unique", up to 2 per collection).
		// Nullable so NULL values never collide across rows without the slot set.
		field.String("u1").MaxLen(255).Optional().Nillable(),
		field.String("u2").MaxLen(255).Optional().Nillable(),
		// Pre-allocated non-unique string slots ("string", up to 3 per collection).
		field.String("s1").MaxLen(255).Optional().Nillable(),
		field.String("s2").MaxLen(255).Optional().Nillable(),
		field.String("s3").MaxLen(255).Optional().Nillable(),
		// Pre-allocated numeric slots ("number", up to 3 per collection).
		field.Float("n1").Optional().Nillable(),
		field.Float("n2").Optional().Nillable(),
		field.Float("n3").Optional().Nillable(),
		// Pre-allocated timestamp slots ("time", up to 2 per collection).
		field.Time("t1").SchemaType(datetimeType).Optional().Nillable(),
		field.Time("t2").SchemaType(datetimeType).Optional().Nillable(),
		field.Time("created_at").SchemaType(datetimeType).Default(time.Now).Immutable(),
		field.Time("updated_at").SchemaType(datetimeType).Default(time.Now).UpdateDefault(time.Now),
	}
}

func (PluginDoc) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("plugin", "collection", "doc_id").Unique(),
		index.Fields("plugin", "collection", "u1").Unique(),
		index.Fields("plugin", "collection", "u2").Unique(),
		index.Fields("plugin", "collection", "s1"),
		index.Fields("plugin", "collection", "s2"),
		index.Fields("plugin", "collection", "s3"),
		index.Fields("plugin", "collection", "n1"),
		index.Fields("plugin", "collection", "n2"),
		index.Fields("plugin", "collection", "n3"),
		index.Fields("plugin", "collection", "t1"),
		index.Fields("plugin", "collection", "t2"),
	}
}
