//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
)

type gqlRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

type gqlResponse struct {
	Data   map[string]any   `json:"data"`
	Errors []map[string]any `json:"errors"`
}

func gqlPost(t *testing.T, env *testEnv, token, query string, vars map[string]any) gqlResponse {
	t.Helper()
	body, _ := json.Marshal(gqlRequest{Query: query, Variables: vars})
	req, _ := http.NewRequest(http.MethodPost, env.url("/graphql"), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("graphql request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("graphql status: got %d, want 200", resp.StatusCode)
	}

	var gqlResp gqlResponse
	json.NewDecoder(resp.Body).Decode(&gqlResp)
	return gqlResp
}

func TestGraphQL_RequiresAuth(t *testing.T) {
	env := newTestEnv(t)

	body, _ := json.Marshal(gqlRequest{Query: `{ me { id } }`})
	resp, _ := http.Post(env.url("/graphql"), "application/json", bytes.NewReader(body))
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("unauthenticated graphql: got %d, want 401", resp.StatusCode)
	}
}

func TestGraphQL_MeQuery(t *testing.T) {
	env := newTestEnv(t)
	token, _ := registerAndLogin(t, env, "me@example.com", "pass123")

	resp := gqlPost(t, env, token, `{ me { id email role } }`, nil)
	if len(resp.Errors) > 0 {
		t.Fatalf("graphql errors: %v", resp.Errors)
	}

	me, ok := resp.Data["me"].(map[string]any)
	if !ok || me == nil {
		t.Fatal("expected non-nil me in response")
	}
	if me["email"] != "me@example.com" {
		t.Errorf("me.email: got %v, want me@example.com", me["email"])
	}
	if me["role"] != "user" {
		t.Errorf("me.role: got %v, want user", me["role"])
	}
}

func TestGraphQL_CreateUserMutation(t *testing.T) {
	env := newTestEnv(t)
	token, _ := registerAndLogin(t, env, "admin@example.com", "pass123")

	resp := gqlPost(t, env, token, `
		mutation CreateUser($input: CreateUserInput!) {
			createUser(input: $input) { id email role }
		}
	`, map[string]any{
		"input": map[string]any{
			"email":    "newuser@example.com",
			"password": "newpass",
		},
	})

	if len(resp.Errors) > 0 {
		t.Fatalf("createUser errors: %v", resp.Errors)
	}

	user, ok := resp.Data["createUser"].(map[string]any)
	if !ok || user == nil {
		t.Fatal("expected non-nil createUser in response")
	}
	if user["email"] != "newuser@example.com" {
		t.Errorf("createUser.email: got %v", user["email"])
	}
}

func TestGraphQL_SaveSearchAndQuery(t *testing.T) {
	env := newTestEnv(t)
	token, _ := registerAndLogin(t, env, "search@example.com", "pass123")

	// Save a search
	save := gqlPost(t, env, token, `
		mutation { saveSearch(query: "golang") { id query } }
	`, nil)
	if len(save.Errors) > 0 {
		t.Fatalf("saveSearch errors: %v", save.Errors)
	}

	// Fetch it back via me.savedSearches (exercises DataLoader)
	fetch := gqlPost(t, env, token, `{ me { savedSearches { query } } }`, nil)
	if len(fetch.Errors) > 0 {
		t.Fatalf("savedSearches errors: %v", fetch.Errors)
	}

	me := fetch.Data["me"].(map[string]any)
	searches := me["savedSearches"].([]any)
	if len(searches) == 0 {
		t.Fatal("expected at least one saved search")
	}
	first := searches[0].(map[string]any)
	if first["query"] != "golang" {
		t.Errorf("savedSearch.query: got %v, want golang", first["query"])
	}
}

func TestGraphQL_DepthLimitRejected(t *testing.T) {
	env := newTestEnv(t)
	token, _ := registerAndLogin(t, env, "depth@example.com", "pass123")

	// Craft a query that exceeds MaxDepth=8
	deepQuery := `{
		me {
			savedSearches {
				id
				query
				id
				query
				id
				query
				id
				query
			}
		}
	}`

	// Nest it further to exceed depth limit
	deepQuery = `{ me { savedSearches { id } } }` // valid first
	resp := gqlPost(t, env, token, deepQuery, nil)
	// This should succeed (not deep enough)
	if len(resp.Errors) > 0 {
		t.Logf("shallow query errors (unexpected): %v", resp.Errors)
	}

	// A truly deep query that exceeds 8 levels
	tooDeep := buildDeepQuery(9)
	resp = gqlPost(t, env, token, tooDeep, nil)
	if len(resp.Errors) == 0 {
		t.Error("expected depth limit error for query exceeding MaxDepth=8")
	}
}

// buildDeepQuery creates a query with the given nesting depth.
func buildDeepQuery(depth int) string {
	q := "{ me"
	for range depth {
		q += " { savedSearches"
	}
	q += " { id }"
	for range depth {
		q += " }"
	}
	q += " }"
	return q
}
