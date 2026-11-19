package iam

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/modules/iam/loginclient"
	"github.com/tesserix/helivanta/pkg/ratelimit"
)

const (
	tenantA = "11111111-1111-4111-8111-111111111111"
	tenantB = "22222222-2222-4222-8222-222222222222"
)

type targetResolverFunc func(context.Context, string) ([]loginclient.LoginTarget, bool, error)

func (f targetResolverFunc) TenantLoginTargets(ctx context.Context, email string) ([]loginclient.LoginTarget, bool, error) {
	return f(ctx, email)
}

// multiAccountZitadel accepts password "right" for every user in accepts and
// refuses it otherwise; each session lives in the user's own organisation.
type multiAccountZitadel struct {
	mu        sync.Mutex
	accepts   map[string]bool
	created   []map[string]string
	deleted   []string
	finalized []string
}

func (z *multiAccountZitadel) client(t *testing.T) *loginclient.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v2/sessions", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Checks struct {
				User     map[string]string `json:"user"`
				Password struct {
					Password string `json:"password"`
				} `json:"password"`
			} `json:"checks"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		z.mu.Lock()
		z.created = append(z.created, in.Checks.User)
		z.mu.Unlock()
		user := in.Checks.User["userId"] + in.Checks.User["loginName"]
		if in.Checks.Password.Password != "right" || !z.accepts[user] {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"code":3,"message":"Password is invalid (COMMAND-3M0fs)"}`))
			return
		}
		_, _ = w.Write([]byte(`{"sessionId":"sess-` + user + `","sessionToken":"tok-` + user + `"}`))
	})
	mux.HandleFunc("GET /v2/sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		user := strings.TrimPrefix(r.PathValue("id"), "sess-")
		org := "helivanta-" + map[string]string{"user-a": tenantA, "user-b": tenantB}[user]
		_, _ = w.Write([]byte(`{"session":{"id":"sess-` + user + `","factors":{"user":{"id":"` + user + `","organizationId":"` + org + `"},"password":{"verifiedAt":"2026-01-02T00:00:00Z"}}}}`))
	})
	mux.HandleFunc("DELETE /v2/sessions/{id}", func(w http.ResponseWriter, r *http.Request) {
		z.mu.Lock()
		z.deleted = append(z.deleted, r.PathValue("id"))
		z.mu.Unlock()
		_, _ = w.Write([]byte(`{}`))
	})
	registerZitadelPolicyOK(mux)
	registerPasswordCurrent(mux)
	mux.HandleFunc("GET /v2/users/{id}/authentication_methods", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"authMethodTypes":["AUTHENTICATION_METHOD_TYPE_PASSWORD"]}`))
	})
	mux.HandleFunc("POST /v2/oidc/auth_requests/{id}", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Session struct {
				SessionID string `json:"sessionId"`
			} `json:"session"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		z.mu.Lock()
		z.finalized = append(z.finalized, in.Session.SessionID)
		z.mu.Unlock()
		_, _ = w.Write([]byte(`{"callbackUrl":"https://hms.test/api/auth/callback?code=abc&state=xyz"}`))
	})
	return newZitadelTestClient(t, mux)
}

func bothTargets(context.Context, string) ([]loginclient.LoginTarget, bool, error) {
	return []loginclient.LoginTarget{
		{TenantID: tenantA, OrganizationID: "helivanta-" + tenantA, Subject: "user-a", Name: "City Hospital", Account: "same@example.test"},
		{TenantID: tenantB, OrganizationID: "helivanta-" + tenantB, Subject: "user-b", Name: "Corner Pharmacy", Account: "same@example.test"},
	}, false, nil
}

func newAccountRouter(h *LoginUIHandlers) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/v1/auth/login/password", h.Password)
	r.POST("/v1/auth/login/account", h.Account)
	return r
}

func post(r *gin.Engine, path, body, ip string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = ip + ":12345"
	r.ServeHTTP(w, req)
	return w
}

func passwordBody(loginName, password string) string {
	return `{"auth_request_id":"` + loginUITestAuthRequestID + `","login_name":"` + loginName + `","password":"` + password + `"}`
}

