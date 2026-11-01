package iam_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/iam" //nolint:depguard // External test exercises its own module.
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func TestFirstMembershipEnrollmentPersistsOrdinaryMembershipAndExactReceipt(t *testing.T) {
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"admin": testutil.TenantA}, Perms: map[string][]authz.Permission{"admin": {iam.PermMemberManage}}, Modules: []platform.Module{iam.New(nil).SetMembershipIdentityReader(membershipIdentity{})},
	})
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) VALUES(?,'user-admin','tenant_admin')`, testutil.TenantA).Error
	}))
	id := uuid.Must(uuid.NewV7())
	path := "/v1/iam/membership-enrollments/" + id.String()
	body := `{"subject":"synthetic-first-enrollment","reason":"Enroll verified account without operational authority"}`
	first := membershipCommandRequest(r, http.MethodPut, path, "admin", body)
	require.Equal(t, http.StatusAccepted, first.Code, first.Body.String())
	var receipt struct {
		ID              uuid.UUID
		Subject         string
		RoleKey         string    `json:"role_key"`
		MembershipID    uuid.UUID `json:"membership_id"`
		MembershipEpoch int64     `json:"membership_epoch"`
		ActorSubject    string    `json:"actor_subject"`
	}
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &receipt))
	require.Equal(t, id, receipt.ID)
	require.Equal(t, "synthetic-first-enrollment", receipt.Subject)
	require.Equal(t, "staff_member", receipt.RoleKey)
	require.EqualValues(t, 1, receipt.MembershipEpoch)
	require.Equal(t, "user-admin", receipt.ActorSubject)
	require.Equal(t, uuid.Version(7), receipt.MembershipID.Version())
	replay := membershipCommandRequest(r, http.MethodPut, path, "admin", body)
	require.Equal(t, http.StatusAccepted, replay.Code, replay.Body.String())
	require.JSONEq(t, first.Body.String(), replay.Body.String())
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		require.NoError(t, tx.Table("iam_members").Where("subject=? AND role_key='staff_member'", receipt.Subject).Count(&count).Error)
		require.EqualValues(t, 1, count)
		require.NoError(t, tx.Table("iam_membership_enrollments").Count(&count).Error)
		require.EqualValues(t, 1, count)
		require.NoError(t, tx.Table("outbox_events").Count(&count).Error)
		require.EqualValues(t, 1, count)
		return nil
	}))
}

type enrollmentIdentity struct {
	calls atomic.Int32
	check func(context.Context, string) (bool, error)
}

func (p *enrollmentIdentity) MembershipEligible(ctx context.Context, subject string) (bool, error) {
	p.calls.Add(1)
	if p.check != nil {
		return p.check(ctx, subject)
	}
	return true, nil
}
func enrollmentHarness(t *testing.T, identity iam.MembershipIdentityReader, failOutbox ...bool) (*gin.Engine, *tenantdb.DB, context.Context) {
	t.Helper()
	tokens := map[string]string{"admin": testutil.TenantA, "other-admin": testutil.TenantA, "stale-admin": testutil.TenantA, "restricted": testutil.TenantA, "foreign-admin": testutil.TenantB}
	perms := map[string][]authz.Permission{}
	for _, key := range []string{"admin", "other-admin", "stale-admin", "foreign-admin"} {
		perms[key] = []authz.Permission{iam.PermMemberManage}
	}
	base := iam.New(nil).SetMembershipIdentityReader(identity)
	var module platform.Module = base
	if len(failOutbox) > 0 && failOutbox[0] {
		module = membershipOutboxFailureModule{base}
	}
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{Tokens: tokens, Perms: perms, Modules: []platform.Module{module}})
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) VALUES(?,'user-admin','tenant_admin'),(?,'user-other-admin','tenant_admin')`, testutil.TenantA, testutil.TenantA).Error
	}))
	require.NoError(t, db.WithTenant(ctx, testutil.TenantB, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO iam_members(tenant_id,subject,role_key) VALUES(?,'user-foreign-admin','tenant_admin')`, testutil.TenantB).Error
	}))
	return r, db, ctx
}
func TestFirstEnrollmentRefusesInvalidIntentAndStaleActorsBeforeProviderRead(t *testing.T) {
	identity := &enrollmentIdentity{}
	r, db, ctx := enrollmentHarness(t, identity)
	valid := `{"subject":"synthetic-validation-target","reason":"Verified first enrollment"}`
	for _, tc := range []struct {
		name, body, token, expectedTenant, expectedSubject         string
		omitTenant, omitSubject, duplicateTenant, duplicateSubject bool
		status                                                     int
	}{
		{name: "missing_reason", body: `{"subject":"synthetic-validation-target"}`, status: 400},
		{name: "blank_reason", body: `{"subject":"synthetic-validation-target","reason":" \t"}`, status: 400},
		{name: "privileged_input", body: `{"subject":"synthetic-validation-target","reason":"No privilege","role_key":"tenant_admin"}`, status: 400},
		{name: "body_actor", body: `{"subject":"synthetic-validation-target","reason":"No actor override","actor_subject":"user-other-admin"}`, status: 400},
		{name: "bad_subject", body: `{"subject":"someone#staff","reason":"No userset input"}`, status: 400},
		{name: "second_document", body: valid + `{}`, status: 400},
		{name: "too_large", body: `{"subject":"synthetic-validation-target","reason":"` + strings.Repeat("x", 5000) + `"}`, status: 400},
		{name: "long_reason", body: `{"subject":"synthetic-validation-target","reason":"` + strings.Repeat("x", 501) + `"}`, status: 400},
		{name: "removed_source_admin", token: "stale-admin", status: 403},
		{name: "restricted", token: "restricted", status: 403},
		{name: "old_account", expectedSubject: "user-previous-admin", status: 409},
		{name: "old_tenant", expectedTenant: testutil.TenantB, status: 409},
		{name: "missing_account", omitSubject: true, status: 409},
		{name: "missing_tenant", omitTenant: true, status: 409},
		{name: "duplicate_account", duplicateSubject: true, status: 409},
		{name: "duplicate_tenant", duplicateTenant: true, status: 409},
	} {
		t.Run(tc.name, func(t *testing.T) {
			token := tc.token
			if token == "" {
				token = "admin"
			}
			body := tc.body
			if body == "" {
				body = valid
			}
			req := httptest.NewRequest(http.MethodPut, "/v1/iam/membership-enrollments/"+uuid.Must(uuid.NewV7()).String(), strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "application/json")
			if !tc.omitSubject {
				subject := tc.expectedSubject
				if subject == "" {
					subject = "user-" + token
				}
				req.Header.Set("X-Helivanta-Expected-Subject", subject)
			}
			if !tc.omitTenant {
				tenant := tc.expectedTenant
				if tenant == "" {
					tenant = testutil.TenantA
				}
				req.Header.Set("X-Helivanta-Expected-Tenant", tenant)
			}
			if tc.duplicateSubject {
				req.Header.Add("X-Helivanta-Expected-Subject", "user-"+token)
			}
			if tc.duplicateTenant {
				req.Header.Add("X-Helivanta-Expected-Tenant", testutil.TenantA)
			}
			res := httptest.NewRecorder()
			r.ServeHTTP(res, req)
			require.Equal(t, tc.status, res.Code, res.Body.String())
		})
	}
	require.Zero(t, identity.calls.Load())
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		require.NoError(t, tx.Table("iam_membership_enrollments").Count(&count).Error)
		require.Zero(t, count)
		return nil
	}))
}
func TestFirstEnrollmentHistoryRetainsEvidenceWithoutReopeningMembership(t *testing.T) {
	identity := &enrollmentIdentity{}
	r, db, ctx := enrollmentHarness(t, identity)
	id := uuid.Must(uuid.NewV7())
	path := "/v1/iam/membership-enrollments/" + id.String()
	body := `{"subject":"synthetic-history-target","reason":"Verified historical first enrollment"}`
	first := membershipCommandRequest(r, http.MethodPut, path, "admin", body)
	require.Equal(t, 202, first.Code, first.Body.String())
	var receipt iam.MembershipEnrollment
	require.NoError(t, json.Unmarshal(first.Body.Bytes(), &receipt))
	for _, tc := range []struct{ token, body string }{{"admin", strings.Replace(body, "Verified historical", "Changed", 1)}, {"other-admin", body}} {
		res := membershipCommandRequest(r, http.MethodPut, path, tc.token, tc.body)
		require.Equal(t, 409, res.Code, res.Body.String())
	}
	received := membershipCommandRequest(r, http.MethodGet, path, "other-admin", "")
	require.Equal(t, 200, received.Code, received.Body.String())
	require.JSONEq(t, first.Body.String(), received.Body.String())
	missing := membershipCommandRequest(r, http.MethodGet, path, "foreign-admin", "")
	require.Equal(t, 404, missing.Code, missing.Body.String())
	history := membershipCommandRequest(r, http.MethodGet, "/v1/iam/membership-enrollments?limit=1", "admin", "")
	require.Equal(t, 200, history.Code, history.Body.String())
	require.Contains(t, history.Body.String(), receipt.Subject)
	foreign := membershipCommandRequest(r, http.MethodGet, "/v1/iam/membership-enrollments?limit=1", "foreign-admin", "")
	require.Equal(t, 200, foreign.Code, foreign.Body.String())
	require.NotContains(t, foreign.Body.String(), receipt.Subject)
	removeID := uuid.Must(uuid.NewV7())
	remove := fmt.Sprintf(`{"operation":"remove","basis_id":%q,"subject":%q,"role_key":"staff_member","expected_membership_epoch":1,"reason":"End ordinary membership with retained evidence"}`, receipt.MembershipID, receipt.Subject)
	removed := membershipCommandRequest(r, http.MethodPut, "/v1/iam/membership-commands/"+removeID.String(), "admin", remove)
	require.Equal(t, 202, removed.Code, removed.Body.String())
	identity.check = func(context.Context, string) (bool, error) { return false, errors.New("synthetic provider outage") }
	replay := membershipCommandRequest(r, http.MethodPut, path, "admin", body)
	require.Equal(t, 202, replay.Code, replay.Body.String())
	require.JSONEq(t, first.Body.String(), replay.Body.String())
	another := membershipCommandRequest(r, http.MethodPut, "/v1/iam/membership-enrollments/"+uuid.Must(uuid.NewV7()).String(), "admin", body)
	require.Equal(t, 409, another.Code, another.Body.String())
	require.EqualValues(t, 1, identity.calls.Load())
	for _, sql := range []string{`UPDATE iam_membership_enrollments SET reason='Changed'`, `DELETE FROM iam_membership_enrollments`, `TRUNCATE iam_membership_enrollments`} {
		err := db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error { return tx.Exec(sql).Error })
		require.Error(t, err)
	}
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		require.NoError(t, tx.Table("iam_members").Where("subject=?", receipt.Subject).Count(&count).Error)
		require.Zero(t, count)
		require.NoError(t, tx.Table("iam_membership_enrollments").Count(&count).Error)
		require.EqualValues(t, 1, count)
		require.NoError(t, tx.Table("audit_events").Where("action IN ('membership_enrollment_read','membership_enrollments_read')").Count(&count).Error)
		require.GreaterOrEqual(t, count, int64(3))
		return nil
	}))
}
func TestFirstEnrollmentProviderFailureAndOutboxFailureLeaveNoSourceOrReceipt(t *testing.T) {
	identity := &enrollmentIdentity{check: func(context.Context, string) (bool, error) { return false, nil }}
	r, db, ctx := enrollmentHarness(t, identity)
	path := "/v1/iam/membership-enrollments/" + uuid.Must(uuid.NewV7()).String()
	body := `{"subject":"synthetic-atomic-enrollment","reason":"Verified atomic enrollment"}`
	refusal := membershipCommandRequest(r, http.MethodPut, path, "admin", body)
	require.Equal(t, 409, refusal.Code, refusal.Body.String())
	identity.check = func(context.Context, string) (bool, error) { return false, errors.New("synthetic provider outage") }
	outage := membershipCommandRequest(r, http.MethodPut, path, "admin", body)
	require.Equal(t, 503, outage.Code, outage.Body.String())
	identity.check = nil
	success := membershipCommandRequest(r, http.MethodPut, path, "admin", body)
	require.Equal(t, 202, success.Code, success.Body.String())
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		require.NoError(t, tx.Table("iam_membership_enrollments").Count(&count).Error)
		require.EqualValues(t, 1, count)
		return nil
	}))
}
func TestFirstEnrollmentOutboxFailureRollsBackSourceReceiptEpochAndAudit(t *testing.T) {
	r, db, ctx := enrollmentHarness(t, &enrollmentIdentity{}, true)
	path := "/v1/iam/membership-enrollments/" + uuid.Must(uuid.NewV7()).String()
	body := `{"subject":"synthetic-atomic-enrollment","reason":"Verified atomic enrollment"}`
	failed := membershipCommandRequest(r, http.MethodPut, path, "admin", body)
	require.Equal(t, 500, failed.Code, failed.Body.String())
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		for _, table := range []string{"iam_membership_enrollments", "outbox_events"} {
			var count int64
			require.NoError(t, tx.Table(table).Count(&count).Error)
			require.Zero(t, count)
		}
		var count int64
		require.NoError(t, tx.Table("iam_members").Where("subject='synthetic-atomic-enrollment'").Count(&count).Error)
		require.Zero(t, count)
		require.NoError(t, tx.Table("iam_subject_membership_epochs").Where("subject='synthetic-atomic-enrollment'").Count(&count).Error)
		require.Zero(t, count)
		require.NoError(t, tx.Table("audit_events").Where("action='membership_enroll'").Count(&count).Error)
		require.Zero(t, count)
		return nil
	}))
	replay := membershipCommandRequest(r, http.MethodPut, path, "admin", body)
	require.Equal(t, 500, replay.Code, replay.Body.String())
}
func TestFirstEnrollmentRechecksActorAfterProviderAndSerializesConcurrentCommands(t *testing.T) {
	identity := &enrollmentIdentity{}
	r, db, ctx := enrollmentHarness(t, identity)
	identity.check = func(context.Context, string) (bool, error) {
		return true, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Exec(`SELECT set_config('app.actor_subject','synthetic-fixture',true);DELETE FROM iam_members WHERE subject='user-admin' AND role_key='tenant_admin'`).Error
		})
	}
	refused := membershipCommandRequest(r, http.MethodPut, "/v1/iam/membership-enrollments/"+uuid.Must(uuid.NewV7()).String(), "admin", `{"subject":"synthetic-recheck-target","reason":"No stale administrator write"}`)
	require.Equal(t, 403, refused.Code, refused.Body.String())
	identity.check = nil
	body := `{"subject":"synthetic-concurrent-enrollment","reason":"One first enrollment under concurrent requests"}`
	path := "/v1/iam/membership-enrollments/" + uuid.Must(uuid.NewV7()).String()
	var wg sync.WaitGroup
	results := make(chan *httptest.ResponseRecorder, 2)
	for range 2 {
		wg.Go(func() { results <- membershipCommandRequest(r, http.MethodPut, path, "other-admin", body) })
	}
	wg.Wait()
	close(results)
	var first string
	for res := range results {
		require.Equal(t, 202, res.Code, res.Body.String())
		if first == "" {
			first = res.Body.String()
		} else {
			require.JSONEq(t, first, res.Body.String())
		}
	}
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		require.NoError(t, tx.Table("iam_membership_enrollments").Count(&count).Error)
		require.EqualValues(t, 1, count)
		return nil
	}))
}

func TestFirstEnrollmentCannotManufactureReceiptForExistingSource(t *testing.T) {
	r, db, ctx := enrollmentHarness(t, &enrollmentIdentity{})
	memberID := uuid.Must(uuid.NewV7())
	commandID := uuid.Must(uuid.NewV7())
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO iam_members(id,tenant_id,subject,role_key) VALUES(?,?,'synthetic-legacy-first','staff_member')`, memberID, testutil.TenantA).Error
	}))
	err := db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.actor_subject','user-admin',true),set_config('app.membership_enrollment',?,true)`, commandID.String()).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO iam_membership_enrollments(tenant_id,id,subject,reason,membership_id,role_key,membership_epoch,actor_subject) VALUES(?,?,'synthetic-legacy-first','Manufactured intent',?,'staff_member',1,'user-admin')`, testutil.TenantA, commandID, memberID).Error
	})
	require.ErrorContains(t, err, "attributed membership transition")
	res := membershipCommandRequest(r, http.MethodPut, "/v1/iam/membership-enrollments/"+commandID.String(), "admin", `{"subject":"synthetic-legacy-first","reason":"Cannot retrofit first-enrollment intent"}`)
	require.Equal(t, 409, res.Code, res.Body.String())
}

