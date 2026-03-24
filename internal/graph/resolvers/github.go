package resolvers

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rs/zerolog"
	"github.com/meteaksoyy/nexus/internal/upstream"
)

// GitHubResolver handles GraphQL queries that fetch data from the GitHub REST API.
type GitHubResolver struct {
	client *upstream.Client
	token  string
	log    zerolog.Logger
}

func NewGitHubResolver(client *upstream.Client, token string, log zerolog.Logger) *GitHubResolver {
	return &GitHubResolver{client: client, token: token, log: log}
}

// ── Query resolvers ──────────────────────────────────────────────────────────

func (r *GitHubResolver) GithubUser(ctx context.Context, args struct{ Login string }) (*GitHubUserObject, error) {
	body, status, err := r.client.Get(ctx, "https://api.github.com/users/"+args.Login, r.headers())
	if err != nil {
		return nil, fmt.Errorf("github upstream error")
	}
	if status == 404 {
		return nil, fmt.Errorf("github user %q not found", args.Login)
	}

	var data struct {
		Login       string `json:"login"`
		Name        string `json:"name"`
		Bio         string `json:"bio"`
		PublicRepos int    `json:"public_repos"`
		Followers   int    `json:"followers"`
		AvatarURL   string `json:"avatar_url"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("github response parse error")
	}
	return &GitHubUserObject{
		login:       data.Login,
		name:        data.Name,
		bio:         data.Bio,
		publicRepos: data.PublicRepos,
		followers:   data.Followers,
		avatarURL:   data.AvatarURL,
	}, nil
}

func (r *GitHubResolver) GithubRepo(ctx context.Context, args struct {
	Owner string
	Name  string
}) (*GitHubRepoObject, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s", args.Owner, args.Name)
	body, status, err := r.client.Get(ctx, url, r.headers())
	if err != nil {
		return nil, fmt.Errorf("github upstream error")
	}
	if status == 404 {
		return nil, fmt.Errorf("github repo %s/%s not found", args.Owner, args.Name)
	}

	var data struct {
		Name        string `json:"name"`
		FullName    string `json:"full_name"`
		Description string `json:"description"`
		Stars       int    `json:"stargazers_count"`
		Forks       int    `json:"forks_count"`
		Language    string `json:"language"`
		OpenIssues  int    `json:"open_issues_count"`
		HTMLURL     string `json:"html_url"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("github response parse error")
	}
	return &GitHubRepoObject{
		name:        data.Name,
		fullName:    data.FullName,
		description: data.Description,
		stars:       data.Stars,
		forks:       data.Forks,
		language:    data.Language,
		openIssues:  data.OpenIssues,
		htmlURL:     data.HTMLURL,
	}, nil
}

func (r *GitHubResolver) headers() map[string]string {
	h := map[string]string{"Accept": "application/vnd.github+json"}
	if r.token != "" {
		h["Authorization"] = "Bearer " + r.token
	}
	return h
}

// ── Object resolvers ─────────────────────────────────────────────────────────

type GitHubUserObject struct {
	login       string
	name        string
	bio         string
	publicRepos int
	followers   int
	avatarURL   string
}

func (u *GitHubUserObject) Login() string       { return u.login }
func (u *GitHubUserObject) Name() *string       { if u.name == "" { return nil }; return &u.name }
func (u *GitHubUserObject) Bio() *string        { if u.bio == "" { return nil }; return &u.bio }
func (u *GitHubUserObject) PublicRepos() int32  { return int32(u.publicRepos) }
func (u *GitHubUserObject) Followers() int32    { return int32(u.followers) }
func (u *GitHubUserObject) AvatarUrl() *string  { if u.avatarURL == "" { return nil }; return &u.avatarURL }

type GitHubRepoObject struct {
	name        string
	fullName    string
	description string
	stars       int
	forks       int
	language    string
	openIssues  int
	htmlURL     string
}

func (r *GitHubRepoObject) Name() string         { return r.name }
func (r *GitHubRepoObject) FullName() string     { return r.fullName }
func (r *GitHubRepoObject) Description() *string { if r.description == "" { return nil }; return &r.description }
func (r *GitHubRepoObject) Stars() int32         { return int32(r.stars) }
func (r *GitHubRepoObject) Forks() int32         { return int32(r.forks) }
func (r *GitHubRepoObject) Language() *string    { if r.language == "" { return nil }; return &r.language }
func (r *GitHubRepoObject) OpenIssues() int32    { return int32(r.openIssues) }
func (r *GitHubRepoObject) HtmlUrl() string      { return r.htmlURL }
