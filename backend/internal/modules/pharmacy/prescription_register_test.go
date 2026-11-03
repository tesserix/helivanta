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

	patientmastercontract "github.com/tesserix/helivanta/internal/modules/patientmaster/contract" //nolint:depguard // external test feeding the consumer its published contract
	"github.com/tesserix/helivanta/internal/modules/pharmacy"                                     //nolint:depguard // external test importing its own module
	"github.com/tesserix/helivanta/internal/testutil"
)

// #1062: the prescription register, Drugs Rules 1945 Rule 65(3)(1).

type rxRegisterEntry struct {
	Serial            string  `json:"serial"`
	SupplyDate        string  `json:"supply_date"`
	InvoiceNumber     string  `json:"invoice_number"`
	LineNumber        int     `json:"line_number"`
	RegulatoryClass   string  `json:"regulatory_class"`
	PrescriberName    string  `json:"prescriber_name"`
	PrescriberAddress *string `json:"prescriber_address"`
	PatientName       string  `json:"patient_name"`
	PatientAddress    *string `json:"patient_address"`
	PatientID         *string `json:"patient_id"`
	ProductID         string  `json:"product_id"`
	Manufacturer      string  `json:"manufacturer"`
	LotNumber         string  `json:"lot_number"`
	ExpiresOn         string  `json:"expires_on"`
	QuantityUnits     int64   `json:"quantity_units"`
	ReturnedUnits     int64   `json:"returned_units"`
	Pharmacist        string  `json:"pharmacist"`
}

func rxRegisterPath(suffix string) string { return licencePath("/registers/prescriptions" + suffix) }

func rxRegister(t *testing.T, w saleWorld, query string) []rxRegisterEntry {
	t.Helper()
	res := do(w.r, "GET", rxRegisterPath(query), "pharmacist", "")
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	var page struct{ Data []rxRegisterEntry }
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &page))
	return page.Data
}

// rxRegisterWorld sells H1 (with a return), H, G without addresses, and OTC.
func rxRegisterWorld(t *testing.T) (saleWorld, map[string]string) {
	t.Helper()
	w, _ := h1World(t)
	g := w.product(t, "TEST-G-DRUG", "G")
	w.shelve(t, g, "2028-06-30", 12500, 30)
	code, _, out := w.sell(t, rxSale(g, 10, tenUnits, w.prescribe(t, g, 10, 1, true), ""))
	require.Equal(t, http.StatusCreated, code, out)
	ids := map[string]string{}
	var products []struct{ ID, SKU string }
	require.NoError(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT id::text AS id, sku FROM pharmacy_catalogue_products`).Scan(&products).Error
	}))
	for _, p := range products {
		ids[p.SKU] = p.ID
	}
	return w, ids
}

func TestThePrescriptionRegisterReadsEverySupplyOnPrescription(t *testing.T) {
	w, ids := rxRegisterWorld(t)
	all := rxRegister(t, w, "?from=2026-11-01&to=2026-11-01")
	require.Len(t, all, 3, "G, H and H1 supplies; never an OTC sale")
	byClass := map[string]rxRegisterEntry{}
	for _, e := range all {
		byClass[e.RegulatoryClass] = e
	}
	h1 := byClass["H1"]
	require.Equal(t, "TS/2627/1 · line 1", h1.Serial)
	require.Equal(t, "2026-11-01", h1.SupplyDate)
	require.Equal(t, "TEST Dr Asha Rao", h1.PrescriberName)
	require.Equal(t, prescriberAddress, *h1.PrescriberAddress)
	require.Equal(t, "TEST Ravi Kumar", h1.PatientName)
	require.Equal(t, patientAddress, *h1.PatientAddress)
	require.NotEmpty(t, h1.Manufacturer)
	require.NotEmpty(t, h1.LotNumber)
	require.Equal(t, "2028-06-30", h1.ExpiresOn)
	require.EqualValues(t, 10, h1.QuantityUnits)
	require.EqualValues(t, 3, h1.ReturnedUnits, "a return is shown against its supply")
	require.Equal(t, "user-pharmacist", h1.Pharmacist)
	g := byClass["G"]
	require.Nil(t, g.PrescriberAddress, "a G prescription without addresses is shown, not hidden")
	require.Nil(t, g.PatientAddress)

	only := rxRegister(t, w, "?from=2026-11-01&to=2026-11-01&class=H")
	require.Len(t, only, 1)
	require.Equal(t, "H", only[0].RegulatoryClass)
	byProduct := rxRegister(t, w, "?from=2026-11-01&to=2026-11-01&product_id="+ids["TEST-G-DRUG"])
	require.Len(t, byProduct, 1)
	require.Equal(t, ids["TEST-G-DRUG"], byProduct[0].ProductID)
	require.Empty(t, rxRegister(t, w, "?from=2026-11-01&to=2026-11-01&patient_id=0199e000-0000-7000-8000-000000000999"))
	require.Empty(t, rxRegister(t, w, "?from=2026-11-02&to=2026-11-30"))

	for _, bad := range []string{"&class=X", "&class=none", "&product_id=nope", "&patient_id=nope"} {
		require.Equal(t, http.StatusBadRequest, do(w.r, "GET", rxRegisterPath("?from=2026-11-01&to=2026-11-01"+bad), "pharmacist", "").Code, bad)
	}
	require.Equal(t, http.StatusBadRequest, do(w.r, "GET", rxRegisterPath("?from=2025-10-31&to=2026-11-01"), "pharmacist", "").Code)
	require.Equal(t, http.StatusForbidden, do(w.r, "GET", rxRegisterPath("?from=2026-11-01&to=2026-11-01"), "owner", "").Code, "the owner does not see patients")
	require.Equal(t, http.StatusNotFound, do(w.r, "GET", rxRegisterPath("?from=2026-11-01&to=2026-11-01"), "foreign", "").Code)

	var reads int64
	require.NoError(t, w.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT count(*) FROM audit_events WHERE resource_type='pharmacy_prescription_register' AND action='register_read' AND actor_subject='user-pharmacist'`).Scan(&reads).Error
	}))
	require.EqualValues(t, 5, reads, "every successful read is recorded; a refused one is not")
}

