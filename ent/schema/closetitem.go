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

// ClosetItem is a texture in a user's closet (wardrobe), under a name of the user's
// choosing. Players can only wear textures from their owner's closet. Both foreign
// keys cascade, so no orphan rows are left behind (Blessing Skin C11).
type ClosetItem struct {
	ent.Schema
}

func (ClosetItem) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "closet_items"}}
}

func (ClosetItem) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Int64("user_id").Immutable(),
		field.Int64("texture_id").Immutable(),
		field.String("name").MaxRuneLen(50).NotEmpty(),
		field.Time("created_at").SchemaType(datetimeType).Default(time.Now).Immutable(),
	}
}

func (ClosetItem) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).Ref("closet_items").Field("user_id").Unique().Required().Immutable(),
		edge.From("texture", Texture.Type).Ref("closet_items").Field("texture_id").Unique().Required().Immutable(),
	}
}

func (ClosetItem) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "texture_id").Unique(),
		index.Fields("texture_id"),
	}
}
