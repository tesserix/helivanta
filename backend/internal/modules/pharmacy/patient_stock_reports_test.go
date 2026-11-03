package pharmacy_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	patientmastercontract "github.com/tesserix/helivanta/internal/modules/patientmaster/contract" //nolint:depguard // external test feeding the consumer its published contract
	"github.com/tesserix/helivanta/internal/modules/pharmacy"                                     //nolint:depguard // external test importing its own module
	"github.com/tesserix/helivanta/internal/testutil"
)

// #1060: patient-wise sales and the sales stock report.

type patientSaleRow struct {
	ID            string  `json:"id"`
	SaleDate      string  `json:"sale_date"`
	InvoiceNumber string  `json:"invoice_number"`
	LineNumber    int     `json:"line_number"`
	BedCode       *string `json:"admission_bed_code"`
	ProductID     string  `json:"product_id"`
	SKU           string  `json:"sku"`
	LotNumber     string  `json:"lot_number"`
	ExpiresOn     string  `json:"expires_on"`
	Units         int64   `json:"quantity_units"`
	MRPMinor      int64   `json:"mrp_minor"`
	DiscountMinor int64   `json:"discount_minor"`
	AmountMinor   int64   `json:"amount_minor"`
	ReturnedUnits int64   `json:"returned_units"`
}

type stockSalesRow struct {
	ProductID     string `json:"product_id"`
	SKU           string `json:"sku"`
	UnitsSold     int64  `json:"units_sold"`
	UnitsReturned int64  `json:"units_returned"`
	OnHand        int64  `json:"on_hand_units"`
	Sellable      int64  `json:"sellable_units"`
}

// patientWorld sells to a registered patient (and takes two units back the
// next day, when it buys one more), to an admitted patient in bed B-7, and to a walk-in buyer.
type patientWorld struct {
	saleWorld
	drug, batch       string
	patient, admitted string
	patientSale       saleResp
}

func newPatientWorld(t *testing.T) patientWorld {
	t.Helper()
	w := patientWorld{saleWorld: newSaleWorld(t, true)}
	w.drug = w.product(t, "TEST-PCM-500", "none")
	w.batch = w.shelve(t, w.drug, "2028-06-30", 12500, 20)
	d := newWard(t, w.saleWorld)
	d.verify()
	w.patient = uuid.NewString()
	require.NoError(t, d.handle("pharmacy-patient-registered", patientmastercontract.PatientRegisteredData{PatientID: w.patient, HospitalID: d.hospital}))
	id := uuid.Must(uuid.NewV7()).String()
	res := do(w.r, "PUT", salePath("/"+id), "pharmacist", fmt.Sprintf(`{"lines":[{"product_id":%q,"quantity_units":5}],"tenders":[{"kind":"cash","amount_minor":6250,"tendered_minor":7000}],"patient":{"patient_id":%q,"hospital_id":%q}}`,
		w.drug, w.patient, d.hospital))
	require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &w.patientSale))
	stay := d.admit("R-1", "B-7")
	w.admitted = stay.patient
	code, _, out := w.sell(t, admittedSale(w.drug, stay.occupancy))
	require.Equal(t, http.StatusCreated, code, out)
	code, _, out = w.sell(t, cashSale(w.drug, 1, 1250, 1250))
	require.Equal(t, http.StatusCreated, code, out)
	pharmacy.SetClock(t, func() time.Time { return time.Date(2026, 11, 2, 6, 30, 0, 0, time.UTC) })
	code, _, out = w.returnUnits(t, returnBody(id, 2, w.quarantine, 2500))
	require.Equal(t, http.StatusCreated, code, out)
	code, _, out = w.sell(t, fmt.Sprintf(`{"lines":[{"product_id":%q,"quantity_units":1}],"tenders":[{"kind":"cash","amount_minor":1250,"tendered_minor":1250}],"patient":{"patient_id":%q,"hospital_id":%q}}`,
		w.drug, w.patient, d.hospital))
	require.Equal(t, http.StatusCreated, code, out)
	return w
}

func patientSalesReads(t *testing.T, w saleWorld) (count int64, fields []string) {
	t.Helper()
	require.NoError(t, w.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Raw(`SELECT count(*) FROM audit_events WHERE resource_type='pharmacy_patient_sales' AND action='report_read' AND actor_subject='user-pharmacist'`).Scan(&count).Error; err != nil {
			return err
		}
		return tx.Raw(`SELECT DISTINCT array_to_string(changed_fields,',') FROM audit_events WHERE resource_type='pharmacy_patient_sales'`).Scan(&fields).Error
	}))
	return count, fields
}

