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

// OAuthClient is an application a user registered to sign people in with this site
// (OAuth 2.0). The id is the client_id, numeric like Blessing Skin's Passport clients
// so migrated applications keep theirs. Confidential clients have a secret, of which
// only a hash is stored; public clients (no secret) must use PKCE.
type OAuthClient struct {
	ent.Schema
}

func (OAuthClient) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "oauth_clients"}}
}

func (OAuthClient) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Int64("user_id").Immutable(),
		field.String("name").MaxRuneLen(64).NotEmpty(), // characters: ent's MaxLen counts bytes
		field.String("secret_hash").MaxLen(64).Optional().Nillable().Sensitive(),
		field.Strings("redirect_uris"),
		field.Time("created_at").SchemaType(datetimeType).Default(time.Now).Immutable(),
		field.Time("updated_at").SchemaType(datetimeType).Default(time.Now).UpdateDefault(time.Now),
	}
}

func (OAuthClient) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("owner", User.Type).Ref("oauth_clients").Field("user_id").Unique().Required().Immutable(),
		edge.To("grants", OAuthGrant.Type).Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("codes", OAuthCode.Type).Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("tokens", OAuthToken.Type).Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (OAuthClient) Indexes() []ent.Index {
	return []ent.Index{index.Fields("user_id")}
}

// OAuthGrant is what a user allowed an application: the scopes they approved. Asking
// again for no more than these skips the consent screen; revoking it signs the
// application out.
type OAuthGrant struct {
	ent.Schema
}

func (OAuthGrant) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "oauth_grants"}}
}

func (OAuthGrant) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Int64("user_id").Immutable(),
		field.Int64("client_id").Immutable(),
		field.Strings("scopes"),
		field.Time("created_at").SchemaType(datetimeType).Default(time.Now).Immutable(),
		field.Time("updated_at").SchemaType(datetimeType).Default(time.Now).UpdateDefault(time.Now),
	}
}

func (OAuthGrant) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).Ref("oauth_grants").Field("user_id").Unique().Required().Immutable(),
		edge.From("client", OAuthClient.Type).Ref("grants").Field("client_id").Unique().Required().Immutable(),
	}
}

func (OAuthGrant) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "client_id").Unique(),
		index.Fields("client_id"),
	}
}

// OAuthCode is an authorization code waiting to be exchanged for tokens: one use,
// minutes long. Only a hash is stored.
type OAuthCode struct {
	ent.Schema
}

func (OAuthCode) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "oauth_codes"}}
}

func (OAuthCode) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.String("code_hash").MaxLen(64).NotEmpty().Unique().Immutable().Sensitive(),
		field.Int64("client_id").Immutable(),
		field.Int64("user_id").Immutable(),
		field.String("redirect_uri").MaxLen(2048).Immutable(),
		field.Strings("scopes").Immutable(),
		// PKCE (RFC 7636): the challenge and its method (S256 or plain), empty without.
		field.String("code_challenge").MaxLen(128).Default("").Immutable(),
		field.String("code_challenge_method").MaxLen(8).Default("").Immutable(),
		field.Time("expires_at").SchemaType(datetimeType).Immutable(),
	}
}

func (OAuthCode) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("client", OAuthClient.Type).Ref("codes").Field("client_id").Unique().Required().Immutable(),
		edge.From("user", User.Type).Ref("oauth_codes").Field("user_id").Unique().Required().Immutable(),
	}
}

func (OAuthCode) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("client_id"),
		index.Fields("user_id"),
		index.Fields("expires_at"),
	}
}

// OAuthToken is an access token for the resource API (/api/*): issued to an
// application (kind oauth, with a refresh token) or created by the user for scripts
// (kind personal, named, no client). Only hashes are stored; deleting a row revokes it.
type OAuthToken struct {
	ent.Schema
}

func (OAuthToken) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "oauth_tokens"}}
}

func (OAuthToken) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Enum("kind").Values("oauth", "personal").Immutable(),
		field.String("token_hash").MaxLen(64).NotEmpty().Unique().Immutable().Sensitive(),
		field.String("refresh_hash").MaxLen(64).Optional().Nillable().Unique().Immutable().Sensitive(),
		field.Int64("user_id").Immutable(),
		field.Int64("client_id").Optional().Nillable().Immutable(),
		// The name a user gave a personal access token.
		field.String("name").MaxRuneLen(64).Default("").Immutable(),
		field.Strings("scopes").Immutable(),
		field.Time("created_at").SchemaType(datetimeType).Default(time.Now).Immutable(),
		// Nil: a personal access token that never expires.
		field.Time("expires_at").SchemaType(datetimeType).Optional().Nillable().Immutable(),
		field.Time("refresh_until").SchemaType(datetimeType).Optional().Nillable().Immutable(),
		field.Time("last_used_at").SchemaType(datetimeType).Optional().Nillable(),
	}
}

func (OAuthToken) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).Ref("oauth_tokens").Field("user_id").Unique().Required().Immutable(),
		edge.From("client", OAuthClient.Type).Ref("tokens").Field("client_id").Unique().Immutable(),
	}
}

func (OAuthToken) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "kind"),
		index.Fields("client_id", "user_id"),
		index.Fields("refresh_until"),
		index.Fields("expires_at"),
	}
}
