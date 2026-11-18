package schema

import (
	"errors"
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// KVValue holds arbitrary raw JSON bytes cleanly across Go 1.26 and Go 1.27 without
// depending on toolchain-specific json.RawMessage / jsontext.Value aliases in generated code.
type KVValue []byte

// MarshalJSON outputs raw JSON bytes (or null if empty).
func (v KVValue) MarshalJSON() ([]byte, error) {
	if len(v) == 0 {
		return []byte("null"), nil
	}
	return v, nil
}

// UnmarshalJSON stores raw JSON bytes directly.
func (v *KVValue) UnmarshalJSON(data []byte) error {
	if v == nil {
		return errors.New("schema.KVValue: UnmarshalJSON on nil pointer")
	}
	*v = append((*v)[0:0], data...)
	return nil
}

// PluginKV stores dialect-neutral key/value pairs scoped per plugin with
// optional TTL expiration.
type PluginKV struct {
	ent.Schema
}

func (PluginKV) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "plugin_kv"}}
}

func (PluginKV) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("id"),
		field.String("plugin").MaxLen(40).NotEmpty(),
		field.String("key").MaxLen(255).NotEmpty(),
		field.JSON("value", KVValue{}),
		field.Time("expires_at").SchemaType(datetimeType).Optional().Nillable(),
		field.Time("updated_at").SchemaType(datetimeType).Default(time.Now).UpdateDefault(time.Now),
	}
}

func (PluginKV) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("plugin", "key").Unique(),
		index.Fields("expires_at"),
	}
}
