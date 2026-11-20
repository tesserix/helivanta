package customeronboarding

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestSignupRejectsCrossOriginPostsBeforePrivateProvisioner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handlers{origin: "https://helivanta.app"}
	router := gin.New()
	router.POST("/v1/auth/signup", h.Start)
	for _, origin := range []string{"", "https://other.example", "https://helivanta.app.evil.example"} {
		request := httptest.NewRequest(http.MethodPost, "/v1/auth/signup", strings.NewReader(`{}`))
		request.Header.Set("Origin", origin)
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("foreign origin reached provisioner: %d", response.Code)
		}
	}
}

func TestNewSignupStatusIssuesOnlySecurePrivateReceiptAndNoTenant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := &Handlers{origin: "https://helivanta.app"}
	router.GET("/v1/auth/signup", h.Status)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/auth/signup", nil))
	if response.Code != 200 || strings.TrimSpace(response.Body.String()) != `{"state":"not_started"}` {
		t.Fatal("new signup disclosed an uncreated tenant")
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != cookieName || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].Path != "/" || cookies[0].Domain != "" || cookies[0].SameSite != http.SameSiteLaxMode || cookies[0].MaxAge != 7*24*60*60 {
		t.Fatal("signup receipt cookie is not private and host scoped")
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/auth/signup", nil)
	request.AddCookie(cookies[0])
	request.AddCookie(cookies[0])
	duplicate := httptest.NewRecorder()
	router.ServeHTTP(duplicate, request)
	if duplicate.Code != 400 {
		t.Fatalf("duplicate receipt cookies accepted: %d", duplicate.Code)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("signup receipt may be cached")
	}
}

func TestMalformedDuplicateReceiptCannotFallBackToTheValidCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := &Handlers{origin: "https://helivanta.app"}
	router.GET("/v1/auth/signup", h.Status)
	cap, err := newCapability()
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/auth/signup", nil)
	request.Header.Set("Cookie", cookieName+"="+cap.id.String()+"."+cap.token+"; "+cookieName+"=\"")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 400 {
		t.Fatalf("malformed duplicate receipt accepted: %d", response.Code)
	}
}

// #1763: the identity owner's 422 (password refused by policy) must reach the
// form as 422 password_policy, not be folded into the 400 "link expired"
// refusal, because the link is still usable and the form must say so.
func TestVerifyEmailPassesAPolicyRefusalThroughAs422(t *testing.T) {
	gin.SetMode(gin.TestMode)
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/customer-signups/verify-email" {
			t.Errorf("unexpected owner call %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"status":422,"detail":"password refused by policy"}`))
	}))
	defer owner.Close()
	tokenFile := t.TempDir() + "/token"
	if err := os.WriteFile(tokenFile, []byte("synthetic-workload"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := &Handlers{origin: "https://helivanta.app", base: owner.URL, tokenFile: tokenFile, client: owner.Client()}
	router := gin.New()
	router.POST("/v1/auth/signup/verify-email", h.VerifyEmail)
	request := httptest.NewRequest(http.MethodPost, "/v1/auth/signup/verify-email", strings.NewReader(`{"userId":"owner-synthetic","code":"proof","password":"short"}`))
	request.Header.Set("Origin", "https://helivanta.app")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), `"password_policy"`) {
		t.Fatalf("policy refusal became %d %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "refused by policy") {
		t.Fatal("identity owner's wording leaked to the browser")
	}
}
