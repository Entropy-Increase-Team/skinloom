package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

// Plugin stores installed plugins and themes, their lifecycle state, granted
// permissions, and typed configuration settings.
type Plugin struct {
	ent.Schema
}

func (Plugin) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "plugins"}}
}

func (Plugin) Fields() []ent.Field {
	return []ent.Field{
		// Primary key id is the manifest kebab-case identifier (max 40 chars).
		field.String("id").MaxLen(40).NotEmpty().Immutable(),
		field.Enum("kind").Values("plugin", "theme").Default("plugin"),
		field.String("version").MaxLen(64).NotEmpty(),
		field.Enum("status").Values("enabled", "disabled", "failed").Default("disabled"),
		field.JSON("granted_permissions", []string{}).Optional(),
		field.JSON("settings", map[string]any{}).Optional(),
		field.String("error").MaxLen(1024).Optional().Default(""),
		field.String("source").MaxLen(32).Default("package"), // "local" | "package" | "market"
		field.String("package_sha256").MaxLen(64).Optional(),
		field.Time("installed_at").SchemaType(datetimeType).Default(time.Now).Immutable(),
		field.Time("updated_at").SchemaType(datetimeType).Default(time.Now).UpdateDefault(time.Now),
	}
}
