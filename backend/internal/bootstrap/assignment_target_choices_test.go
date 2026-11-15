package bootstrap_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/modules/iam"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"github.com/tesserix/onboarding-service/api/customercontract"
)

func exerciseGovernedAssignmentTargetChoices(t *testing.T, router *gin.Engine, db *tenantdb.DB, hospital, otherHospital, staff uuid.UUID, identity *governanceAssignmentIdentityReader) {
	t.Helper()
	path := "/v1/iam/hospitals/" + hospital.String() + "/assignment-target-choices"
	target := iam.AssignmentTargetChoice{HospitalID: hospital, StaffID: staff, Subject: "user-ordinary", Name: "Synthetic ordinary colleague", Email: "synthetic-ordinary@example.invalid"}
	var reads atomic.Int64
	eligible := governanceAssignmentIdentityReader(func(_ context.Context, tenant, subject string) (*customercontract.AssignmentCandidateIdentity, error) {
		if tenant != testutil.TenantA || subject != target.Subject {
			return nil, fmt.Errorf("unexpected identity selection")
		}
		reads.Add(1)
		return &customercontract.AssignmentCandidateIdentity{DisplayName: target.Name, Email: target.Email}, nil
	})
	*identity = eligible
	get := func(t *testing.T, url, actor string, status int) []iam.AssignmentTargetChoice {
		t.Helper()
		request := httptest.NewRequest(http.MethodGet, url, nil)
		request.Header.Set("Authorization", "Bearer "+actor)
		tenant := testutil.TenantA
		if actor == "other" {
			tenant = testutil.TenantB
		}
		request.Header.Set("X-Helivanta-Expected-Tenant", tenant)
		request.Header.Set("X-Helivanta-Expected-Subject", "user-"+actor)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		require.Equal(t, status, response.Code, response.Body.String())
		if status != http.StatusOK {
			require.NotContains(t, response.Body.String(), target.Email)
			require.NotContains(t, response.Body.String(), `"data"`)
			return nil
		}
		require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
		var page struct {
			Data []iam.AssignmentTargetChoice `json:"data"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &page))
		for _, private := range []string{"membership_epoch", "employment_epoch", "generation_id", "link_id", "staff_version"} {
			require.NotContains(t, response.Body.String(), `"`+private+`"`)
		}
		return page.Data
	}
	require.Empty(t, get(t, path, "admin", 200), "digital employment alone supplies no reviewed login link")
	require.Zero(t, reads.Load(), "unlinked people must not reach the identity provider")
	t.Run("application capture privilege disables qualification", func(t *testing.T) {
		require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_choice_capture_granted", SQL: "GRANT EXECUTE ON FUNCTION iam_staff_selection_capture_integrity() TO hms_app"}}))
		defer func() {
			require.NoError(t, db.Migrate(context.WithoutCancel(t.Context()), []tenantdb.Migration{{ID: "test_choice_capture_revoked", SQL: "REVOKE EXECUTE ON FUNCTION iam_staff_selection_capture_integrity() FROM hms_app"}}))
		}()
		get(t, path, "admin", 503)
	})
	require.Empty(t, get(t, path, "admin", 200), "restoring schema-owner-only capture restores qualification")
	get(t, path, "auditor", 404)
	t.Run("foreign ordinary membership cannot discover another tenant hospital", func(t *testing.T) {
		get(t, path, "other", 404)
		staleTenant := governanceRequest(router, http.MethodGet, path, "other", "")
		require.Equal(t, 409, staleTenant.Code, "a stale browser tenant precondition is distinct from foreign resource access")
		require.NotContains(t, staleTenant.Body.String(), target.Email)
	})
	get(t, path+"?subject=user-ordinary", "admin", 400)
	get(t, "/v1/iam/hospitals/"+uuid.Must(uuid.NewV7()).String()+"/assignment-target-choices", "admin", 404)
	link := uuid.Must(uuid.NewV7())
	linkPath := "/v1/workforce/hospitals/" + hospital.String() + "/staff/" + staff.String() + "/login-links/" + link.String()
	proposed := governanceRequest(router, http.MethodPut, linkPath, "admin", `{"subject":"user-ordinary","reason":"Synthetic reviewed chooser account"}`)
	require.Equal(t, 200, proposed.Code, proposed.Body.String())
	require.Empty(t, get(t, path, "admin", 200), "pending links confer no choice eligibility")
	approved := governanceRequest(router, http.MethodPost, linkPath+"/decision", "owner2", `{"expected_version":1,"decision":"approve","reason":"Independent owner confirms the exact account"}`)
	require.Equal(t, 200, approved.Code, approved.Body.String())
	require.Equal(t, []iam.AssignmentTargetChoice{target}, get(t, path+"?status=eligible&limit=20", "admin", 200))
	require.EqualValues(t, 1, reads.Load())
	assertAssignmentQualificationLocksAreReadOnly(t, db, t.Context(), hospital, staff, link)
	exerciseNativeAssignmentBrowser(t, router, hospital, staff, target.Subject, target.Name, target.Email)
	for _, audit := range []struct{ table, action string }{
		{"audit_events", "iam.assignment.target_choices.read"},
		{"workforce_audit", "workforce.assignment_target.qualification.read"},
	} {
		t.Run("withholds choices when "+audit.table+" rejects read audit", func(t *testing.T) {
			constraint := "synthetic_assignment_choice_audit"
			require.NoError(t, db.Migrate(t.Context(), []tenantdb.Migration{{ID: "test_choice_audit_failure_" + audit.table, SQL: "ALTER TABLE " + audit.table + " ADD CONSTRAINT " + constraint + " CHECK(action<>'" + audit.action + "') NOT VALID"}}))
			defer func() {
				require.NoError(t, db.Migrate(context.WithoutCancel(t.Context()), []tenantdb.Migration{{ID: "test_choice_audit_restored_" + audit.table, SQL: "ALTER TABLE " + audit.table + " DROP CONSTRAINT " + constraint}}))
			}()
			get(t, path, "admin", 503)
		})
	}
	require.Equal(t, []iam.AssignmentTargetChoice{target}, get(t, path, "admin", 200), "audit recovery restores real eligible choices")
	require.Empty(t, get(t, "/v1/iam/hospitals/"+otherHospital.String()+"/assignment-target-choices", "admin", 200), "a link does not cross hospital scope")
	*identity = func(context.Context, string, string) (*customercontract.AssignmentCandidateIdentity, error) {
		return nil, errors.New("synthetic private identity dependency failure")
	}
	get(t, path, "admin", 503)
	*identity = func(context.Context, string, string) (*customercontract.AssignmentCandidateIdentity, error) {
		return nil, nil
	}
	require.Empty(t, get(t, path, "admin", 200), "an ineligible owned identity must not be disclosed")
	*identity = func(ctx context.Context, tenant, subject string) (*customercontract.AssignmentCandidateIdentity, error) {
		// A normal write during identity I/O also proves the chooser released target locks.
		revoked := governanceRequest(router, http.MethodPost, linkPath+"/decision", "admin", `{"expected_version":2,"decision":"revoke","reason":"Synthetic source drift during external lookup"}`)
		if revoked.Code != http.StatusOK {
			return nil, fmt.Errorf("concurrent link revocation returned %d", revoked.Code)
		}
		return eligible(ctx, tenant, subject)
	}
	get(t, path, "admin", 409)
	*identity = eligible
	require.Empty(t, get(t, path, "admin", 200), "a revoked link cannot reappear on a fresh request")
}
