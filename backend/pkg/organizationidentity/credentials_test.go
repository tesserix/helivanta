package organizationidentity_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/tesserix/helivanta/pkg/organizationidentity"
)

func syntheticMachineKey(t *testing.T) ([]byte, *rsa.PublicKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]string{"type": "serviceaccount", "keyId": "TEST-key-1", "userId": "TEST-product-machine", "key": string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))})
	if err != nil {
		t.Fatal(err)
	}
	return raw, &key.PublicKey
}

func TestMachineCredentialUsesTheExactAudienceAndReusesOnlyAnExpiringAccessToken(t *testing.T) {
	raw, publicKey := syntheticMachineKey(t)
	var calls atomic.Int32
	var issuer string
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/oauth/v2/token" || r.Method != http.MethodPost || r.Header.Get("Cookie") != "" || r.ParseForm() != nil {
			t.Error("unexpected token request or leaked browser cookie")
		}
		if r.PostForm.Get("scope") != "openid urn:zitadel:iam:org:project:id:TEST-onboarding-project:aud" || r.PostForm.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
			t.Error("machine credential was not scoped to the configured onboarding project")
		}
		claims := &jwt.RegisteredClaims{}
		_, err := jwt.ParseWithClaims(r.PostForm.Get("assertion"), claims, func(token *jwt.Token) (any, error) {
			if token.Header["kid"] != "TEST-key-1" {
				t.Error("missing machine key identifier")
			}
			return publicKey, nil
		}, jwt.WithValidMethods([]string{"RS256"}), jwt.WithIssuer("TEST-product-machine"), jwt.WithSubject("TEST-product-machine"), jwt.WithAudience(issuer), jwt.WithExpirationRequired(), jwt.WithIssuedAt())
		if err != nil || claims.ExpiresAt == nil || claims.ExpiresAt.After(time.Now().Add(62*time.Second)) {
			t.Error("invalid machine assertion or excessive assertion lifetime")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"TEST-only-access-token","token_type":"Bearer","expires_in":120}`))
	}))
	defer server.Close()
	issuer = server.URL
	tokens, err := organizationidentity.NewKeyTokens(raw, issuer, "TEST-onboarding-project", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		token, err := tokens.Token(context.Background())
		if err != nil || token != "TEST-only-access-token" {
			t.Fatal("machine exchange did not return the bounded access credential")
		}
	}
	if calls.Load() != 1 {
		t.Fatal("access credential was not reused until its refresh margin")
	}
	if strings.Contains(fmt.Sprintf("%+v|%#v", tokens, *tokens), "TEST-only-access-token") {
		t.Fatal("credential formatting exposed a cached access token")
	}
}

func TestMachineCredentialsNeverCacheInvalidResponsesOrExposeProviderDetails(t *testing.T) {
	raw, _ := syntheticMachineKey(t)
	for _, body := range []string{
		`PRIVATE_PROVIDER_DATA`,
		`{"access_token":"PRIVATE_PROVIDER_DATA","token_type":"Bearer"}`,
		`{"access_token":"PRIVATE_PROVIDER_DATA","token_type":"Bearer","expires_in":0}`,
		`{"access_token":"PRIVATE_PROVIDER_DATA","token_type":"Bearer","expires_in":86400}`,
		`{"access_token":"PRIVATE_PROVIDER_DATA","token_type":"wrong","expires_in":120}`,
		`{"access_token":"PRIVATE_PROVIDER_DATA\r\n","token_type":"Bearer","expires_in":120}`,
		strings.Repeat("PRIVATE_PROVIDER_DATA", 1024),
	} {
		t.Run(body[:min(30, len(body))], func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				_, _ = io.Copy(io.Discard, r.Body)
				_, _ = w.Write([]byte(body))
			}))
			defer server.Close()
			tokens, err := organizationidentity.NewKeyTokens(raw, server.URL, "TEST-project", server.Client())
			if err != nil {
				t.Fatal(err)
			}
			for range 2 {
				token, err := tokens.Token(context.Background())
				if !errors.Is(err, organizationidentity.ErrUnavailable) || err.Error() != "organisation identity unavailable" || token != "" {
					t.Fatal("untrusted credential response was cached, accepted or disclosed")
				}
			}
			if calls.Load() != 2 {
				t.Fatal("invalid credentials were cached")
			}
		})
	}
}

func TestMachineCredentialCannotRedirectAssertionOrReuseHumanCookies(t *testing.T) {
	raw, _ := syntheticMachineKey(t)
	var forwarded atomic.Int32
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Add(1) }))
	defer target.Close()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cookie") != "" {
			t.Error("human cookie reached credential exchange")
		}
		_, _ = io.Copy(io.Discard, r.Body)
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	client := server.Client()
	client.Jar, _ = cookiejar.New(nil)
	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	client.Jar.SetCookies(u, []*http.Cookie{{Name: "human_session", Value: "PRIVATE", Path: "/"}})
	tokens, err := organizationidentity.NewKeyTokens(raw, server.URL, "TEST-project", client)
	if err != nil {
		t.Fatal(err)
	}
	value, err := tokens.Token(context.Background())
	if value != "" || !errors.Is(err, organizationidentity.ErrUnavailable) || forwarded.Load() != 0 {
		t.Fatal("machine assertion escaped the configured identity authority")
	}
}

func TestMachineCredentialWaitIsCancellable(t *testing.T) {
	raw, _ := syntheticMachineKey(t)
	entered := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_ = r.Body.Close()
		close(entered)
		<-r.Context().Done()
	}))
	defer server.Close()
	tokens, err := organizationidentity.NewKeyTokens(raw, server.URL, "TEST-project", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, e := tokens.Token(ctx); done <- e }()
	select {
	case <-entered:
	case <-time.After(4 * time.Second):
		t.Fatal("credential request did not reach the fixture")
	}
	blocked, stop := context.WithCancel(context.Background())
	stop()
	if token, err := tokens.Token(blocked); token != "" || !errors.Is(err, organizationidentity.ErrUnavailable) {
		t.Fatal("cancelled credential wait did not fail closed")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, organizationidentity.ErrUnavailable) {
			t.Fatal("unresponsive credential source returned a usable token")
		}
	case <-time.After(4 * time.Second):
		t.Fatal("credential exchange ignored caller cancellation")
	}
}
