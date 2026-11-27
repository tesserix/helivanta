package loginclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	creationOptions  = `{"publicKey":{"attestation":"none","challenge":"abc","rp":{"id":"helivanta.localhost","name":"ZITADEL"}}}`
	assertionOptions = `{"publicKey":{"challenge":"xyz","rpId":"helivanta.localhost"}}`
)

// webauthnZitadel serves one fixed answer and records the last request body.
func webauthnZitadel(t *testing.T, status int, answer string, gotBody *string) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v2/sessions/s":
			_, _ = w.Write([]byte(`{"session":{"factors":{"user":{"id":"u1","organizationId":"o1"},"password":{"verifiedAt":"2026-11-27T00:00:00Z"},"webAuthN":{"verifiedAt":"2026-11-27T00:00:01Z"}}}}`))
		default:
			b, _ := io.ReadAll(r.Body)
			*gotBody = string(b)
			w.WriteHeader(status)
			_, _ = w.Write([]byte(answer))
		}
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL, "synthetic", srv.Client())
}

func TestRegisterSecurityKeySendsConfiguredDomainOnly(t *testing.T) {
	var got string
	c := webauthnZitadel(t, 200, `{"details":{},"u2fId":"401","publicKeyCredentialCreationOptions":`+creationOptions+`}`, &got)
	reg, err := c.RegisterSecurityKey(context.Background(), "s", "helivanta.localhost")
	require.NoError(t, err)
	require.Equal(t, `{"domain":"helivanta.localhost"}`, got)
	require.Equal(t, "401", reg.ID)
	require.JSONEq(t, creationOptions, string(reg.CreationOptions))
}

func TestRegisterSecurityKeyMissingOptionsIsUnavailable(t *testing.T) {
	for _, answer := range []string{`{"u2fId":"401"}`, `{"publicKeyCredentialCreationOptions":` + creationOptions + `}`} {
		var got string
		c := webauthnZitadel(t, 200, answer, &got)
		_, err := c.RegisterSecurityKey(context.Background(), "s", "helivanta.localhost")
		require.ErrorIs(t, err, ErrUnavailable)
	}
}

func TestVerifySecurityKeyWrongCredentialIsCredentialRefusal(t *testing.T) {
	var got string
	c := webauthnZitadel(t, 400, `{"code":3,"message":"bad (COMMAND-x)"}`, &got)
	err := c.VerifySecurityKey(context.Background(), "s", "401", json.RawMessage(`{"id":"cred"}`), "Security key")
	require.True(t, IsCredentialRefusal(err), "%v", err)
	require.Equal(t, `{"publicKeyCredential":{"id":"cred"},"tokenName":"Security key"}`, got)

	c = webauthnZitadel(t, 200, `{"details":{}}`, &got)
	require.NoError(t, c.VerifySecurityKey(context.Background(), "s", "401", json.RawMessage(`{"id":"cred"}`), "k"))
}

func TestChallengeAndCheckSecurityKey(t *testing.T) {
	var got string
	c := webauthnZitadel(t, 200, `{"sessionToken":"t2","challenges":{"webAuthN":{"publicKeyCredentialRequestOptions":`+assertionOptions+`}}}`, &got)
	s, opts, err := c.ChallengeSecurityKey(context.Background(), Session{ID: "s", Token: "t1"}, "helivanta.localhost")
	require.NoError(t, err)
	require.Equal(t, `{"challenges":{"webAuthN":{"domain":"helivanta.localhost","userVerificationRequirement":"USER_VERIFICATION_REQUIREMENT_DISCOURAGED"}},"sessionToken":"t1"}`, got)
	require.Equal(t, Session{ID: "s", Token: "t2"}, s)
	require.JSONEq(t, assertionOptions, string(opts))

	c = webauthnZitadel(t, 200, `{"sessionToken":"t3"}`, &got)
	s, err = c.CheckSecurityKey(context.Background(), Session{ID: "s", Token: "t2"}, json.RawMessage(`{"id":"a"}`))
	require.NoError(t, err)
	require.Equal(t, `{"checks":{"webAuthN":{"credentialAssertionData":{"id":"a"}}},"sessionToken":"t2"}`, got)
	require.Equal(t, Session{ID: "s", Token: "t3"}, s)

	c = webauthnZitadel(t, 400, `{"message":"bad"}`, &got)
	_, err = c.CheckSecurityKey(context.Background(), Session{ID: "s", Token: "t2"}, json.RawMessage(`{}`))
	require.True(t, IsCredentialRefusal(err), "%v", err)

	c = webauthnZitadel(t, 200, `{"sessionToken":"t2","challenges":{"webAuthN":{}}}`, &got)
	_, _, err = c.ChallengeSecurityKey(context.Background(), Session{ID: "s", Token: "t1"}, "helivanta.localhost")
	require.ErrorIs(t, err, ErrUnavailable)
}

