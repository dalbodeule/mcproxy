package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// Audit logs administrative changes and block events.
// event: e.g., policy.update, rule.create, connect.blocked
// actor: who made the change (token id, username), optional
// details: JSON payload with context
// Optional scoping to server
type Audit struct{ ent.Schema }

func (Audit) Fields() []ent.Field {
	return []ent.Field{
		field.String("event").NotEmpty(),
		field.String("actor").Optional().Nillable(),
		field.JSON("details", map[string]any{}).Optional().Default(map[string]any{}),
		field.Time("created_at").Default(time.Now),
	}
}

func (Audit) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("server", Server.Type).Ref("audits").Unique(),
	}
}