func TestPatientWiseSalesListsOnePatientsPurchases(t *testing.T) {
	w := newPatientWorld(t)
	code, body, out := reportPage[patientSaleRow](t, w.saleWorld, "pharmacist", "/reports/patient-sales?from=2026-11-01&to=2026-11-02&patient_id="+w.patient)
	require.Equal(t, http.StatusOK, code, out)
	require.Len(t, body.Data, 2, "only this patient's lines, not the admitted patient's or the walk-in's")
	require.Equal(t, "2026-11-02", body.Data[1].SaleDate, "oldest first")
	require.EqualValues(t, 1, body.Data[1].Units)
	r := body.Data[0]
	require.Equal(t, "2026-11-01", r.SaleDate)
	require.Equal(t, w.patientSale.InvoiceNumber, r.InvoiceNumber)
	require.Equal(t, 1, r.LineNumber)
	require.Nil(t, r.BedCode)
	require.Equal(t, w.drug, r.ProductID)
	require.Equal(t, "TEST-PCM-500", r.SKU)
	require.Equal(t, "TEST-"+strings.ToUpper(w.batch[24:]), r.LotNumber)
	require.Equal(t, "2028-06-30", r.ExpiresOn)
	require.EqualValues(t, 5, r.Units)
	require.EqualValues(t, 12500, r.MRPMinor)
	require.Zero(t, r.DiscountMinor)
	require.Equal(t, w.patientSale.TotalMinor, r.AmountMinor)
	require.EqualValues(t, 2, r.ReturnedUnits)

	// A return credited after the period is not counted, nor a later sale.
	_, body, out = reportPage[patientSaleRow](t, w.saleWorld, "pharmacist", "/reports/patient-sales?from=2026-11-01&to=2026-11-01&patient_id="+w.patient)
	require.Len(t, body.Data, 1, out)
	require.Equal(t, "2026-11-01", body.Data[0].SaleDate)
	require.Zero(t, body.Data[0].ReturnedUnits)
	// Nor an earlier one.
	_, body, out = reportPage[patientSaleRow](t, w.saleWorld, "pharmacist", "/reports/patient-sales?from=2026-11-02&to=2026-11-02&patient_id="+w.patient)
	require.Len(t, body.Data, 1, out)
	require.Equal(t, "2026-11-02", body.Data[0].SaleDate)

	// Paging walks the patient's lines once each.
	_, first, out := reportPage[patientSaleRow](t, w.saleWorld, "pharmacist", "/reports/patient-sales?from=2026-11-01&to=2026-11-02&limit=1&patient_id="+w.patient)
	require.Len(t, first.Data, 1, out)
	_, second, out := reportPage[patientSaleRow](t, w.saleWorld, "pharmacist", "/reports/patient-sales?from=2026-11-01&to=2026-11-02&limit=1&patient_id="+w.patient+"&cursor="+url.QueryEscape(*first.Page.NextCursor))
	require.Len(t, second.Data, 1, out)
	require.NotEqual(t, first.Data[0].ID, second.Data[0].ID)

	// A sale to an admission names the bed.
	_, body, out = reportPage[patientSaleRow](t, w.saleWorld, "pharmacist", "/reports/patient-sales?from=2026-11-01&to=2026-11-01&patient_id="+w.admitted)
	require.Len(t, body.Data, 1, out)
	require.Equal(t, "B-7", *body.Data[0].BedCode)
	require.EqualValues(t, 2, body.Data[0].Units)

	reads, fields := patientSalesReads(t, w.saleWorld)
	require.EqualValues(t, 6, reads, "every page read is recorded")
	require.Equal(t, []string{"from,to,patient_id"}, fields)
}

func TestPatientWiseSalesNeedsAPatientAndIsScoped(t *testing.T) {
	w := newPatientWorld(t)
	for _, bad := range []string{"", "&patient_id=nope"} {
		code, _, out := reportPage[patientSaleRow](t, w.saleWorld, "pharmacist", "/reports/patient-sales?from=2026-11-01&to=2026-11-01"+bad)
		require.Equal(t, http.StatusBadRequest, code, bad+out)
	}
	code, _, _ := reportPage[patientSaleRow](t, w.saleWorld, "pharmacist", "/reports/patient-sales?from=2025-10-31&to=2026-11-01&patient_id="+w.patient)
	require.Equal(t, http.StatusBadRequest, code, "the period is bounded")
	code, _, _ = reportPage[patientSaleRow](t, w.saleWorld, "stocker", "/reports/patient-sales?from=2026-11-01&to=2026-11-01&patient_id="+w.patient)
	require.Equal(t, http.StatusForbidden, code, "stock read does not show patients")
	code, _, _ = reportPage[patientSaleRow](t, w.saleWorld, "foreign", "/reports/patient-sales?from=2026-11-01&to=2026-11-01&patient_id="+w.patient)
	require.Equal(t, http.StatusNotFound, code)
	reads, _ := patientSalesReads(t, w.saleWorld)
	require.Zero(t, reads, "a refused read records nothing")
	_, body, out := reportPage[patientSaleRow](t, w.saleWorld, "pharmacist", "/reports/patient-sales?from=2026-11-01&to=2026-11-02&patient_id="+uuid.NewString())
	require.Empty(t, body.Data, out, "an unknown patient has no purchases")
}

