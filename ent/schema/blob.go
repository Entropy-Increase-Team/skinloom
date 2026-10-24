package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// Blob is one stored texture file, addressed by the SHA-256 of its bytes. Several
// textures can share a blob (the same image uploaded privately by two users):
// ref_count counts them, and the file may only be deleted once it is zero (design 04
// §5.2; Blessing Skin deleted shared files and broke other users' textures, C06).
type Blob struct {
	ent.Schema
}

func (Blob) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "blobs"}}
}

func (Blob) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").StorageKey("hash").MaxLen(64).NotEmpty().Immutable(),
		field.Int64("size").Immutable(),
		field.Int("width").Immutable(),
		field.Int("height").Immutable(),
		field.Int("ref_count").Default(0),
		field.Time("created_at").SchemaType(datetimeType).Default(time.Now).Immutable(),
	}
}

func (Blob) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("textures", Texture.Type),
	}
}
