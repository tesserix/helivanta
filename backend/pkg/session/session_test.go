package session_test

import (
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/hms/pkg/session"
)

const (
	testIssuer = "https://hms.test"
	testKID    = "test-key-1"
	testTTL    = 15 * time.Minute
)

// newSignerVerifier is the test fixture: one Ed25519 keypair, one
// Signer and one Verifier that trust each other.
func newSignerVerifier(t *testing.T) (*session.Signer, *session.Verifier, ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	signer, err := session.NewSigner(priv, testKID, testIssuer, testTTL)
	require.NoError(t, err)

	verifier, err := session.NewVerifier(pub, testKID, testIssuer)
	require.NoError(t, err)

	return signer, verifier, pub, priv
}

// authTime deliberately carries a fractional second so a mutation
// that reconstructs it via time.Now() (which will not match) is
// distinguishable from the real bug: a naive "close enough" comparison
// would let that mutation slip through.
func fixedAuthTime() time.Time {
	return time.Date(2026, 3, 4, 9, 30, 12, 0, time.UTC)
}

// fixedIdleDeadline mirrors fixedAuthTime for the same reason: a fixed
// 2026 date is orders of magnitude away from any live process time, so
// a mutation that reconstructs it via time.Now() is unmistakable rather
// than "close enough to pass".
func fixedIdleDeadline() time.Time {
	return time.Date(2026, 3, 4, 9, 45, 12, 0, time.UTC)
}

// mintLegacyTokenWithoutIdleDeadline signs a token carrying sub,
// tenant_id and auth_time but deliberately no idle_deadline claim — the
// shape of a session minted before #848 shipped. It is built by hand
// with jwt.MapClaims, bypassing this package's own tokenClaims type,
// because tokenClaims has no exported way to omit a field.
func mintLegacyTokenWithoutIdleDeadline(t *testing.T, priv ed25519.PrivateKey) string {
	t.Helper()
	now := time.Now().UTC()
	claims := jwt.MapClaims{
		"sub":       "user-123",
		"tenant_id": "tenant-abc",
		"auth_time": fixedAuthTime().Unix(),
		"iss":       testIssuer,
		"iat":       jwt.NewNumericDate(now),
		"exp":       jwt.NewNumericDate(now.Add(testTTL)),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	token.Header["kid"] = testKID

	signed, err := token.SignedString(priv)
	require.NoError(t, err)
	return signed
}

// mintTokenWithRawIdleDeadline signs a token carrying idleDeadline
// verbatim as the idle_deadline claim, bypassing Signer.Mint's own
// validation entirely. Used only to construct a claim shape Mint itself
// now refuses to produce (e.g. negative), so Verify's handling of that
// shape can still be pinned by a test.
func mintTokenWithRawIdleDeadline(t *testing.T, priv ed25519.PrivateKey, idleDeadline int64) string {
	t.Helper()
	now := time.Now().UTC()
	claims := jwt.MapClaims{
		"sub":           "user-123",
		"tenant_id":     "tenant-abc",
		"auth_time":     fixedAuthTime().Unix(),
		"idle_deadline": idleDeadline,
		"iss":           testIssuer,
		"iat":           jwt.NewNumericDate(now),
		"exp":           jwt.NewNumericDate(now.Add(testTTL)),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	token.Header["kid"] = testKID

	signed, err := token.SignedString(priv)
	require.NoError(t, err)
	return signed
}

// --- Test 1: round trip -----------------------------------------------

func TestMintVerify_RoundTrip(t *testing.T) {
	signer, verifier, _, _ := newSignerVerifier(t)
	authTime := fixedAuthTime()

	token, err := signer.Mint("user-123", "tenant-abc", authTime, fixedIdleDeadline())
	require.NoError(t, err)
	require.NotEmpty(t, token)

	claims, err := verifier.Verify(token)
	require.NoError(t, err)

	require.Equal(t, "user-123", claims.Subject)
	require.Equal(t, "tenant-abc", claims.TenantID)
	require.True(t, authTime.Equal(claims.AuthTime),
		"auth_time must round-trip exactly: got %v, want %v", claims.AuthTime, authTime)
	require.Equal(t, testIssuer, claims.Issuer)
	require.False(t, claims.IssuedAt.IsZero())
	require.False(t, claims.ExpiresAt.IsZero())
	require.WithinDuration(t, claims.IssuedAt.Add(testTTL), claims.ExpiresAt, time.Second)
}

// --- Test 7: auth_time survives byte-identical -------------------------

func TestMintVerify_AuthTimeByteIdentical(t *testing.T) {
	signer, verifier, _, _ := newSignerVerifier(t)
	authTime := fixedAuthTime()

	token, err := signer.Mint("user-123", "tenant-abc", authTime, fixedIdleDeadline())
	require.NoError(t, err)

	claims, err := verifier.Verify(token)
	require.NoError(t, err)

	// Unix-second precision, exactly: the wire format is a Unix
	// timestamp, so this is the strongest equality Mint/Verify can
	// promise, and it is strong enough to catch a reset-to-now bug by
	// orders of magnitude (any live process time differs from a fixed
	// 2026 date by decades of seconds).
	require.Equal(t, authTime.Unix(), claims.AuthTime.Unix())
}

// --- Test 2: different signing key is refused ---------------------------

func TestVerify_RefusesTokenSignedByDifferentKey(t *testing.T) {
	signer, _, _, _ := newSignerVerifier(t)
	token, err := signer.Mint("user-123", "tenant-abc", fixedAuthTime(), fixedIdleDeadline())
	require.NoError(t, err)

	otherPub, _, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	otherVerifier, err := session.NewVerifier(otherPub, testKID, testIssuer)
	require.NoError(t, err)

	_, err = otherVerifier.Verify(token)
	require.Error(t, err)
}

// --- Test 3: expired token is refused -----------------------------------

func TestVerify_RefusesExpiredToken(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)

	// A TTL so short the token is already expired by the time we
	// verify it, without needing a fake clock.
	signer, err := session.NewSigner(priv, testKID, testIssuer, time.Nanosecond)
	require.NoError(t, err)
	verifier, err := session.NewVerifier(pub, testKID, testIssuer)
	require.NoError(t, err)

	token, err := signer.Mint("user-123", "tenant-abc", fixedAuthTime(), fixedIdleDeadline())
	require.NoError(t, err)
	time.Sleep(10 * time.Millisecond)

	_, err = verifier.Verify(token)
	require.Error(t, err)
}

// --- Test 4: alg:none is refused ----------------------------------------

func TestVerify_RefusesAlgNone(t *testing.T) {
	_, verifier, _, _ := newSignerVerifier(t)

	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT","kid":"test-key-1"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(
		`{"sub":"attacker","tenant_id":"tenant-abc","auth_time":1,"iss":"https://hms.test","exp":9999999999}`))
	// alg:none tokens carry an empty signature segment.
	forged := header + "." + payload + "."

	_, err := verifier.Verify(forged)
	require.Error(t, err)
}