// A 200 without a rotated token is not a verified session: the empty token
// would be stored and every later call on it refused, so it fails early.
func TestCheckSecurityKeyMissingTokenIsUnavailable(t *testing.T) {
	var got string
	c := webauthnZitadel(t, 200, `{}`, &got)
	_, err := c.CheckSecurityKey(context.Background(), Session{ID: "s", Token: "t2"}, json.RawMessage(`{"id":"a"}`))
	require.ErrorIs(t, err, ErrUnavailable)
	require.False(t, IsCredentialRefusal(err), "a malformed answer must not spend a guess")
}

func TestSessionFactorsReadsWebAuthN(t *testing.T) {
	var got string
	c := webauthnZitadel(t, 200, `{}`, &got)
	f, err := c.SessionFactors(context.Background(), "s")
	require.NoError(t, err)
	require.Equal(t, Factors{Password: true, WebAuthN: true}, f)
}

func TestSufficiencyNeedsPasswordWithWebAuthN(t *testing.T) {
	require.True(t, Factors{Password: true, WebAuthN: true}.SecondFactorProven())
	require.False(t, Factors{WebAuthN: true}.SecondFactorProven())
	require.False(t, Factors{Password: true}.SecondFactorProven())
	require.True(t, Factors{Password: true, TOTP: true}.SecondFactorProven())
}

func TestSessionUserIDReadsTheSessionsUser(t *testing.T) {
	var got string
	c := webauthnZitadel(t, 200, `{}`, &got)
	id, err := c.SessionUserID(context.Background(), "s")
	require.NoError(t, err)
	require.Equal(t, "u1", id)

	_, err = c.SessionUserID(context.Background(), "unknown")
	require.Error(t, err, "a session Zitadel cannot describe must not yield a subject")
}

func TestReadySecurityKeyIDsReturnsOnlyReadyKeys(t *testing.T) {
	answer := `{"result":[{"state":"AUTH_FACTOR_STATE_READY","u2f":{"id":"401","name":"spike key"}},` +
		`{"state":"AUTH_FACTOR_STATE_NOT_READY","u2f":{"id":"402","name":"half"}},` +
		`{"state":"AUTH_FACTOR_STATE_READY","otp":{}},{"state":"AUTH_FACTOR_STATE_READY","newKind":{"id":"403"}}]}`
	var gotBody, gotOrg, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/v2/sessions/s" {
			_, _ = w.Write([]byte(`{"session":{"factors":{"user":{"id":"u1","organizationId":"o1"}}}}`))
			return
		}
		b, _ := io.ReadAll(r.Body)
		gotBody, gotOrg, gotPath = string(b), r.Header.Get("x-zitadel-orgid"), r.Method+" "+r.URL.Path
		_, _ = w.Write([]byte(answer))
	}))
	t.Cleanup(srv.Close)
	ids, err := New(srv.URL, "synthetic", srv.Client()).ReadySecurityKeyIDs(context.Background(), "s")
	require.NoError(t, err)
	require.Equal(t, []string{"401"}, ids)
	require.Equal(t, "POST /management/v1/users/u1/auth_factors/_search", gotPath)
	require.Equal(t, `{}`, gotBody)
	require.Equal(t, "o1", gotOrg)

	// protojson omits an empty repeated field: no keys at all is `{}`.
	for _, empty := range []string{`{}`, `{"result":null}`, `{"result":[]}`} {
		answer = empty
		ids, err := New(srv.URL, "synthetic", srv.Client()).ReadySecurityKeyIDs(context.Background(), "s")
		require.NoError(t, err, empty)
		require.Equal(t, []string{}, ids, empty)
	}

	for _, malformed := range []string{`{"result":{"state":"x"}}`, `{"result":"x"}`, `not json`} {
		answer = malformed
		_, err := New(srv.URL, "synthetic", srv.Client()).ReadySecurityKeyIDs(context.Background(), "s")
		require.ErrorIs(t, err, ErrUnavailable, malformed)
	}
}
