package graph

import (
	"github.com/meteaksoyy/nexus/internal/graph/resolvers"
)

// RootResolver is the top-level resolver registered with graphql-go.
// Each field group is delegated to a sub-resolver for separation of concerns.
type RootResolver struct {
	*resolvers.UserResolver
	*resolvers.GitHubResolver
	*resolvers.IBKRResolver
}

func NewRootResolver(
	user *resolvers.UserResolver,
	github *resolvers.GitHubResolver,
	ibkr *resolvers.IBKRResolver,
) *RootResolver {
	return &RootResolver{
		UserResolver:   user,
		GitHubResolver: github,
		IBKRResolver:   ibkr,
	}
}
