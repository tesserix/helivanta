package iam_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam" //nolint:depguard // External test exercises its own module.
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func TestLegacyGrantRejectsForeignAndUnavailableNewIdentities(t *testing.T) {
	for _, tc := range []struct {
		name   string
		reader iam.MembershipIdentityReader
		status int
	}{
		{name: "foreign", reader: &enrollmentIdentity{check: func(_ context.Context, _ string) (bool, error) { return false, nil }}, status: 409},
		{name: "unavailable", reader: &enrollmentIdentity{check: func(_ context.Context, _ string) (bool, error) { return false, errors.New("synthetic outage") }}, status: 503},
		{name: "missing", status: 503},
		{name: "owned", reader: &enrollmentIdentity{}, status: 202},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, db, ctx := enrollmentHarness(t, tc.reader)
			got := testutil.Do(r, http.MethodPost, "/v1/iam/members", "admin", `{"subject":"synthetic-new-role-target","role_key":"staff_member"}`)
			require.Equal(t, tc.status, got.Code, got.Body.String())
			require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
				var roles, events int64
				require.NoError(t, tx.Table("iam_members").Where("subject=?", "synthetic-new-role-target").Count(&roles).Error)
				require.NoError(t, tx.Table("outbox_events").Count(&events).Error)
				expected := int64(0)
				if tc.status == 202 {
					expected = 1
				}
				require.Equal(t, expected, roles)
				require.Equal(t, expected, events)
				return nil
			}))
		})
	}
}

func TestLegacyGrantRepairPreservesExistingIdentityWithoutAddingAnotherRole(t *testing.T) {
	reader := &enrollmentIdentity{check: func(_ context.Context, _ string) (bool, error) { return false, nil }}
	r, db, ctx := enrollmentHarness(t, reader)
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) VALUES(?,'synthetic-retained-foreign','staff_member')`, testutil.TenantA).Error
	}))
	body := `{"subject":"synthetic-retained-foreign","role_key":"staff_member"}`
	first := testutil.Do(r, http.MethodPost, "/v1/iam/members", "admin", body)
	replay := testutil.Do(r, http.MethodPost, "/v1/iam/members", "admin", body)
	require.Equal(t, http.StatusAccepted, first.Code, first.Body.String())
	require.Equal(t, http.StatusAccepted, replay.Code, replay.Body.String())
	require.JSONEq(t, first.Body.String(), replay.Body.String())
	require.Zero(t, reader.calls.Load(), "repairing the exact retained grant must not need native availability")
	denied := testutil.Do(r, http.MethodPost, "/v1/iam/members", "admin", `{"subject":"synthetic-retained-foreign","role_key":"tenant_admin"}`)
	require.Equal(t, http.StatusConflict, denied.Code, denied.Body.String())
	require.EqualValues(t, 1, reader.calls.Load())
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		require.NoError(t, tx.Table("iam_members").Where("subject=?", "synthetic-retained-foreign").Count(&count).Error)
		require.EqualValues(t, 1, count)
		return nil
	}))
}

func TestLegacyRepairCannotRecreateAConcurrentlyRemovedGrant(t *testing.T) {
	reader := &enrollmentIdentity{check: func(context.Context, string) (bool, error) { return false, nil }}
	type repairRequestKey struct{}
	seen, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	var paused atomic.Bool
	callback := "synthetic_repair_race"
	module := legacyRepairRaceModule{
		Module: iam.New(nil).SetMembershipIdentityReader(reader),
		t:      t,
		register: func(tx *gorm.DB) error {
			return tx.Callback().Query().After("gorm:query").Register(callback, func(query *gorm.DB) {
				if query.Statement.Context.Value(repairRequestKey{}) == true && query.Statement.Table == "iam_members" && paused.CompareAndSwap(false, true) {
					close(seen)
					select {
					case <-release:
					case <-query.Statement.Context.Done():
					}
				}
			})
		},
	}
	r, db, ctx := enrollmentHarnessWithModule(t, module)
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) VALUES(?,'synthetic-repair-race','staff_member')`, testutil.TenantA).Error
	}))
	req := httptest.NewRequest(http.MethodPost, "/v1/iam/members", strings.NewReader(`{"subject":"synthetic-repair-race","role_key":"staff_member"}`)).WithContext(context.WithValue(t.Context(), repairRequestKey{}, true))
	req.Header.Set("Authorization", "Bearer admin")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantA)
	response := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { defer close(done); r.ServeHTTP(response, req) }()
	select {
	case <-seen:
	case <-time.After(5 * time.Second):
		t.Fatal("repair did not observe the original grant")
	}
	require.NoError(t, db.WithTenant(tenantdb.WithActor(ctx, "user-admin"), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`DELETE FROM iam_members WHERE tenant_id=? AND subject='synthetic-repair-race' AND role_key='staff_member'`, testutil.TenantA).Error
	}))
	close(release)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("repair did not resolve the concurrent removal")
	}
	require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
	require.Zero(t, reader.calls.Load())
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		require.NoError(t, tx.Table("iam_members").Where("subject=?", "synthetic-repair-race").Count(&count).Error)
		require.Zero(t, count)
		require.NoError(t, tx.Table("outbox_events").Count(&count).Error)
		require.Zero(t, count, "repair of a removed grant must not publish access")
		return nil
	}))
}

type legacyRepairRaceModule struct {
	*iam.Module
	register func(*gorm.DB) error
	t        *testing.T
}

func (m legacyRepairRaceModule) Routes(r *platform.Router, deps platform.Deps) {
	m.t.Helper()
	require.NoError(m.t, deps.DB.WithTenant(m.t.Context(), testutil.TenantA, m.register))
	m.Module.Routes(r, deps)
}
