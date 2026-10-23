package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

// SystemState holds small process-wide counters, e.g. config_version, which every
// node polls to learn that settings, themes or plugins changed.
type SystemState struct {
	ent.Schema
}

func (SystemState) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "system_state"}}
}

func (SystemState) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").MaxLen(64).NotEmpty().Immutable().StorageKey("key"),
		field.Int64("int_value").Default(0),
		field.Time("updated_at").SchemaType(datetimeType).Default(time.Now).UpdateDefault(time.Now),
	}
}
