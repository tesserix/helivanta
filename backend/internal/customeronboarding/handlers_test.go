package customeronboarding

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
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
