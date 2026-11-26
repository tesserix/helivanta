package pharmacy_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external module test
	"github.com/tesserix/helivanta/internal/testutil"
)

func TestPrescriptionReviewContextIsAuditedAndOutletScoped(t *testing.T) {
	w := newPrescriptionWorld(t, nil)
	id := w.record(t)
	path := prescriptionPath("/" + id + "/review-context")
	require.Equal(t, http.StatusUnauthorized, do(w.r, "GET", path, "", "").Code)
	response := do(w.r, "GET", path, "pharmacist", "")
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	var view pharmacy.PrescriptionReviewContext
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &view))
	require.Equal(t, testutil.TenantA, view.OwnerTenantID.String())
	require.Equal(t, pharmaOrganization, view.PharmacyID.String())
	require.Equal(t, id, view.Record.ID.String())
	require.Equal(t, "user-pharmacist", view.ReaderSubject)
	require.Equal(t, "not_evaluated", view.ClinicalAuthority)
	require.Equal(t, "unavailable", view.ClinicalContext.Allergies)
	require.Equal(t, int64(20), view.Record.Lines[0].UnitsRemaining)
	require.Len(t, view.SourceStateHash, 64)
	require.NoError(t, w.db.WithSystem(t.Context(), func(tx *gorm.DB) error {
		var count int64
		if err := tx.Table("audit_events").Where("tenant_id=? AND resource_type='pharmacy_prescription' AND resource_id=? AND actor_subject='user-pharmacist' AND action='record_read'", testutil.TenantA, id).Count(&count).Error; err != nil {
			return err
		}
		require.Equal(t, int64(1), count, "read disclosure must be audited before returning context")
		return nil
	}))
	for _, token := range []string{"owner", "viewer"} {
		require.Equal(t, http.StatusForbidden, do(w.r, "GET", path, token, "").Code)
	}
	require.Equal(t, http.StatusNotFound, do(w.r, "GET", path, "foreign", "").Code)
	require.Equal(t, http.StatusNotFound, do(w.r, "GET", prescriptionPath("/"+uuid.Must(uuid.NewV7()).String()+"/review-context"), "pharmacist", "").Code)
	otherOutlet := strings.Replace(path, pharmaOutlet, uuid.Must(uuid.NewV7()).String(), 1)
	require.Equal(t, http.StatusNotFound, do(w.r, "GET", otherOutlet, "pharmacist", "").Code)
	otherPharmacy := strings.Replace(path, pharmaOrganization, uuid.Must(uuid.NewV7()).String(), 1)
	require.Equal(t, http.StatusNotFound, do(w.r, "GET", otherPharmacy, "pharmacist", "").Code)
	request := httptest.NewRequest("GET", path, nil)
	request.Header.Set("Authorization", "Bearer pharmacist")
	request.Header.Set("X-Helivanta-Expected-Subject", "synthetic-other-reader")
	changed := httptest.NewRecorder()
	w.r.ServeHTTP(changed, request)
	require.Equal(t, http.StatusConflict, changed.Code)
	require.NotContains(t, changed.Body.String(), "source_state_hash")
}

func TestPrescriptionReviewContextDoesNotDiscloseWhenAuditFails(t *testing.T) {
	w := newPrescriptionWorld(t, nil)
	id := w.record(t)
	pharmacy.SetReadRecordFault(t)
	response := do(w.r, "GET", prescriptionPath("/"+id+"/review-context"), "pharmacist", "")
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	require.NotContains(t, response.Body.String(), "TEST Patient")
	require.NotContains(t, response.Body.String(), "source_state_hash")
}