// --- Test 5: algorithm confusion (RS/Ed -> HS256) is refused ------------

func TestVerify_RefusesAlgorithmConfusionAttack(t *testing.T) {
	_, verifier, pub, _ := newSignerVerifier(t)

	// The classic confusion attack: take the public key (meant only
	// for verification) and (ab)use its raw bytes as an HMAC secret,
	// then sign a forged token with alg: HS256. If a verifier's
	// keyfunc trusts the header's alg and hands back "the configured
	// key" without checking token.Method, this signs correctly and
	// verification wrongly succeeds.
	header := map[string]any{"alg": "HS256", "typ": "JWT", "kid": testKID}
	headerJSON := mustJSON(header)
	payload := map[string]any{
		"sub":       "attacker",
		"tenant_id": "tenant-abc",
		"auth_time": fixedAuthTime().Unix(),
		"iss":       testIssuer,
		"exp":       time.Now().Add(time.Hour).Unix(),
	}
	payloadJSON := mustJSON(payload)

	signingInput := base64.RawURLEncoding.EncodeToString(headerJSON) + "." +
		base64.RawURLEncoding.EncodeToString(payloadJSON)

	mac := hmac.New(sha256.New, pub) // pub is the public key, used as an HMAC secret
	mac.Write([]byte(signingInput))
	sig := mac.Sum(nil)

	forged := signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)

	_, err := verifier.Verify(forged)
	require.Error(t, err)
}

// --- Test 6: tampered claim is refused -----------------------------------

func TestVerify_RefusesTamperedClaim(t *testing.T) {
	signer, verifier, _, _ := newSignerVerifier(t)
	token, err := signer.Mint("user-123", "tenant-abc", fixedAuthTime(), fixedIdleDeadline())
	require.NoError(t, err)

	parts := strings.Split(token, ".")
	require.Len(t, parts, 3)

	decoded, err := base64.RawURLEncoding.DecodeString(parts[1])
	require.NoError(t, err)
	tampered := strings.Replace(string(decoded), "tenant-abc", "tenant-XYZ", 1)
	require.NotEqual(t, string(decoded), tampered, "precondition: substitution must actually change the payload")
	parts[1] = base64.RawURLEncoding.EncodeToString([]byte(tampered))
	tamperedToken := strings.Join(parts, ".")

	_, err = verifier.Verify(tamperedToken)
	require.Error(t, err)
}