func TestFirstEnrollmentWaitsForActorRowThenRejectsWithdrawalBeforeWrite(t *testing.T) {
	identity := &enrollmentIdentity{}
	r, db, ctx := enrollmentHarness(t, identity)
	providerStarted := make(chan struct{})
	locked := make(chan struct{})
	release := make(chan struct{})
	holderDone := make(chan error, 1)
	unlock := sync.OnceFunc(func() { close(release) })
	defer unlock()
	var holderPID atomic.Int64
	go func() {
		<-providerStarted
		holderDone <- db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			var pid int64
			if err := tx.Raw(`SELECT pg_backend_pid()`).Scan(&pid).Error; err != nil {
				return err
			}
			holderPID.Store(pid)
			if err := tx.Exec(`SELECT id FROM iam_members WHERE subject='user-admin' AND role_key='tenant_admin' FOR UPDATE`).Error; err != nil {
				return err
			}
			close(locked)
			<-release
			if err := tx.Exec(`SELECT set_config('app.actor_subject','synthetic-fixture',true)`).Error; err != nil {
				return err
			}
			return tx.Exec(`DELETE FROM iam_members WHERE subject='user-admin' AND role_key='tenant_admin'`).Error
		})
	}()
	identity.check = func(context.Context, string) (bool, error) {
		close(providerStarted)
		select {
		case <-locked:
			return true, nil
		case <-time.After(time.Second):
			return false, errors.New("fixture could not obtain actor lock")
		}
	}
	response := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		response <- membershipCommandRequest(r, http.MethodPut, "/v1/iam/membership-enrollments/"+uuid.Must(uuid.NewV7()).String(), "admin", `{"subject":"synthetic-lock-wait-target","reason":"Recheck administrator after an actual lock wait"}`)
	}()
	select {
	case <-locked:
	case err := <-holderDone:
		t.Fatalf("actor lock fixture failed: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("provider/lock fixture did not start")
	}
	require.Eventually(t, func() bool {
		var blocked bool
		err := db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE ?=ANY(pg_blocking_pids(pid)))`, holderPID.Load()).Scan(&blocked).Error
		})
		return err == nil && blocked
	}, 2*time.Second, 10*time.Millisecond, "the write must actually wait on the observed actor lock")
	unlock()
	require.NoError(t, <-holderDone)
	select {
	case res := <-response:
		require.Equal(t, 403, res.Code, res.Body.String())
	case <-time.After(3 * time.Second):
		t.Fatal("enrollment did not finish after actor withdrawal")
	}
	require.EqualValues(t, 1, identity.calls.Load())
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		var count int64
		require.NoError(t, tx.Table("iam_membership_enrollments").Count(&count).Error)
		require.Zero(t, count)
		require.NoError(t, tx.Table("iam_members").Where("subject='synthetic-lock-wait-target'").Count(&count).Error)
		require.Zero(t, count)
		require.NoError(t, tx.Table("outbox_events").Count(&count).Error)
		require.Zero(t, count)
		return nil
	}))
}
