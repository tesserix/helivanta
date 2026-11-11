package loginclient

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTenantPasswordSessionResolvesEmailOnlyInsideNativeRequestedOrganization(t *testing.T) {
	const org = "helivanta-11111111-1111-4111-8111-111111111111"
	for _, tc := range []struct {
		name, resultOrg string
		count           int
		wantErr         bool
	}{
		{"own tenant", org, 1, false}, {"foreign tenant", "other-org", 1, true},
		{"ambiguous email", org, 2, true}, {"unknown email", org, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sessions := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Method + " " + r.URL.Path {
				case "GET /v2/oidc/auth_requests/V2_test":
					_ = json.NewEncoder(w).Encode(map[string]any{"authRequest": map[string]any{"id": "V2_test", "clientId": "web", "scope": []string{"openid", "urn:zitadel:iam:org:id:" + org}}})
				case "POST /v2/users":
					var in map[string]any
					if json.NewDecoder(r.Body).Decode(&in) != nil {
						t.Error("invalid user search")
					}
					queries, _ := in["queries"].([]any)
					foundOrg, foundEmail := false, false
					for _, q := range queries {
						m, _ := q.(map[string]any)
						if v, ok := m["organizationIdQuery"].(map[string]any); ok {
							foundOrg = v["organizationId"] == org
						}
						if v, ok := m["emailQuery"].(map[string]any); ok {
							foundEmail = v["emailAddress"] == "same@example.test"
						}
					}
					if !foundOrg || !foundEmail {
						t.Error("email search escaped requested organisation")
					}
					users := []any{}
					for range tc.count {
						users = append(users, map[string]any{"userId": "tenant-owner", "state": "USER_STATE_ACTIVE", "details": map[string]string{"resourceOwner": tc.resultOrg}, "human": map[string]any{"email": map[string]any{"email": "same@example.test", "isVerified": true}}})
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"result": users})
				case "POST /v2/sessions":
					sessions++
					var in struct {
						Checks struct {
							User map[string]string `json:"user"`
						} `json:"checks"`
					}
					_ = json.NewDecoder(r.Body).Decode(&in)
					if in.Checks.User["userId"] != "tenant-owner" || in.Checks.User["loginName"] != "" {
						t.Error("password checked another account")
					}
					_, _ = w.Write([]byte(`{"sessionId":"session","sessionToken":"private"}`))
				case "GET /v2/sessions/session":
					_ = json.NewEncoder(w).Encode(map[string]any{"session": map[string]any{"factors": map[string]any{"user": map[string]string{"id": "tenant-owner", "organizationId": org}}}})
				default:
					t.Errorf("unexpected native request %s %s", r.Method, r.URL.Path)
					w.WriteHeader(500)
				}
			}))
			defer server.Close()
			c := New(server.URL, "native-test", server.Client())
			s, err := c.CreatePasswordSessionForRequest(t.Context(), "V2_test", "web", "same@example.test", "test-password")
			if tc.wantErr {
				if !errors.Is(err, ErrUserNotFound) || sessions != 0 || s.ID != "" {
					t.Fatalf("foreign/ambiguous login accepted: %v sessions=%d", err, sessions)
				}
			} else if err != nil || s.ID != "session" || sessions != 1 {
				t.Fatalf("scoped login failed: %v sessions=%d", err, sessions)
			}
		})
	}
}

func TestTenantPasswordSessionRejectsUnsafeNativeRequestBeforePassword(t *testing.T) {
	const scope = "urn:zitadel:iam:org:id:helivanta-11111111-1111-4111-8111-111111111111"
	for _, tc := range []struct {
		name, id, client string
		scopes           []string
	}{
		{"foreign client", "V2_test", "other", []string{scope}},
		{"wrong request", "V2_other", "web", []string{scope}},
		{"malformed org", "V2_test", "web", []string{"urn:zitadel:iam:org:id:other"}},
		{"duplicate org scope", "V2_test", "web", []string{scope, scope}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" || r.URL.Path != "/v2/oidc/auth_requests/V2_test" {
					t.Error("unsafe request reached credential lookup")
					w.WriteHeader(500)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"authRequest": map[string]any{"id": tc.id, "clientId": tc.client, "scope": tc.scopes}})
			}))
			defer server.Close()
			s, err := New(server.URL, "synthetic", server.Client()).CreatePasswordSessionForRequest(t.Context(), "V2_test", "web", "same@example.test", "test-password")
			if !errors.Is(err, ErrAuthRequestInvalid) || s.ID != "" {
				t.Fatalf("unsafe native request accepted: %v", err)
			}
		})
	}
}