func TestTheSalesStockReportSetsSalesBesideStock(t *testing.T) {
	w := newItemWorld(t)
	// The product helper declares from 1 November.
	pharmacy.SetClock(t, func() time.Time { return time.Date(2026, 11, 1, 6, 30, 0, 0, time.UTC) })
	held := w.product(t, "TEST-QTN-100", "none")
	receive(t, w.r, held, w.quarantine, "2028-06-30", `,"mrp_minor":12500`)

	code, body, out := reportPage[stockSalesRow](t, w.saleWorld, "stocker", "/reports/stock-sales?from=2026-11-01&to=2026-11-02")
	require.Equal(t, http.StatusOK, code, out)
	require.Len(t, body.Data, 3, out)
	p, q, h := body.Data[0], body.Data[1], body.Data[2]
	require.Equal(t, []string{w.p, w.q, held}, []string{p.ProductID, q.ProductID, h.ProductID}, "oldest product first")
	// Two batches of 100 received, 15 shelved, 7 sold, 2 returned to quarantine.
	require.Equal(t, stockSalesRow{ProductID: w.p, SKU: "TEST-PCM-500", UnitsSold: 7, UnitsReturned: 2, OnHand: 195, Sellable: 8}, p)
	require.Equal(t, stockSalesRow{ProductID: w.q, SKU: "TEST-ORS-200", UnitsSold: 3, OnHand: 97, Sellable: 7}, q)
	require.Equal(t, stockSalesRow{ProductID: held, SKU: "TEST-QTN-100", OnHand: 100}, h, "quarantined stock is on hand, not sellable")

	// Sold and returned units equal sales by item for the same period.
	_, items, out := reportPage[itemRow](t, w.saleWorld, "pharmacist", "/reports/items?from=2026-11-01&to=2026-11-01")
	require.Len(t, items.Data, 2, out)
	_, one, out := reportPage[stockSalesRow](t, w.saleWorld, "stocker", "/reports/stock-sales?from=2026-11-01&to=2026-11-01")
	require.Len(t, one.Data, 3, out)
	for i, it := range items.Data {
		require.Equal(t, []int64{it.UnitsSold, it.UnitsReturned}, []int64{one.Data[i].UnitsSold, one.Data[i].UnitsReturned}, it.SKU)
	}
	require.EqualValues(t, 195, one.Data[0].OnHand, "stock is now, whatever the period")

	// Paging walks every product once.
	seen := map[string]bool{}
	path := "/reports/stock-sales?from=2026-11-01&to=2026-11-02&limit=1"
	for {
		_, pg, out := reportPage[stockSalesRow](t, w.saleWorld, "stocker", path)
		require.Len(t, pg.Data, 1, out)
		require.False(t, seen[pg.Data[0].ProductID])
		seen[pg.Data[0].ProductID] = true
		if pg.Page.NextCursor == nil {
			break
		}
		path = "/reports/stock-sales?from=2026-11-01&to=2026-11-02&limit=1&cursor=" + url.QueryEscape(*pg.Page.NextCursor)
	}
	require.Len(t, seen, 3)

	code, _, _ = reportPage[stockSalesRow](t, w.saleWorld, "owner", "/reports/stock-sales?from=2026-11-01&to=2026-11-02")
	require.Equal(t, http.StatusForbidden, code, "sale read alone does not show stock")
	code, _, _ = reportPage[stockSalesRow](t, w.saleWorld, "foreign", "/reports/stock-sales?from=2026-11-01&to=2026-11-02")
	require.Equal(t, http.StatusNotFound, code)
	code, _, _ = reportPage[stockSalesRow](t, w.saleWorld, "stocker", "/reports/stock-sales?from=2026-11-02&to=2026-11-01")
	require.Equal(t, http.StatusBadRequest, code)
}
