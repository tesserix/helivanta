package customeronboarding

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/ratelimit"
	"github.com/tesserix/onboarding-service/api/customercontract"
	"sync/atomic"
)

func TestPrivateSigninRefusalPreservesCurrentReceipt(t *testing.T) {
	cap, err := newCapability()
	require.NoError(t, err)
	private := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"temporarily_unavailable"}`))
	}))
	t.Cleanup(private.Close)
	file := t.TempDir() + "/token"
	require.NoError(t, os.WriteFile(file, []byte("synthetic-workload"), 0600))
	h := &Handlers{origin: "https://helivanta.app", base: private.URL, tokenFile: file, client: private.Client()}
	r := gin.New()
	r.POST("/v1/auth/signup/sign-in", h.Signin(SigninDeps{Tokens: signinVerifier{principal: authn.Principal{Subject: "native-owner", AuthTime: time.Now()}}, Revocations: signinRevocations{}}))
	request := httptest.NewRequest(http.MethodPost, "/v1/auth/signup/sign-in", strings.NewReader(`{"idToken":"native-id-token"}`))
	request.Header.Set("Origin", "https://helivanta.app")
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: cookieName, Value: cap.id.String() + "." + cap.token})
	result := httptest.NewRecorder()
	r.ServeHTTP(result, request)
	require.Equal(t, http.StatusServiceUnavailable, result.Code)
	require.Empty(t, result.Result().Cookies())
}

type signinVerifier struct {
	principal authn.Principal
	err       error
}

func (v signinVerifier) Verify(context.Context, string) (authn.Principal, error) {
	return v.principal, v.err
}

type signinRevocations struct {
	at  time.Time
	err error
}

func (r signinRevocations) RevokedAfter(context.Context, string) (time.Time, error) {
	return r.at, r.err
}

func TestExistingSigninRequiresFreshNativeProofAndRotatesOnlyAfterPrivateAcceptance(t *testing.T) {
	for _, tc := range []struct {
		name                                                                                 string
		age                                                                                  time.Duration
		invalid, revoked, revocationUnavailable, foreignOrigin, foreignField, missingReceipt bool
		missingVerifier, missingRevocations, limited                                         bool
		want                                                                                 int
	}{
		{name: "matching identity", want: 200},
		{name: "missing verifier", missingVerifier: true, want: 503},
		{name: "missing revocation source", missingRevocations: true, want: 503},
		{name: "rate limited", limited: true, want: 429},
		{name: "invalid token", invalid: true, want: 401},
		{name: "stale authentication", age: -time.Hour, want: 401},
		{name: "future authentication", age: time.Hour, want: 401},
		{name: "revoked credential", revoked: true, want: 401},
		{name: "revocation unavailable", revocationUnavailable: true, want: 503},
		{name: "cross origin", foreignOrigin: true, want: 403},
		{name: "subject injection", foreignField: true, want: 400},
		{name: "missing receipt", missingReceipt: true, want: 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			cap, err := newCapability()
			require.NoError(t, err)
			private := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				assert.Equal(t, "/internal/v1/customer-signups/"+cap.id.String()+"/existing-owner", r.URL.Path)
				assert.Equal(t, "Bearer synthetic-workload", r.Header.Get("Authorization"))
				assert.Equal(t, cap.hash(), r.Header.Get("X-Signup-Receipt-Hash"))
				var proof customercontract.ExistingOwnerInput
				assert.NoError(t, json.NewDecoder(r.Body).Decode(&proof))
				assert.Equal(t, "native-owner", proof.Subject)
				assert.Len(t, proof.NextReceiptHash, 64)
				assert.NotEqual(t, cap.hash(), proof.NextReceiptHash)
				assert.NotContains(t, proof.Subject, "native-id-token")
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"state":"provisioning"}`))
			}))
			t.Cleanup(private.Close)
			file := t.TempDir() + "/token"
			require.NoError(t, os.WriteFile(file, []byte("synthetic-workload"), 0600))
			h := &Handlers{origin: "https://helivanta.app", base: private.URL, tokenFile: file, client: private.Client()}
			verifier := signinVerifier{principal: authn.Principal{Subject: "native-owner", AuthTime: time.Now().Add(tc.age)}}
			if tc.invalid {
				verifier.err = errors.New("invalid signature")
			}
			rev := signinRevocations{}
			if tc.revoked {
				rev.at = time.Now().Add(time.Second)
			}
			if tc.revocationUnavailable {
				rev.err = errors.New("database unavailable")
			}
			gin.SetMode(gin.TestMode)
			r := gin.New()
			deps := SigninDeps{Tokens: verifier, Revocations: rev}
			if tc.missingVerifier {
				deps.Tokens = nil
			}
			if tc.missingRevocations {
				deps.Revocations = nil
			}
			if tc.limited {
				deps.Limiter = signinLimited{}
			}
			r.POST("/v1/auth/signup/sign-in", h.Signin(deps))
			body := `{"idToken":"native-id-token"}`
			if tc.foreignField {
				body = `{"idToken":"native-id-token","subject":"foreign"}`
			}
			request := httptest.NewRequest(http.MethodPost, "/v1/auth/signup/sign-in", strings.NewReader(body))
			origin := "https://helivanta.app"
			if tc.foreignOrigin {
				origin = "https://foreign.test"
			}
			request.Header.Set("Origin", origin)
			request.Header.Set("Content-Type", "application/json")
			if !tc.missingReceipt {
				request.AddCookie(&http.Cookie{Name: cookieName, Value: cap.id.String() + "." + cap.token})
			}
			result := httptest.NewRecorder()
			r.ServeHTTP(result, request)
			require.Equal(t, tc.want, result.Code, result.Body.String())
			if tc.want == 200 {
				require.EqualValues(t, 1, calls.Load())
				require.Len(t, result.Result().Cookies(), 1)
				require.NotEqual(t, cap.id.String()+"."+cap.token, result.Result().Cookies()[0].Value)
			} else {
				require.Zero(t, calls.Load())
				require.Empty(t, result.Result().Cookies())
			}
		})
	}
}

type signinLimited struct{}

func (signinLimited) Allow(string, ratelimit.Rule, time.Time) ratelimit.Decision {
	return ratelimit.Decision{Allowed: false, RetryAfter: time.Minute, Limit: 1}
}
