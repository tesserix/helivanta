package customeronboarding

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/tesserix/onboarding-service/api/customercontract"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestDiscoveryRequiresSameOriginAndSeparateBrowserCapability(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handlers{origin: "https://helivanta.app"}
	r := gin.New()
	r.POST("/v1/auth/accounts", h.DiscoverAccounts)
	r.POST("/v1/auth/accounts/verify", h.VerifyAccounts)
	r.GET("/v1/auth/accounts", h.AccountChoices)
	for _, path := range []string{"/v1/auth/accounts", "/v1/auth/accounts/verify"} {
		req := httptest.NewRequest("POST", path, strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		res := httptest.NewRecorder()
		r.ServeHTTP(res, req)
		if res.Code != 403 {
			t.Fatal("cross-origin discovery admitted", res.Code)
		}
	}
	cap, err := newCapability()
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/v1/auth/accounts", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: cap.id.String() + "." + cap.token})
	res := httptest.NewRecorder()
	r.ServeHTTP(res, req)
	if res.Code != 404 {
		t.Fatal("signup receipt permitted account discovery", res.Code)
	}
}

func TestDiscoveryProxyBindsPrivateProofToDistinctSecureCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var captured customercontract.DiscoveryStart
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer synthetic-workload" {
			t.Error("missing workload proof")
		}
		switch r.URL.Path {
		case "/internal/v1/customer-signups/account-discovery/start":
			if json.NewDecoder(r.Body).Decode(&captured) != nil || captured.Validate() != nil {
				t.Error("invalid private challenge")
			}
			w.WriteHeader(202)
			_, _ = w.Write([]byte(`{"state":"check_email"}`))
		case "/internal/v1/customer-signups/account-discovery/verify":
			var proof customercontract.DiscoveryProof
			if json.NewDecoder(r.Body).Decode(&proof) != nil || proof.BrowserHash != captured.BrowserHash || proof.ID != captured.ID || proof.Code != "12345678" {
				t.Error("proof did not bind browser")
			}
			_, _ = w.Write([]byte(`{"state":"verified"}`))
		case "/internal/v1/customer-signups/account-discovery/choices":
			_, _ = w.Write([]byte(`{"accounts":[{"tenantId":"11111111-1111-4111-8111-111111111111","name":"Yes Hospital","account":"owner@example.test"}]}`))
		default:
			t.Error("unexpected identity route")
			w.WriteHeader(500)
		}
	}))
	defer owner.Close()
	file := t.TempDir() + "/token"
	if os.WriteFile(file, []byte("synthetic-workload"), 0600) != nil {
		t.Fatal("token fixture")
	}
	h := &Handlers{origin: "https://helivanta.app", base: owner.URL, client: owner.Client(), tokenFile: file}
	r := gin.New()
	_ = r.SetTrustedProxies(nil)
	r.POST("/v1/auth/accounts", h.DiscoverAccounts)
	r.POST("/v1/auth/accounts/verify", h.VerifyAccounts)
	r.GET("/v1/auth/accounts", h.AccountChoices)
	call := func(method, path, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Origin", "https://helivanta.app")
		req.Header.Set("Content-Type", "application/json")
		if cookie != nil {
			req.AddCookie(cookie)
		}
		res := httptest.NewRecorder()
		r.ServeHTTP(res, req)
		return res
	}
	start := call("POST", "/v1/auth/accounts", `{"email":"owner@example.test"}`, nil)
	if start.Code != 202 || strings.Contains(start.Body.String(), "tenantId") {
		t.Fatal("start leaked identity or failed", start.Code)
	}
	cookies := start.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != discoveryCookieName || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].SameSite != http.SameSiteStrictMode || cookies[0].MaxAge != 600 {
		t.Fatal("discovery capability is not host-private")
	}
	if got := call("POST", "/v1/auth/accounts/verify", `{"code":"12345678"}`, cookies[0]); got.Code != 200 {
		t.Fatal("verification failed", got.Code)
	}
	if got := call("GET", "/v1/auth/accounts", "", cookies[0]); got.Code != 200 || !strings.Contains(got.Body.String(), "Yes Hospital") || got.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("choices unavailable or cacheable", got.Code)
	}
}
