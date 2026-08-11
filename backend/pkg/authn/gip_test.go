package authn

import (
	"testing"

	"firebase.google.com/go/v4/auth"
	"github.com/stretchr/testify/require"
)

func TestPrincipalFromToken_StringTenantID(t *testing.T) {
	tok := &auth.Token{
		UID: "u1",
		Claims: map[string]interface{}{
			"tenant_id": "3fa85f64-5717-4562-b3fc-2c963f66afa6",
		},
	}
	p, err := principalFromToken(tok)
	require.NoError(t, err)
	require.Equal(t, "u1", p.Subject)
	require.Equal(t, "3fa85f64-5717-4562-b3fc-2c963f66afa6", p.TenantID)
}

func TestPrincipalFromToken_CanonicalizesMixedCaseUUID(t *testing.T) {
	tok := &auth.Token{
		UID: "u1",
		Claims: map[string]interface{}{
			"tenant_id": "AbC12345-5717-4562-B3fc-2C963f66aFA6",
		},
	}
	p, err := principalFromToken(tok)
	require.NoError(t, err)
	require.Equal(t, "abc12345-5717-4562-b3fc-2c963f66afa6", p.TenantID)
}

func TestPrincipalFromToken_NonUUIDTenantID(t *testing.T) {
	tok := &auth.Token{
		UID: "u1",
		Claims: map[string]interface{}{
			"tenant_id": "not-a-uuid",
		},
	}
	_, err := principalFromToken(tok)
	require.ErrorIs(t, err, ErrNoTenantClaim)
}

func TestPrincipalFromToken_MissingTenantID(t *testing.T) {
	tok := &auth.Token{
		UID:    "u1",
		Claims: map[string]interface{}{},
	}
	_, err := principalFromToken(tok)
	require.ErrorIs(t, err, ErrNoTenantClaim)
}

func TestPrincipalFromToken_NonStringTenantID(t *testing.T) {
	tok := &auth.Token{
		UID: "u1",
		Claims: map[string]interface{}{
			"tenant_id": 42,
		},
	}
	_, err := principalFromToken(tok)
	require.ErrorIs(t, err, ErrNoTenantClaim)
}
