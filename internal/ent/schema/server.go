package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// Server represents a backend Minecraft server entry and per-server scoping root.
type Server struct{ ent.Schema }

func (Server) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").Unique().NotEmpty(),
		field.String("public_host").Optional().Nillable(),
		field.String("public_addr").Optional().Nillable(),
		field.String("upstream").NotEmpty(), // host:port or address used by Gate to route
		field.String("forwarding_mode").Optional().Default(""), // empty uses the global fallback; BungeeGuard must be selected per server
		field.String("ping_motd").Optional().Default(""),
		field.String("bungeeguard_allowed_token").Optional().Default("").Sensitive(),
		field.Int("priority").Default(100),
		field.Bool("enabled").Default(true),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (Server) Edges() []ent.Edge {
	return []ent.Edge{
		edge.To("policies", AccessPolicy.Type),
		edge.To("rules", Rule.Type),
		edge.To("counters", Counter.Type),
		edge.To("audits", Audit.Type),
	}
}
