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

// SocialAccount links a user to an account at a sign-in provider (GitHub, Microsoft,
// an OpenID Connect provider) configured in auth.social.providers. provider is the
// configured id, subject the provider's stable user id; email and name are what the
// provider reported, for display only.
type SocialAccount struct {
	ent.Schema
}

func (SocialAccount) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "social_accounts"}}
}

func (SocialAccount) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.String("provider").MaxLen(32).NotEmpty().Immutable(),
		field.String("subject").MaxLen(255).NotEmpty().Immutable(),
		field.Int64("user_id").Immutable(),
		field.String("email").MaxLen(255).Default(""),
		field.String("name").MaxLen(255).Default(""),
		field.Time("created_at").SchemaType(datetimeType).Default(time.Now).Immutable(),
		field.Time("last_used_at").SchemaType(datetimeType).Optional().Nillable(),
	}
}

func (SocialAccount) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).Ref("social_accounts").Field("user_id").Unique().Required().Immutable(),
	}
}

func (SocialAccount) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("provider", "subject").Unique(),
		// One account per provider per user.
		index.Fields("user_id", "provider").Unique(),
	}
}

// SocialFlow is a sign-in with a provider in progress. A login or link flow lives from
// the redirect to the provider until its callback; a signup flow holds the identity the
// provider returned while the person chooses a nickname (and email) for a new account.
// id is the hash of the random value in the visitor's cookie (and, for login and link,
// the OAuth state parameter).
type SocialFlow struct {
	ent.Schema
}

func (SocialFlow) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "social_flows"}}
}

func (SocialFlow) Fields() []ent.Field {
	return []ent.Field{
		field.String("id").MaxLen(64).NotEmpty().Immutable(),
		field.Enum("purpose").Values("login", "link", "signup").Immutable(),
		field.String("provider").MaxLen(32).NotEmpty().Immutable(),
		// The user linking an account (purpose link).
		field.Int64("user_id").Optional().Nillable().Immutable(),
		// PKCE verifier and OpenID Connect nonce (login, link).
		field.String("verifier").MaxLen(128).Default("").Immutable().Sensitive(),
		field.String("nonce").MaxLen(64).Default("").Immutable(),
		// Where to go after signing in, a path on this site.
		field.String("next").MaxLen(512).Default("").Immutable(),
		field.Bool("remember").Default(false).Immutable(),
		// The identity (signup).
		field.String("subject").MaxLen(255).Default("").Immutable(),
		field.String("email").MaxLen(255).Default("").Immutable(),
		field.Bool("email_verified").Default(false).Immutable(),
		field.String("name").MaxLen(255).Default("").Immutable(),
		field.String("nickname").MaxLen(255).Default("").Immutable(),
		field.Time("created_at").SchemaType(datetimeType).Default(time.Now).Immutable(),
		field.Time("expires_at").SchemaType(datetimeType).Immutable(),
	}
}

func (SocialFlow) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).Ref("social_flows").Field("user_id").Unique().Immutable(),
	}
}

func (SocialFlow) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id"),
		index.Fields("expires_at"),
	}
}
