package loginclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPasswordReproofRotatesTheSessionAndHonorsTheNativeSecondBoundary(t *testing.T) {
	changed := time.Date(2026, 11, 18, 10, 11, 12, 900_000_000, time.UTC)
	for _, tc := range []struct {
		name   string
		first  time.Time
		checks int32
	}{
		{"already_fresh", changed.Add(time.Second), 1},
		{"fractional_boundary", changed.Add(50 * time.Millisecond), 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var checks atomic.Int32
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/v2/users/u1":
					_, _ = w.Write([]byte(`{"user":{"human":{"passwordChanged":"` + changed.Format(time.RFC3339Nano) + `"}}}`))
				case r.Method == http.MethodGet && r.URL.Path == "/v2/sessions/s":
					at := tc.first
					if checks.Load() > 1 {
						at = changed.Add(time.Second)
					}
					_, _ = w.Write([]byte(`{"session":{"id":"s","factors":{"user":{"id":"u1","organizationId":"o1"},"password":{"verifiedAt":"` + at.Format(time.RFC3339Nano) + `"}}}}`))
				case r.Method == http.MethodPatch && r.URL.Path == "/v2/sessions/s":
					var body struct {
						SessionToken string `json:"sessionToken"`
						Checks       struct {
							Password struct {
								Password string `json:"password"`
							} `json:"password"`
						} `json:"checks"`
					}
					require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
					require.Equal(t, "synthetic-new-password", body.Checks.Password.Password)
					n := checks.Add(1)
					want := "old-token"
					if n > 1 {
						want = fmt.Sprintf("rotated-%d", n-1)
					}
					require.Equal(t, want, body.SessionToken)
					_, _ = fmt.Fprintf(w, `{"sessionToken":"rotated-%d"}`, n)
				default:
					t.Errorf("unexpected native reproof request: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			})
			out, err := client.ReprovePasswordAfterChange(t.Context(), Session{ID: "s", Token: "old-token"}, "synthetic-new-password")
			require.NoError(t, err)
			require.Equal(t, tc.checks, checks.Load())
			require.Equal(t, Session{ID: "s", Token: fmt.Sprintf("rotated-%d", tc.checks)}, out)
		})
	}
}

func TestPasswordReproofDoesNotRetryAfterCancellation(t *testing.T) {
	changed := time.Date(2026, 11, 18, 10, 11, 12, 100_000_000, time.UTC)
	var checks atomic.Int32
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v2/users/u1":
			_, _ = w.Write([]byte(`{"user":{"human":{"passwordChanged":"` + changed.Format(time.RFC3339Nano) + `"}}}`))
		case r.Method == http.MethodGet:
			_, _ = w.Write([]byte(`{"session":{"id":"s","factors":{"user":{"id":"u1","organizationId":"o1"},"password":{"verifiedAt":"` + changed.Add(time.Millisecond).Format(time.RFC3339Nano) + `"}}}}`))
			if checks.Load() > 0 {
				cancel()
			}
		case r.Method == http.MethodPatch:
			checks.Add(1)
			_, _ = w.Write([]byte(`{"sessionToken":"rotated"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	out, err := client.ReprovePasswordAfterChange(ctx, Session{ID: "s", Token: "old-token"}, "synthetic-new-password")
	require.ErrorIs(t, err, ErrUnavailable)
	require.Equal(t, int32(1), checks.Load())
	require.Empty(t, out)
}

func TestPasswordReproofRetainsFailureInsteadOfInventingANewAuthentication(t *testing.T) {
	changed := time.Date(2026, 11, 18, 10, 11, 12, 900_000_000, time.UTC)
	for _, tc := range []struct {
		name                                string
		required, missingDate, missingToken bool
		patchStatus                         int
		proof                               time.Time
		checks                              int32
		want                                error
	}{
		{name: "still_required", required: true, checks: 0, want: ErrPasswordChangeUnconfirmed},
		{name: "unknown_native_change", missingDate: true, checks: 0, want: ErrUnavailable},
		{name: "missing_rotated_token", missingToken: true, checks: 1, want: ErrUnavailable},
		{name: "native_credential_refused", patchStatus: 400, checks: 1, want: ErrBadCredentials},
		{name: "native_authentication_unavailable", patchStatus: 401, checks: 1, want: ErrUnavailable},
		{name: "native_outage", patchStatus: 503, checks: 1, want: ErrUnavailable},
		{name: "old_primary_proof", proof: changed.Add(-time.Second), checks: 1, want: ErrUnavailable},
		{name: "clock_did_not_advance", proof: changed.Add(50 * time.Millisecond), checks: 2, want: ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var checks atomic.Int32
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == http.MethodGet && r.URL.Path == "/v2/users/u1":
					human := map[string]any{"passwordChangeRequired": tc.required}
					if !tc.missingDate {
						human["passwordChanged"] = changed.Format(time.RFC3339Nano)
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"user": map[string]any{"human": human}})
				case r.Method == http.MethodGet && r.URL.Path == "/v2/sessions/s":
					proof := tc.proof
					if proof.IsZero() {
						proof = changed.Add(time.Second)
					}
					_, _ = w.Write([]byte(`{"session":{"id":"s","factors":{"user":{"id":"u1","organizationId":"o1"},"password":{"verifiedAt":"` + proof.Format(time.RFC3339Nano) + `"}}}}`))
				case r.Method == http.MethodPatch && r.URL.Path == "/v2/sessions/s":
					checks.Add(1)
					if tc.patchStatus != 0 {
						w.WriteHeader(tc.patchStatus)
						if tc.patchStatus == http.StatusBadRequest {
							_, _ = w.Write([]byte(`{"code":3,"message":"invalid credential","details":[{"id":"COMMAND-3M0fs"}]}`))
						} else {
							_, _ = w.Write([]byte(`{}`))
						}
						return
					}
					if tc.missingToken {
						_, _ = w.Write([]byte(`{}`))
						return
					}
					_, _ = w.Write([]byte(`{"sessionToken":"rotated"}`))
				default:
					t.Errorf("unexpected native recovery call: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			})
			out, err := client.ReprovePasswordAfterChange(t.Context(), Session{ID: "s", Token: "old"}, "synthetic-new-password")
			require.ErrorIs(t, err, tc.want)
			require.Empty(t, out)
			require.Equal(t, tc.checks, checks.Load())
		})
	}
}
