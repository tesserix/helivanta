package pharmacy_test

import (
	"fmt"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPrescriptionSaleRequiresReleasedImageEvidence(t *testing.T) {
	for _, class := range []string{"G", "H", "H1"} {
		t.Run(class, func(t *testing.T) {
			w := newSaleWorld(t, true)
			product := w.product(t, "TEST-EVIDENCE-"+class, class)
			w.shelve(t, product, "2028-06-30", 12500, 30)
			rx := w.prescribeWithoutImage(t, product, 20, 2, false)
			quote := fmt.Sprintf(`{"lines":[{"product_id":%q,"quantity_units":10,"prescription":{"prescription_id":%q,"line_number":1}}]}`, product, rx)
			result := do(w.r, "POST", salePath("/quote"), "pharmacist", quote)
			require.Equal(t, http.StatusConflict, result.Code, result.Body.String())
			require.Contains(t, result.Body.String(), "released prescription image")
			code, _, out := w.sell(t, rxSale(product, 10, tenUnits, rx, ""))
			require.Equal(t, http.StatusConflict, code, out)
			available, _ := w.available(t, product)
			require.EqualValues(t, 30, available)
		})
	}
}

func TestPrescriptionEvidenceMustBeCleanAndBelongToTheCitedPrescription(t *testing.T) {
	w := newSaleWorld(t, true)
	product := w.product(t, "TEST-EVIDENCE-CLEAN", "H")
	w.shelve(t, product, "2028-06-30", 12500, 30)
	other := w.prescribe(t, product, 20, 1, false)
	rx := w.prescribeWithoutImage(t, product, 20, 1, false)
	require.NotEqual(t, other, rx)
	saleID := uuid.Must(uuid.NewV7()).String()
	body := rxSale(product, 10, tenUnits, rx, "")
	refused := func() {
		result := do(w.r, "PUT", salePath("/"+saleID), "pharmacist", body)
		require.Equal(t, http.StatusConflict, result.Code, result.Body.String())
		require.Contains(t, result.Body.String(), "released prescription image")
	}
	refused() // Another prescription's released image grants nothing.
	rejected := w.prescriptionImage(t, rx)
	require.Equal(t, 201, do(w.r, "PUT", rejected.path, "pharmacist", rejected.body).Code)
	refused() // upload_pending
	require.Equal(t, 202, rejected.upload("pharmacist", "image/png", rejected.data).Code)
	refused() // scan_pending
	status, err := rejected.processor(t, "stream: Eicar-Test-Signature FOUND\x00").Process(t.Context(), rejected.input())
	require.NoError(t, err)
	require.Equal(t, "rejected", status)
	refused()
	clean := w.prescriptionImage(t, rx)
	require.Equal(t, 201, do(w.r, "PUT", clean.path, "pharmacist", clean.body).Code)
	require.Equal(t, 202, clean.upload("pharmacist", "image/png", clean.data).Code)
	status, err = clean.processor(t, "UNKNOWN COMMAND\x00").Process(t.Context(), clean.input())
	require.Error(t, err)
	require.Equal(t, "scan_error", status)
	refused()
	require.Equal(t, 202, do(w.r, "POST", clean.path+"/retry/2", "pharmacist", "").Code)
	next := clean.input()
	next.ScanAttempt = 2
	status, err = clean.processor(t, "stream: OK\x00").Process(t.Context(), next)
	require.NoError(t, err)
	require.Equal(t, "available", status)
	posted := do(w.r, "PUT", salePath("/"+saleID), "pharmacist", body)
	require.Equal(t, http.StatusCreated, posted.Code, posted.Body.String())
	replay := do(w.r, "PUT", salePath("/"+saleID), "pharmacist", body)
	require.Equal(t, http.StatusOK, replay.Code, replay.Body.String())
	require.JSONEq(t, posted.Body.String(), replay.Body.String())
	available, _ := w.available(t, product)
	require.EqualValues(t, 20, available)
}

func TestDatabaseRefusesPrescriptionSaleWithoutReleasedEvidence(t *testing.T) {
	w := newSaleWorld(t, true)
	product := w.product(t, "TEST-EVIDENCE-DB", "H")
	w.shelve(t, product, "2028-06-30", 12500, 30)
	rx := w.prescribe(t, product, 20, 2, false)
	raw := w.prescribeWithoutImage(t, product, 20, 2, false)
	code, posted, out := w.sell(t, rxSale(product, 10, tenUnits, rx, ""))
	require.Equal(t, http.StatusCreated, code, out)
	require.ErrorContains(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
		sale := uuid.Must(uuid.NewV7())
		if err := tx.Exec(`INSERT INTO pharmacy_sales (id,tenant_id,outlet_id,financial_year,sequence_number,invoice_number,sale_date,invoice_profile_id,seller_legal_name,seller_address,seller_gstin,place_of_supply,currency,total_minor,taxable_minor,cgst_minor,sgst_minor,intent_sha256,actor_subject)
   SELECT ?,tenant_id,outlet_id,financial_year,99,'TS/2627/99',sale_date,invoice_profile_id,seller_legal_name,seller_address,seller_gstin,place_of_supply,currency,total_minor,taxable_minor,cgst_minor,sgst_minor,intent_sha256,'user-editor' FROM pharmacy_sales WHERE invoice_number=?`, sale, posted.InvoiceNumber).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO pharmacy_sale_lines (tenant_id,outlet_id,sale_id,line_number,product_id,batch_id,location_id,lot_number,expires_on,regulatory_class,schedule_declaration_id,classification_code,supply_nature,rate_ppm,mrp_minor,pack_size,quantity_units,amount_minor,taxable_minor,cgst_minor,sgst_minor,prescription_id,prescription_line)
   SELECT l.tenant_id,l.outlet_id,?,1,l.product_id,l.batch_id,l.location_id,l.lot_number,l.expires_on,l.regulatory_class,l.schedule_declaration_id,l.classification_code,l.supply_nature,l.rate_ppm,l.mrp_minor,l.pack_size,l.quantity_units,l.amount_minor,l.taxable_minor,l.cgst_minor,l.sgst_minor,?,1 FROM pharmacy_sale_lines l JOIN pharmacy_sales s ON s.tenant_id=l.tenant_id AND s.outlet_id=l.outlet_id AND s.id=l.sale_id WHERE s.invoice_number=?`, sale, raw, posted.InvoiceNumber).Error
	}), "released prescription image")
}
