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

// Player is an in-game character of a user: a name that game clients look up
// (/{name}.json, Yggdrasil), wearing a skin and a cape from the owner's closet.
type Player struct {
	ent.Schema
}

func (Player) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "players"}}
}

func (Player) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Int64("user_id"), // administrators can give a player to another user
		field.String("name").MaxRuneLen(32).NotEmpty(),
		// name_lower is the uniqueness and lookup key: names are unique ignoring case
		// in every database (Blessing Skin had no unique index at all, C01).
		field.String("name_lower").MaxRuneLen(32).NotEmpty().Unique(),
		// The Yggdrasil UUID, 32 hex digits without dashes, kept on the player so it
		// follows the player and not its name (design 02 §2.4).
		field.String("uuid").MaxLen(32).NotEmpty().Unique(),
		field.Int64("skin_id").Optional().Nillable(),
		field.Int64("cape_id").Optional().Nillable(),
		field.Time("created_at").SchemaType(datetimeType).Default(time.Now).Immutable(),
		// updated_at changes with the name or the textures: game clients get it as
		// Last-Modified (CSL) and last_update (USM).
		field.Time("updated_at").SchemaType(datetimeType).Default(time.Now).UpdateDefault(time.Now),
	}
}

func (Player) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("owner", User.Type).Ref("players").Field("user_id").Unique().Required(),
		edge.From("skin", Texture.Type).Ref("skin_players").Field("skin_id").Unique(),
		edge.From("cape", Texture.Type).Ref("cape_players").Field("cape_id").Unique(),
	}
}

func (Player) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id"),
		index.Fields("skin_id"),
		index.Fields("cape_id"),
	}
}
