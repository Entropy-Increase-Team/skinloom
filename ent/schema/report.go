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

// Report is a user's complaint about a texture, reviewed by an administrator: delete
// the texture, ban its uploader, or reject the report. Reports outlive the texture
// and the users involved (their ids become null), so moderation keeps its history.
type Report struct {
	ent.Schema
}

func (Report) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "reports"}}
}

func (Report) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Int64("texture_id").Optional().Nillable(),
		// The texture's uploader when it was reported: who a ban is for, even after the
		// texture is gone.
		field.Int64("uploader_id").Optional().Nillable(),
		field.Int64("reporter_id").Optional().Nillable(),
		field.String("reason").MaxRuneLen(500).NotEmpty().Immutable(),
		field.Enum("status").Values("pending", "resolved", "rejected").Default("pending"),
		// What the reviewer did: delete (the texture), ban (the uploader) or reject.
		field.Enum("action").Values("delete", "ban", "reject").Optional().Nillable(),
		field.Int64("handler_id").Optional().Nillable(),
		field.Time("handled_at").SchemaType(datetimeType).Optional().Nillable(),
		field.Time("created_at").SchemaType(datetimeType).Default(time.Now).Immutable(),
	}
}

func (Report) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("texture", Texture.Type).Ref("reports").Field("texture_id").Unique(),
		edge.From("uploader", User.Type).Ref("reports_against").Field("uploader_id").Unique(),
		edge.From("reporter", User.Type).Ref("reports_filed").Field("reporter_id").Unique(),
		edge.From("handler", User.Type).Ref("reports_handled").Field("handler_id").Unique(),
	}
}

func (Report) Indexes() []ent.Index {
	return []ent.Index{
		// One report per user and texture; the database decides between two at once.
		index.Fields("reporter_id", "texture_id").Unique(),
		index.Fields("texture_id"),
		index.Fields("uploader_id"),
		index.Fields("handler_id"),
		// The review queue: one status, newest first.
		index.Fields("status", "id"),
	}
}
