package schema

import "entgo.io/ent/dialect"

// datetimeType must be applied to every time column:
//
//	field.Time("created_at").SchemaType(datetimeType)
//
// ent maps time.Time to TIMESTAMP on MySQL, which overflows in 2038 and is converted by
// the session time zone; DATETIME(6) has neither problem (the driver always reads and
// writes UTC). Other dialects keep ent's defaults.
var datetimeType = map[string]string{dialect.MySQL: "datetime(6)"}
