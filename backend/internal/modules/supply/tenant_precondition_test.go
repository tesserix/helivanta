package supply_test

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testutil"
	"gorm.io/gorm"
)

func supplyRequestWithExpectedTenant(r *gin.Engine, method, path, body, expected string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer owner")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Helivanta-Expected-Tenant", expected)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestSupplyRejectsStaleExpectedTenantAcrossEveryRouteFamily(t *testing.T) {
	s := reviewedEvidenceScenario(t)
	var licence struct {
		PremiseID string `json:"premise_id"`
	}
	w := testutil.Do(s.r, "GET", s.licence, "owner", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &licence))
	premise := s.root + "/premises/" + licence.PremiseID
	for _, tc := range []struct{ method, path string }{
		{"GET", "/v1/supply/parties"},
		{"GET", s.root},
		{"PUT", s.root},
		{"GET", s.root + "/revisions"},
		{"GET", s.root + "/access"},
		{"PUT", s.root + "/access/user-test/auditor"},
		{"DELETE", s.root + "/access/user-test/auditor"},
		{"GET", s.root + "/premises"},
		{"GET", premise},
		{"PUT", premise},
		{"GET", premise + "/revisions"},
		{"GET", s.root + "/licenses"},
		{"GET", s.licence},
		{"PUT", s.licence},
		{"GET", s.licence + "/revisions"},
		{"GET", s.licence + "/evidence"},
		{"PUT", s.path},
		{"PUT", s.path + "/content"},
		{"GET", s.path + "/download"},
		{"POST", s.path + "/revoke"},
		{"POST", s.path + "/retry/2"},
		{"GET", s.licence + "/review"},
		{"GET", s.licence + "/reviews"},
		{"PUT", newReview(s)},
	} {
		t.Run(tc.method+" "+strings.TrimPrefix(tc.path, s.root), func(t *testing.T) {
			w := supplyRequestWithExpectedTenant(s.r, tc.method, tc.path, `{}`, testutil.TenantB)
			require.Equal(t, 409, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), `"error":"session_changed"`)
			require.NotContains(t, w.Body.String(), testutil.TenantA)
			require.NotContains(t, w.Body.String(), testutil.TenantB)
		})
	}
}

func TestSupplyExpectedTenantPreconditionCannotCreateReviewSideEffects(t *testing.T) {
	s := reviewedEvidenceScenario(t)
	counts := func() (values [3]int64) {
		require.NoError(t, partyTransaction(t, s.db, evidenceScanInput(s).PartyID, func(tx *gorm.DB) error {
			for i, table := range []string{"supply_license_reviews", "audit_events", "outbox_events"} {
				if err := tx.Table(table).Count(&values[i]).Error; err != nil {
					return err
				}
			}
			return nil
		}))
		return values
	}
	before := counts()
	w := supplyRequestWithExpectedTenant(s.r, "PUT", newReview(s), reviewBody(s, 0, "submit"), testutil.TenantB)
	require.Equal(t, 409, w.Code, w.Body.String())
	require.Equal(t, before, counts())
}

func TestSupplyExpectedTenantValidatesUUIDAndPreservesOptionalCompatibility(t *testing.T) {
	s := reviewedEvidenceScenario(t)
	for _, expected := range []string{"not-a-tenant", "00000000-0000-0000-0000-000000000000", " "} {
		w := supplyRequestWithExpectedTenant(s.r, "GET", s.licence+"/review", "", expected)
		require.Equal(t, 409, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), `"error":"session_changed"`)
	}
	for _, expected := range []string{"", testutil.TenantA, "urn:uuid:" + testutil.TenantA} {
		w := supplyRequestWithExpectedTenant(s.r, "GET", s.licence+"/review", "", expected)
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), `"trading_approved":false`)
	}
}
