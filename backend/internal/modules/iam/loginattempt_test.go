package iam

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// countLoginAttemptRows counts rows directly against Postgres, bypassing
// the store's own read path entirely. Get returning errAttemptNotFound
// is not proof the row is gone — that is exactly what Get returns
// whether or not its own delete committed — so the exhaustion test below
// must observe the table, not the store's opinion of it.
func countLoginAttemptRows(t *testing.T, store *loginAttemptStore, ctx context.Context, authRequestID string) int64 {
	t.Helper()
	var n int64
	require.NoError(t, store.db.WithSystem(ctx, func(tx *gorm.DB) error {
		return tx.Table("login_attempt").Where("auth_request_id = ?", authRequestID).Count(&n).Error
	}))
	return n
}

// newTestLoginAttemptStore boots a real Postgres with only the
// login_attempt table migrated — the store has no dependency on
// iam_roles/iam_members/iam_credential_revocations or on
// hms_tenant_visible (the table carries no tenant_id at all), so pulling
// in the rest of the iam module's migrations here would only be noise.
// Mirrors revocationHarness in revocation_test.go.
func newTestLoginAttemptStore(t *testing.T) (*loginAttemptStore, context.Context) {
	t.Helper()
	appDSN, adminDSN, systemDSN := testinfra.StartPostgres(t)
	db, err := tenantdb.OpenWithSystem(appDSN, adminDSN, systemDSN)
	require.NoError(t, err)

	migrateLoginAttempt(t, db)

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	return newLoginAttemptStore(db), ctx
}

// loginAttemptMigrationIDs is every migration that shapes login_attempt:
// the table (0004_iam), its enrolling column (0006_iam, #948) and its stage
// column (0007_iam, #856). Tests apply exactly these, in order — the store
// depends on nothing else in the module.
var loginAttemptMigrationIDs = []string{"0004_iam", "0006_iam", "0007_iam"}

// migrateLoginAttempt applies loginAttemptMigrationIDs to db, failing the
// test if any of them is missing from the module.
func migrateLoginAttempt(t *testing.T, db *tenantdb.DB) {
	t.Helper()
	byID := map[string]tenantdb.Migration{}
	for _, m := range New(nil).Migrations() {
		byID[m.ID] = m
	}
	var migs []tenantdb.Migration
	for _, id := range loginAttemptMigrationIDs {
		m, ok := byID[id]
		require.True(t, ok, "precondition: %s shapes login_attempt", id)
		migs = append(migs, m)
	}
	require.NoError(t, db.Migrate(context.Background(), migs))
}

