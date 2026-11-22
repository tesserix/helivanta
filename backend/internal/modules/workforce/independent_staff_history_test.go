package workforce_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/workforce" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func TestIndependentStaffHistoryReview(t *testing.T) {
	h := newRequestHarness(t, func(opts *testutil.HarnessOptions) {
		opts.Perms["admin"] = append(opts.Perms["admin"], workforce.PermIndependentLoginManage)
		opts.Perms["profiler"] = append(opts.Perms["profiler"], workforce.PermIndependentLoginReview)
	})
	owner := &establishmentHarness{db: h.db}
	employer := owner.pharmacy(t, "independent")
	owner.grantPharmacyAccess(t, employer)
	require.NoError(t, owner.tx(t, employer, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO workforce_hospital_access(tenant_id,hospital_id,subject,level)VALUES(?,?,'user-profiler','manager')`, testutil.TenantA, employer).Error
	}))
	root := "/v1/workforce/pharmacies/" + employer
	call := func(method, path, actor string, body any, status int) {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		req := httptest.NewRequest(method, path, strings.NewReader(string(raw)))
		req.Header.Set("Authorization", "Bearer "+actor)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Helivanta-Expected-Tenant", testutil.TenantA)
		req.Header.Set("X-Helivanta-Expected-Subject", "user-"+actor)
		response := httptest.NewRecorder()
		h.handler.ServeHTTP(response, req)
		require.Equal(t, status, response.Code, response.Body.String())
	}
	create := func(code, subject string) authz.IndependentStaffReference {
		staff, link := uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7())
		path := root + "/staff/" + staff.String()
		call("PUT", path+"?directory_context=v1", "admin", map[string]any{"employee_code": code, "display_name": "TEST " + code, "job_category": "pharmacist", "employment_status": "active", "expected_version": 0, "directory_context_version": "v1"}, 200)
		call("PUT", path+"/account-links/"+link.String(), "admin", map[string]any{"subject": subject, "expected_staff_version": 1, "reason": "TEST retained source identity"}, 201)
		call("PUT", path+"/account-links/"+link.String()+"/commands/"+uuid.Must(uuid.NewV7()).String(), "profiler", map[string]any{"expected_version": 1, "decision": "approve", "reason": "TEST independent identity review"}, 201)
		return authz.IndependentStaffReference{StaffID: staff, StaffVersion: 1, LinkID: link, LinkVersion: 2}
	}
	participant := authz.IndependentStaffObservation{Subject: "user-clerk", Reference: create("TEST-HISTORY-COUNTER", "user-clerk")}
	reviewer := create("TEST-HISTORY-REVIEWER", "user-reader")
	guard, ok := workforce.New().IndependentStaffIdentities(h.db).(authz.IndependentStaffHistoryGuard)
	require.True(t, ok)
	scope := authz.IndependentStaffScope{TenantID: testutil.TenantA, EmployerKind: "pharmacy", EmployerID: uuid.MustParse(employer), Subject: "user-reader"}
	run := func(observation authz.IndependentStaffObservation, action func(context.Context) error) error {
		ctx := tenantdb.WithActor(t.Context(), scope.Subject)
		return h.db.WithGuardedTenantCommand(ctx, testutil.TenantA, time.Now().Add(5*time.Second), func(*gorm.DB) error { return nil }, func(commandCtx context.Context) error {
			return guard.WithIndependentStaffHistoryReview(commandCtx, scope, reviewer, observation, action)
		})
	}
	calls := 0
	accept := func(context.Context) error { calls++; return nil }
	require.NoError(t, run(participant, accept))
	require.Equal(t, 1, calls)
	linkPath := root + "/staff/" + participant.Reference.StaffID.String() + "/account-links/" + participant.Reference.LinkID.String()
	call("PUT", linkPath+"/commands/"+uuid.Must(uuid.NewV7()).String(), "admin", map[string]any{"expected_version": 2, "decision": "revoke", "reason": "TEST old account retired after an accepted observation"}, 201)
	require.NoError(t, run(participant, accept))
	require.Equal(t, 2, calls)
	require.ErrorIs(t, run(authz.IndependentStaffObservation{Subject: scope.Subject, Reference: reviewer}, accept), authz.ErrScopedCommandDenied)
	changed := participant
	changed.Reference.LinkVersion = 3
	require.ErrorIs(t, run(changed, accept), authz.ErrScopedCommandDenied)
	changed = participant
	changed.Reference.StaffID = uuid.Must(uuid.NewV7())
	require.ErrorIs(t, run(changed, accept), authz.ErrScopedCommandDenied)
	require.Equal(t, 2, calls)
	require.ErrorIs(t, guard.WithIndependentStaffHistoryReview(tenantdb.WithActor(t.Context(), scope.Subject), scope, reviewer, participant, accept), authz.ErrScopedAuthorizationUnavailable)
	audits := func() int64 {
		var count int64
		require.NoError(t, owner.tx(t, employer, func(tx *gorm.DB) error {
			return tx.Table("workforce_audit").Where("tenant_id=? AND hospital_id=? AND action='workforce.independent_staff.review_history'", testutil.TenantA, employer).Count(&count).Error
		}))
		return count
	}
	require.EqualValues(t, 2, audits())
	failure := errors.New("TEST downstream command rolled back")
	require.ErrorIs(t, run(participant, func(context.Context) error { return failure }), failure)
	require.EqualValues(t, 2, audits())

	// A current different Staff row cannot erase the reviewer's known historical account alias.
	reviewerPath := root + "/staff/" + reviewer.StaffID.String()
	call("PUT", reviewerPath+"/account-links/"+reviewer.LinkID.String()+"/commands/"+uuid.Must(uuid.NewV7()).String(), "admin", map[string]any{"expected_version": 2, "decision": "revoke", "reason": "TEST retire current reviewer account before source rotation"}, 201)
	alias := uuid.Must(uuid.NewV7())
	participantPath := root + "/staff/" + participant.Reference.StaffID.String()
	call("PUT", participantPath+"/account-links/"+alias.String(), "admin", map[string]any{"subject": scope.Subject, "expected_staff_version": 1, "reason": "TEST reviewer account previously identified this participant"}, 201)
	call("PUT", participantPath+"/account-links/"+alias.String()+"/commands/"+uuid.Must(uuid.NewV7()).String(), "profiler", map[string]any{"expected_version": 1, "decision": "approve", "reason": "TEST reviewed historical alias"}, 201)
	call("PUT", participantPath+"/account-links/"+alias.String()+"/commands/"+uuid.Must(uuid.NewV7()).String(), "admin", map[string]any{"expected_version": 2, "decision": "revoke", "reason": "TEST retain historical alias after retirement"}, 201)
	reviewer.LinkID = uuid.Must(uuid.NewV7())
	call("PUT", reviewerPath+"/account-links/"+reviewer.LinkID.String(), "admin", map[string]any{"subject": scope.Subject, "expected_staff_version": 1, "reason": "TEST new current Staff link for the same account"}, 201)
	call("PUT", reviewerPath+"/account-links/"+reviewer.LinkID.String()+"/commands/"+uuid.Must(uuid.NewV7()).String(), "profiler", map[string]any{"expected_version": 1, "decision": "approve", "reason": "TEST independently verify the new current source"}, 201)
	require.ErrorIs(t, run(participant, accept), authz.ErrScopedCommandDenied)
	require.Equal(t, 2, calls)
	require.EqualValues(t, 2, audits())
}
