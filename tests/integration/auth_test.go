//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

func TestAuth_RegisterAndLogin(t *testing.T) {
	env := newTestEnv(t)

	// ── Register ──────────────────────────────────────────────────────────────
	body, _ := json.Marshal(map[string]string{
		"email":    "test@example.com",
		"password": "hunter2",
	})
	resp, err := http.Post(env.url("/auth/register"), "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("register status: got %d, want 201", resp.StatusCode)
	}

	// ── Login ─────────────────────────────────────────────────────────────────
	body, _ = json.Marshal(map[string]string{
		"email":    "test@example.com",
		"password": "hunter2",
	})
	resp, err = http.Post(env.url("/auth/token"), "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("token status: got %d, want 200", resp.StatusCode)
	}

	var tokenResp struct {
		Token        string `json:"token"`
		RefreshToken string `json:"refresh_token"`
	}
	json.NewDecoder(resp.Body).Decode(&tokenResp)
	if tokenResp.Token == "" {
		t.Fatal("expected non-empty token")
	}
	if tokenResp.RefreshToken == "" {
		t.Fatal("expected non-empty refresh_token")
	}
}

func TestAuth_WrongPassword(t *testing.T) {
	env := newTestEnv(t)

	body, _ := json.Marshal(map[string]string{"email": "a@b.com", "password": "pass"})
	http.Post(env.url("/auth/register"), "application/json", bytes.NewReader(body)) //nolint:errcheck

	body, _ = json.Marshal(map[string]string{"email": "a@b.com", "password": "wrong"})
	resp, _ := http.Post(env.url("/auth/token"), "application/json", bytes.NewReader(body))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong password: got %d, want 401", resp.StatusCode)
	}
}

func TestAuth_RefreshRotation(t *testing.T) {
	env := newTestEnv(t)
	token, refresh := registerAndLogin(t, env, "refresh@example.com", "pass123")
	_ = token

	// ── First refresh ─────────────────────────────────────────────────────────
	body, _ := json.Marshal(map[string]string{"refresh_token": refresh})
	resp, err := http.Post(env.url("/auth/refresh"), "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("refresh status: got %d, want 200", resp.StatusCode)
	}

	var r struct {
		Token        string `json:"token"`
		RefreshToken string `json:"refresh_token"`
	}
	json.NewDecoder(resp.Body).Decode(&r)
	if r.RefreshToken == refresh {
		t.Error("refresh token should rotate — new token should differ from old")
	}

	// ── Old refresh token must be invalid now ─────────────────────────────────
	body, _ = json.Marshal(map[string]string{"refresh_token": refresh})
	resp2, _ := http.Post(env.url("/auth/refresh"), "application/json", bytes.NewReader(body))
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Errorf("reuse of old refresh token: got %d, want 401", resp2.StatusCode)
	}
}

func TestAuth_LogoutRevokesJWT(t *testing.T) {
	env := newTestEnv(t)
	token, refresh := registerAndLogin(t, env, "logout@example.com", "pass123")

	client := &http.Client{}

	// ── Logout ────────────────────────────────────────────────────────────────
	body, _ := json.Marshal(map[string]string{"refresh_token": refresh})
	req, _ := http.NewRequest(http.MethodPost, env.url("/auth/logout"), bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("logout: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("logout status: got %d, want 204", resp.StatusCode)
	}

	// ── Token should now be denied ─────────────────────────────────────────────
	req2, _ := http.NewRequest(http.MethodGet, env.url("/api/v1/github/users/octocat"), nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	resp2, _ := client.Do(req2)
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Errorf("revoked JWT: got %d, want 401", resp2.StatusCode)
	}
}

func TestAuth_DuplicateEmail(t *testing.T) {
	env := newTestEnv(t)

	body, _ := json.Marshal(map[string]string{"email": "dup@example.com", "password": "pass"})
	http.Post(env.url("/auth/register"), "application/json", bytes.NewReader(body)) //nolint:errcheck

	body, _ = json.Marshal(map[string]string{"email": "dup@example.com", "password": "pass2"})
	resp, _ := http.Post(env.url("/auth/register"), "application/json", bytes.NewReader(body))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Errorf("duplicate email: got %d, want 409", resp.StatusCode)
	}
}

// registerAndLogin is a test helper that creates a user and returns (jwt, refreshToken).
func registerAndLogin(t *testing.T, env *testEnv, email, password string) (string, string) {
	t.Helper()

	body, _ := json.Marshal(map[string]string{"email": email, "password": password})
	resp, err := http.Post(env.url("/auth/register"), "application/json", bytes.NewReader(body))
	if err != nil || resp.StatusCode != http.StatusCreated {
		t.Fatalf("register helper failed: status %d", resp.StatusCode)
	}
	resp.Body.Close()

	body, _ = json.Marshal(map[string]string{"email": email, "password": password})
	resp, err = http.Post(env.url("/auth/token"), "application/json", bytes.NewReader(body))
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("login helper failed: status %d", resp.StatusCode)
	}
	defer resp.Body.Close()

	var r struct {
		Token        string `json:"token"`
		RefreshToken string `json:"refresh_token"`
	}
	json.NewDecoder(resp.Body).Decode(&r)
	return r.Token, r.RefreshToken
}
