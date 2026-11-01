package iam_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam" //nolint:depguard // External test exercises its own module.
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
)

func TestLegacyMembershipGrantCannotReopenEndedMembership(t *testing.T) {
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"admin": testutil.TenantA}, Perms: map[string][]authz.Permission{"admin": {iam.PermMemberManage}}, Modules: []platform.Module{iam.New(nil).SetMembershipIdentityReader(membershipIdentity{})}})
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) VALUES(?,'user-admin','tenant_admin')`, testutil.TenantA).Error
	}))
	subject := "synthetic-legacy-ended"
	original := testutil.Do(r, http.MethodPost, "/v1/iam/members", "admin", fmt.Sprintf(`{"subject":%q,"role_key":"staff_member"}`, subject))
	require.Equal(t, http.StatusAccepted, original.Code, original.Body.String())
	var membership iam.MemberGrantResult
	require.NoError(t, json.Unmarshal(original.Body.Bytes(), &membership))
	removalID := uuid.Must(uuid.NewV7())
	body := fmt.Sprintf(`{"operation":"remove","basis_id":%q,"subject":%q,"role_key":"staff_member","expected_membership_epoch":1,"reason":"End synthetic membership before recovery checks"}`, membership.ID, subject)
	removed := membershipCommandRequest(r, http.MethodPut, "/v1/iam/membership-commands/"+removalID.String(), "admin", body)
	require.Equal(t, http.StatusAccepted, removed.Code, removed.Body.String())
	for _, settings := range []string{
		"SELECT 1",
		"SELECT set_config('app.membership_command','01900000-0000-7000-8000-000000000001',true)",
		"SELECT set_config('app.membership_recovery_basis','01900000-0000-7000-8000-000000000002',true)",
	} {
		err := db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(settings).Error; err != nil {
				return err
			}
			return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) VALUES(?,?,'staff_member')`, testutil.TenantA, subject).Error
		})
		var pg *pgconn.PgError
		require.ErrorAs(t, err, &pg)
		require.Equal(t, "23514", pg.Code)
		require.Equal(t, "iam_membership_recovery_required", pg.ConstraintName)
	}
	// Both contexts alone cannot forge the attributable retained command.
	err := db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.membership_command',?,true),set_config('app.membership_recovery_basis',?,true),set_config('app.membership_expected_epoch','2',true),set_config('app.membership_reason','Forged recovery context',true),set_config('app.actor_subject','synthetic-non-admin',true)`, uuid.Must(uuid.NewV7()).String(), removalID.String()).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) VALUES(?,?,'staff_member')`, testutil.TenantA, subject).Error
	})
	require.ErrorContains(t, err, "current attributed membership command required")
	require.NoError(t, db.WithTenant(ctx, testutil.TenantB, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) VALUES(?,?,'staff_member')`, testutil.TenantB, subject).Error
	}))
	var beforeEvents, beforeAudit int64
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		require.NoError(t, tx.Table("outbox_events").Count(&beforeEvents).Error)
		return tx.Table("audit_events").Count(&beforeAudit).Error
	}))
	for _, role := range []string{"staff_member", "doctor", "tenant_admin"} {
		res := testutil.Do(r, http.MethodPost, "/v1/iam/members", "admin", fmt.Sprintf(`{"subject":%q,"role_key":%q}`, subject, role))
		require.Equal(t, http.StatusConflict, res.Code, res.Body.String())
		require.Contains(t, res.Body.String(), `"error":"membership_recovery_required"`)
		require.Equal(t, "no-store", res.Header().Get("Cache-Control"))
	}
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		require.NoError(t, tx.Table("iam_members").Where("subject=?", subject).Count(&count).Error)
		require.Zero(t, count)
		require.NoError(t, tx.Table("outbox_events").Count(&count).Error)
		require.Equal(t, beforeEvents, count)
		require.NoError(t, tx.Table("audit_events").Count(&count).Error)
		require.Equal(t, beforeAudit, count)
		var epoch struct {
			Epoch       int64
			MemberCount int64
		}
		require.NoError(t, tx.Table("iam_subject_membership_epochs").Where("subject=?", subject).Take(&epoch).Error)
		require.EqualValues(t, 2, epoch.Epoch)
		require.Zero(t, epoch.MemberCount)
		return nil
	}))
	recovery := fmt.Sprintf(`{"operation":"recover","basis_id":%q,"subject":%q,"role_key":"staff_member","expected_membership_epoch":2,"reason":"Use explicit ordinary recovery after legacy refusal"}`, removalID, subject)
	recovered := membershipCommandRequest(r, http.MethodPut, "/v1/iam/membership-commands/"+uuid.Must(uuid.NewV7()).String(), "admin", recovery)
	require.Equal(t, http.StatusAccepted, recovered.Code, recovered.Body.String())
	var receipt iam.MembershipCommand
	require.NoError(t, json.Unmarshal(recovered.Body.Bytes(), &receipt))
	repair := testutil.Do(r, http.MethodPost, "/v1/iam/members", "admin", fmt.Sprintf(`{"subject":%q,"role_key":"staff_member"}`, subject))
	require.Equal(t, http.StatusAccepted, repair.Code, repair.Body.String())
	require.Contains(t, repair.Body.String(), receipt.ResultMembershipID.String())
}

func TestLegacyMembershipGrantWaitsForFinalRemovalThenRefusesRestoration(t *testing.T) {
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: map[string]string{"admin": testutil.TenantA}, Perms: map[string][]authz.Permission{"admin": {iam.PermMemberManage}}, Modules: []platform.Module{iam.New(nil)}})
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) VALUES(?,'synthetic-contended-end','staff_member')`, testutil.TenantA).Error
	}))
	locked, release := make(chan struct{}), make(chan struct{})
	unlock := sync.OnceFunc(func() { close(release) })
	defer unlock()
	done := make(chan error, 1)
	var holderPID atomic.Int64
	go func() {
		done <- db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			var pid int64
			if err := tx.Raw(`SELECT pg_backend_pid()`).Scan(&pid).Error; err != nil {
				return err
			}
			holderPID.Store(pid)
			if err := tx.Exec(`SELECT set_config('app.actor_subject','synthetic-fixture',true)`).Error; err != nil {
				return err
			}
			if err := tx.Exec(`DELETE FROM iam_members WHERE subject='synthetic-contended-end'`).Error; err != nil {
				return err
			}
			close(locked)
			<-release
			return nil
		})
	}()
	select {
	case <-locked:
	case err := <-done:
		t.Fatalf("removal fixture failed: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("removal did not obtain lock")
	}
	response := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		response <- testutil.Do(r, http.MethodPost, "/v1/iam/members", "admin", `{"subject":"synthetic-contended-end","role_key":"doctor"}`)
	}()
	require.Eventually(t, func() bool {
		var blocked bool
		err := db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE ?=ANY(pg_blocking_pids(pid)))`, holderPID.Load()).Scan(&blocked).Error
		})
		return err == nil && blocked
	}, 2*time.Second, 10*time.Millisecond, "legacy grant must actually wait behind the final removal")
	unlock()
	require.NoError(t, <-done)
	select {
	case res := <-response:
		require.Equal(t, http.StatusConflict, res.Code, res.Body.String())
		require.Contains(t, res.Body.String(), `"error":"membership_recovery_required"`)
	case <-time.After(3 * time.Second):
		t.Fatal("grant did not finish after removal committed")
	}
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		require.NoError(t, tx.Table("iam_members").Where("subject='synthetic-contended-end'").Count(&count).Error)
		require.Zero(t, count)
		require.NoError(t, tx.Table("outbox_events").Count(&count).Error)
		require.Zero(t, count)
		return nil
	}))
}
