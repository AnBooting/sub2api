package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/edge"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// APIKeyGroupLink is an ordered, explicitly selected Key group binding.
type APIKeyGroupLink struct{ ent.Schema }

func (APIKeyGroupLink) Annotations() []schema.Annotation {
	return []schema.Annotation{entsql.Annotation{Table: "api_key_group_links"}}
}

func (APIKeyGroupLink) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("api_key_id"),
		field.Int64("group_id"),
		field.Enum("role").Values("normal", "ha").Default("normal"),
		field.Int("position").NonNegative(),
		field.Time("enabled_by_user_at").Optional().Nillable(),
	}
}

func (APIKeyGroupLink) Edges() []ent.Edge {
	return []ent.Edge{
		edge.From("api_key", APIKey.Type).Ref("group_links").Field("api_key_id").Unique().Required().Annotations(entsql.OnDelete(entsql.Cascade)),
		edge.From("group", Group.Type).Ref("api_key_links").Field("group_id").Unique().Required().Annotations(entsql.OnDelete(entsql.Cascade)),
	}
}

func (APIKeyGroupLink) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("api_key_id", "group_id").Unique(),
		index.Fields("api_key_id", "role", "position").Unique(),
		index.Fields("group_id"),
	}
}
