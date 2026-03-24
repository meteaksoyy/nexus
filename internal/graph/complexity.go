package graph

import (
	"github.com/graph-gophers/graphql-go"
)

const (
	MaxDepth      = 8
	MaxComplexity = 100
)

// SchemaOpts returns the graphql-go options that enforce depth and complexity limits.
func SchemaOpts() []graphql.SchemaOpt {
	return []graphql.SchemaOpt{
		graphql.MaxDepth(MaxDepth),
		graphql.MaxParallelism(10),
	}
}

// ComplexityLimit returns the maximum allowed query complexity.
// Used by the HTTP handler to reject queries that exceed the budget.
func ComplexityLimit() int {
	return MaxComplexity
}
