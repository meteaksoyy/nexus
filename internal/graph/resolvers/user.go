package resolvers

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"
	"github.com/meteaksoyy/nexus/internal/auth"
	"github.com/meteaksoyy/nexus/internal/db"
	"github.com/meteaksoyy/nexus/internal/db/queries"
	"github.com/meteaksoyy/nexus/internal/graph/dataloader"
)

// UserResolver handles GraphQL queries and mutations related to users and saved searches.
type UserResolver struct {
	users   *queries.UserQueries
	saves   *queries.SavedSearchQueries
	apiKeys *auth.APIKeyService
	log     zerolog.Logger
}

func NewUserResolver(
	users *queries.UserQueries,
	saves *queries.SavedSearchQueries,
	apiKeys *auth.APIKeyService,
	log zerolog.Logger,
) *UserResolver {
	return &UserResolver{users: users, saves: saves, apiKeys: apiKeys, log: log}
}

// ── Query resolvers ──────────────────────────────────────────────────────────

func (r *UserResolver) User(ctx context.Context, args struct{ ID string }) (*UserObject, error) {
	u, err := r.users.GetUserByID(ctx, args.ID)
	if err != nil {
		return nil, fmt.Errorf("user not found")
	}
	return &UserObject{user: &u, saves: r.saves}, nil
}

func (r *UserResolver) Me(ctx context.Context) (*UserObject, error) {
	id := auth.UserIDFromContext(ctx)
	if id == "" {
		return nil, fmt.Errorf("not authenticated")
	}
	u, err := r.users.GetUserByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("user not found")
	}
	return &UserObject{user: &u, saves: r.saves}, nil
}

// ── Mutation resolvers ───────────────────────────────────────────────────────

func (r *UserResolver) CreateUser(ctx context.Context, args struct {
	Input struct {
		Email    string
		Password string
	}
}) (*UserObject, error) {
	u, err := r.users.CreateUser(ctx, args.Input.Email, args.Input.Password, "user")
	if err != nil {
		r.log.Warn().Err(err).Msg("create user failed")
		return nil, fmt.Errorf("could not create user")
	}
	return &UserObject{user: &u, saves: r.saves}, nil
}

func (r *UserResolver) SaveSearch(ctx context.Context, args struct{ Query string }) (*SavedSearchObject, error) {
	userID := auth.UserIDFromContext(ctx)
	if userID == "" {
		return nil, fmt.Errorf("not authenticated")
	}
	s, err := r.saves.CreateSavedSearch(ctx, userID, args.Query)
	if err != nil {
		return nil, fmt.Errorf("could not save search")
	}
	return &SavedSearchObject{s: &s}, nil
}

// ── Object resolvers ─────────────────────────────────────────────────────────

// UserObject wraps a db.User and exposes GraphQL field methods.
type UserObject struct {
	user *db.User
	saves *queries.SavedSearchQueries
}

func (u *UserObject) ID() string        { return u.user.ID }
func (u *UserObject) Email() string     { return u.user.Email }
func (u *UserObject) Role() string      { return u.user.Role }
func (u *UserObject) CreatedAt() string { return u.user.CreatedAt.Format("2006-01-02T15:04:05Z") }

func (u *UserObject) SavedSearches(ctx context.Context) ([]*SavedSearchObject, error) {
	loader := dataloader.FromContext(ctx)
	if loader != nil {
		searches, err := loader.LoadSearches(ctx, u.user.ID)
		if err != nil {
			return nil, err
		}
		out := make([]*SavedSearchObject, len(searches))
		for i, s := range searches {
			s := s
			out[i] = &SavedSearchObject{s: &s}
		}
		return out, nil
	}
	// Fallback: direct query (no DataLoader in context)
	searches, err := u.saves.GetSavedSearchesByUserID(ctx, u.user.ID)
	if err != nil {
		return nil, fmt.Errorf("could not load saved searches")
	}
	out := make([]*SavedSearchObject, len(searches))
	for i, s := range searches {
		s := s
		out[i] = &SavedSearchObject{s: &s}
	}
	return out, nil
}

// SavedSearchObject wraps a db.SavedSearch.
type SavedSearchObject struct {
	s *db.SavedSearch
}

func (s *SavedSearchObject) ID() string        { return s.s.ID }
func (s *SavedSearchObject) Query() string     { return s.s.Query }
func (s *SavedSearchObject) CreatedAt() string { return s.s.CreatedAt.Format("2006-01-02T15:04:05Z") }
