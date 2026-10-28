package transit_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/pkg/transit"
)

func client(t *testing.T, ttl time.Duration) *transit.Client {
	t.Helper()
	addr, _ := testinfra.StartOpenBao(t)
	user, password := testinfra.OpenBaoUser(t, ttl, "helivanta-api-transit")
	c, err := transit.New(addr, "transit", transit.UserpassLogin(user, password), nil)
	require.NoError(t, err)
	return c
}

func staffKey() string { return "helivanta-staff-" + uuid.NewString() }

func TestTransitRoundTripBindsCiphertextToKeyAndContext(t *testing.T) {
	c, ctx := client(t, time.Hour), context.Background()
	key, other := staffKey(), staffKey()
	derivation := []byte("helivanta/workforce/staff-identifier/v1|h|s|pan")
	ciphertext, err := c.Encrypt(ctx, key, derivation, []byte("ABCPE1234F"))
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(ciphertext, "vault:v1:"), ciphertext)
	require.NotContains(t, ciphertext, "ABCPE1234F")
	again, err := c.Encrypt(ctx, key, derivation, []byte("ABCPE1234F"))
	require.NoError(t, err)
	require.NotEqual(t, ciphertext, again, "encryption is randomised")

	plain, err := c.Decrypt(ctx, key, derivation, ciphertext)
	require.NoError(t, err)
	require.Equal(t, "ABCPE1234F", string(plain))

	_, err = c.Decrypt(ctx, key, []byte("helivanta/workforce/staff-identifier/v1|h|other|pan"), ciphertext)
	require.ErrorIs(t, err, transit.ErrIntegrity, "a ciphertext moved to another staff member does not authenticate")
	_, err = c.Encrypt(ctx, other, derivation, []byte("x"))
	require.NoError(t, err)
	_, err = c.Decrypt(ctx, other, derivation, ciphertext)
	require.ErrorIs(t, err, transit.ErrIntegrity, "another tenant's key cannot open it")
	tampered := ciphertext[:len(ciphertext)-4] + "AAAA"
	_, err = c.Decrypt(ctx, key, derivation, tampered)
	require.ErrorIs(t, err, transit.ErrIntegrity)
	_, err = c.Decrypt(ctx, key, derivation, "plain")
	require.ErrorIs(t, err, transit.ErrIntegrity)
}

