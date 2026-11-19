package session_test

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

func TestSignedRecoveryGenerationSurvivesVerificationAndRemint(t *testing.T) {
	signer, verifier, _, _ := newSignerVerifier(t)
	const generation = "019f39c2-1738-7998-b5de-6d4214f614e0"
	raw, err := signer.Mint("recovered-staff", "tenant-a", fixedAuthTime(), fixedIdleDeadline(), generation)
	require.NoError(t, err)
	claims, err := verifier.Verify(raw)
	require.NoError(t, err)
	require.Equal(t, generation, claims.RecoveryGeneration)
	evidence, err := verifier.RenewalEvidence(raw)
	require.NoError(t, err)
	require.Equal(t, generation, evidence.RecoveryGeneration)
	reminted, err := signer.Mint(claims.Subject, "tenant-b", claims.AuthTime, claims.IdleDeadline, claims.RecoveryGeneration)
	require.NoError(t, err)
	after, err := verifier.Verify(reminted)
	require.NoError(t, err)
	require.Equal(t, generation, after.RecoveryGeneration)
	require.Equal(t, "tenant-b", after.TenantID)
	require.Equal(t, claims.AuthTime, after.AuthTime)
	require.Equal(t, claims.IdleDeadline, after.IdleDeadline)
}

func TestMalformedRecoveryGenerationCannotBeMintedOrVerified(t *testing.T) {
	signer, verifier, _, key := newSignerVerifier(t)
	for _, generation := range []string{"not-a-generation", "11111111-1111-4111-8111-111111111111", "00000000-0000-0000-0000-000000000000", strings.ToUpper("019f39c2-1738-7998-b5de-6d4214f614e0")} {
		t.Run(generation, func(t *testing.T) {
			raw, err := signer.Mint("recovered-staff", "tenant-a", fixedAuthTime(), fixedIdleDeadline(), generation)
			require.Error(t, err)
			require.Empty(t, raw)
			claims := jwt.MapClaims{"sub": "recovered-staff", "tenant_id": "tenant-a", "auth_time": fixedAuthTime().Unix(), "idle_deadline": fixedIdleDeadline().Unix(), "iss": testIssuer, "iat": time.Now().Unix(), "exp": time.Now().Add(testTTL).Unix(), "recovery_generation": generation}
			token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
			token.Header["kid"] = testKID
			raw, err = token.SignedString(key)
			require.NoError(t, err)
			_, err = verifier.Verify(raw)
			require.Error(t, err)
			_, err = verifier.RenewalEvidence(raw)
			require.Error(t, err)
		})
	}
}

func TestSessionWithoutRecoveryRemainsExplicitlyUnadmitted(t *testing.T) {
	signer, verifier, _, _ := newSignerVerifier(t)
	raw, err := signer.Mint("ordinary-staff", "tenant-a", fixedAuthTime(), fixedIdleDeadline(), "")
	require.NoError(t, err)
	claims, err := verifier.Verify(raw)
	require.NoError(t, err)
	require.Empty(t, claims.RecoveryGeneration)
}

func TestSignedNullRecoveryGenerationIsNotLegacyAbsence(t *testing.T) {
	_, verifier, _, key := newSignerVerifier(t)
	claims := jwt.MapClaims{"sub": "ordinary-staff", "tenant_id": "tenant-a", "auth_time": fixedAuthTime().Unix(), "idle_deadline": fixedIdleDeadline().Unix(), "iss": testIssuer, "iat": time.Now().Unix(), "exp": time.Now().Add(testTTL).Unix(), "recovery_generation": nil}
	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	token.Header["kid"] = testKID
	raw, err := token.SignedString(key)
	require.NoError(t, err)
	_, err = verifier.Verify(raw)
	require.Error(t, err, "an explicit malformed claim cannot become a legacy unadmitted session")
}
