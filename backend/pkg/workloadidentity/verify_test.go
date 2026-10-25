package workloadidentity_test

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/workloadidentity"
	"k8s.io/client-go/rest"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWorkloadAudienceAndExactAccountAreBothRequired(t *testing.T) {
	for _, tc := range []struct {
		name, user, aud string
		authenticated   bool
		want            bool
	}{{"approved", "system:serviceaccount:onboarding:onboarding-api", "helivanta-provisioning", true, true}, {"foreign", "system:serviceaccount:foreign:onboarding-api", "helivanta-provisioning", true, false},
		{"wrong service account", "system:serviceaccount:onboarding:onboarding-web", "helivanta-provisioning", true, false},
		{"missing audience", "system:serviceaccount:onboarding:onboarding-api", "", true, false}, {"wrong audience", "system:serviceaccount:onboarding:onboarding-api", "other", true, false}, {"revoked", "system:serviceaccount:onboarding:onboarding-api", "helivanta-provisioning", false, false}} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				var v struct {
					Spec struct {
						Token     string   `json:"token"`
						Audiences []string `json:"audiences"`
					} `json:"spec"`
				}
				if e := json.NewDecoder(r.Body).Decode(&v); e != nil {
					t.Fatal(e)
				}
				if v.Spec.Token != "projected" || len(v.Spec.Audiences) != 1 || v.Spec.Audiences[0] != "helivanta-provisioning" {
					t.Error("unscoped review")
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"apiVersion": "authentication.k8s.io/v1", "kind": "TokenReview", "status": map[string]any{"authenticated": tc.authenticated, "audiences": []string{tc.aud}, "user": map[string]any{"username": tc.user, "uid": "uid"}}})
			}))
			defer s.Close()
			cfg := &rest.Config{Host: s.URL, TLSClientConfig: rest.TLSClientConfig{CAData: pemCertificate(s.Certificate().Raw)}}
			verifier, e := workloadidentity.New(cfg, "helivanta-provisioning", "system:serviceaccount:onboarding:onboarding-api")
			if e != nil {
				t.Fatal(e)
			}
			e = verifier.Verify(context.Background(), "Bearer projected")
			if (e == nil) != tc.want {
				t.Fatalf("decision=%v want permitted=%v", e, tc.want)
			}
		})
	}
}

func pemCertificate(der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func TestWorkloadVerifierSeparatesDependencyFailureFromRejectedCredentials(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) }))
	t.Cleanup(api.Close)
	v, err := workloadidentity.New(&rest.Config{Host: api.URL}, "helivanta-mediconnect-claim", "system:serviceaccount:helivanta:patientmaster-api")
	require.NoError(t, err)
	require.Equal(t, "system:serviceaccount:helivanta:patientmaster-api", v.Subject())
	require.ErrorIs(t, v.Verify(t.Context(), "Bearer synthetic-projected-token"), workloadidentity.ErrUnavailable)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, v.Verify(ctx, "Bearer synthetic-projected-token"), workloadidentity.ErrUnavailable)
	for _, credential := range []string{"", "Bearer ", "Basic invalid", "Bearer whitespace token", "Bearer " + strings.Repeat("x", 8200)} {
		require.ErrorIs(t, v.Verify(t.Context(), credential), workloadidentity.ErrRejected)
	}
}
