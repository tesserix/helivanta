package customeronboarding

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewSignupReceiptRotatesOnlyBrowserStateWithoutCallingTheOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handlers{origin: "https://helivanta.app"}
	r := gin.New()
	r.POST("/v1/auth/signup/new", h.NewReceipt)
	old, err := newCapability()
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/signup/new", strings.NewReader(`{}`))
	req.Header.Set("Origin", h.origin)
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: cookieName, Value: old.id.String() + "." + old.token})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("fresh receipt refused: %d", w.Code)
	}
	var body struct {
		State string `json:"state"`
	}
	if json.Unmarshal(w.Body.Bytes(), &body) != nil || body.State != "not_started" {
		t.Fatal("fresh receipt fabricated provisioning")
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure || !cookies[0].HttpOnly || cookies[0].Path != "/" || cookies[0].SameSite != http.SameSiteLaxMode {
		t.Fatal("fresh receipt cookie is unsafe")
	}
	nextReq := httptest.NewRequest(http.MethodGet, "/", nil)
	nextReq.AddCookie(cookies[0])
	next, exists, err := readCapability(nextReq)
	if err != nil || !exists || next.id == old.id || next.token == old.token {
		t.Fatal("new signup reused the old receipt")
	}
}
func TestNewSignupReceiptRejectsCrossSiteAndTenantCommands(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handlers{origin: "https://helivanta.app"}
	r := gin.New()
	r.POST("/v1/auth/signup/new", h.NewReceipt)
	for _, tc := range []struct {
		origin, body string
		status       int
	}{{"https://foreign.example", `{}`, 403}, {h.origin, `{"tenantId":"foreign"}`, 400}} {
		req := httptest.NewRequest(http.MethodPost, "/v1/auth/signup/new", strings.NewReader(tc.body))
		req.Header.Set("Origin", tc.origin)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != tc.status || len(w.Result().Cookies()) != 0 {
			t.Fatal("untrusted receipt rotation accepted")
		}
	}
}
