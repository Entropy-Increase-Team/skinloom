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

// User is a site account. Email uniqueness is decided only by the unique index on
// email_normalized (design 04 §5.2), never by a read-then-insert check.
type User struct {
	ent.Schema
}

func (User) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "users"}}
}

func (User) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.String("email").MaxLen(255).NotEmpty(),
		field.String("email_normalized").MaxLen(255).NotEmpty().Unique(),
		field.String("nickname").MaxRuneLen(64).NotEmpty(), // characters: ent's MaxLen counts bytes
		// password_algo says how password_hash is encoded: "argon2id" for every hash
		// SkinLoom writes, or one of the Blessing Skin algorithms kept from an import
		// until the user's next login upgrades it (internal/account/password.go).
		field.String("password_hash").MaxLen(255).Sensitive(),
		field.String("password_algo").MaxLen(32).Default("argon2id"),
		field.Enum("role").Values("user", "admin", "superadmin").Default("user"),
		field.Enum("status").Values("active", "banned", "deleted").Default("active"),
		field.Int64("score").Default(0),
		field.String("locale").MaxLen(16).Default("").Comment("empty = follow the browser"),
		field.Time("email_verified_at").SchemaType(datetimeType).Optional().Nillable(),
		field.String("register_ip").MaxLen(45).Default(""),
		field.Time("last_login_at").SchemaType(datetimeType).Optional().Nillable(),
		field.Time("deleted_at").SchemaType(datetimeType).Optional().Nillable(),
		field.Time("created_at").SchemaType(datetimeType).Default(time.Now).Immutable(),
		field.Time("updated_at").SchemaType(datetimeType).Default(time.Now).UpdateDefault(time.Now),
	}
}

func (User) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("sessions", Session.Type).Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("auth_tokens", AuthToken.Type).Annotations(entsql.OnDelete(entsql.Cascade)),
		// Public textures outlive their uploader (shown as a deleted user).
		edge.To("textures", Texture.Type).Annotations(entsql.OnDelete(entsql.SetNull)),
		edge.To("closet_items", ClosetItem.Type).Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.To("score_entries", ScoreEntry.Type).Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (User) Indexes() []ent.Index {
	return []ent.Index{
		// registrations per IP (auth.max_registrations_per_ip)
		index.Fields("register_ip", "created_at"),
	}
}