// The attempt counter is a SECURITY control, not bookkeeping: a TOTP code is
// six digits, and Zitadel applies a delay rather than a lockout (spec D6).
// These tests therefore assert on the exhaustion boundary, not just on
// increment.
func TestLoginAttempt_BumpExhaustsAtFive(t *testing.T) {
	store, ctx := newTestLoginAttemptStore(t)

	require.NoError(t, store.Put(ctx, loginAttempt{
		Stage:         stageFactor,
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
	_, err = store.Get(ctx, "V2_test", stageFactor)
	require.ErrorIs(t, err, errAttemptNotFound)
}

func TestLoginAttempt_ExpiredIsNotFound(t *testing.T) {
	store, ctx := newTestLoginAttemptStore(t)
	require.NoError(t, store.Put(ctx, loginAttempt{
		Stage:         stageFactor,
		AuthRequestID: "V2_old", SessionID: "s", SessionToken: "t",
		Subject: "u", ExpiresAt: time.Now().Add(-time.Second),
	}))
	_, err := store.Get(ctx, "V2_old", stageFactor)
	require.ErrorIs(t, err, errAttemptNotFound)
}

// TestLoginAttempt_ExpiredRowIsActuallyDeleted is the row-count proof
// Finding 1 called for: Get returning errAttemptNotFound is not evidence
// the row is gone, because that is exactly what Get returns whether or
// not its internal delete committed. Every row holds a live Zitadel
// session token (spec D2), so a delete that silently rolled back would
// be a slow leak of credentials into a table nothing sweeps (spec D6
// promises no cleanup job is needed for correctness).
func TestLoginAttempt_ExpiredRowIsActuallyDeleted(t *testing.T) {
	store, ctx := newTestLoginAttemptStore(t)
	require.NoError(t, store.Put(ctx, loginAttempt{
		Stage:         stageFactor,
		AuthRequestID: "V2_leak", SessionID: "s", SessionToken: "t",
		Subject: "u", ExpiresAt: time.Now().Add(-time.Second),
	}))

	_, err := store.Get(ctx, "V2_leak", stageFactor)
	require.ErrorIs(t, err, errAttemptNotFound)

	require.Equal(t, int64(0), countLoginAttemptRows(t, store, ctx, "V2_leak"),
		"the expired row, and the Zitadel session token it carries, must actually be deleted — not just reported as not-found")
}

func TestLoginAttempt_UpdateTokenReplaces(t *testing.T) {
	store, ctx := newTestLoginAttemptStore(t)
	require.NoError(t, store.Put(ctx, loginAttempt{
		Stage:         stageFactor,
		AuthRequestID: "V2_rot", SessionID: "s", SessionToken: "old",
		Subject: "u", ExpiresAt: time.Now().Add(time.Minute),
	}))
	require.NoError(t, store.UpdateToken(ctx, "V2_rot", "new"))
	got, err := store.Get(ctx, "V2_rot", stageFactor)
	require.NoError(t, err)
	require.Equal(t, "new", got.SessionToken, "the rotated token must replace the old one (spec D3)")
}

// #856: a row is only readable at the stage it was written for. A missing
// stage check would let password-change resume a session whose TOTP is still
// unproven, so the wrong-stage answer must be exactly the missing-row answer.
func TestLoginAttempt_GetIsBoundToTheStage(t *testing.T) {
	store, ctx := newTestLoginAttemptStore(t)
	require.NoError(t, store.Put(ctx, loginAttempt{
		Stage:         stageFactor,
		AuthRequestID: "V2_stage", SessionID: "s", SessionToken: "t",
		Subject: "u", ExpiresAt: time.Now().Add(5 * time.Minute),
	}))

	_, err := store.Get(ctx, "V2_stage", stagePasswordChange)
	require.ErrorIs(t, err, errAttemptNotFound, "a factor-stage row was readable as a password-change row")

	got, err := store.Get(ctx, "V2_stage", stageFactor)
	require.NoError(t, err)
	require.Equal(t, stageFactor, got.Stage)
}

func TestLoginAttempt_AdvanceToPasswordChange(t *testing.T) {
	store, ctx := newTestLoginAttemptStore(t)
	require.NoError(t, store.Put(ctx, loginAttempt{
		Stage:         stageFactor,
		AuthRequestID: "V2_adv", SessionID: "s", SessionToken: "t",
		Subject: "u", ExpiresAt: time.Now().Add(5 * time.Minute),
	}))

	require.NoError(t, store.AdvanceToPasswordChange(ctx, "V2_adv"))
	_, err := store.Get(ctx, "V2_adv", stageFactor)
	require.ErrorIs(t, err, errAttemptNotFound, "an advanced row must no longer resume the factor step")
	got, err := store.Get(ctx, "V2_adv", stagePasswordChange)
	require.NoError(t, err)
	require.Equal(t, "t", got.SessionToken)

	// Only a factor row advances.
	require.ErrorIs(t, store.AdvanceToPasswordChange(ctx, "V2_adv"), errAttemptNotFound)
	require.ErrorIs(t, store.AdvanceToPasswordChange(ctx, "V2_absent"), errAttemptNotFound)
}

// A password-change row is never a TOTP guessing budget: BumpAndGet only
// counts factor-stage rows.
func TestLoginAttempt_BumpIgnoresAPasswordChangeRow(t *testing.T) {
	store, ctx := newTestLoginAttemptStore(t)
	require.NoError(t, store.Put(ctx, loginAttempt{
		Stage:         stagePasswordChange,
		AuthRequestID: "V2_pc", SessionID: "s", SessionToken: "t",
		Subject: "u", ExpiresAt: time.Now().Add(5 * time.Minute),
	}))
	_, err := store.BumpAndGet(ctx, "V2_pc")
	require.ErrorIs(t, err, errAttemptNotFound)
}

// The CHECK constraint (0007_iam) refuses a row with no stage, or a misspelt
// one, rather than storing a row no endpoint will ever accept.
func TestLoginAttempt_StageIsConstrained(t *testing.T) {
	store, ctx := newTestLoginAttemptStore(t)
	for _, stage := range []attemptStage{"", "Factor", "password-change"} {
		err := store.Put(ctx, loginAttempt{
			Stage:         stage,
			AuthRequestID: "V2_bad", SessionID: "s", SessionToken: "t",
			Subject: "u", ExpiresAt: time.Now().Add(5 * time.Minute),
		})
		require.Error(t, err, "stage %q was stored", stage)
	}
}

// 0007_iam must keep every row already in flight on the factor step: a row
// written before the column existed reads back as stageFactor.
func TestLoginAttempt_StageMigrationKeepsInFlightRowsOnTheFactorStep(t *testing.T) {
	appDSN, adminDSN, systemDSN := testinfra.StartPostgres(t)
	db, err := tenantdb.OpenWithSystem(appDSN, adminDSN, systemDSN)
	require.NoError(t, err)
	ctx := context.Background()

	byID := map[string]tenantdb.Migration{}
	for _, m := range New(nil).Migrations() {
		byID[m.ID] = m
	}
	require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{byID["0004_iam"], byID["0006_iam"]}))
	require.NoError(t, db.WithSystem(ctx, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO login_attempt
			(auth_request_id, zitadel_session_id, zitadel_session_token, subject, expires_at)
			VALUES ('V2_inflight', 's', 't', 'u', now() + interval '5 minutes')`).Error
	}))
	require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{byID["0004_iam"], byID["0006_iam"], byID["0007_iam"]}))

	got, err := newLoginAttemptStore(db).Get(ctx, "V2_inflight", stageFactor)
	require.NoError(t, err)
	require.Equal(t, stageFactor, got.Stage)
}
