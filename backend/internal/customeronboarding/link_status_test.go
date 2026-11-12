package customeronboarding

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSignupLinkStatusNarrowsPrivateReceiptToExpectedOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cap, err := newCapability()
	if err != nil {
		t.Fatal(err)
	}
	private := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/v1/customer-signups/"+cap.id.String()+"/users/owner-synthetic" {
			t.Errorf("status did not constrain the owner: %s", r.URL.Path)
			w.WriteHeader(404)
			return
		}
		if r.Header.Get("X-Signup-Receipt-Hash") != cap.hash() {
			t.Error("missing receipt scope")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"state":"password_action_required"}`))
	}))
	t.Cleanup(private.Close)
	file := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(file, []byte("synthetic-workload"), 0600); err != nil {
		t.Fatal(err)
	}
	h := &Handlers{origin: "https://helivanta.app", base: private.URL, tokenFile: file, client: private.Client()}
	router := gin.New()
	router.GET("/v1/auth/signup", h.Status)
	req := httptest.NewRequest(http.MethodGet, "/v1/auth/signup?userId=owner-synthetic", nil)
	req.AddCookie(&http.Cookie{Name: cookieName, Value: cap.id.String() + "." + cap.token})
	result := httptest.NewRecorder()
	router.ServeHTTP(result, req)
	if result.Code != 200 || result.Body.String() != `{"state":"password_action_required"}` {
		t.Fatalf("owner status unavailable: %d %s", result.Code, result.Body.String())
	}
}

func TestSignupLinkStatusRejectsAmbiguousAndInvalidOwners(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := &Handlers{origin: "https://helivanta.app", base: "http://must-not-contact.invalid", tokenFile: "must-not-read"}
	router := gin.New()
	router.GET("/v1/auth/signup", h.Status)
	for _, query := range []string{"userId=", "userId=owner-a&userId=owner-b", "userId=..%2Fforeign"} {
		result := httptest.NewRecorder()
		router.ServeHTTP(result, httptest.NewRequest(http.MethodGet, "/v1/auth/signup?"+query, nil))
		if result.Code != 400 {
			t.Fatalf("invalid owner accepted: %q %d", query, result.Code)
		}
	}
}
