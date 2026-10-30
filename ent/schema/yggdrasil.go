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

// SiteSecret is a secret every instance of a site must share and a backup must keep,
// such as the Yggdrasil signing key: game servers cache the public key, so losing it
// breaks every server that trusts this site.
type SiteSecret struct {
	ent.Schema
}

func (SiteSecret) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "site_secrets"}}
}

func (SiteSecret) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").MaxLen(64).NotEmpty().Immutable().StorageKey("key"),
		field.Text("value").Immutable().Sensitive(),
		field.Time("created_at").SchemaType(datetimeType).Default(time.Now).Immutable(),
	}
}

// YggToken is a launcher's access token (Yggdrasil authserver). Only a hash of the
// token is stored. It is valid until expires_at and can be refreshed until
// refresh_until; deleting a row revokes the token.
type YggToken struct {
	ent.Schema
}

func (YggToken) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "ygg_tokens"}}
}

func (YggToken) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.String("token_hash").MaxLen(64).NotEmpty().Unique().Immutable().Sensitive(),
		field.String("client_token").MaxLen(128).Immutable(),
		field.Int64("user_id").Immutable(),
		// The profile (player) the token is bound to, if any.
		field.Int64("player_id").Optional().Nillable().Immutable(),
		field.Time("created_at").SchemaType(datetimeType).Default(time.Now).Immutable(),
		field.Time("expires_at").SchemaType(datetimeType).Immutable(),
		field.Time("refresh_until").SchemaType(datetimeType).Immutable(),
	}
}

func (YggToken) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).Ref("ygg_tokens").Field("user_id").Unique().Required().Immutable(),
		edge.From("player", Player.Type).Ref("ygg_tokens").Field("player_id").Unique().Immutable(),
	}
}

func (YggToken) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "id"),
		index.Fields("player_id"),
		index.Fields("refresh_until"), // the cleanup of dead tokens
	}
}

// YggSession records that a player joined a game server (sessionserver join), for
// the server's hasJoined check moments later. Kept in the database so any instance
// can answer.
type YggSession struct {
	ent.Schema
}

func (YggSession) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "ygg_sessions"}}
}

func (YggSession) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.String("server_id").MaxLen(128).NotEmpty().Unique(),
		field.Int64("player_id"),
		field.String("ip").MaxLen(45).Default(""),
		field.Time("expires_at").SchemaType(datetimeType),
	}
}

func (YggSession) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("player", Player.Type).Ref("ygg_sessions").Field("player_id").Unique().Required(),
	}
}

func (YggSession) Indexes() []ent.Index {
	return []ent.Index{index.Fields("player_id"), index.Fields("expires_at")}
}
