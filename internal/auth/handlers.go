package auth

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"golang.org/x/crypto/bcrypt"
)

// Handlers exposes HTTP handlers for auth endpoints.
type Handlers struct {
	userQ         *queries.UserQueries
	apiKeySvc     *APIKeyService
	refreshSvc    *RefreshService
	denylist      *Denylist
	jwtSecret     string
	jwtExpiryMins int
	log           zerolog.Logger
}

func NewHandlers(
	userQ *queries.UserQueries,
	apiKeySvc *APIKeyService,
	refreshSvc *RefreshService,
	denylist *Denylist,
	jwtSecret string,
	jwtExpiryMins int,
	log zerolog.Logger,
) *Handlers {
	return &Handlers{
		userQ:         userQ,
		apiKeySvc:     apiKeySvc,
		refreshSvc:    refreshSvc,
		denylist:      denylist,
		jwtSecret:     jwtSecret,
		jwtExpiryMins: jwtExpiryMins,
		log:           log,
	}
}

// Token issues a JWT + refresh token in exchange for email/password.
// POST /auth/token
func (h *Handlers) Token(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	user, err := h.userQ.GetUserByEmail(ctx, req.Email)
	if err != nil {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		http.Error(w, "invalid credentials", http.StatusUnauthorized)
		return
	}

	token, _, err := IssueJWT(h.jwtSecret, h.jwtExpiryMins, user.ID, user.Email, user.Role)
	if err != nil {
		h.log.Error().Err(err).Msg("issue jwt")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	refreshToken, err := h.refreshSvc.Issue(ctx, user.ID)
	if err != nil {
		h.log.Error().Err(err).Msg("issue refresh token")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"token":         token,
		"refresh_token": refreshToken,
		"expires_in":    h.jwtExpiryMins * 60,
	})
}

// Refresh exchanges a refresh token for a new JWT + rotated refresh token.
// POST /auth/refresh
func (h *Handlers) Refresh(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.RefreshToken == "" {
		http.Error(w, "refresh_token required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	oldToken, newRefreshToken, err := h.refreshSvc.Rotate(ctx, req.RefreshToken)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	user, err := h.userQ.GetUserByID(ctx, oldToken.UserID)
	if err != nil {
		h.log.Error().Err(err).Msg("fetch user on refresh")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	token, _, err := IssueJWT(h.jwtSecret, h.jwtExpiryMins, user.ID, user.Email, user.Role)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"token":         token,
		"refresh_token": newRefreshToken,
		"expires_in":    h.jwtExpiryMins * 60,
	})
}

// Logout revokes the current JWT (adds JTI to denylist) and the provided refresh token.
// POST /auth/logout
func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Revoke the JWT — expiry time was stored in context by auth middleware
	jti := JTIFromContext(ctx)
	if jti != "" {
		expiresAt := ExpiresAtFromContext(ctx)
		_ = h.denylist.Revoke(ctx, jti, expiresAt)
	}

	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.RefreshToken != "" {
		_ = h.refreshSvc.Revoke(ctx, req.RefreshToken)
	}

	w.WriteHeader(http.StatusNoContent)
}

// CreateAPIKey issues a new named API key for the authenticated user.
// POST /auth/apikey
func (h *Handlers) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}

	userID := UserIDFromContext(r.Context())
	plaintext, key, err := h.apiKeySvc.Create(r.Context(), userID, req.Name)
	if err != nil {
		h.log.Error().Err(err).Msg("create api key")
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":        key.ID,
		"name":      key.Name,
		"key":       plaintext, // shown once
		"created_at": key.CreatedAt.Format(time.RFC3339),
	})
}

// DeleteAPIKey revokes an API key owned by the authenticated user.
// DELETE /auth/apikey/{id}
func (h *Handlers) DeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	userID := UserIDFromContext(r.Context())

	if err := h.apiKeySvc.Delete(r.Context(), id, userID); err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// Register creates a new user account.
// POST /auth/register
func (h *Handlers) Register(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" || req.Password == "" {
		http.Error(w, "email and password required", http.StatusBadRequest)
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	user, err := h.userQ.CreateUser(r.Context(), req.Email, string(hash), "user")
	if err != nil {
		http.Error(w, "email already registered", http.StatusConflict)
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"id":         user.ID,
		"email":      user.Email,
		"role":       user.Role,
		"created_at": user.CreatedAt.Format(time.RFC3339),
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
