package config_test

import (
	"crypto/ed25519"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/config"
	"github.com/tesserix/helivanta/pkg/session"
)

func TestDefaultSessionStaysValidUntilTwelveHours(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		pub, priv, err := ed25519.GenerateKey(nil)
		require.NoError(t, err)
		signer, err := session.NewSigner(priv, "test", "https://hms.test", config.DefaultSessionTTL)
		require.NoError(t, err)
		verifier, err := session.NewVerifier(pub, "test", "https://hms.test")
		require.NoError(t, err)
		now := time.Now().UTC()
		token, err := signer.Mint("synthetic-owner", "synthetic-tenant", now, now.Add(config.DefaultIdleTimeout))
		require.NoError(t, err)
		time.Sleep(12*time.Hour - time.Second)
		claims, err := verifier.Verify(token)
		require.NoError(t, err)
		require.Equal(t, "synthetic-owner", claims.Subject)
		require.Equal(t, now.Add(12*time.Hour), claims.IdleDeadline)
		time.Sleep(time.Second)
		_, err = verifier.Verify(token)
		require.ErrorIs(t, err, session.ErrInvalidToken)
	})
}
