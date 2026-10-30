package pharmacy_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // external test importing its own module
	"github.com/tesserix/helivanta/internal/testutil"
)

// #1386 C-2 (#1062): the Schedule H1 register, Drugs Rules 1945 Rule 65(3)(h).

type h1Page struct {
	Data []struct {
		SupplyDate        string `json:"supply_date"`
		InvoiceNumber     string `json:"invoice_number"`
		PrescriberName    string `json:"prescriber_name"`
		PrescriberAddress string `json:"prescriber_address"`
		PatientName       string `json:"patient_name"`
		BrandName         string `json:"brand_name"`
		LotNumber         string `json:"lot_number"`
		QuantityUnits     int64  `json:"quantity_units"`
		ReturnedUnits     int64  `json:"returned_units"`
		Pharmacist        string `json:"pharmacist"`
	} `json:"data"`
}

func h1Path(suffix string) string { return licencePath("/registers/h1" + suffix) }

// h1World sells H1, H and OTC lines and returns some H1 units.
func h1World(t *testing.T) (saleWorld, string) {
	t.Helper()
	w := newSaleWorld(t, true)
	h1 := w.product(t, "TEST-H1-DRUG", "H1")
	h := w.product(t, "TEST-H-DRUG", "H")
	otc := w.product(t, "TEST-PCM-500", "none")
	for _, p := range []string{h1, h, otc} {
		w.shelve(t, p, "2028-06-30", 12500, 30)
	}
	rx := w.prescribe(t, h1, 20, 2, false)
	code, _, out := w.sell(t, rxSale(h1, 10, tenUnits, rx, ""))
	require.Equal(t, http.StatusCreated, code, out)
	var sale struct {
		ID string `json:"id"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &sale))
	code, _, out = w.sell(t, rxSale(h, 10, tenUnits, w.prescribe(t, h, 10, 1, false), ""))
	require.Equal(t, http.StatusCreated, code, out)
	code, _, out = w.sell(t, rxSale(otc, 10, tenUnits, "", ""))
	require.Equal(t, http.StatusCreated, code, out)
	code, _, out = w.returnUnits(t, returnBody(sale.ID, 3, w.quarantine, 3750))
	require.Equal(t, http.StatusCreated, code, out)
	return w, sale.ID
}

func TestTheH1RegisterReadsEachH1SupplyAndItsReturns(t *testing.T) {
	w, _ := h1World(t)
	res := do(w.r, "GET", h1Path("?from=2026-11-01&to=2026-11-01"), "pharmacist", "")
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	var page h1Page
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &page))
	require.Len(t, page.Data, 1, "only H1 lines are in the H1 register")
	e := page.Data[0]
	require.Equal(t, "2026-11-01", e.SupplyDate)
	require.Equal(t, "TS/2627/1", e.InvoiceNumber)
	require.Equal(t, "TEST Dr Asha Rao", e.PrescriberName)
	require.Equal(t, prescriberAddress, e.PrescriberAddress)
	require.Equal(t, "TEST Ravi Kumar", e.PatientName)
	require.Equal(t, "TEST TEST-H1-DRUG", e.BrandName)
	require.NotEmpty(t, e.LotNumber)
	require.EqualValues(t, 10, e.QuantityUnits)
	require.EqualValues(t, 3, e.ReturnedUnits, "a return is shown against its supply, never by removing it")
	require.Equal(t, "user-pharmacist", e.Pharmacist)

	empty := do(w.r, "GET", h1Path("?from=2026-11-02&to=2026-11-30"), "pharmacist", "")
	require.Equal(t, http.StatusOK, empty.Code)
	require.Contains(t, empty.Body.String(), `"data":[]`)

	var reads int64
	require.NoError(t, w.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT count(*) FROM audit_events WHERE resource_type='pharmacy_h1_register' AND action='register_read' AND actor_subject='user-pharmacist'`).Scan(&reads).Error
	}))
	require.EqualValues(t, 2, reads, "every read of the register is recorded")
}