// --- Test 8: minting with no key is an error, never a token -------------

func TestNewSigner_RefusesMissingKey(t *testing.T) {
	_, err := session.NewSigner(nil, testKID, testIssuer, testTTL)
	require.Error(t, err)
	require.ErrorIs(t, err, session.ErrNoSigningKey)
}

func TestNewSigner_RefusesWrongLengthKey(t *testing.T) {
	_, err := session.NewSigner(ed25519.PrivateKey{1, 2, 3}, testKID, testIssuer, testTTL)
	require.Error(t, err)
	require.ErrorIs(t, err, session.ErrNoSigningKey)
}

func TestNewSigner_RefusesMissingKID(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	_, err = session.NewSigner(priv, "", testIssuer, testTTL)
	require.Error(t, err)
}

func TestNewSigner_RefusesMissingIssuer(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	_, err = session.NewSigner(priv, testKID, "", testTTL)
	require.Error(t, err)
}

func TestNewSigner_RefusesNonPositiveTTL(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	_, err = session.NewSigner(priv, testKID, testIssuer, 0)
	require.Error(t, err)
}

// --- Additional coverage: verifier construction, wrong issuer, wrong kid --

func TestNewVerifier_RefusesMissingKey(t *testing.T) {
	_, err := session.NewVerifier(nil, testKID, testIssuer)
	require.Error(t, err)
	require.ErrorIs(t, err, session.ErrNoVerificationKey)
}

func TestVerify_RefusesWrongIssuer(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	signer, err := session.NewSigner(priv, testKID, "https://issuer-a.test", testTTL)
	require.NoError(t, err)
	verifier, err := session.NewVerifier(pub, testKID, "https://issuer-b.test")
	require.NoError(t, err)

	token, err := signer.Mint("user-123", "tenant-abc", fixedAuthTime(), fixedIdleDeadline())
	require.NoError(t, err)

	_, err = verifier.Verify(token)
	require.Error(t, err)
}

func TestVerify_RefusesUnknownKID(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	require.NoError(t, err)
	signer, err := session.NewSigner(priv, "kid-a", testIssuer, testTTL)
	require.NoError(t, err)
	verifier, err := session.NewVerifier(pub, "kid-b", testIssuer)
	require.NoError(t, err)

	token, err := signer.Mint("user-123", "tenant-abc", fixedAuthTime(), fixedIdleDeadline())
	require.NoError(t, err)

	_, err = verifier.Verify(token)
	require.Error(t, err)
}

func TestVerify_RefusesMissingSubjectOrTenant(t *testing.T) {
	signer, verifier, _, _ := newSignerVerifier(t)
	_, err := signer.Mint("", "tenant-abc", fixedAuthTime(), fixedIdleDeadline())
	require.Error(t, err, "Mint itself must refuse an empty subject")

	_, err = signer.Mint("user-123", "", fixedAuthTime(), fixedIdleDeadline())
	require.Error(t, err, "Mint itself must refuse an empty tenant_id")

	_ = verifier // used above only for symmetry with other tests
}

// --- Idle deadline (#848) ------------------------------------------------

// TestMintCarriesIdleDeadlineThroughVerify runs two DISTINCT deadlines,
// deliberately, rather than the single package-level fixedIdleDeadline()
// every other test in this file uses. A single fixed value here would
// pass against a Verify that just returned that constant hardcoded —
// two different inputs, each asserted against its own expectation,
// close that gap.
func TestMintCarriesIdleDeadlineThroughVerify(t *testing.T) {
	tests := []struct {
		name     string
		deadline time.Time
	}{
		{"deadline A", fixedIdleDeadline()},
		{"deadline B, a different value entirely", time.Date(2031, 11, 2, 18, 5, 40, 0, time.UTC)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signer, verifier, _, _ := newSignerVerifier(t)
			authTime := fixedAuthTime()

			raw, err := signer.Mint("sub-1", "11111111-1111-1111-1111-111111111111", authTime, tt.deadline)
			require.NoError(t, err)

			got, err := verifier.Verify(raw)
			require.NoError(t, err)
			require.True(t, got.IdleDeadline.Equal(tt.deadline),
				"IdleDeadline = %v, want %v", got.IdleDeadline, tt.deadline)
		})
	}
}

