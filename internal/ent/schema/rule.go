package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
)

// Rule represents IP/Name allow/deny items and optional per-server scoping.
// kind: ip-allow | ip-deny | name-allow | name-deny
// target: CIDR, IP, or nickname string depending on kind
// expires_at: optional TTL for temporary rules
// enabled: soft-delete toggle
type Rule struct{ ent.Schema }

func (Rule) Fields() []ent.Field {
	return []ent.Field{
		field.String("kind").NotEmpty(),
		field.String("target").NotEmpty(),
		field.Bool("is_cidr").Default(false),
		field.String("reason").Optional().Nillable(),
		field.Bool("enabled").Default(true),
		field.Time("expires_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

func (Rule) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("server", Server.Type).Ref("rules").Unique(),
	}
}