func TestTheH1RegisterIsThePharmacistsAlone(t *testing.T) {
	w, _ := h1World(t)
	period := "?from=2026-11-01&to=2026-11-01"
	require.Equal(t, http.StatusForbidden, do(w.r, "GET", h1Path(period), "clerk", "").Code, "patient and prescriber details are not for every outlet reader")
	require.Equal(t, http.StatusForbidden, do(w.r, "GET", h1Path("/export"+period), "clerk", "").Code)
	require.Equal(t, http.StatusNotFound, do(w.r, "GET", h1Path(period), "foreign", "").Code)
	require.Equal(t, http.StatusForbidden, do(w.r, "GET", h1Path(period), "owner", "").Code, "decision 3: the owner does not see patient details")
	// 366 days inclusive is the longest period, in an ordinary and a leap year.
	for _, bad := range []string{"", "?from=2026-11-01", "?from=2026-11-02&to=2026-11-01", "?from=2025-10-31&to=2026-11-01",
		"?from=2023-12-31&to=2024-12-31", "?from=01/11/2026&to=2026-11-01"} {
		require.Equal(t, http.StatusBadRequest, do(w.r, "GET", h1Path(bad), "pharmacist", "").Code, bad)
	}
	for _, good := range []string{"?from=2025-11-01&to=2026-11-01", "?from=2024-01-01&to=2024-12-31", "?from=2026-11-01&to=2026-11-01"} {
		require.Equal(t, http.StatusOK, do(w.r, "GET", h1Path(good), "pharmacist", "").Code, good)
	}
}

func TestTheH1RegisterExportsAnAuditedCSV(t *testing.T) {
	w, _ := h1World(t)
	res := do(w.r, "GET", h1Path("/export?from=2026-11-01&to=2026-11-01"), "pharmacist", "")
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	require.Equal(t, "text/csv; charset=utf-8", res.Header().Get("Content-Type"))
	require.Equal(t, "private, no-store", res.Header().Get("Cache-Control"))
	require.Equal(t, "1", res.Header().Get("X-Export-Row-Count"))
	lines := strings.Split(strings.TrimSpace(res.Body.String()), "\n")
	require.Len(t, lines, 2)
	require.True(t, strings.HasPrefix(lines[0], "supply_date,invoice_number,line_number,prescriber_name"))
	require.Contains(t, lines[1], "TEST Ravi Kumar")
	require.Contains(t, lines[1], ",10,3,user-pharmacist")
	var exports int64
	require.NoError(t, w.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT count(*) FROM audit_events WHERE resource_type='pharmacy_h1_register' AND action='register_export'`).Scan(&exports).Error
	}))
	require.EqualValues(t, 1, exports)
}

// A cell that a spreadsheet would run as a formula is neutralised.
func TestTheH1RegisterExportCannotCarryAFormula(t *testing.T) {
	w := newSaleWorld(t, true)
	h1 := w.product(t, "TEST-H1-DRUG", "H1")
	w.shelve(t, h1, "2028-06-30", 12500, 10)
	id := uuid.Must(uuid.NewV7()).String()
	body := fmt.Sprintf(`{"prescriber_name":"=HYPERLINK(1)","prescriber_registration":"MMC/2011/04567","prescriber_council":"Maharashtra Medical Council","prescribed_on":"2026-10-30","patient_name":"TEST Ravi Kumar","prescriber_address":%q,"patient_address":%q,"lines":[{"prescribed_as":"TEST as written","product_id":%q,"quantity_units":10,"dispensings_allowed":1}]}`,
		prescriberAddress, patientAddress, h1)
	require.Equal(t, http.StatusCreated, do(w.r, "PUT", licencePath("/prescriptions/"+id), "pharmacist", body).Code)
	w.releasePrescriptionImage(t, id)
	code, _, out := w.sell(t, rxSale(h1, 10, tenUnits, id, ""))
	require.Equal(t, http.StatusCreated, code, out)
	res := do(w.r, "GET", h1Path("/export?from=2026-11-01&to=2026-11-01"), "pharmacist", "")
	require.Equal(t, http.StatusOK, res.Code)
	require.NotContains(t, res.Body.String(), ",=HYPERLINK")
}

// An export over its bound is refused, never truncated, and as nothing was
// disclosed it records no export.
func TestAnOversizedH1ExportIsRefusedAndRecordsNothing(t *testing.T) {
	w, _ := h1World(t)
	pharmacy.SetH1ExportRows(t, 0)
	res := do(w.r, "GET", h1Path("/export?from=2026-11-01&to=2026-11-01"), "pharmacist", "")
	require.Equal(t, http.StatusRequestEntityTooLarge, res.Code, res.Body.String())
	require.Contains(t, res.Body.String(), "register_export_too_large")
	var exports int64
	require.NoError(t, w.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT count(*) FROM audit_events WHERE resource_type='pharmacy_h1_register' AND action='register_export'`).Scan(&exports).Error
	}))
	require.Zero(t, exports)
}
