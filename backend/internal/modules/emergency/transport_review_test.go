package emergency_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
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
	for _, token := range []string{"other", "reader"} {
		w = testutil.Do(f.r, "GET", path+"/assessments", token, "")
		if token == "other" {
			require.Equal(t, 404, w.Code, w.Body.String())
		} else {
			require.Equal(t, 200, w.Code, w.Body.String())
		}
	}
}
