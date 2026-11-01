package loginclient

import (
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

const verifiedMembershipHuman = `{"user":{"userId":"synthetic-human","state":"USER_STATE_ACTIVE","human":{"email":{"email":"private-contact@example.invalid","isVerified":true}}}}`

func TestMembershipEligibleRequiresOneMatchingVerifiedHumanProof(t *testing.T) {
	for _, tc := range []struct {
		name, body            string
		status                int
		eligible, unavailable bool
	}{
		{name: "verified_human", body: verifiedMembershipHuman, eligible: true},
		{name: "unverified_human", body: strings.Replace(verifiedMembershipHuman, `"isVerified":true`, `"isVerified":false`, 1)},
		{name: "elided_false", body: strings.Replace(verifiedMembershipHuman, `,"isVerified":true`, ``, 1)},
		{name: "inactive", body: strings.Replace(verifiedMembershipHuman, "USER_STATE_ACTIVE", "USER_STATE_INACTIVE", 1)},
		{name: "locked", body: strings.Replace(verifiedMembershipHuman, "USER_STATE_ACTIVE", "USER_STATE_LOCKED", 1)},
		{name: "suspended", body: strings.Replace(verifiedMembershipHuman, "USER_STATE_ACTIVE", "USER_STATE_SUSPEND", 1)},
		{name: "initial", body: strings.Replace(verifiedMembershipHuman, "USER_STATE_ACTIVE", "USER_STATE_INITIAL", 1)},
		{name: "deleted", body: strings.Replace(verifiedMembershipHuman, "USER_STATE_ACTIVE", "USER_STATE_DELETED", 1)},
		{name: "machine", body: `{"user":{"userId":"synthetic-human","state":"USER_STATE_ACTIVE","machine":{"name":"synthetic service"}}}`},
		{name: "missing_identity", status: http.StatusNotFound, body: `{}`},
		{name: "provider_outage", status: http.StatusServiceUnavailable, body: `{"message":"private-contact@example.invalid"}`, unavailable: true},
		{name: "provider_refusal", status: http.StatusForbidden, body: `{"message":"private-contact@example.invalid"}`, unavailable: true},
		{name: "wrong_subject", body: strings.Replace(verifiedMembershipHuman, "synthetic-human", "another-human", 1), unavailable: true},
		{name: "missing_subject", body: strings.Replace(verifiedMembershipHuman, `"userId":"synthetic-human",`, ``, 1), unavailable: true},
		{name: "unknown_state", body: strings.Replace(verifiedMembershipHuman, "USER_STATE_ACTIVE", "USER_STATE_FUTURE", 1), unavailable: true},
		{name: "missing_state", body: strings.Replace(verifiedMembershipHuman, `"state":"USER_STATE_ACTIVE",`, ``, 1), unavailable: true},
		{name: "duplicate_subject", body: strings.Replace(verifiedMembershipHuman, `"userId":`, `"userId":"another-human","userId":`, 1), unavailable: true},
		{name: "duplicate_user", body: strings.Replace(verifiedMembershipHuman, `{"user":`, `{"user":{},"user":`, 1), unavailable: true},
		{name: "conflicting_verification", body: strings.Replace(verifiedMembershipHuman, `"isVerified":true`, `"isVerified":false,"isVerified":true`, 1), unavailable: true},
		{name: "recased_verification", body: strings.Replace(verifiedMembershipHuman, `"isVerified":true`, `"IsVerified":false,"isVerified":true`, 1), unavailable: true},
		{name: "renamed_verification", body: strings.Replace(verifiedMembershipHuman, "isVerified", "is_verified", 1), unavailable: true},
		{name: "nonboolean_verification", body: strings.Replace(verifiedMembershipHuman, `"isVerified":true`, `"isVerified":"true"`, 1), unavailable: true},
		{name: "null_verification", body: strings.Replace(verifiedMembershipHuman, `"isVerified":true`, `"isVerified":null`, 1), unavailable: true},
		{name: "missing_email", body: strings.Replace(verifiedMembershipHuman, `"email":"private-contact@example.invalid",`, ``, 1), unavailable: true},
		{name: "empty_verified_email", body: strings.Replace(verifiedMembershipHuman, "private-contact@example.invalid", "", 1), unavailable: true},
		{name: "malformed_verified_email", body: strings.Replace(verifiedMembershipHuman, "private-contact@example.invalid", "invalid address", 1), unavailable: true},
		{name: "display_name_in_email", body: strings.Replace(verifiedMembershipHuman, "private-contact@example.invalid", "Staff <private-contact@example.invalid>", 1), unavailable: true},
		{name: "ambiguous_kind", body: strings.Replace(verifiedMembershipHuman, `"human":`, `"machine":{},"human":`, 1), unavailable: true},
		{name: "unknown_kind", body: `{"user":{"userId":"synthetic-human","state":"USER_STATE_ACTIVE"}}`, unavailable: true},
		{name: "null_human", body: `{"user":{"userId":"synthetic-human","state":"USER_STATE_ACTIVE","human":null}}`, unavailable: true},
		{name: "empty_response", body: `{}`, unavailable: true},
		{name: "invalid_json", body: `{`, unavailable: true},
		{name: "second_document", body: verifiedMembershipHuman + `{}`, unavailable: true},
		{name: "oversized_valid_prefix", body: verifiedMembershipHuman + strings.Repeat(" ", maxSuccessBodyBytes), unavailable: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				require.Equal(t, http.MethodGet, r.Method)
				require.Equal(t, "/v2/users/synthetic-human", r.URL.Path)
				require.Equal(t, "Bearer test-pat", r.Header.Get("Authorization"))
				if tc.status != 0 {
					w.WriteHeader(tc.status)
				}
				_, _ = w.Write([]byte(tc.body))
			})
			eligible, err := client.MembershipEligible(t.Context(), "synthetic-human")
			require.Equal(t, tc.eligible, eligible)
			require.EqualValues(t, 1, calls.Load(), "identity state and contact must share one observation")
			if tc.unavailable {
				require.ErrorIs(t, err, ErrUnavailable)
				require.NotContains(t, err.Error(), "private-contact@example.invalid")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestMembershipEligibilityMissingClientAndCancelledReadFailClosed(t *testing.T) {
	var missing *Client
	eligible, err := missing.MembershipEligible(t.Context(), "synthetic-human")
	require.False(t, eligible)
	require.ErrorIs(t, err, ErrUnavailable)
	var calls atomic.Int32
	client := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(verifiedMembershipHuman))
	})
	eligible, err = client.MembershipEligible(t.Context(), "")
	require.False(t, eligible)
	require.ErrorIs(t, err, ErrUnavailable)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	eligible, err = client.MembershipEligible(ctx, "synthetic-human")
	require.False(t, eligible)
	require.ErrorIs(t, err, ErrUnavailable)
	require.Zero(t, calls.Load())
}
