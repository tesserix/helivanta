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
			"tenant_id": "t1",
		},
	}
	p, err := principalFromToken(tok)
	require.NoError(t, err)
	require.Equal(t, "u1", p.Subject)
	require.Equal(t, "t1", p.TenantID)
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
