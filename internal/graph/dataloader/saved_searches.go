package dataloader

import (
	"context"

	"github.com/graph-gophers/dataloader/v7"
	"github.com/meteaksoyy/nexus/internal/db"
	"github.com/meteaksoyy/nexus/internal/db/queries"
)

type contextKey struct{}

// Loader holds the per-request DataLoader for saved searches.
type Loader struct {
	searches *dataloader.Loader[string, []db.SavedSearch]
}

// New creates a Loader that batches saved search queries within a single
// request tick. batch is called once per tick with all collected user IDs.
func New(q *queries.SavedSearchQueries) *Loader {
	batchFn := func(ctx context.Context, keys []string) []*dataloader.Result[[]db.SavedSearch] {
		results := make([]*dataloader.Result[[]db.SavedSearch], len(keys))

		// Fetch all saved searches for all user IDs in one query (returns map[userID][]SavedSearch).
		byUser, err := q.GetSavedSearchesByUserIDs(ctx, keys)
		if err != nil {
			for i := range results {
				results[i] = &dataloader.Result[[]db.SavedSearch]{Error: err}
			}
			return results
		}

		for i, key := range keys {
			results[i] = &dataloader.Result[[]db.SavedSearch]{Data: byUser[key]}
		}
		return results
	}

	return &Loader{
		searches: dataloader.NewBatchedLoader(batchFn),
	}
}

// LoadSearches fetches saved searches for a single user ID, coalescing with
// other in-flight requests via the DataLoader batch function.
func (l *Loader) LoadSearches(ctx context.Context, userID string) ([]db.SavedSearch, error) {
	thunk := l.searches.Load(ctx, userID)
	return thunk()
}

// WithLoader attaches the loader to the context for retrieval in resolvers.
func WithLoader(ctx context.Context, l *Loader) context.Context {
	return context.WithValue(ctx, contextKey{}, l)
}

// FromContext retrieves the loader from the context. Returns nil if absent.
func FromContext(ctx context.Context) *Loader {
	l, _ := ctx.Value(contextKey{}).(*Loader)
	return l
}
