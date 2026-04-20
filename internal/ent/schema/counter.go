package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// Counter stores aggregated counts for windows; can be periodically compacted.
// kind: ip | name
// key: ip string or normalized nickname
// window_sec: e.g., 10 or 60
// count: observed attempts within the window snapshot
// last_seen: last update time
// Note: For high perf, in-memory counters will be primary; this table provides persistence/analytics.
type Counter struct{ ent.Schema }

func (Counter) Fields() []ent.Field {
	return []ent.Field{
		field.String("kind").NotEmpty(),
		field.String("key").NotEmpty(),
		field.Int("window_sec").Positive(),
		field.Int("count").NonNegative().Default(0),
		field.Time("last_seen").Default(time.Now),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (Counter) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("server", Server.Type).Ref("counters").Unique(),
	}
}
