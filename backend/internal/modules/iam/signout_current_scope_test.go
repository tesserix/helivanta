package iam

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	iamcontract "github.com/tesserix/helivanta/internal/modules/iam/contract"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func TestAdminRevokeRejectsFormerMemberDespiteStaleGraph(t *testing.T) {
	r, db, _, _ := newSignoutHarness(t, signoutHarnessConfig{
		verifier:   fixedVerifier{"actor": {Subject: "synthetic-security-admin", TenantID: signoutTestTenantA, AuthTime: time.Now()}},
		membership: fakeMembership{"synthetic-security-admin": true},
		perms:      stubResolver{"synthetic-security-admin": {PermCredentialRevoke}},
		roles:      stubRoleLister{"synthetic-former-member": {{TenantID: signoutTestTenantA, Role: authz.RoleStaffMember}}},
	})
	require.NoError(t, db.WithTenant(t.Context(), signoutTestTenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) VALUES(?,'synthetic-security-admin','tenant_admin')`, signoutTestTenantA).Error
	}))
	require.NoError(t, db.WithTenant(t.Context(), signoutTestTenantB, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) VALUES(?,'synthetic-former-member','staff_member')`, signoutTestTenantB).Error
	}))
	w := doRequest(r, http.MethodPost, "/v1/iam/subjects/synthetic-former-member/revoke", "actor", "")
	require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	require.True(t, watermarkFor(t, db, "synthetic-former-member").IsZero(), "a stale graph binding cannot authorize a global credential side effect")
}

