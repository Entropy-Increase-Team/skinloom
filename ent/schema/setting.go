package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

// Setting is one runtime setting. Values are JSON so every key keeps its own type;
// the typed definitions live in internal/settings.
type Setting struct {
	ent.Schema
}

func (Setting) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "settings"}}
}

func (Setting) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").MaxLen(128).NotEmpty().Immutable().StorageKey("key"),
		field.Text("value"),
		field.Int64("updated_by").Optional().Nillable(),
		field.Time("updated_at").SchemaType(datetimeType).Default(time.Now).UpdateDefault(time.Now),
	}
}
