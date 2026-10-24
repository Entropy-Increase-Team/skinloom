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

// Texture is an uploaded skin or cape. Its id is the "tid" of Blessing Skin URLs
// (/skinlib/show/{tid}, /raw/{tid}). The image itself is the blob named by hash.
type Texture struct {
	ent.Schema
}

func (Texture) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "textures"}}
}

func (Texture) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.String("name").MaxRuneLen(50).NotEmpty(),
		// steve and alex are skins (the classic and slim arm models); cape is a cape.
		field.Enum("type").Values("steve", "alex", "cape"),
		field.String("hash").MaxLen(64).NotEmpty().Immutable(),
		field.Int64("size").Immutable().Comment("bytes of the stored file"),
		// Optional: the uploader's account may be gone; public textures stay.
		field.Int64("uploader_id").Optional().Nillable(),
		field.Enum("visibility").Values("public", "private").Default("public"),
		// likes is the number of closets holding the texture, kept in step by atomic
		// increments in the transaction that adds or removes the closet item.
		field.Int("likes").Default(0),
		field.Time("created_at").SchemaType(datetimeType).Default(time.Now).Immutable(),
		field.Time("updated_at").SchemaType(datetimeType).Default(time.Now).UpdateDefault(time.Now),
	}
}

func (Texture) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("blob", Blob.Type).Ref("textures").Field("hash").Unique().Required().Immutable(),
		edge.From("uploader", User.Type).Ref("textures").Field("uploader_id").Unique(),
		edge.To("closet_items", ClosetItem.Type).Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (Texture) Indexes() []ent.Index {
	return []ent.Index{
		// The skin library: newest first (ids grow with upload time) and most liked,
		// both with the id as the cursor tiebreaker, over all types and per type, so
		// every page is one ordered index range.
		index.Fields("visibility", "id"),
		index.Fields("visibility", "likes", "id"),
		index.Fields("visibility", "type", "id"),
		index.Fields("visibility", "type", "likes", "id"),
		// One texture per image per uploader: the duplicate check of an upload races
		// with a concurrent upload of the same image, and this decides it. Also the
		// index for "uploads of this user".
		index.Fields("uploader_id", "hash").Unique(),
		index.Fields("hash"),
	}
}