func TestTransitKeyIsDerivedNonExportableAndUndeletable(t *testing.T) {
	c, ctx := client(t, time.Hour), context.Background()
	key := staffKey()
	_, err := c.Encrypt(ctx, key, []byte("ctx"), []byte("v"))
	require.NoError(t, err)
	addr, root := testinfra.StartOpenBao(t)
	req, _ := http.NewRequest(http.MethodGet, addr+"/v1/transit/keys/"+key, nil)
	req.Header.Set("X-Vault-Token", root)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	var out struct {
		Data struct {
			Type             string `json:"type"`
			Derived          bool   `json:"derived"`
			Exportable       bool   `json:"exportable"`
			DeletionAllowed  bool   `json:"deletion_allowed"`
			AllowPlaintextBk bool   `json:"allow_plaintext_backup"`
		} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	require.Equal(t, "aes256-gcm96", out.Data.Type)
	require.True(t, out.Data.Derived)
	require.False(t, out.Data.Exportable)
	require.False(t, out.Data.DeletionAllowed)
	require.False(t, out.Data.AllowPlaintextBk)
}

func TestTransitHMACIsStablePerKeyAndSurvivesRotation(t *testing.T) {
	c, ctx := client(t, time.Hour), context.Background()
	key, other := staffKey(), staffKey()
	for _, k := range []string{key, other} {
		_, err := c.Encrypt(ctx, k, []byte("ctx"), []byte("v"))
		require.NoError(t, err)
	}
	first, err := c.HMAC(ctx, key, []byte("ABCPE1234F"))
	require.NoError(t, err)
	second, err := c.HMAC(ctx, key, []byte("ABCPE1234F"))
	require.NoError(t, err)
	require.Equal(t, first, second)
	foreign, err := c.HMAC(ctx, other, []byte("ABCPE1234F"))
	require.NoError(t, err)
	require.NotEqual(t, first, foreign, "fingerprints never match across tenants")

	addr, root := testinfra.StartOpenBao(t)
	require.NoError(t, testinfra.OpenBaoWrite(addr, root, http.MethodPost, "/v1/transit/keys/"+key+"/rotate", ""))
	rotated, err := c.HMAC(ctx, key, []byte("ABCPE1234F"))
	require.NoError(t, err)
	require.Equal(t, first, rotated, "the fingerprint is pinned to key version 1")
}

func TestTransitRefusesKeysOutsideThePolicy(t *testing.T) {
	c, ctx := client(t, time.Hour), context.Background()
	_, err := c.Encrypt(ctx, "helivanta-patient-"+uuid.NewString(), []byte("ctx"), []byte("v"))
	require.ErrorIs(t, err, transit.ErrUnavailable)
	_, err = c.Encrypt(ctx, "../keys/helivanta-staff-x", []byte("ctx"), []byte("v"))
	require.ErrorIs(t, err, transit.ErrUnavailable, "a key name cannot escape its path")
}

func TestTransitLogsInAgainAfterTheTokenExpires(t *testing.T) {
	addr, _ := testinfra.StartOpenBao(t)
	user, password := testinfra.OpenBaoUser(t, 2*time.Second, "helivanta-api-transit")
	var logins atomic.Int32
	login := transit.UserpassLogin(user, password)
	c, err := transit.New(addr, "transit", func(ctx context.Context, h *http.Client, a string) (string, time.Duration, error) {
		logins.Add(1)
		token, _, err := login(ctx, h, a)
		// Claiming a long lease forces the expiry path through OpenBao's own 403, not the client's clock.
		return token, time.Hour, err
	}, nil)
	require.NoError(t, err)
	ctx, key := context.Background(), staffKey()
	_, err = c.Encrypt(ctx, key, []byte("ctx"), []byte("v"))
	require.NoError(t, err)
	time.Sleep(3 * time.Second)
	_, err = c.Encrypt(ctx, key, []byte("ctx"), []byte("v"))
	require.NoError(t, err)
	require.EqualValues(t, 2, logins.Load(), "an expired token triggers exactly one fresh login")
}

func TestTransitWithoutPolicyIsUnavailable(t *testing.T) {
	addr, _ := testinfra.StartOpenBao(t)
	user, password := testinfra.OpenBaoUser(t, time.Hour, "default")
	c, err := transit.New(addr, "transit", transit.UserpassLogin(user, password), nil)
	require.NoError(t, err)
	_, err = c.Encrypt(context.Background(), staffKey(), []byte("ctx"), []byte("v"))
	require.ErrorIs(t, err, transit.ErrUnavailable)
	bad, err := transit.New(addr, "transit", transit.UserpassLogin(user, "wrong"), nil)
	require.NoError(t, err)
	_, err = bad.Encrypt(context.Background(), staffKey(), []byte("ctx"), []byte("v"))
	require.ErrorIs(t, err, transit.ErrUnavailable)
}

func TestTransitUnreachableIsUnavailable(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	addr := server.URL
	server.Close()
	c, err := transit.New(addr, "transit", func(context.Context, *http.Client, string) (string, time.Duration, error) {
		return "t", time.Hour, nil
	}, &http.Client{Timeout: time.Second})
	require.NoError(t, err)
	_, err = c.Encrypt(context.Background(), staffKey(), []byte("ctx"), []byte("v"))
	require.ErrorIs(t, err, transit.ErrUnavailable)
	_, err = c.Decrypt(context.Background(), staffKey(), []byte("ctx"), "vault:v1:x")
	require.ErrorIs(t, err, transit.ErrUnavailable)
}

func TestKubernetesLoginSendsRoleAndCurrentToken(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "token")
	require.NoError(t, os.WriteFile(path, []byte("jwt-one\n"), 0o600))
	var seen []map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/auth/kubernetes/login", r.URL.Path)
		var body map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		seen = append(seen, body)
		_, _ = w.Write([]byte(`{"auth":{"client_token":"t","lease_duration":3600}}`))
	}))
	defer server.Close()
	login := transit.KubernetesLogin("helivanta-api-transit", path)
	token, ttl, err := login(context.Background(), server.Client(), server.URL)
	require.NoError(t, err)
	require.Equal(t, "t", token)
	require.Equal(t, time.Hour, ttl)
	require.NoError(t, os.WriteFile(path, []byte("jwt-two"), 0o600))
	_, _, err = login(context.Background(), server.Client(), server.URL)
	require.NoError(t, err)
	require.Equal(t, []map[string]string{{"role": "helivanta-api-transit", "jwt": "jwt-one"}, {"role": "helivanta-api-transit", "jwt": "jwt-two"}}, seen, "a rotated projected token is re-read")

	_, _, err = transit.KubernetesLogin("r", filepath.Join(dir, "missing"))(context.Background(), server.Client(), server.URL)
	require.ErrorIs(t, err, transit.ErrUnavailable)
}

func TestNewRejectsUnsafeConfiguration(t *testing.T) {
	login := transit.UserpassLogin("u", "p")
	for _, addr := range []string{"", "openbao:8200", "ftp://x", "http://x/v1", "http://x?q=1"} {
		_, err := transit.New(addr, "transit", login, nil)
		require.Error(t, err, addr)
	}
	_, err := transit.New("http://x", "transit/../kv", login, nil)
	require.Error(t, err)
	_, err = transit.New("http://x", "transit", nil, nil)
	require.Error(t, err)
}

