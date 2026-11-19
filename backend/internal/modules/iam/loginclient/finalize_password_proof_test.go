package loginclient

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestFinalizerRefusesAPasswordProofClearedByNativeRecovery(t *testing.T) {
	finalized := false
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/v2/sessions/1" {
			_, _ = w.Write([]byte(`{"session":{"id":"1","factors":{"user":{"id":"user","organizationId":"org"},"totp":{"verifiedAt":"2026-11-18T10:11:12Z"}}}}`))
			return
		}
		finalized = true
		_, _ = w.Write([]byte(`{"callbackUrl":"https://helivanta.app/callback?code=synthetic"}`))
	})
	callback, err := client.finalize(t.Context(), "V2_1", Session{ID: "1", Token: "synthetic"}, sufficient{})
	require.ErrorIs(t, err, ErrAuthRequestInvalid)
	require.Empty(t, callback)
	require.False(t, finalized, "a fresh TOTP factor must not replace the cleared primary password proof")
}

func TestPasswordProofRejectsMalformedOrAmbiguousNativeEvidence(t *testing.T) {
	const good = `{"session":{"id":"1","factors":{"password":{"verifiedAt":"2026-11-18T10:11:12.123Z"}}}}`
	for _, tc := range []struct {
		name, body string
		want       error
	}{
		{"valid", good, nil},
		{"another_session", strings.Replace(good, `"id":"1"`, `"id":"2"`, 1), ErrUnavailable},
		{"missing_primary", `{"session":{"id":"1","factors":{}}}`, ErrAuthRequestInvalid},
		{"null_primary", `{"session":{"id":"1","factors":{"password":null}}}`, ErrAuthRequestInvalid},
		{"empty_primary", `{"session":{"id":"1","factors":{"password":{}}}}`, ErrUnavailable},
		{"empty_date", strings.Replace(good, "2026-11-18T10:11:12.123Z", "", 1), ErrUnavailable},
		{"invalid_date", strings.Replace(good, "2026-11-18T10:11:12.123Z", "not-a-date", 1), ErrUnavailable},
		{"zero_date", strings.Replace(good, "2026-11-18T10:11:12.123Z", "0001-01-01T00:00:00Z", 1), ErrUnavailable},
		{"duplicate_date", strings.Replace(good, `"verifiedAt":`, `"verifiedAt":"2026-11-18T10:11:11Z","verifiedAt":`, 1), ErrUnavailable},
		{"renamed_date", strings.Replace(good, `"verifiedAt"`, `"verified_at"`, 1), ErrUnavailable},
		{"recased_factor", strings.Replace(good, `"password"`, `"Password"`, 1), ErrUnavailable},
		{"duplicate_factor", strings.Replace(good, `"password":`, `"password":{},"password":`, 1), ErrUnavailable},
		{"duplicate_session", strings.Replace(good, `"session":`, `"session":{},"session":`, 1), ErrUnavailable},
		{"null_session", `{"session":null}`, ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodGet, r.Method)
				require.Equal(t, "/v2/sessions/1", r.URL.Path)
				_, _ = w.Write([]byte(tc.body))
			})
			proof, err := client.passwordProof(t.Context(), Session{ID: "1", Token: "synthetic"})
			if tc.want != nil {
				require.ErrorIs(t, err, tc.want)
				require.True(t, proof.IsZero())
				return
			}
			require.NoError(t, err)
			require.Equal(t, "2026-11-18T10:11:12.123Z", proof.UTC().Format(time.RFC3339Nano))
		})
	}
}