func accountBody(tenant string) string {
	return `{"auth_request_id":"` + loginUITestAuthRequestID + `","tenant_id":"` + tenant + `"}`
}

// One password matching two accounts asks which one, completes only the
// chosen account, terminates the other session and cannot be picked twice.
func TestPasswordMatchingTwoAccountsOffersAChoiceAndCompletesOnlyTheChosenOne(t *testing.T) {
	z := &multiAccountZitadel{accepts: map[string]bool{"user-a": true, "user-b": true}}
	client := z.client(t)
	client.SetTenantTargetResolver(targetResolverFunc(bothTargets))
	h := newFactorTestHandlers(t, client)
	h.SetNativeWebClientID("helivanta-web")
	r := newAccountRouter(h)

	w := post(r, "/v1/auth/login/password", passwordBody("same@example.test", "right"), "203.0.113.20")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var offer struct {
		Choices []accountChoiceWire `json:"account_choice_required"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &offer))
	require.Equal(t, []accountChoiceWire{
		{TenantID: tenantA, Name: "City Hospital", Account: "same@example.test"},
		{TenantID: tenantB, Name: "Corner Pharmacy", Account: "same@example.test"},
	}, offer.Choices)
	require.NotContains(t, w.Body.String(), "tok-", "a session token reached the browser")
	require.Empty(t, z.finalized, "nothing completes before the person picks")

	// A pick that is not on offer changes nothing.
	w = post(r, "/v1/auth/login/account", accountBody("33333333-3333-4333-8333-333333333333"), "203.0.113.20")
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "auth_request_invalid")

	w = post(r, "/v1/auth/login/account", accountBody(tenantB), "203.0.113.20")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "callback_url")
	require.Equal(t, []string{"sess-user-b"}, z.finalized)
	require.Equal(t, []string{"sess-user-a"}, z.deleted, "the unchosen session must be terminated")

	w = post(r, "/v1/auth/login/account", accountBody(tenantA), "203.0.113.20")
	require.Equal(t, http.StatusBadRequest, w.Code, "a second pick must not resume the sibling")
	require.Equal(t, []string{"sess-user-b"}, z.finalized)
}

func TestPasswordMatchingOneOfTwoAccountsCompletesWithoutAChoice(t *testing.T) {
	z := &multiAccountZitadel{accepts: map[string]bool{"user-b": true}}
	client := z.client(t)
	client.SetTenantTargetResolver(targetResolverFunc(bothTargets))
	w := postPassword(t, client, "same@example.test", "right")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "callback_url")
	require.Equal(t, []string{"sess-user-b"}, z.finalized)
	require.Len(t, z.created, 2, "both accounts were checked")
}

func TestEmailWithNoAccountMatchingIsTheEqualisedRefusal(t *testing.T) {
	z := &multiAccountZitadel{accepts: map[string]bool{"user-a": true, "user-b": true}}
	client := z.client(t)
	client.SetTenantTargetResolver(targetResolverFunc(bothTargets))
	wrong := postPassword(t, client, "same@example.test", "wrong")

	unknown := &multiAccountZitadel{}
	other := unknown.client(t)
	other.SetTenantTargetResolver(targetResolverFunc(func(context.Context, string) ([]loginclient.LoginTarget, bool, error) {
		return []loginclient.LoginTarget{}, false, nil
	}))
	none := postPassword(t, other, "nobody@example.test", "right")

	require.Equal(t, http.StatusUnauthorized, wrong.Code)
	require.Equal(t, wrong.Code, none.Code)
	require.JSONEq(t, wrong.Body.String(), none.Body.String())
	require.Equal(t, []map[string]string{{"loginName": "nobody@example.test"}}, unknown.created,
		"an email with no account costs one ordinary password check")
}

func TestTooManyAccountsChecksNoPassword(t *testing.T) {
	z := &multiAccountZitadel{}
	client := z.client(t)
	client.SetTenantTargetResolver(targetResolverFunc(func(context.Context, string) ([]loginclient.LoginTarget, bool, error) {
		return []loginclient.LoginTarget{}, true, nil
	}))
	w := postPassword(t, client, "busy@example.test", "right")
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "too_many_accounts")
	require.Empty(t, z.created)
}

func TestTargetLookupOutageIsUnavailableAndChecksNoPassword(t *testing.T) {
	z := &multiAccountZitadel{}
	client := z.client(t)
	client.SetTenantTargetResolver(targetResolverFunc(func(context.Context, string) ([]loginclient.LoginTarget, bool, error) {
		return nil, false, loginclient.ErrUnavailable
	}))
	w := postPassword(t, client, "same@example.test", "right")
	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.Empty(t, z.created)
}

func TestLoginNameWithoutAtSkipsTheEmailLookup(t *testing.T) {
	z := &multiAccountZitadel{accepts: map[string]bool{"user-a": true}}
	client := z.client(t)
	client.SetTenantTargetResolver(targetResolverFunc(func(context.Context, string) ([]loginclient.LoginTarget, bool, error) {
		t.Error("a login name without @ reached the email lookup")
		return nil, false, nil
	}))
	w := postPassword(t, client, "user-a", "right")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, []map[string]string{{"loginName": "user-a"}}, z.created)
}

// The per-email bucket (#1751 spec D6) holds across client IPs, and
// case-folds so variants of one address share it.
func TestPerEmailBucketThrottlesASprayAcrossIPs(t *testing.T) {
	z := &multiAccountZitadel{}
	client := z.client(t)
	client.SetTenantTargetResolver(targetResolverFunc(func(context.Context, string) ([]loginclient.LoginTarget, bool, error) {
		return []loginclient.LoginTarget{}, false, nil
	}))
	h := NewLoginUIHandlers(client, nil, ratelimit.NewMemory(100), ratelimit.Rule{Rate: 1000, Burst: 1000, Per: time.Minute}, ratelimit.Rule{})
	h.SetNativeWebClientID("helivanta-web")
	r := newAccountRouter(h)
	for i := range emailPasswordRule.Burst {
		email := "victim@example.test"
		if i%2 == 1 {
			email = "Victim@Example.test"
		}
		w := post(r, "/v1/auth/login/password", passwordBody(email, "wrong"), "198.51.100."+string(rune('1'+i)))
		require.Equal(t, http.StatusUnauthorized, w.Code, "attempt %d", i)
	}
	w := post(r, "/v1/auth/login/password", passwordBody("victim@example.test", "wrong"), "198.51.100.200")
	require.Equal(t, http.StatusTooManyRequests, w.Code, "a new IP must not reset the email's budget")
	w = post(r, "/v1/auth/login/password", passwordBody("other@example.test", "wrong"), "198.51.100.200")
	require.Equal(t, http.StatusUnauthorized, w.Code, "another email keeps its own budget")
}

// Only wrong passwords spend the per-email budget: a clinician signing in
// again and again (idle timeouts, shift handovers) never meets it.
func TestPerEmailBucketRefundsARightPassword(t *testing.T) {
	z := &multiAccountZitadel{accepts: map[string]bool{"user-a": true}}
	client := z.client(t)
	client.SetTenantTargetResolver(targetResolverFunc(func(context.Context, string) ([]loginclient.LoginTarget, bool, error) {
		targets, _, _ := bothTargets(context.Background(), "")
		return targets[:1], false, nil
	}))
	h := NewLoginUIHandlers(client, nil, ratelimit.NewMemory(100), ratelimit.Rule{Rate: 1000, Burst: 1000, Per: time.Minute}, ratelimit.Rule{})
	h.SetNativeWebClientID("helivanta-web")
	r := newAccountRouter(h)
	for i := range 3 * emailPasswordRule.Burst {
		w := post(r, "/v1/auth/login/password", passwordBody("same@example.test", "right"), "198.51.100.7")
		require.Equal(t, http.StatusOK, w.Code, "sign-in %d: %s", i, w.Body.String())
	}
	for i := range emailPasswordRule.Burst {
		w := post(r, "/v1/auth/login/password", passwordBody("same@example.test", "wrong"), "198.51.100.7")
		require.Equal(t, http.StatusUnauthorized, w.Code, "wrong password %d", i)
	}
	w := post(r, "/v1/auth/login/password", passwordBody("same@example.test", "right"), "198.51.100.7")
	require.Equal(t, http.StatusTooManyRequests, w.Code, "five wrong passwords still close the bucket")
}