func TestThePrescriptionRegisterFindsARegisteredPatientsSupplies(t *testing.T) {
	w := newSaleWorld(t, true)
	h := w.product(t, "TEST-H-DRUG", "H")
	w.shelve(t, h, "2028-06-30", 12500, 30)
	d := newWard(t, w)
	d.verify()
	patient := uuid.NewString()
	require.NoError(t, d.handle("pharmacy-patient-registered", patientmastercontract.PatientRegisteredData{PatientID: patient, HospitalID: d.hospital}))
	code, _, out := w.sell(t, rxSale(h, 10, tenUnits, w.prescribe(t, h, 10, 1, false), fmt.Sprintf(`,"patient":{"patient_id":%q,"hospital_id":%q}`, patient, d.hospital)))
	require.Equal(t, http.StatusCreated, code, out)
	got := rxRegister(t, w, "?from=2026-11-01&to=2026-11-01&patient_id="+patient)
	require.Len(t, got, 1)
	require.Equal(t, patient, *got[0].PatientID)
}

func TestThePrescriptionRegisterExportsAnAuditedCSV(t *testing.T) {
	w, _ := rxRegisterWorld(t)
	res := do(w.r, "GET", rxRegisterPath("/export?from=2026-11-01&to=2026-11-01&class=G"), "pharmacist", "")
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	require.Equal(t, "text/csv; charset=utf-8", res.Header().Get("Content-Type"))
	require.Equal(t, "private, no-store", res.Header().Get("Cache-Control"))
	require.Equal(t, "1", res.Header().Get("X-Export-Row-Count"))
	lines := strings.Split(strings.TrimSpace(res.Body.String()), "\n")
	require.Len(t, lines, 2)
	require.True(t, strings.HasPrefix(lines[0], "serial,supply_date,regulatory_class,prescriber_name"), lines[0])
	require.Contains(t, lines[1], "not recorded", "a missing address says so in the CSV")

	pharmacy.SetRxRegisterExportRows(t, 0)
	res = do(w.r, "GET", rxRegisterPath("/export?from=2026-11-01&to=2026-11-01"), "pharmacist", "")
	require.Equal(t, http.StatusRequestEntityTooLarge, res.Code, res.Body.String())
	var exports int64
	require.NoError(t, w.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT count(*) FROM audit_events WHERE resource_type='pharmacy_prescription_register' AND action='register_export'`).Scan(&exports).Error
	}))
	require.EqualValues(t, 1, exports, "the refused export records nothing")
}
