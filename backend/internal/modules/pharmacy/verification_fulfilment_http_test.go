package pharmacy_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // module under test
)

func TestVerificationFulfilmentReviewIsScopedAuditedAndCreatesNoSale(t *testing.T) {
	w := newPrescriptionWorld(t, nil)
	rx := w.record(t)
	sourceResponse := do(w.r, "GET", prescriptionPath("/"+rx+"/review-context"), "pharmacist", "")
	require.Equal(t, 200, sourceResponse.Code, sourceResponse.Body.String())
	var source pharmacy.PrescriptionReviewContext
	require.NoError(t, json.Unmarshal(sourceResponse.Body.Bytes(), &source))
	path := prescriptionPath("/" + rx + "/fulfilment-review-context")
	body := fmt.Sprintf(`{"source_state_hash":%q,"lines":[{"line_number":1,"quantity_units":5}]}`, source.SourceStateHash)
	result := do(w.r, "POST", path, "pharmacist", body)
	require.Equal(t, 200, result.Code, result.Body.String())
	require.Equal(t, "no-store", result.Header().Get("Cache-Control"))
	var view pharmacy.PrescriptionFulfilmentReviewContext
	require.NoError(t, json.Unmarshal(result.Body.Bytes(), &view))
	require.Equal(t, rx, view.Source.Record.ID.String())
	require.Equal(t, "not_evaluated", view.ClinicalAuthority)
	require.Equal(t, "not_reserved", view.StockReservation)
	require.EqualValues(t, 5, view.Lines[0].QuantityUnits)
	require.Equal(t, w.product, view.Lines[0].ProductID.String())
	require.Equal(t, 200, do(w.r, "POST", path, "viewer", body).Code)
	for _, check := range []struct {
		actor  string
		status int
	}{{"", 401}, {"owner", 403}, {"foreign", 404}} {
		response := do(w.r, "POST", path, check.actor, body)
		require.Equal(t, check.status, response.Code, response.Body.String())
		require.NotContains(t, response.Body.String(), "TEST Ravi")
	}
	wrongParent := strings.Replace(path, pharmaOrganization, uuid.Must(uuid.NewV7()).String(), 1)
	require.Equal(t, 404, do(w.r, "POST", wrongParent, "pharmacist", body).Code)
	for _, invalid := range []string{
		strings.Replace(body, source.SourceStateHash, strings.Repeat("a", 64), 1),
		strings.Replace(body, `"quantity_units":5`, `"quantity_units":21`, 1),
	} {
		response := do(w.r, "POST", path, "pharmacist", invalid)
		require.Equal(t, 409, response.Code, response.Body.String())
		require.NotContains(t, response.Body.String(), "TEST Ravi")
	}
	assertion := strings.TrimSuffix(body, "}") + `,"clinical_authority":"approved"}`
	require.Equal(t, 400, do(w.r, "POST", path, "pharmacist", assertion).Code)
	require.NoError(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
		for _, table := range []string{"pharmacy_sales", "pharmacy_stock_movements", "pharmacy_sale_tenders"} {
			var count int64
			if err := tx.Table(table).Count(&count).Error; err != nil {
				return err
			}
			require.Zero(t, count, table)
		}
		var count int64
		if err := tx.Table("audit_events").Where("action='fulfilment_review_read' AND resource_type='pharmacy_prescription_fulfilment_review'").Count(&count).Error; err != nil {
			return err
		}
		require.Positive(t, count)
		return nil
	}))
	pharmacy.SetReadRecordFault(t)
	denied := do(w.r, "POST", path, "pharmacist", body)
	require.Equal(t, 503, denied.Code, denied.Body.String())
	require.NotContains(t, denied.Body.String(), "fulfilment_state_hash")
	require.NotContains(t, denied.Body.String(), "TEST Ravi")
}

func TestVerificationFulfilmentReviewInvalidatesAfterPartialAndFinalSupply(t *testing.T) {
	w := newSaleWorld(t, true)
	product := w.product(t, "TEST-REVIEW-H", "H")
	w.shelve(t, product, "2028-06-30", 12500, 30)
	rx := w.prescribe(t, product, 20, 2, false)
	sourcePath := prescriptionPath("/" + rx + "/review-context")
	source := func() pharmacy.PrescriptionReviewContext {
		response := do(w.r, "GET", sourcePath, "pharmacist", "")
		require.Equal(t, 200, response.Code, response.Body.String())
		var view pharmacy.PrescriptionReviewContext
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &view))
		return view
	}
	initial := source()
	path := prescriptionPath("/" + rx + "/fulfilment-review-context")
	body := func(hash string) string {
		return fmt.Sprintf(`{"source_state_hash":%q,"lines":[{"line_number":1,"quantity_units":5}]}`, hash)
	}
	before := do(w.r, "POST", path, "pharmacist", body(initial.SourceStateHash))
	require.Equal(t, 200, before.Code, before.Body.String())
	partial := do(w.r, "PUT", salePath("/"+uuid.Must(uuid.NewV7()).String()), "pharmacist", rxSale(product, 2, 2500, rx, ""))
	require.Equal(t, 201, partial.Code, partial.Body.String())
	stale := do(w.r, "POST", path, "pharmacist", body(initial.SourceStateHash))
	require.Equal(t, 409, stale.Code, stale.Body.String())
	require.Contains(t, stale.Body.String(), "review_source_changed")
	current := source()
	require.EqualValues(t, 18, current.Record.Lines[0].UnitsRemaining)
	after := do(w.r, "POST", path, "pharmacist", body(current.SourceStateHash))
	require.Equal(t, 200, after.Code, after.Body.String())
	require.NotEqual(t, before.Body.String(), after.Body.String())
	final := do(w.r, "PUT", salePath("/"+uuid.Must(uuid.NewV7()).String()), "pharmacist", rxSale(product, 18, 22500, rx, ""))
	require.Equal(t, 201, final.Code, final.Body.String())
	exhausted := source()
	require.Zero(t, exhausted.Record.Lines[0].UnitsRemaining)
	denied := do(w.r, "POST", path, "pharmacist", body(exhausted.SourceStateHash))
	require.Equal(t, 409, denied.Code, denied.Body.String())
	require.Contains(t, denied.Body.String(), "fulfilment_not_available")
}
