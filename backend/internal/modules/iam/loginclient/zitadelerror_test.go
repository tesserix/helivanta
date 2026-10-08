package loginclient

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestBadRequestIsClassifiedByZitadelErrorID is #901's core claim: a 400 is
// classified by the id Zitadel sent, not by its status. Each id is read from
// zitadel's internal/command at 8a54a2a (spec D2). The response is the
// gRPC-gateway shape the spike observed live, with the id in details[].
func TestBadRequestIsClassifiedByZitadelErrorID(t *testing.T) {
	cases := []struct {
		id   string
		want error
	}{
		{"COMMAND-3M0fs", ErrBadCredentials},
		{"EVENT-8isk2", ErrBadCredentials},
		{"TOTP-Auw0a", ErrBadCredentials},
		{"COMMAND-JLK35", ErrAccountLocked},
		{"COMMAND-SFA3t", ErrAccountLocked},
		{"COMMAND-SF3fg", ErrAccountLocked},
		{"COMMAND-3nJ4t", ErrPasswordNotSet},
		{"COMMAND-3n77z", ErrUserNotFound},
		{"COMMAND-SOMETHING-NEW", ErrRejected},
		{"", ErrRejected},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"code":3,"message":"x","details":[{"id":"` + tc.id + `","failedAttempts":3}]}`))
			})
			_, err := c.CreatePasswordSession(context.Background(), "u", "p")

			require.ErrorIs(t, err, tc.want)
			for _, other := range []error{ErrBadCredentials, ErrAccountLocked, ErrPasswordNotSet, ErrUserNotFound, ErrRejected} {
				if other != tc.want {
					require.NotErrorIs(t, err, other, "a 400 must map to exactly one sentinel")
				}
			}
			require.Equal(t, tc.id, ZitadelErrorID(err))
			require.Equal(t, http.StatusBadRequest, ZitadelStatus(err))
			require.True(t, IsCredentialRefusal(err), "every 400 from a credential check is a credential-class refusal")
			require.NotContains(t, err.Error(), "failedAttempts")
		})
	}
}

// TestUnknownBadRequestIsNotBadCredentials pins the half of #901 that removed
// a false claim: an earlier version mapped EVERY 400 to ErrBadCredentials, so
// a locked account or an unrecognised refusal logged as a wrong password.
func TestUnknownBadRequestIsNotBadCredentials(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`not json at all`))
	})
	_, err := c.CreatePasswordSession(context.Background(), "u", "p")
	require.ErrorIs(t, err, ErrRejected)
	require.NotErrorIs(t, err, ErrBadCredentials)
	require.Equal(t, "", ZitadelErrorID(err))
}

// TestZitadelErrorIDFallsBackToTheMessageSuffix covers a response whose
// details do not carry the id as a plain field (a connect-protocol answer
// encodes details as base64). Zitadel formats every message as
// "<key> (<id>)", so the id — and therefore the classification — survives.
func TestZitadelErrorIDFallsBackToTheMessageSuffix(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":"failed_precondition","message":"Errors.User.Locked (COMMAND-JLK35)","details":[{"type":"zitadel.v1.ErrorDetail","value":"Cg1DT01NQU5ELUpMSzM1"}]}`))
	})
	_, err := c.CreatePasswordSession(context.Background(), "u", "p")
	require.ErrorIs(t, err, ErrAccountLocked)
	require.Equal(t, "COMMAND-JLK35", ZitadelErrorID(err))
}

// TestNonCredentialStatusesAreNotCredentialRefusals: a 5xx or a 401/403 (the
// login client PAT itself refused) is an outage, never a credential refusal —
// it must not be equalised into "email or password is incorrect".
func TestNonCredentialStatusesAreNotCredentialRefusals(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusInternalServerError} {
		c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"message":"x (AUTH-5mWD2)"}`))
		})
		_, err := c.CreatePasswordSession(context.Background(), "u", "p")
		require.ErrorIs(t, err, ErrUnavailable, "status %d", status)
		require.False(t, IsCredentialRefusal(err), "status %d", status)
		require.Equal(t, status, ZitadelStatus(err))
	}
}

func TestFailureOutcomeNamesEachRefusal(t *testing.T) {
	cases := map[error]string{
		ErrBadCredentials: "bad_credentials",
		ErrUserNotFound:   "user_not_found",
		ErrAccountLocked:  "account_locked",
		ErrPasswordNotSet: "password_not_set",
		ErrRejected:       "rejected",
	}
	for sentinel, want := range cases {
		wrapped := &ZitadelError{Status: 400, Kind: sentinel}
		require.Equal(t, want, FailureOutcome(wrapped))
	}
	require.Equal(t, "unknown", FailureOutcome(errors.New("other")))
	require.False(t, strings.Contains(FailureOutcome(ErrRejected), "credential"))
}
