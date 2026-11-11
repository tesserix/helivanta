package loginclient

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

type tenantResolverFunc func(context.Context, string, string) (string, error)

func (f tenantResolverFunc) TenantLoginSubject(ctx context.Context, org, email string) (string, error) {
	return f(ctx, org, email)
}

func TestTenantPasswordSessionUsesTheBrokerAndRechecksTheNativeSessionOrg(t *testing.T) {
	const org = "helivanta-11111111-1111-4111-8111-111111111111"
	for _, tc := range []struct {
		name, sessionOrg       string
		resolverErr, errorWant error
	}{
		{"own", org, nil, nil}, {"foreign native session", "other-org", nil, ErrAuthRequestInvalid}, {"unknown target", org, ErrUserNotFound, ErrUserNotFound}, {"broker outage", org, ErrUnavailable, ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sessions, lookups := 0, 0
			native := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.Method + " " + r.URL.Path {
				case "GET /v2/oidc/auth_requests/V2_test":
					_ = json.NewEncoder(w).Encode(map[string]any{"authRequest": map[string]any{"id": "V2_test", "clientId": "web", "scope": []string{"openid", organizationScopePrefix + org, "urn:zitadel:iam:org:project:id:product:aud"}}})
				case "POST /v2/sessions":
					sessions++
					var in struct {
						Checks struct {
							User map[string]string `json:"user"`
						} `json:"checks"`
					}
					_ = json.NewDecoder(r.Body).Decode(&in)
					if in.Checks.User["userId"] != "tenant-owner" || in.Checks.User["loginName"] != "" {
						t.Error("password was not checked for exact broker subject")
					}
					_, _ = w.Write([]byte(`{"sessionId":"session","sessionToken":"private"}`))
				case "GET /v2/sessions/session":
					_ = json.NewEncoder(w).Encode(map[string]any{"session": map[string]any{"factors": map[string]any{"user": map[string]string{"id": "tenant-owner", "organizationId": tc.sessionOrg}}}})
				default:
					t.Errorf("unexpected native request %s %s", r.Method, r.URL.Path)
					w.WriteHeader(500)
				}
			}))
			defer native.Close()
			c := New(native.URL, "synthetic", native.Client())
			c.SetTenantUserResolver(tenantResolverFunc(func(_ context.Context, target, email string) (string, error) {
				lookups++
				if target != org || email != "same@example.test" {
					t.Error("lookup escaped native org scope")
				}
				if tc.resolverErr != nil {
					return "", tc.resolverErr
				}
				return "tenant-owner", nil
			}))
			session, err := c.CreatePasswordSessionForRequest(t.Context(), "V2_test", "web", "same@example.test", "test-password")
			if lookups != 1 {
				t.Fatal("broker not used")
			}
			if tc.errorWant != nil {
				if !errors.Is(err, tc.errorWant) || session.ID != "" {
					t.Fatalf("unproven session accepted: %v", err)
				}
			} else if err != nil || session.ID != "session" {
				t.Fatalf("own account refused: %v", err)
			}
			if tc.resolverErr != nil && sessions != 0 {
				t.Fatal("broker refusal reached native credential check")
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
		{"unsupported domain selector", "V2_test", "web", []string{"urn:zitadel:iam:org:domain:primary:foreign.example"}},
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
