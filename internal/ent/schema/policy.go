package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// AccessPolicy defines global or per-server thresholds and evaluation options.
// If linked to a Server via the optional edge, it's a server-level policy; otherwise global.
type AccessPolicy struct{ ent.Schema }

func (AccessPolicy) Fields() []ent.Field {
	return []ent.Field{
		// Thresholds (examples; tune later)
		field.Int("ip_burst_10s").Default(20),
		field.Int("ip_burst_60s").Default(80),
		field.Int("name_burst_10s").Default(15),
		field.Int("name_burst_60s").Default(60),

		// Behavior
		field.Bool("deny_first").Default(false),
		field.String("kick_message").Default("Connection denied by policy"),

		// Geo
		field.String("geo_mode").Default("disabled"), // disabled|allow|deny
		field.JSON("geo_list", []string{}).Optional().Default([]string{}),

		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (AccessPolicy) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("server", Server.Type).Ref("policies").Unique(),
	}
}
