package customeronboarding

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/tesserix/helivanta/internal/modules/iam/loginclient"
)

func TestIdentityProjectionNeverForwardsWorkloadCredentialToRedirect(t *testing.T) {
	var redirects, disclosures atomic.Int32
	sink := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { disclosures.Add(1) }))
	defer sink.Close()
	owner := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirects.Add(1)
		if r.Header.Get("Authorization") != "Bearer synthetic-workload" {
			t.Error("initial request lost workload authentication")
		}
		http.Redirect(w, r, sink.URL, http.StatusTemporaryRedirect)
	}))
	defer owner.Close()
	h, err := New("http://onboarding-api.onboarding.svc.cluster.local:8080", "/var/run/customer-onboarding/token", "https://helivanta.app")
	if err != nil {
		t.Fatal(err)
	}
	h.base = owner.URL
	h.tokenFile = filepath.Join(t.TempDir(), "token")
	if err = os.WriteFile(h.tokenFile, []byte("synthetic-workload"), 0600); err != nil {
		t.Fatal(err)
	}
	view, err := h.TenantIdentitySnapshot(t.Context(), "11111111-1111-4111-8111-111111111111", "synthetic-staff", loginclient.IdentityReadStatus)
	if !errors.Is(err, loginclient.ErrUnavailable) || view.State != 0 || view.Profile != nil || redirects.Load() != 1 || disclosures.Load() != 0 {
		t.Fatalf("redirect escaped identity trust boundary: state=%v owner=%d disclosure=%d err=%v", view.State, redirects.Load(), disclosures.Load(), err)
	}
}
