package rest

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"github.com/meteaksoyy/nexus/internal/upstream"
)

// GitHubHandlers proxies requests to the GitHub REST API.
type GitHubHandlers struct {
	client *upstream.Client
	token  string
	log    zerolog.Logger
}

func NewGitHubHandlers(client *upstream.Client, token string, log zerolog.Logger) *GitHubHandlers {
	return &GitHubHandlers{
		client: client,
		token:  token,
		log:    log.With().Str("handler", "github").Logger(),
	}
}

// GetUser handles GET /api/v1/github/users/:login
func (h *GitHubHandlers) GetUser(w http.ResponseWriter, r *http.Request) {
	login := chi.URLParam(r, "login")
	url := "https://api.github.com/users/" + login

	headers := h.authHeaders()
	body, status, err := h.client.Get(r.Context(), url, headers)
	if err != nil {
		h.log.Warn().Err(err).Str("login", login).Msg("github user fetch failed")
		http.Error(w, `{"error":"upstream error"}`, http.StatusBadGateway)
		return
	}
	if status == http.StatusNotFound {
		http.Error(w, `{"error":"user not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(body)
}

// GetRepo handles GET /api/v1/github/repos/:owner/:repo
func (h *GitHubHandlers) GetRepo(w http.ResponseWriter, r *http.Request) {
	owner := chi.URLParam(r, "owner")
	repo := chi.URLParam(r, "repo")
	url := "https://api.github.com/repos/" + owner + "/" + repo

	headers := h.authHeaders()
	body, status, err := h.client.Get(r.Context(), url, headers)
	if err != nil {
		h.log.Warn().Err(err).Str("repo", owner+"/"+repo).Msg("github repo fetch failed")
		http.Error(w, `{"error":"upstream error"}`, http.StatusBadGateway)
		return
	}
	if status == http.StatusNotFound {
		http.Error(w, `{"error":"repo not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	w.Write(body)
}

// authHeaders returns Authorization and Accept headers for GitHub API requests.
// If no token is configured the header is omitted (unauthenticated, lower rate limit).
func (h *GitHubHandlers) authHeaders() map[string]string {
	headers := map[string]string{
		"Accept": "application/vnd.github+json",
	}
	if h.token != "" {
		headers["Authorization"] = "Bearer " + h.token
	}
	return headers
}

// writeJSON is a small helper used by handlers that build their own response bodies.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