func TestConsentReceiptHMACUsesDedicatedNonExportableKeyAndDetectsTampering(t *testing.T) {
	addr, root := testinfra.StartOpenBao(t)
	policy := testinfra.ClinicalConsentTransitPolicy
	encoded, err := json.Marshal(map[string]string{"policy": policy})
	require.NoError(t, err)
	require.NoError(t, testinfra.OpenBaoWrite(addr, root, http.MethodPut, "/v1/sys/policies/acl/clinical-consent-test", string(encoded)))
	user, password := testinfra.OpenBaoUser(t, time.Hour, "clinical-consent-test")
	c, err := transit.New(addr, "transit", transit.UserpassLogin(user, password), nil)
	require.NoError(t, err)
	key := "helivanta-consent-" + uuid.NewString()
	require.NoError(t, c.EnsureHMACKey(t.Context(), key))
	require.NoError(t, c.EnsureHMACKey(t.Context(), key))
	receipt := []byte(`{"schema_version":1,"purpose":"care_coordination","recipient_id":"synthetic-recipient"}`)
	signature, err := c.HMAC(t.Context(), key, receipt)
	require.NoError(t, err)
	valid, err := c.VerifyHMAC(t.Context(), key, receipt, signature)
	require.NoError(t, err)
	require.True(t, valid)
	valid, err = c.VerifyHMAC(t.Context(), key, []byte(`{"purpose":"marketing"}`), signature)
	require.NoError(t, err)
	require.False(t, valid)
	valid, err = c.VerifyHMAC(t.Context(), key, receipt, "malformed")
	require.NoError(t, err)
	require.False(t, valid)
	require.ErrorIs(t, c.EnsureHMACKey(t.Context(), staffKey()), transit.ErrUnavailable)
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, addr+"/v1/transit/keys/"+key, nil)
	require.NoError(t, err)
	req.Header.Set("X-Vault-Token", root)
	response, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = response.Body.Close() }()
	var metadata struct {
		Data struct {
			Type                 string `json:"type"`
			Exportable           bool   `json:"exportable"`
			DeletionAllowed      bool   `json:"deletion_allowed"`
			AllowPlaintextBackup bool   `json:"allow_plaintext_backup"`
			KeySize              int    `json:"key_size"`
		} `json:"data"`
	}
	require.NoError(t, json.NewDecoder(response.Body).Decode(&metadata))
	require.Equal(t, "hmac", metadata.Data.Type)
	require.False(t, metadata.Data.Exportable)
	require.False(t, metadata.Data.DeletionAllowed)
	require.False(t, metadata.Data.AllowPlaintextBackup)
	require.Equal(t, 32, metadata.Data.KeySize)
	roleToken, _, err := transit.UserpassLogin(user, password)(t.Context(), http.DefaultClient, addr)
	require.NoError(t, err)
	require.ErrorContains(t, testinfra.OpenBaoWrite(addr, roleToken, http.MethodPost, "/v1/transit/keys/"+key+"/rotate", "{}"), "status 403")
	for _, size := range []int{1, 16, 24, 31, 33, 128} {
		weak := fmt.Sprintf(`{"type":"hmac","key_size":"%d","exportable":false,"allow_plaintext_backup":false}`, size)
		require.ErrorContains(t, testinfra.OpenBaoWrite(addr, roleToken, http.MethodPost, "/v1/transit/keys/helivanta-consent-"+uuid.NewString(), weak), "status 403")
	}
	for _, unsafe := range []string{
		`{"type":"hmac","key_size":"32","exportable":true,"allow_plaintext_backup":false}`,
		`{"type":"hmac","key_size":"32","exportable":false,"allow_plaintext_backup":true}`,
		`{"type":"aes256-gcm96","key_size":"32","exportable":false,"allow_plaintext_backup":false}`,
	} {
		require.ErrorContains(t, testinfra.OpenBaoWrite(addr, roleToken, http.MethodPost, "/v1/transit/keys/helivanta-consent-"+uuid.NewString(), unsafe), "status 403")
	}
	require.NoError(t, testinfra.OpenBaoWrite(addr, root, http.MethodPost, "/v1/transit/keys/"+key+"/rotate", "{}"))
	valid, err = c.VerifyHMAC(t.Context(), key, receipt, signature)
	require.NoError(t, err)
	require.True(t, valid)
}

func TestTransitDoesNotRedirectCredentialsOrReceiptPayload(t *testing.T) {
	var calls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"hmac":"vault:v1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="}}`))
	}))
	t.Cleanup(target.Close)
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(source.Close)
	c, err := transit.New(source.URL, "transit", func(context.Context, *http.Client, string) (string, time.Duration, error) {
		return "synthetic-transit-token", time.Hour, nil
	}, http.DefaultClient)
	require.NoError(t, err)
	_, err = c.HMAC(t.Context(), staffKey(), []byte("synthetic receipt"))
	require.ErrorIs(t, err, transit.ErrUnavailable)
	require.Zero(t, calls.Load())
}

func TestTransitRefusesMalformedHMACObservations(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"hmac":"vault:v1:garbage"}}`))
	}))
	t.Cleanup(server.Close)
	c, err := transit.New(server.URL, "transit", func(context.Context, *http.Client, string) (string, time.Duration, error) {
		return "synthetic-token", time.Hour, nil
	}, nil)
	require.NoError(t, err)
	value, err := c.HMAC(t.Context(), staffKey(), []byte("synthetic receipt"))
	require.ErrorIs(t, err, transit.ErrUnavailable)
	require.Empty(t, value)
}