func TestAdminRevokeCurrentMemberPreservesGlobalSecurityAction(t *testing.T) {
	now := time.Now()
	r, db, _, bus := newSignoutHarness(t, signoutHarnessConfig{
		verifier: fixedVerifier{
			"actor":  {Subject: "synthetic-security-admin", TenantID: signoutTestTenantA, AuthTime: now},
			"target": {Subject: "synthetic-current-target", TenantID: signoutTestTenantB, AuthTime: now.Add(-time.Minute)},
		},
		membership: fakeMembership{"synthetic-security-admin": true, "synthetic-current-target": true},
		perms:      stubResolver{"synthetic-security-admin": {PermCredentialRevoke}},
		roles:      stubRoleLister{"synthetic-current-target": {{TenantID: signoutTestTenantA, Role: authz.RoleStaffMember}}},
	})
	nc, err := nats.Connect(testinfra.StartNATS(t))
	require.NoError(t, err)
	t.Cleanup(nc.Close)
	sub, err := nc.SubscribeSync(bus.Subject(iamcontract.SubjectCredentialRevoked))
	require.NoError(t, err)
	require.NoError(t, nc.Flush())
	seedRevocationMember(t, db, signoutTestTenantA, "synthetic-security-admin", authz.RoleTenantAdmin)
	for _, tenant := range []string{signoutTestTenantA, signoutTestTenantB} {
		seedRevocationMember(t, db, tenant, "synthetic-current-target", authz.RoleStaffMember)
	}
	before := doRequest(r, http.MethodGet, "/v1/iam/me/permissions", "target", "")
	require.Equal(t, http.StatusOK, before.Code, before.Body.String())
	w := doRequest(r, http.MethodPost, "/v1/iam/subjects/synthetic-current-target/revoke", "actor", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.False(t, watermarkFor(t, db, "synthetic-current-target").IsZero())
	require.True(t, watermarkFor(t, db, "synthetic-security-admin").IsZero())
	after := doRequest(r, http.MethodGet, "/v1/iam/me/permissions", "target", "")
	require.Equal(t, http.StatusUnauthorized, after.Code, "explicit security revocation remains global and invalidates this replica's warmed cache")
	message, err := sub.NextMsg(5 * time.Second)
	require.NoError(t, err)
	var event events.Event
	require.NoError(t, json.Unmarshal(message.Data, &event))
	require.Empty(t, event.TenantID, "credential revocation must keep its existing global invalidation event")
	var data iamcontract.CredentialRevokedData
	require.NoError(t, json.Unmarshal(event.Data, &data))
	require.Equal(t, "synthetic-current-target", data.Subject)
	require.NoError(t, db.WithTenant(t.Context(), signoutTestTenantB, func(tx *gorm.DB) error {
		var memberships int64
		require.NoError(t, tx.Table("iam_members").Where("subject=?", "synthetic-current-target").Count(&memberships).Error)
		require.Equal(t, int64(1), memberships, "security session revocation must not delete other-tenant membership")
		return nil
	}))
	first := watermarkFor(t, db, "synthetic-current-target")
	retried := doRequest(r, http.MethodPost, "/v1/iam/subjects/synthetic-current-target/revoke", "actor", "")
	require.Equal(t, http.StatusOK, retried.Code, retried.Body.String())
	require.False(t, watermarkFor(t, db, "synthetic-current-target").Before(first), "repeated explicit security revocation cannot move the cutoff backwards")
}

func seedRevocationMember(t *testing.T, db *tenantdb.DB, tenant, subject string, role authz.Role) {
	t.Helper()
	require.NoError(t, db.WithTenant(t.Context(), tenant, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) VALUES(?,?,?)`, tenant, subject, role).Error
	}))
}

func TestAdminRevokeRechecksCurrentActingRole(t *testing.T) {
	r, db, _, _ := newSignoutHarness(t, signoutHarnessConfig{
		verifier:   fixedVerifier{"actor": {Subject: "synthetic-former-admin", TenantID: signoutTestTenantA, AuthTime: time.Now()}},
		membership: fakeMembership{"synthetic-former-admin": true},
		perms:      stubResolver{"synthetic-former-admin": {PermCredentialRevoke}},
		roles:      stubRoleLister{"synthetic-current-target": {{TenantID: signoutTestTenantA, Role: authz.RoleStaffMember}}},
	})
	seedRevocationMember(t, db, signoutTestTenantA, "synthetic-former-admin", authz.RoleStaffMember)
	seedRevocationMember(t, db, signoutTestTenantA, "synthetic-current-target", authz.RoleStaffMember)
	w := doRequest(r, http.MethodPost, "/v1/iam/subjects/synthetic-current-target/revoke", "actor", "")
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	require.True(t, watermarkFor(t, db, "synthetic-current-target").IsZero())
}

type revocationRoleLookup func(context.Context, string) ([]authz.RoleBinding, error)

func (f revocationRoleLookup) ListRoles(ctx context.Context, subject string) ([]authz.RoleBinding, error) {
	return f(ctx, subject)
}

func TestAdminRevokeRechecksActorCredentialAfterRoleLookup(t *testing.T) {
	var duringLookup func(context.Context)
	r, db, checker, _ := newSignoutHarness(t, signoutHarnessConfig{
		verifier:   fixedVerifier{"actor": {Subject: "synthetic-security-admin", TenantID: signoutTestTenantA, AuthTime: time.Now().Add(-time.Minute)}},
		membership: fakeMembership{"synthetic-security-admin": true},
		perms:      stubResolver{"synthetic-security-admin": {PermCredentialRevoke}},
		roles: revocationRoleLookup(func(ctx context.Context, _ string) ([]authz.RoleBinding, error) {
			duringLookup(ctx)
			return []authz.RoleBinding{{TenantID: signoutTestTenantA, Role: authz.RoleStaffMember}}, nil
		}),
	})
	seedRevocationMember(t, db, signoutTestTenantA, "synthetic-security-admin", authz.RoleTenantAdmin)
	seedRevocationMember(t, db, signoutTestTenantA, "synthetic-current-target", authz.RoleStaffMember)
	duringLookup = func(ctx context.Context) {
		require.NoError(t, db.WithSystem(ctx, func(tx *gorm.DB) error {
			return checker.RevokeTx(tx, "synthetic-security-admin", time.Now().UTC(), "admin_revoke", "synthetic-other-admin")
		}))
	}
	w := doRequest(r, http.MethodPost, "/v1/iam/subjects/synthetic-current-target/revoke", "actor", "")
	require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	require.True(t, watermarkFor(t, db, "synthetic-current-target").IsZero(), "a cached actor credential must be rechecked before affecting the target")
}

func TestAdminRevokeRechecksMembershipAfterRoleLookup(t *testing.T) {
	for _, tc := range []struct {
		name    string
		removed string
		status  int
	}{
		{"actor", "synthetic-security-admin", http.StatusForbidden},
		{"target", "synthetic-current-target", http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var duringLookup func(context.Context)
			r, db, _, _ := newSignoutHarness(t, signoutHarnessConfig{
				verifier:   fixedVerifier{"actor": {Subject: "synthetic-security-admin", TenantID: signoutTestTenantA, AuthTime: time.Now()}},
				membership: fakeMembership{"synthetic-security-admin": true},
				perms:      stubResolver{"synthetic-security-admin": {PermCredentialRevoke}},
				roles: revocationRoleLookup(func(ctx context.Context, _ string) ([]authz.RoleBinding, error) {
					duringLookup(ctx)
					return []authz.RoleBinding{{TenantID: signoutTestTenantA, Role: authz.RoleStaffMember}}, nil
				}),
			})
			seedRevocationMember(t, db, signoutTestTenantA, "synthetic-security-admin", authz.RoleTenantAdmin)
			seedRevocationMember(t, db, signoutTestTenantA, "synthetic-current-target", authz.RoleStaffMember)
			duringLookup = func(ctx context.Context) {
				require.NoError(t, db.WithTenant(tenantdb.WithActor(ctx, "synthetic-other-admin"), signoutTestTenantA, func(tx *gorm.DB) error {
					return tx.Exec(`DELETE FROM iam_members WHERE tenant_id=? AND subject=?`, signoutTestTenantA, tc.removed).Error
				}))
			}
			w := doRequest(r, http.MethodPost, "/v1/iam/subjects/synthetic-current-target/revoke", "actor", "")
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.True(t, watermarkFor(t, db, "synthetic-current-target").IsZero())
		})
	}
}

func TestAdminRevokeSelfDoesNotUpgradeCredentialLocks(t *testing.T) {
	r, db, _, _ := newSignoutHarness(t, signoutHarnessConfig{
		verifier:   fixedVerifier{"actor": {Subject: "synthetic-security-admin", TenantID: signoutTestTenantA, AuthTime: time.Now()}},
		membership: fakeMembership{"synthetic-security-admin": true},
		perms:      stubResolver{"synthetic-security-admin": {PermCredentialRevoke}},
		roles:      stubRoleLister{"synthetic-security-admin": {{TenantID: signoutTestTenantA, Role: authz.RoleTenantAdmin}}},
	})
	seedRevocationMember(t, db, signoutTestTenantA, "synthetic-security-admin", authz.RoleTenantAdmin)
	w := doRequest(r, http.MethodPost, "/v1/iam/subjects/synthetic-security-admin/revoke", "actor", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.False(t, watermarkFor(t, db, "synthetic-security-admin").IsZero())
	require.Equal(t, http.StatusUnauthorized, doRequest(r, http.MethodGet, "/v1/iam/me/permissions", "actor", "").Code)
}

func TestAdminReciprocalRevocationSerializesWithoutDeadlock(t *testing.T) {
	arrived, release := make(chan struct{}, 2), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	r, db, _, _ := newSignoutHarness(t, signoutHarnessConfig{
		verifier: fixedVerifier{
			"a": {Subject: "synthetic-admin-a", TenantID: signoutTestTenantA, AuthTime: time.Now().Add(-time.Minute)},
			"b": {Subject: "synthetic-admin-b", TenantID: signoutTestTenantA, AuthTime: time.Now().Add(-time.Minute)},
		},
		membership: fakeMembership{"synthetic-admin-a": true, "synthetic-admin-b": true},
		perms:      stubResolver{"synthetic-admin-a": {PermCredentialRevoke}, "synthetic-admin-b": {PermCredentialRevoke}},
		roles: revocationRoleLookup(func(ctx context.Context, _ string) ([]authz.RoleBinding, error) {
			arrived <- struct{}{}
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return []authz.RoleBinding{{TenantID: signoutTestTenantA, Role: authz.RoleTenantAdmin}}, nil
		}),
	})
	t.Cleanup(unblock)
	for _, subject := range []string{"synthetic-admin-a", "synthetic-admin-b"} {
		seedRevocationMember(t, db, signoutTestTenantA, subject, authz.RoleTenantAdmin)
	}
	finished := make(chan int, 2)
	go func() {
		finished <- doRequest(r, http.MethodPost, "/v1/iam/subjects/synthetic-admin-b/revoke", "a", "").Code
	}()
	go func() {
		finished <- doRequest(r, http.MethodPost, "/v1/iam/subjects/synthetic-admin-a/revoke", "b", "").Code
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	for range 2 {
		select {
		case <-arrived:
		case <-ctx.Done():
			t.Fatal("both requests must pass initial authentication before the race")
		}
	}
	unblock()
	var statuses []int
	for range 2 {
		select {
		case status := <-finished:
			statuses = append(statuses, status)
		case <-ctx.Done():
			t.Fatal("reciprocal revocation did not finish")
		}
	}
	slices.Sort(statuses)
	require.Equal(t, []int{http.StatusOK, http.StatusUnauthorized}, statuses, "only the first current credential may commit; the second must observe its revocation")
}

func TestAdminRevokeReturnsUnavailableWhenIAMSourceClosesDuringLookup(t *testing.T) {
	var duringLookup func()
	r, db, _, _ := newSignoutHarness(t, signoutHarnessConfig{
		verifier:   fixedVerifier{"actor": {Subject: "synthetic-security-admin", TenantID: signoutTestTenantA, AuthTime: time.Now()}},
		membership: fakeMembership{"synthetic-security-admin": true},
		perms:      stubResolver{"synthetic-security-admin": {PermCredentialRevoke}},
		roles: revocationRoleLookup(func(context.Context, string) ([]authz.RoleBinding, error) {
			duringLookup()
			return []authz.RoleBinding{{TenantID: signoutTestTenantA, Role: authz.RoleStaffMember}}, nil
		}),
	})
	seedRevocationMember(t, db, signoutTestTenantA, "synthetic-security-admin", authz.RoleTenantAdmin)
	seedRevocationMember(t, db, signoutTestTenantA, "synthetic-current-target", authz.RoleStaffMember)
	duringLookup = func() { require.NoError(t, db.Close()) }
	w := doRequest(r, http.MethodPost, "/v1/iam/subjects/synthetic-current-target/revoke", "actor", "")
	require.Equal(t, http.StatusServiceUnavailable, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"error":"authz_unavailable"`)
}

func TestAdminRevokeRechecksAuthorityAfterCredentialLockWait(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
	}{
		{"actor_credential", http.StatusUnauthorized},
		{"actor_membership", http.StatusForbidden},
		{"target_membership", http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, db, checker, _ := newSignoutHarness(t, signoutHarnessConfig{
				verifier:   fixedVerifier{"actor": {Subject: "synthetic-security-admin", TenantID: signoutTestTenantA, AuthTime: time.Now().Add(-time.Minute)}},
				membership: fakeMembership{"synthetic-security-admin": true},
				perms:      stubResolver{"synthetic-security-admin": {PermCredentialRevoke}},
				roles:      stubRoleLister{"synthetic-current-target": {{TenantID: signoutTestTenantA, Role: authz.RoleStaffMember}}},
			})
			seedRevocationMember(t, db, signoutTestTenantA, "synthetic-security-admin", authz.RoleTenantAdmin)
			seedRevocationMember(t, db, signoutTestTenantA, "synthetic-current-target", authz.RoleStaffMember)
			require.Equal(t, http.StatusOK, doRequest(r, http.MethodGet, "/v1/iam/me/permissions", "actor", "").Code)
			locked, release := make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			t.Cleanup(unblock)
			blocker := make(chan error, 1)
			key := "synthetic-current-target"
			if tc.name == "actor_credential" {
				key = "synthetic-security-admin"
			}
			go func() {
				blocker <- db.WithSystem(t.Context(), func(tx *gorm.DB) error {
					if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended('iam.credential:' || ?,0))`, key).Error; err != nil {
						return err
					}
					if tc.name == "actor_credential" {
						if err := checker.RevokeTx(tx, "synthetic-security-admin", time.Now().UTC(), "admin_revoke", "synthetic-other-admin"); err != nil {
							return err
						}
					}
					close(locked)
					select {
					case <-release:
						return nil
					case <-t.Context().Done():
						return t.Context().Err()
					}
				})
			}()
			select {
			case <-locked:
			case err := <-blocker:
				t.Fatalf("credential blocker failed: %v", err)
			case <-time.After(5 * time.Second):
				t.Fatal("credential blocker did not start")
			}
			finished := make(chan int, 1)
			go func() {
				finished <- doRequest(r, http.MethodPost, "/v1/iam/subjects/synthetic-current-target/revoke", "actor", "").Code
			}()
			require.Eventually(t, func() bool {
				var waiting int64
				err := db.WithSystem(t.Context(), func(tx *gorm.DB) error {
					return tx.Raw(`WITH credential_key AS (SELECT hashtextextended('iam.credential:' || ?,0) AS value)
						SELECT count(*) FROM pg_locks l,credential_key k WHERE l.locktype='advisory' AND NOT l.granted
						AND l.database=(SELECT oid FROM pg_database WHERE datname=current_database())
						AND l.classid=((k.value>>32)&4294967295)::oid AND l.objid=(k.value&4294967295)::oid AND l.objsubid=1`, key).Scan(&waiting).Error
				})
				return err == nil && waiting > 0
			}, 3*time.Second, 10*time.Millisecond, "the command must actually wait on the held credential key")
			if tc.name != "actor_credential" {
				removed := "synthetic-current-target"
				if tc.name == "actor_membership" {
					removed = "synthetic-security-admin"
				}
				require.NoError(t, db.WithTenant(tenantdb.WithActor(t.Context(), "synthetic-other-admin"), signoutTestTenantA, func(tx *gorm.DB) error {
					return tx.Exec(`DELETE FROM iam_members WHERE tenant_id=? AND subject=?`, signoutTestTenantA, removed).Error
				}))
			}
			unblock()
			require.NoError(t, <-blocker)
			select {
			case status := <-finished:
				require.Equal(t, tc.status, status)
			case <-time.After(5 * time.Second):
				t.Fatal("revocation did not finish after releasing its lock")
			}
			require.True(t, watermarkFor(t, db, "synthetic-current-target").IsZero())
		})
	}
}

func TestAdminRevokeRejectsChangedWorkspaceBeforeLookup(t *testing.T) {
	called := false
	r, db, _, _ := newSignoutHarness(t, signoutHarnessConfig{
		verifier:   fixedVerifier{"actor": {Subject: "synthetic-security-admin", TenantID: signoutTestTenantA, AuthTime: time.Now()}},
		membership: fakeMembership{"synthetic-security-admin": true},
		perms:      stubResolver{"synthetic-security-admin": {PermCredentialRevoke}},
		roles: revocationRoleLookup(func(context.Context, string) ([]authz.RoleBinding, error) {
			called = true
			return []authz.RoleBinding{{TenantID: signoutTestTenantA, Role: authz.RoleStaffMember}}, nil
		}),
	})
	seedRevocationMember(t, db, signoutTestTenantA, "synthetic-security-admin", authz.RoleTenantAdmin)
	seedRevocationMember(t, db, signoutTestTenantA, "synthetic-current-target", authz.RoleStaffMember)
	request := httptest.NewRequest(http.MethodPost, "/v1/iam/subjects/synthetic-current-target/revoke", nil)
	request.Header.Set("Authorization", "Bearer actor")
	request.Header.Set("X-Helivanta-Expected-Tenant", signoutTestTenantB)
	response := httptest.NewRecorder()
	r.ServeHTTP(response, request)
	require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), `"error":"session_changed"`)
	require.False(t, called)
	require.True(t, watermarkFor(t, db, "synthetic-current-target").IsZero())
}

func TestAdminRevokeRollsBackWatermarkWhenOutboxInsertFails(t *testing.T) {
	r, db, _, _ := newSignoutHarness(t, signoutHarnessConfig{
		verifier:   fixedVerifier{"actor": {Subject: "synthetic-security-admin", TenantID: signoutTestTenantA, AuthTime: time.Now()}},
		membership: fakeMembership{"synthetic-security-admin": true},
		perms:      stubResolver{"synthetic-security-admin": {PermCredentialRevoke}},
		roles:      stubRoleLister{"synthetic-current-target": {{TenantID: signoutTestTenantA, Role: authz.RoleStaffMember}}},
	})
	seedRevocationMember(t, db, signoutTestTenantA, "synthetic-security-admin", authz.RoleTenantAdmin)
	seedRevocationMember(t, db, signoutTestTenantA, "synthetic-current-target", authz.RoleStaffMember)
	// Tighten the isolated fixture only: make the real outbox INSERT fail after the watermark write.
	require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{
		ID:  "9000_iam_test_outbox_failure",
		SQL: `ALTER TABLE outbox_events ADD CONSTRAINT synthetic_publish_failure CHECK (false) NOT VALID;`,
	}}))
	w := doRequest(r, http.MethodPost, "/v1/iam/subjects/synthetic-current-target/revoke", "actor", "")
	require.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
	require.True(t, watermarkFor(t, db, "synthetic-current-target").IsZero(), "the real watermark and event must roll back together")
}
