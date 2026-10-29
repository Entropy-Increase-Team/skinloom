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

// Announcement is a message an administrator sends to many users at once. Sending
// writes this one row; each user gets their copy (a Notification) the next time they
// look at their notifications (Blessing Skin wrote one row per user at once).
type Announcement struct {
	ent.Schema
}

func (Announcement) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "announcements"}}
}

func (Announcement) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.String("title").MaxRuneLen(64).NotEmpty().Immutable(),
		field.Text("content").Immutable().Comment("Markdown"),
		// Who gets it: all users, or ordinary users only (Blessing Skin's "normal").
		field.Enum("audience").Values("all", "users").Default("all").Immutable(),
		field.Int64("sender_id").Optional().Nillable().Immutable(),
		field.Time("created_at").SchemaType(datetimeType).Default(time.Now).Immutable(),
	}
}

func (Announcement) Indexes() []ent.Index {
	return []ent.Index{index.Fields("sender_id")}
}

func (Announcement) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("sender", User.Type).Ref("announcements_sent").Field("sender_id").Unique().Immutable(),
		edge.To("notifications", Notification.Type).Annotations(entsql.OnDelete(entsql.SetNull)),
	}
}

// Notification is a message in one user's inbox: sent to them alone, or their copy of
// an announcement.
type Notification struct {
	ent.Schema
}

func (Notification) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "notifications"}}
}

func (Notification) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.Int64("user_id").Immutable(),
		field.String("title").MaxRuneLen(64).NotEmpty().Immutable(),
		field.Text("content").Immutable().Comment("Markdown"),
		field.Int64("announcement_id").Optional().Nillable().Immutable(),
		field.Time("read_at").SchemaType(datetimeType).Optional().Nillable(),
		field.Time("created_at").SchemaType(datetimeType).Default(time.Now).Immutable(),
	}
}

func (Notification) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("user", User.Type).Ref("notifications").Field("user_id").Unique().Required().Immutable(),
		edge.From("announcement", Announcement.Type).Ref("notifications").Field("announcement_id").Unique().Immutable(),
	}
}

func (Notification) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "id"),
		// One copy of an announcement per user, however many requests deliver it at once.
		index.Fields("user_id", "announcement_id").Unique(),
		index.Fields("announcement_id"),
	}
}
