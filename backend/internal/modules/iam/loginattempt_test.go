package iam

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// newTestLoginAttemptStore boots a real Postgres with only the
// login_attempt table migrated — the store has no dependency on
// iam_roles/iam_members/iam_credential_revocations or on
// hms_tenant_visible (the table carries no tenant_id at all), so pulling
// in the rest of the iam module's migrations here would only be noise.
// Mirrors revocationHarness in revocation_test.go.
func newTestLoginAttemptStore(t *testing.T) (*loginAttemptStore, context.Context) {
	t.Helper()
	appDSN, adminDSN := testinfra.StartPostgres(t)
	db, err := tenantdb.Open(appDSN, adminDSN)
	require.NoError(t, err)

	migs := New(nil).Migrations()
	var loginAttemptMig *tenantdb.Migration
	for i := range migs {
		if migs[i].ID == "0004_iam" {
			loginAttemptMig = &migs[i]
		}
	}
	require.NotNil(t, loginAttemptMig, "precondition: 0004_iam is the login_attempt migration")
	require.NoError(t, db.Migrate(context.Background(), []tenantdb.Migration{*loginAttemptMig}))

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	return newLoginAttemptStore(db), ctx
}

// The attempt counter is a SECURITY control, not bookkeeping: a TOTP code is
// six digits, and Zitadel applies a delay rather than a lockout (spec D6).
// These tests therefore assert on the exhaustion boundary, not just on
// increment.
func TestLoginAttempt_BumpExhaustsAtFive(t *testing.T) {
	store, ctx := newTestLoginAttemptStore(t)

	require.NoError(t, store.Put(ctx, loginAttempt{
		AuthRequestID: "V2_test", SessionID: "s1", SessionToken: "t1",
		Subject: "u1", ExpiresAt: time.Now().Add(5 * time.Minute),
	}))

	// Four wrong codes are survivable.
	for i := 1; i <= 4; i++ {
		got, err := store.BumpAndGet(ctx, "V2_test")
		require.NoError(t, err, "attempt %d must not exhaust", i)
		require.Equal(t, i, got.FactorAttempts)
	}

	// The fifth exhausts.
	_, err := store.BumpAndGet(ctx, "V2_test")
	require.ErrorIs(t, err, errAttemptsExhausted)

	// And the row is gone, so the session cannot be reused.
	_, err = store.Get(ctx, "V2_test")
	require.ErrorIs(t, err, errAttemptNotFound)
}

func TestLoginAttempt_ExpiredIsNotFound(t *testing.T) {
	store, ctx := newTestLoginAttemptStore(t)
	require.NoError(t, store.Put(ctx, loginAttempt{
		AuthRequestID: "V2_old", SessionID: "s", SessionToken: "t",
		Subject: "u", ExpiresAt: time.Now().Add(-time.Second),
	}))
	_, err := store.Get(ctx, "V2_old")
	require.ErrorIs(t, err, errAttemptNotFound)
}

func TestLoginAttempt_UpdateTokenReplaces(t *testing.T) {
	store, ctx := newTestLoginAttemptStore(t)
	require.NoError(t, store.Put(ctx, loginAttempt{
		AuthRequestID: "V2_rot", SessionID: "s", SessionToken: "old",
		Subject: "u", ExpiresAt: time.Now().Add(time.Minute),
	}))
	require.NoError(t, store.UpdateToken(ctx, "V2_rot", "new"))
	got, err := store.Get(ctx, "V2_rot")
	require.NoError(t, err)
	require.Equal(t, "new", got.SessionToken, "the rotated token must replace the old one (spec D3)")
}
