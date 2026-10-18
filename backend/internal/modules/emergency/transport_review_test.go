package emergency_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"gorm.io/gorm"
)

func reviewerBody(version int, enabled bool) string {
	return fmt.Sprintf(`{"version":%d,"enabled":%t,"evidence_reference":"SYN-MEDICAL-LEAD-VERIFICATION","expires_at":%q}`, version, enabled, time.Now().UTC().Add(24*time.Hour).Format(time.RFC3339))
}
func transportReviewBody() string {
	return fmt.Sprintf(`{"case_version":1,"required_class":"als","policy_reference":"SYN-TRANSPORT-REVIEW-POLICY","valid_until":%q}`, time.Now().UTC().Add(time.Hour).Format(time.RFC3339))
}
func TestTransportReviewNeedsExplicitClinicalVerificationAndBaseAccess(t *testing.T) {
	f := setupWithTransit(t, vault(t))
	createBase(t, f)
	w := testutil.Do(f.r, "PUT", f.path()+"/access/user-doctor", "admin", `{"level":"reader"}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	caseID := newID()
	w = testutil.Do(f.r, "PUT", f.path()+"/cases/"+caseID, "dispatcher", intakeBody())
	require.Equal(t, 202, w.Code, w.Body.String())
	path := f.path() + "/cases/" + caseID + "/assessments/" + newID()
	body := transportReviewBody()
	w = testutil.Do(f.r, "PUT", path, "doctor", body)
	require.Equal(t, 404, w.Code, w.Body.String())
	w = testutil.Do(f.r, "PUT", f.path()+"/clinical-reviewers/user-doctor", "admin", reviewerBody(0, true))
	require.Equal(t, 200, w.Code, w.Body.String())
	for range 2 {
		w = testutil.Do(f.r, "PUT", path, "doctor", body)
		require.Equal(t, 200, w.Code, w.Body.String())
		row := decodeBody(t, w.Body.Bytes())
		require.Equal(t, float64(1), row["sequence"])
		require.Equal(t, "user-doctor", row["reviewer_subject"])
		require.NotContains(t, w.Body.String(), "Synthetic Caller")
	}
	w = testutil.Do(f.r, "PUT", path, "supervisor", body)
	require.Equal(t, 403, w.Code, w.Body.String())
	w = testutil.Do(f.r, "PUT", path, "admin", body)
	require.Equal(t, 404, w.Code, w.Body.String())
}

func TestClinicalVerificationCannotEnableItself(t *testing.T) {
	f := setup(t)
	createBase(t, f)
	w := testutil.Do(f.r, "PUT", f.path()+"/clinical-reviewers/user-admin", "admin", reviewerBody(0, true))
	require.Equal(t, 400, w.Code, w.Body.String())
	w = testutil.Do(f.r, "PUT", f.path()+"/clinical-reviewers/user-admin", "admin", reviewerBody(0, false))
	require.Equal(t, 200, w.Code, w.Body.String(), "self-revocation must remain possible")
}

func TestRevokedClinicalVerificationBlocksNewAssessmentsButPreservesHistory(t *testing.T) {
	f := setupWithTransit(t, vault(t))
	createBase(t, f)
	w := testutil.Do(f.r, "PUT", f.path()+"/access/user-doctor", "admin", `{"level":"reader"}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	w = testutil.Do(f.r, "PUT", f.path()+"/clinical-reviewers/user-doctor", "admin", reviewerBody(0, true))
	require.Equal(t, 200, w.Code, w.Body.String())
	caseID := newID()
	path := f.path() + "/cases/" + caseID
	w = testutil.Do(f.r, "PUT", path, "dispatcher", intakeBody())
	require.Equal(t, 202, w.Code, w.Body.String())
	w = testutil.Do(f.r, "PUT", path+"/assessments/"+newID(), "doctor", transportReviewBody())
	require.Equal(t, 200, w.Code, w.Body.String())
	w = testutil.Do(f.r, "GET", f.path()+"/clinical-reviewer", "doctor", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, true, decodeBody(t, w.Body.Bytes())["enabled"])
	require.Equal(t, true, decodeBody(t, w.Body.Bytes())["currently_active"])
	w = testutil.Do(f.r, "PUT", f.path()+"/clinical-reviewers/user-doctor", "admin", reviewerBody(1, false))
	require.Equal(t, 200, w.Code, w.Body.String())
	w = testutil.Do(f.r, "PUT", path+"/assessments/"+newID(), "doctor", transportReviewBody())
	require.Equal(t, 404, w.Code, w.Body.String())
	w = testutil.Do(f.r, "GET", path+"/assessments", "supervisor", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), "SYN-TRANSPORT-REVIEW-POLICY")
	w = testutil.Do(f.r, "GET", f.path()+"/clinical-reviewers/user-doctor", "admin", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Equal(t, float64(2), decodeBody(t, w.Body.Bytes())["version"])
	require.Equal(t, false, decodeBody(t, w.Body.Bytes())["currently_active"])
	for _, token := range []string{"other", "reader"} {
		w = testutil.Do(f.r, "GET", path+"/assessments", token, "")
		if token == "other" {
			require.Equal(t, 404, w.Code, w.Body.String())
		} else {
			require.Equal(t, 200, w.Code, w.Body.String())
		}
	}
}

func TestClinicalVerificationReplayCannotBeClaimedByAnotherVerifier(t *testing.T) {
	f := setup(t)
	createBase(t, f)
	path := f.path() + "/clinical-reviewers/user-doctor"
	body := reviewerBody(0, true)
	w := testutil.Do(f.r, "PUT", path, "admin", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	w = testutil.Do(f.r, "PUT", path, "admin", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	w = testutil.Do(f.r, "PUT", path, "verifier", body)
	require.Equal(t, 409, w.Code, w.Body.String())
}

func TestClinicalGrantCannotForgeTheIndependentVerifierInSQL(t *testing.T) {
	f := setup(t)
	createBase(t, f)
	err := f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.emergency_subject','user-admin',true),set_config('app.emergency_admin','true',true),set_config('app.emergency_base',?,true)`, f.base).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO emergency_clinical_reviewers(tenant_id,base_id,subject,id,version,enabled,evidence_reference,expires_at,verified_by) VALUES(?,?,'user-admin',?,1,true,'SYN-APPROVAL',now()+interval '1 hour','user-doctor')`, testutil.TenantA, f.base, newID()).Error
	})
	require.Error(t, err, "SQL must not let an admin invent an independent verifier for their own access")
	w := testutil.Do(f.r, "PUT", f.path()+"/clinical-reviewers/user-doctor", "admin", reviewerBody(0, true))
	require.Equal(t, 200, w.Code, w.Body.String())
}

func TestAssessmentRejectsStaleCaseAndValidityBeyondReviewerAccess(t *testing.T) {
	f := setupWithTransit(t, vault(t))
	createBase(t, f)
	caseID := newID()
	casePath := f.path() + "/cases/" + caseID
	require.Equal(t, 202, testutil.Do(f.r, "PUT", casePath, "dispatcher", intakeBody()).Code)
	require.Equal(t, 200, testutil.Do(f.r, "PUT", f.path()+"/access/user-doctor", "admin", `{"level":"reader"}`).Code)
	grant := fmt.Sprintf(`{"version":0,"enabled":true,"evidence_reference":"SYN-REVIEW","expires_at":%q}`, time.Now().UTC().Add(30*time.Minute).Format(time.RFC3339))
	require.Equal(t, 200, testutil.Do(f.r, "PUT", f.path()+"/clinical-reviewers/user-doctor", "admin", grant).Code)
	w := testutil.Do(f.r, "PUT", casePath+"/assessments/"+newID(), "doctor", transportReviewBody())
	require.Equal(t, 400, w.Code, w.Body.String())
	body := fmt.Sprintf(`{"case_version":1,"required_class":"bls","policy_reference":"SYN-TRANSPORT","valid_until":%q}`, time.Now().UTC().Add(10*time.Minute).Format(time.RFC3339))
	w = testutil.Do(f.r, "PUT", casePath+"/assessments/"+newID(), "doctor", strings.Replace(body, `"case_version":1`, `"case_version":2`, 1))
	require.Equal(t, 409, w.Code, w.Body.String())
	w = testutil.Do(f.r, "PUT", casePath+"/assessments/"+newID(), "doctor", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	w = testutil.Do(f.r, "POST", casePath+"/close", "supervisor", `{"version":1,"reason":"entered_in_error","explanation":"Synthetic test report only"}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	w = testutil.Do(f.r, "PUT", casePath+"/assessments/"+newID(), "doctor", body)
	require.Equal(t, 409, w.Code, w.Body.String())
}

func TestConcurrentAssessmentRetriesHaveOneSequenceAndMetadataEvent(t *testing.T) {
	f := setupWithTransit(t, vault(t))
	createBase(t, f)
	caseID := newID()
	require.Equal(t, 202, testutil.Do(f.r, "PUT", f.path()+"/cases/"+caseID, "dispatcher", intakeBody()).Code)
	require.Equal(t, 200, testutil.Do(f.r, "PUT", f.path()+"/access/user-doctor", "admin", `{"level":"reader"}`).Code)
	require.Equal(t, 200, testutil.Do(f.r, "PUT", f.path()+"/clinical-reviewers/user-doctor", "admin", reviewerBody(0, true)).Code)
	path := f.path() + "/cases/" + caseID + "/assessments/" + newID()
	body := transportReviewBody()
	var wg sync.WaitGroup
	codes := make(chan int, 8)
	for range 8 {
		wg.Go(func() { codes <- testutil.Do(f.r, "PUT", path, "doctor", body).Code })
	}
	wg.Wait()
	close(codes)
	for code := range codes {
		require.Equal(t, 200, code)
	}
	w := testutil.Do(f.r, "GET", f.path()+"/cases/"+caseID+"/assessments", "doctor", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	var page struct {
		Data []map[string]any `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
	require.Len(t, page.Data, 1)
	require.Equal(t, float64(1), page.Data[0]["sequence"])
	var events []struct{ Data string }
	require.NoError(t, f.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT payload->'data' AS data FROM outbox_events WHERE subject='helivanta.in.emergency.case_assessed.v1'`).Scan(&events).Error
	}))
	require.Len(t, events, 1)
	require.NotContains(t, events[0].Data, "user-doctor")
	require.NotContains(t, events[0].Data, "SYN-TRANSPORT")
	w = testutil.Do(f.r, "PUT", path, "doctor", strings.Replace(body, `"als"`, `"bls"`, 1))
	require.Equal(t, 409, w.Code, w.Body.String())
}