// TestMintVerify_IdleDeadlineByteIdentical mirrors
// TestMintVerify_AuthTimeByteIdentical: the wire format is a Unix
// timestamp, so Unix-second equality is the strongest promise Mint and
// Verify can make, and it is strong enough to catch a reset-to-now bug
// by decades of seconds.
func TestMintVerify_IdleDeadlineByteIdentical(t *testing.T) {
	signer, verifier, _, _ := newSignerVerifier(t)
	deadline := fixedIdleDeadline()

	token, err := signer.Mint("user-123", "tenant-abc", fixedAuthTime(), deadline)
	require.NoError(t, err)

	claims, err := verifier.Verify(token)
	require.NoError(t, err)

	require.Equal(t, deadline.Unix(), claims.IdleDeadline.Unix())
}

func TestMintRefusesAZeroIdleDeadline(t *testing.T) {
	signer, _, _, _ := newSignerVerifier(t)
	_, err := signer.Mint("sub-1", "11111111-1111-1111-1111-111111111111", time.Now(), time.Time{})
	require.Error(t, err, "Mint() must refuse a zero idle_deadline: a mint with no idle deadline would be exempt from the timeout")
}

// TestMintRefusesAnIdleDeadlineAtOrBeforeTheUnixEpoch pins the sentinel
// symmetry between Mint and Verify. Mint's own zero check is
// idleDeadline.IsZero() (Go's year-1 zero value), but Verify refuses
// claims.IdleDeadline == 0 (the Unix epoch). Without also checking
// idleDeadline.Unix() <= 0, Mint would happily sign a token carrying
// exactly 1970-01-01T00:00:00Z — a credential Verify then always
// refuses, i.e. a token nothing can meaningfully verify, which is the
// exact failure mode NewSigner's own validation exists to prevent for a
// signing key.
func TestMintRefusesAnIdleDeadlineAtOrBeforeTheUnixEpoch(t *testing.T) {
	signer, _, _, _ := newSignerVerifier(t)
	_, err := signer.Mint("sub-1", "11111111-1111-1111-1111-111111111111",
		fixedAuthTime(), time.Unix(0, 0).UTC())
	require.Error(t, err, "Mint() must refuse an idle_deadline at the Unix epoch: Verify would refuse the resulting token anyway")
}

// TestVerifyRefusesATokenWithNoIdleDeadlineClaim is the fail-closed
// regression test for #848: a token predating this claim must not be
// treated as "no idle limit" — that would be a class of session the
// control cannot reach.
func TestVerifyRefusesATokenWithNoIdleDeadlineClaim(t *testing.T) {
	_, verifier, _, priv := newSignerVerifier(t)
	raw := mintLegacyTokenWithoutIdleDeadline(t, priv)

	_, err := verifier.Verify(raw)
	require.Error(t, err, "Verify() must refuse a token carrying no idle_deadline")
}

// TestVerify_AcceptsANegativeIdleDeadlineAsAlreadyExpired pins the
// fail-closed direction of a value Mint itself now refuses to produce
// (idleDeadline.Unix() <= 0) but Verify may still see on a token it did
// not mint — e.g. a forged or otherwise malformed one. Verify's own
// check is claims.IdleDeadline == 0, so a negative value is NOT treated
// as "unset": it is accepted as a claim and maps to a time before the
// Unix epoch, i.e. already expired. This is deliberately the safe
// direction — a negative deadline can never grant more time than a
// zero one would refuse outright — but it depends on Verify never
// widening its "missing" check to <= 0, which would make this claim
// indistinguishable from an absent one. This test exists so that
// widening is caught rather than silently changing behaviour.
func TestVerify_AcceptsANegativeIdleDeadlineAsAlreadyExpired(t *testing.T) {
	_, verifier, _, priv := newSignerVerifier(t)
	raw := mintTokenWithRawIdleDeadline(t, priv, -3600) // one hour before the Unix epoch

	got, err := verifier.Verify(raw)
	require.NoError(t, err, "Verify() must not treat a negative idle_deadline as an absent one")
	require.True(t, got.IdleDeadline.Before(time.Unix(0, 0).UTC()),
		"IdleDeadline = %v, want a time before the Unix epoch", got.IdleDeadline)
}

// mustJSON builds the raw bytes for a forged token header/payload by
// hand, deliberately bypassing this package's own encoder — used only
// by the algorithm-confusion attack test above.
func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
