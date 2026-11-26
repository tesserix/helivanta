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
