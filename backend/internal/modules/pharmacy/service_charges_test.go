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

	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract" //nolint:depguard // external test reading its own contract
)

// #1388: service charges, versioned and reviewed, billed as their own lines.

func chargePath(charge, suffix string) string {
	return licencePath("/service-charges/" + charge + suffix)
}

// ownCharge is a delivery charge with its own SAC at 18%.
func ownCharge(code, channel string, amount int64, effective string) string {
	return fmt.Sprintf(`{"code":%q,"name":"Ward delivery","channel":%q,"amount_minor":%d,"status":"available","tax_treatment":"own_service",
		"sac_code":"996812","supply_nature":"taxable","rate_ppm":180000,"tax_basis":"TEST accountant note 1","effective_from":%q}`, code, channel, amount, effective)
}

func principalCharge(code, channel string, amount int64, effective string) string {
	return fmt.Sprintf(`{"code":%q,"name":"Handling","channel":%q,"amount_minor":%d,"status":"available","tax_treatment":"principal_supply",
		"tax_basis":"TEST composite supply, CGST s.8(a)","effective_from":%q}`, code, channel, amount, effective)
}

type chargeVersionResp struct {
	ID       string  `json:"id"`
	Code     string  `json:"code"`
	Status   string  `json:"status"`
	Decision *string `json:"decision"`
}

type chargeResp struct {
	ID      string              `json:"id"`
	Code    string              `json:"code"`
	InForce *chargeVersionResp  `json:"in_force"`
	Latest  *chargeVersionResp  `json:"latest"`
	History []chargeVersionResp `json:"history"`
}

// declareCharge records a version by the editor and, unless decision is
// empty, has the pharmacist decide it. It returns the version id.
func (w saleWorld) declareCharge(t *testing.T, charge, body, decision string) string {
	t.Helper()
	id := uuid.Must(uuid.NewV7()).String()
	res := do(w.r, "PUT", chargePath(charge, "/versions/"+id), "editor", body)
	require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
	if decision != "" {
		res = do(w.r, "POST", chargePath(charge, "/versions/"+id+"/review"), "pharmacist", `{"decision":"`+decision+`","reason":"checked with the accountant"}`)
		require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	}
	return id
}

func (w saleWorld) charge(t *testing.T, charge string) chargeResp {
	t.Helper()
	res := do(w.r, "GET", chargePath(charge, ""), "clerk", "")
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	var out chargeResp
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &out))
	return out
}

func TestAServiceChargeNeedsASecondPerson(t *testing.T) {
	w := newSaleWorld(t, true)
	charge := uuid.Must(uuid.NewV7()).String()
	id := uuid.Must(uuid.NewV7()).String()
	body := ownCharge("DELIVERY", "counter", 5000, "2026-11-01")
	res := do(w.r, "PUT", chargePath(charge, "/versions/"+id), "editor", body)
	require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
	require.Equal(t, http.StatusOK, do(w.r, "PUT", chargePath(charge, "/versions/"+id), "editor", body).Code, "a retry replays")
	require.Equal(t, http.StatusConflict, do(w.r, "PUT", chargePath(charge, "/versions/"+id), "editor", ownCharge("DELIVERY", "counter", 6000, "2026-11-01")).Code, "the same id with another amount")
	require.Equal(t, http.StatusForbidden, do(w.r, "PUT", chargePath(charge, "/versions/"+uuid.Must(uuid.NewV7()).String()), "clerk", body).Code)
	s := w.charge(t, charge)
	require.Nil(t, s.InForce, "awaiting review, nothing is billed")
	require.Equal(t, id, s.Latest.ID)
	require.Nil(t, s.Latest.Decision)

	res = do(w.r, "POST", chargePath(charge, "/versions/"+id+"/review"), "editor", `{"decision":"approved","reason":"mine"}`)
	require.Equal(t, http.StatusConflict, res.Code, res.Body.String())
	require.Contains(t, res.Body.String(), "four_eyes")
	require.Equal(t, http.StatusForbidden, do(w.r, "POST", chargePath(charge, "/versions/"+id+"/review"), "clerk", `{"decision":"approved","reason":"x"}`).Code)
	require.Equal(t, http.StatusOK, do(w.r, "POST", chargePath(charge, "/versions/"+id+"/review"), "pharmacist", `{"decision":"approved","reason":"checked"}`).Code)
	require.Equal(t, http.StatusConflict, do(w.r, "POST", chargePath(charge, "/versions/"+id+"/review"), "pharmacist", `{"decision":"rejected","reason":"again"}`).Code, "reviewed once")
	s = w.charge(t, charge)
	require.NotNil(t, s.InForce)
	require.Equal(t, id, s.InForce.ID)
	require.Equal(t, "DELIVERY", s.Code)

	// The code is fixed, and unique in the outlet.
	res = do(w.r, "PUT", chargePath(charge, "/versions/"+uuid.Must(uuid.NewV7()).String()), "editor", ownCharge("DELIVERY2", "counter", 5000, "2026-11-01"))
	require.Equal(t, http.StatusConflict, res.Code, res.Body.String())
	res = do(w.r, "PUT", chargePath(uuid.Must(uuid.NewV7()).String(), "/versions/"+uuid.Must(uuid.NewV7()).String()), "editor", body)
	require.Equal(t, http.StatusConflict, res.Code, res.Body.String())
	require.Contains(t, res.Body.String(), "already uses this code")

	for _, bad := range []string{
		`{"code":"X","name":"Handling","channel":"counter","amount_minor":100,"status":"available","tax_treatment":"principal_supply","tax_basis":"TEST basis","effective_from":"2026-11-01"}`,
		`{"code":"HANDLING","name":"Handling","channel":"counter","amount_minor":100,"status":"available","tax_treatment":"principal_supply","sac_code":"996812","tax_basis":"TEST basis","effective_from":"2026-11-01"}`,
		`{"code":"HANDLING","name":"Handling","channel":"counter","amount_minor":100,"status":"available","tax_treatment":"own_service","sac_code":"996812","tax_basis":"TEST basis","effective_from":"2026-11-01"}`,
		`{"code":"HANDLING","name":"Handling","channel":"counter","amount_minor":100,"status":"available","tax_treatment":"own_service","sac_code":"123456","supply_nature":"taxable","rate_ppm":180000,"tax_basis":"TEST basis","effective_from":"2026-11-01"}`,
		`{"code":"HANDLING","name":"Handling","channel":"counter","amount_minor":100,"status":"available","tax_treatment":"own_service","sac_code":"996812","supply_nature":"exempt","rate_ppm":0,"tax_basis":"TEST basis","effective_from":"2026-11-01"}`,
		`{"code":"HANDLING","name":"Handling","channel":"counter","amount_minor":100,"status":"available","tax_treatment":"own_service","sac_code":"996812","supply_nature":"nil_rated","rate_ppm":50000,"tax_basis":"TEST basis","effective_from":"2026-11-01"}`,
		`{"code":"HANDLING","name":"Handling","channel":"home_delivery","amount_minor":100,"status":"available","tax_treatment":"principal_supply","tax_basis":"TEST basis","effective_from":"2026-11-01"}`,
		`{"code":"HANDLING","name":"Handling","channel":"counter","amount_minor":0,"status":"available","tax_treatment":"principal_supply","tax_basis":"TEST basis","effective_from":"2026-11-01"}`,
		`{"code":"HANDLING","name":"Handling","channel":"counter","amount_minor":100,"status":"available","tax_treatment":"principal_supply","tax_basis":"TEST","effective_from":"2026-11-01"}`,
	} {
		require.Equal(t, http.StatusBadRequest, do(w.r, "PUT", chargePath(uuid.Must(uuid.NewV7()).String(), "/versions/"+uuid.Must(uuid.NewV7()).String()), "editor", bad).Code, bad)
	}
	exempt := `{"code":"NURSE","name":"Nursing","channel":"ward_delivery","amount_minor":100,"status":"available","tax_treatment":"own_service","sac_code":"999311","supply_nature":"exempt","rate_ppm":0,"exemption_basis":"TEST Notification 12/2017 entry 74","tax_basis":"TEST basis","effective_from":"2026-11-01"}`
	require.Equal(t, http.StatusCreated, do(w.r, "PUT", chargePath(uuid.Must(uuid.NewV7()).String(), "/versions/"+uuid.Must(uuid.NewV7()).String()), "editor", exempt).Code)
	require.Equal(t, http.StatusNotFound, do(w.r, "GET", chargePath(uuid.Must(uuid.NewV7()).String(), ""), "clerk", "").Code)
	require.Equal(t, http.StatusNotFound, do(w.r, "GET", chargePath(charge, ""), "foreign", "").Code, "another tenant does not see it")

	res = do(w.r, "GET", licencePath("/service-charges"), "clerk", "")
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	var list struct{ Items []chargeResp }
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &list))
	require.Len(t, list.Items, 2)
	require.Equal(t, "DELIVERY", list.Items[0].Code)
}

func TestAPendingVersionStopsTheCharge(t *testing.T) {
	w := newSaleWorld(t, true)
	charge := uuid.Must(uuid.NewV7()).String()
	first := w.declareCharge(t, charge, ownCharge("DELIVERY", "counter", 5000, "2026-10-31"), "approved")
	w.declareCharge(t, charge, ownCharge("DELIVERY", "counter", 6000, "2026-11-02"), "approved")
	require.Equal(t, first, w.charge(t, charge).InForce.ID, "a change dated tomorrow leaves today's version in force")

	withdrawal := w.declareCharge(t, charge, `{"code":"DELIVERY","name":"Ward delivery","channel":"counter","amount_minor":5000,"status":"withdrawn","tax_treatment":"own_service",
		"sac_code":"996812","supply_nature":"taxable","rate_ppm":180000,"tax_basis":"TEST accountant note 1","effective_from":"2026-11-01"}`, "")
	require.Nil(t, w.charge(t, charge).InForce, "a pending withdrawal stops the charge at once")
	require.Equal(t, http.StatusOK, do(w.r, "POST", chargePath(charge, "/versions/"+withdrawal+"/review"), "pharmacist", `{"decision":"approved","reason":"stop"}`).Code)
	s := w.charge(t, charge)
	require.Equal(t, withdrawal, s.InForce.ID)
	require.Equal(t, "withdrawn", s.InForce.Status)

	// Only the latest version is reviewed: a replaced one keeps its state.
	stale := w.declareCharge(t, charge, ownCharge("DELIVERY", "counter", 7000, "2026-11-01"), "")
	w.declareCharge(t, charge, ownCharge("DELIVERY", "counter", 8000, "2026-11-01"), "")
	res := do(w.r, "POST", chargePath(charge, "/versions/"+stale+"/review"), "pharmacist", `{"decision":"approved","reason":"late"}`)
	require.Equal(t, http.StatusConflict, res.Code, res.Body.String())
	require.Contains(t, res.Body.String(), "newer version")
	require.Len(t, w.charge(t, charge).History, 5)
}

type chargeLineResp struct {
	ChargeNumber int     `json:"charge_number"`
	ChargeID     string  `json:"charge_id"`
	VersionID    string  `json:"version_id"`
	Code         string  `json:"code"`
	TaxTreatment string  `json:"tax_treatment"`
	SACCode      *string `json:"sac_code"`
	SupplyNature string  `json:"supply_nature"`
	RatePPM      int64   `json:"rate_ppm"`
	AmountMinor  int64   `json:"amount_minor"`
	TaxableMinor int64   `json:"taxable_minor"`
	CGSTMinor    int64   `json:"cgst_minor"`
	SGSTMinor    int64   `json:"sgst_minor"`
}

type chargedSale struct {
	ID           string                                                 `json:"id"`
	TotalMinor   int64                                                  `json:"total_minor"`
	TaxableMinor int64                                                  `json:"taxable_minor"`
	CGSTMinor    int64                                                  `json:"cgst_minor"`
	Lines        []struct{ AmountMinor, TaxableMinor, CGSTMinor int64 } `json:"lines"`
	Charges      []chargeLineResp                                       `json:"charges"`
}

// chargeRef names a charge with the version a quote would price: in force,
// else the latest, else a made-up one for a charge that does not exist.
func (w saleWorld) chargeRef(t *testing.T, charge string) string {
	t.Helper()
	version := uuid.Must(uuid.NewV7()).String()
	if res := do(w.r, "GET", chargePath(charge, ""), "clerk", ""); res.Code == http.StatusOK {
		var c chargeResp
		require.NoError(t, json.Unmarshal(res.Body.Bytes(), &c))
		if c.InForce != nil {
			version = c.InForce.ID
		} else if c.Latest != nil {
			version = c.Latest.ID
		}
	}
	return fmt.Sprintf(`{"charge_id":%q,"version_id":%q}`, charge, version)
}

func (w saleWorld) chargedCashSale(t *testing.T, product string, units, amount int64, charges ...string) string {
	t.Helper()
	refs := []string{}
	for _, c := range charges {
		refs = append(refs, w.chargeRef(t, c))
	}
	return fmt.Sprintf(`{"lines":[{"product_id":%q,"quantity_units":%d}],"charges":[%s],"tenders":[{"kind":"cash","amount_minor":%d,"tendered_minor":%d}]}`, product, units, strings.Join(refs, ","), amount, amount)
}

func (w saleWorld) postCharged(t *testing.T, body string) (int, chargedSale, string) {
	t.Helper()
	res := do(w.r, "PUT", salePath("/"+uuid.Must(uuid.NewV7()).String()), "pharmacist", body)
	var s chargedSale
	if res.Code < 300 {
		require.NoError(t, json.Unmarshal(res.Body.Bytes(), &s))
	}
	return res.Code, s, res.Body.String()
}

func TestAChargeIsItsOwnLine(t *testing.T) {
	w := newSaleWorld(t, true)
	p := w.product(t, "TEST-PCM-500", "none")
	w.shelve(t, p, "2028-06-30", 12500, 30)
	delivery := uuid.Must(uuid.NewV7()).String()
	version := w.declareCharge(t, delivery, ownCharge("DELIVERY", "counter", 5000, "2026-11-01"), "approved")
	handling := uuid.Must(uuid.NewV7()).String()
	w.declareCharge(t, handling, principalCharge("HANDLING", "counter", 2000, "2026-11-01"), "approved")

	code, plain, out := w.postCharged(t, w.chargedCashSale(t, p, 2, 2500))
	require.Equal(t, http.StatusCreated, code, out)
	require.Empty(t, plain.Charges)

	res := do(w.r, "POST", salePath("/quote"), "pharmacist", fmt.Sprintf(`{"lines":[{"product_id":%q,"quantity_units":2}],"charges":[{"charge_id":%q},{"charge_id":%q}]}`, p, delivery, handling))
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	var quote chargedSale
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &quote))
	require.Equal(t, int64(2500+5000+2000), quote.TotalMinor)

	code, sale, out := w.postCharged(t, w.chargedCashSale(t, p, 2, 9500, delivery, handling))
	require.Equal(t, http.StatusCreated, code, out)
	require.Equal(t, plain.Lines, sale.Lines, "the medicine line is priced exactly as without the charges")
	require.Len(t, sale.Charges, 2)
	own := sale.Charges[0]
	sac := "996812"
	// 5000 at 18% inside: half = round(5000 × 0.18 / 2.36) = 381.
	require.Equal(t, chargeLineResp{ChargeNumber: 1, ChargeID: delivery, VersionID: version, Code: "DELIVERY", TaxTreatment: "own_service", SACCode: &sac,
		SupplyNature: "taxable", RatePPM: 180000, AmountMinor: 5000, TaxableMinor: 4238, CGSTMinor: 381, SGSTMinor: 381}, own)
	principal := sale.Charges[1]
	// Taxed as the medicine, 12%: half = round(2000 × 0.12 / 2.24) = 107.
	require.Nil(t, principal.SACCode)
	require.Equal(t, "taxable", principal.SupplyNature)
	require.Equal(t, int64(120000), principal.RatePPM)
	require.Equal(t, []int64{2000, 1786, 107, 107}, []int64{principal.AmountMinor, principal.TaxableMinor, principal.CGSTMinor, principal.SGSTMinor})
	require.Equal(t, int64(9500), sale.TotalMinor)
	require.Equal(t, plain.TaxableMinor+4238+1786, sale.TaxableMinor)
	require.Equal(t, plain.CGSTMinor+381+107, sale.CGSTMinor)

	// Tenders pay the charges too; a charge appears once; a charge alone is no sale.
	code, _, out = w.postCharged(t, w.chargedCashSale(t, p, 2, 2500, delivery))
	require.Equal(t, http.StatusConflict, code, out)
	code, _, out = w.postCharged(t, w.chargedCashSale(t, p, 2, 12500, delivery, delivery))
	require.Equal(t, http.StatusBadRequest, code, out)
	code, _, out = w.postCharged(t, fmt.Sprintf(`{"lines":[],"charges":[{"charge_id":%q}],"tenders":[{"kind":"cash","amount_minor":5000,"tendered_minor":5000}]}`, delivery))
	require.Equal(t, http.StatusBadRequest, code, out)
	code, _, out = w.postCharged(t, w.chargedCashSale(t, p, 2, 7500, uuid.Must(uuid.NewV7()).String()))
	require.Equal(t, http.StatusConflict, code, out)
	require.Contains(t, out, "not one of this outlet")

	// The database holds a hand-written charge line to the version in force,
	// with that version's values.
	forge := func(name string) error {
		return catalogueTx(t, w.db, func(tx *gorm.DB) error {
			id := uuid.Must(uuid.NewV7())
			if err := tx.Exec(`INSERT INTO pharmacy_sales (id,tenant_id,outlet_id,financial_year,sequence_number,invoice_number,sale_date,invoice_profile_id,seller_legal_name,
				seller_address,seller_gstin,place_of_supply,currency,total_minor,taxable_minor,cgst_minor,sgst_minor,intent_sha256,actor_subject)
				SELECT ?,tenant_id,outlet_id,financial_year,99,'TS/2627/99',sale_date,invoice_profile_id,seller_legal_name,seller_address,seller_gstin,place_of_supply,
				currency,total_minor,taxable_minor,cgst_minor,sgst_minor,intent_sha256,'user-editor' FROM pharmacy_sales WHERE id=?`, id, sale.ID).Error; err != nil {
				return err
			}
			return tx.Exec(`INSERT INTO pharmacy_sale_charge_lines (tenant_id,outlet_id,sale_id,charge_number,charge_id,version_id,code,name,tax_treatment,sac_code,
				supply_nature,rate_ppm,amount_minor,taxable_minor,cgst_minor,sgst_minor) SELECT tenant_id,outlet_id,?,charge_number,charge_id,version_id,code,`+name+`,tax_treatment,sac_code,
				supply_nature,rate_ppm,amount_minor,taxable_minor,cgst_minor,sgst_minor FROM pharmacy_sale_charge_lines WHERE sale_id=? AND charge_number=1`, id, sale.ID).Error
		})
	}
	require.ErrorContains(t, forge("'TEST other name'"), "is the version in force", "a value that is not the version's")
	require.ErrorContains(t, forge("name"), "a sale is its lines", "a faithful copy passes the guard and reaches the balance check")

	// A later version never alters the issued invoice, and replaces the old one.
	w.declareCharge(t, delivery, ownCharge("DELIVERY", "counter", 9000, "2026-11-01"), "approved")
	res = do(w.r, "GET", salePath("/"+sale.ID), "pharmacist", "")
	require.Equal(t, http.StatusOK, res.Code)
	var again chargedSale
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &again))
	require.Equal(t, sale.Charges, again.Charges)
	require.ErrorContains(t, forge("name"), "is the version in force", "a replaced version")
}

func TestPrincipalChargeFollowsTheMedicines(t *testing.T) {
	w := newSaleWorld(t, true)
	twelve := w.product(t, "TEST-PCM-500", "none")
	w.shelve(t, twelve, "2028-06-30", 12500, 30)
	five := w.product(t, "TEST-ORS", "none")
	res := do(w.r, "POST", productPath(five)+"/tax-classifications", "editor", classificationBody("30049099", "taxable", "5", "", w.taxFrom, "TEST corrected rate"))
	require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
	w.shelve(t, five, "2028-06-30", 12500, 30)
	nil0 := w.product(t, "TEST-NIL", "none")
	res = do(w.r, "POST", productPath(nil0)+"/tax-classifications", "editor", classificationBody("30049099", "nil_rated", "0", "", w.taxFrom, "TEST nil rated"))
	require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
	w.shelve(t, nil0, "2028-06-30", 12500, 30)
	handling := uuid.Must(uuid.NewV7()).String()
	w.declareCharge(t, handling, principalCharge("HANDLING", "counter", 2000, "2026-11-01"), "approved")

	mixed := fmt.Sprintf(`{"lines":[{"product_id":%q,"quantity_units":2},{"product_id":%q,"quantity_units":2}],"charges":[%s],"tenders":[{"kind":"cash","amount_minor":7000,"tendered_minor":7000}]}`, twelve, five, w.chargeRef(t, handling))
	code, _, out := w.postCharged(t, mixed)
	require.Equal(t, http.StatusConflict, code, out)
	require.Contains(t, out, "must all have one GST rate")

	code, sale, out := w.postCharged(t, w.chargedCashSale(t, nil0, 2, 4500, handling))
	require.Equal(t, http.StatusCreated, code, out)
	require.Equal(t, "nil_rated", sale.Charges[0].SupplyNature)
	require.Equal(t, []int64{0, 2000, 0}, []int64{sale.Charges[0].RatePPM, sale.Charges[0].TaxableMinor, sale.Charges[0].CGSTMinor})
}

func TestAChargeKeepsToItsChannel(t *testing.T) {
	w := newSaleWorld(t, true)
	p := w.product(t, "TEST-PCM-500", "none")
	w.shelve(t, p, "2028-06-30", 12500, 30)
	counter := uuid.Must(uuid.NewV7()).String()
	w.declareCharge(t, counter, principalCharge("HANDLING", "counter", 2000, "2026-11-01"), "approved")
	room := uuid.Must(uuid.NewV7()).String()
	w.declareCharge(t, room, ownCharge("ROOM", "ward_delivery", 3000, "2026-11-01"), "approved")

	code, _, out := w.postCharged(t, w.chargedCashSale(t, p, 2, 5500, room))
	require.Equal(t, http.StatusConflict, code, out)
	require.Contains(t, out, "not for this kind of sale")
	res := do(w.r, "POST", salePath("/quote"), "pharmacist", fmt.Sprintf(`{"lines":[{"product_id":%q,"quantity_units":2}],"charges":[{"charge_id":%q}],"channel":"ward_delivery"}`, p, counter))
	require.Equal(t, http.StatusConflict, res.Code, res.Body.String())

	// Billed to an admission, the ward charge goes on the account with the medicine.
	d := newWard(t, w)
	d.verify()
	s := d.admit("R-1", "A-1")
	body := fmt.Sprintf(`{"lines":[{"product_id":%q,"quantity_units":2}],"charges":[%s],"tenders":[{"kind":"admission_account","amount_minor":5500}],"admission":{"occupancy_id":%q}}`, p, w.chargeRef(t, room), s.occupancy)
	code, sale, out := w.postCharged(t, body)
	require.Equal(t, http.StatusCreated, code, out)
	require.Equal(t, int64(5500), sale.TotalMinor)
	var amounts []int64
	require.NoError(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT (payload->'data'->>'amount_minor')::bigint FROM outbox_events WHERE subject=? ORDER BY created_at`, pharmacycontract.SubjectAdmissionChargePosted).Scan(&amounts).Error
	}))
	require.Equal(t, []int64{5500}, amounts, "the admission is charged what was billed, the service charge included")
	body = fmt.Sprintf(`{"lines":[{"product_id":%q,"quantity_units":2}],"charges":[%s],"tenders":[{"kind":"admission_account","amount_minor":4500}],"admission":{"occupancy_id":%q}}`, p, w.chargeRef(t, counter), s.occupancy)
	code, _, out = w.postCharged(t, body)
	require.Equal(t, http.StatusConflict, code, out)
}

func TestAChargeChangedUnderTheSaleIsRefused(t *testing.T) {
	w := newSaleWorld(t, true)
	p := w.product(t, "TEST-PCM-500", "none")
	w.shelve(t, p, "2028-06-30", 12500, 30)
	delivery := uuid.Must(uuid.NewV7()).String()
	w.declareCharge(t, delivery, ownCharge("DELIVERY", "counter", 5000, "2026-11-01"), "approved")
	// A newer version awaiting review: the charge is not billed at all.
	w.declareCharge(t, delivery, ownCharge("DELIVERY", "counter", 6000, "2026-11-01"), "")
	code, _, out := w.postCharged(t, w.chargedCashSale(t, p, 2, 7500, delivery))
	require.Equal(t, http.StatusConflict, code, out)
	require.Contains(t, out, "not available today")
}

func chargeReturn(sale string, charges []int, lines string, refund int64, kind string) string {
	list := ""
	for i, c := range charges {
		if i > 0 {
			list += ","
		}
		list += fmt.Sprintf(`{"sale_charge":%d}`, c)
	}
	return fmt.Sprintf(`{"sale_id":%q,"lines":[%s],"charges":[%s],"refunds":[{"kind":%q,"amount_minor":%d}],"reason":"TEST delivery not made",%s}`,
		sale, lines, list, kind, refund, recipient)
}

type creditedResp struct {
	TotalMinor   int64 `json:"total_minor"`
	TaxableMinor int64 `json:"taxable_minor"`
	CGSTMinor    int64 `json:"cgst_minor"`
	Lines        []struct {
		AmountMinor int64 `json:"amount_minor"`
	} `json:"lines"`
	Charges []struct {
		SaleCharge   int    `json:"sale_charge"`
		Code         string `json:"code"`
		AmountMinor  int64  `json:"amount_minor"`
		TaxableMinor int64  `json:"taxable_minor"`
		CGSTMinor    int64  `json:"cgst_minor"`
	} `json:"charges"`
}

func TestAChargeIsCreditedOnce(t *testing.T) {
	w := newSaleWorld(t, true)
	p := w.product(t, "TEST-PCM-500", "none")
	w.shelve(t, p, "2028-06-30", 12500, 30)
	delivery := uuid.Must(uuid.NewV7()).String()
	w.declareCharge(t, delivery, ownCharge("DELIVERY", "counter", 5000, "2026-11-01"), "approved")
	handling := uuid.Must(uuid.NewV7()).String()
	w.declareCharge(t, handling, principalCharge("HANDLING", "counter", 2000, "2026-11-01"), "approved")
	code, sale, out := w.postCharged(t, w.chargedCashSale(t, p, 2, 9500, delivery, handling))
	require.Equal(t, http.StatusCreated, code, out)

	// The delivery alone, whole, with no stock moving.
	res := do(w.r, "PUT", licencePath("/credit-notes/"+uuid.Must(uuid.NewV7()).String()), "pharmacist", chargeReturn(sale.ID, []int{1}, "", 5000, "cash"))
	require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
	var note creditedResp
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &note))
	require.Equal(t, []int64{5000, 4238, 381}, []int64{note.TotalMinor, note.TaxableMinor, note.CGSTMinor})
	require.Empty(t, note.Lines)
	require.Len(t, note.Charges, 1)
	require.Equal(t, "DELIVERY", note.Charges[0].Code)
	units, _ := w.available(t, p)
	require.Equal(t, int64(28), units, "a credited charge moves no stock")
	var lines []string
	require.NoError(t, catalogueTx(t, w.db, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT (payload->'data'->'lines')::text FROM outbox_events WHERE subject=?`, pharmacycontract.SubjectCreditNoteIssued).Scan(&lines).Error
	}))
	require.Equal(t, []string{"[]"}, lines, "the event lists no returned batch")

	// Credited once: again, alone or beside a medicine line, is refused.
	res = do(w.r, "PUT", licencePath("/credit-notes/"+uuid.Must(uuid.NewV7()).String()), "pharmacist", chargeReturn(sale.ID, []int{1}, "", 5000, "cash"))
	require.Equal(t, http.StatusConflict, res.Code, res.Body.String())
	require.Contains(t, res.Body.String(), "already been credited")
	medicine := fmt.Sprintf(`{"sale_line":1,"quantity_units":1,"quarantine_location_id":%q}`, w.quarantine)
	res = do(w.r, "PUT", licencePath("/credit-notes/"+uuid.Must(uuid.NewV7()).String()), "pharmacist", chargeReturn(sale.ID, []int{2}, medicine, 1250+2000, "cash"))
	require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &note))
	require.Equal(t, int64(3250), note.TotalMinor)
	require.Len(t, note.Lines, 1)
	require.Equal(t, 2, note.Charges[0].SaleCharge)

	res = do(w.r, "GET", salePath("/"+sale.ID), "pharmacist", "")
	require.Equal(t, http.StatusOK, res.Code)
	require.Contains(t, res.Body.String(), `"credited_minor":5000`)
	require.Contains(t, res.Body.String(), `"credited_minor":2000`)

	for _, bad := range []string{
		chargeReturn(sale.ID, nil, "", 0, "cash"),
		chargeReturn(sale.ID, []int{1, 1}, "", 10000, "cash"),
		chargeReturn(sale.ID, []int{6}, "", 5000, "cash"),
	} {
		res = do(w.r, "PUT", licencePath("/credit-notes/"+uuid.Must(uuid.NewV7()).String()), "pharmacist", bad)
		require.Equal(t, http.StatusBadRequest, res.Code, bad)
	}
	res = do(w.r, "PUT", licencePath("/credit-notes/"+uuid.Must(uuid.NewV7()).String()), "pharmacist", chargeReturn(sale.ID, []int{3}, "", 5000, "cash"))
	require.Equal(t, http.StatusConflict, res.Code, res.Body.String())
	require.Contains(t, res.Body.String(), "no service charge 3")

	// The database credits a charge whole, of the note's own sale.
	err := catalogueTx(t, w.db, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO pharmacy_credit_note_charge_lines (tenant_id,outlet_id,credit_note_id,charge_number,sale_id,sale_charge,amount_minor,taxable_minor,cgst_minor,sgst_minor)
			SELECT k.tenant_id,k.outlet_id,k.credit_note_id,2,k.sale_id,k.sale_charge,k.amount_minor,k.taxable_minor,k.cgst_minor,k.sgst_minor FROM pharmacy_credit_note_charge_lines k WHERE k.sale_id=? AND k.sale_charge=1`, sale.ID).Error
	})
	require.ErrorContains(t, err, "cannot be added to", "an issued credit note is sealed")
	err = catalogueTx(t, w.db, func(tx *gorm.DB) error {
		id := uuid.Must(uuid.NewV7())
		if err := tx.Exec(`INSERT INTO pharmacy_credit_notes SELECT (jsonb_populate_record(NULL::pharmacy_credit_notes, to_jsonb(n) || jsonb_build_object(
			'id', ?::text, 'sequence_number', n.sequence_number+100, 'credit_note_number', regexp_replace(n.credit_note_number,'[0-9]+$',(n.sequence_number+100)::text),
			'actor_subject', 'user-editor', 'posted_xact', pg_current_xact_id()::text))).* FROM pharmacy_credit_notes n WHERE n.sale_id=? ORDER BY n.created_at LIMIT 1`, id, sale.ID).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO pharmacy_credit_note_charge_lines (tenant_id,outlet_id,credit_note_id,charge_number,sale_id,sale_charge,amount_minor,taxable_minor,cgst_minor,sgst_minor)
			SELECT tenant_id,outlet_id,?,1,sale_id,charge_number,amount_minor-1,taxable_minor-1,cgst_minor,sgst_minor FROM pharmacy_sale_charge_lines WHERE sale_id=? AND charge_number=1`, id, sale.ID).Error
	})
	require.ErrorContains(t, err, "reverses a charge of its credit note's sale, whole", "part of a charge")
}

func TestTheDailyReportSeparatesServiceCharges(t *testing.T) {
	w := newSaleWorld(t, true)
	p := w.product(t, "TEST-PCM-500", "none")
	w.shelve(t, p, "2028-06-30", 12500, 30)
	delivery := uuid.Must(uuid.NewV7()).String()
	w.declareCharge(t, delivery, ownCharge("DELIVERY", "counter", 5000, "2026-11-01"), "approved")
	code, sale, out := w.postCharged(t, w.chargedCashSale(t, p, 2, 7500, delivery))
	require.Equal(t, http.StatusCreated, code, out)
	code, _, out = w.postCharged(t, w.chargedCashSale(t, p, 2, 2500))
	require.Equal(t, http.StatusCreated, code, out)
	res := do(w.r, "PUT", licencePath("/credit-notes/"+uuid.Must(uuid.NewV7()).String()), "pharmacist", chargeReturn(sale.ID, []int{1}, "", 5000, "cash"))
	require.Equal(t, http.StatusCreated, res.Code, res.Body.String())

	res = do(w.r, "GET", licencePath("/reports/daily?from=2026-11-01&to=2026-11-01"), "pharmacist", "")
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	var body struct {
		Days []struct {
			SalesMinor                       int64 `json:"sales_minor"`
			CreditsMinor                     int64 `json:"credits_minor"`
			ServiceChargesMinor              int64 `json:"service_charges_minor"`
			ServiceChargesTaxableMinor       int64 `json:"service_charges_taxable_minor"`
			ServiceChargeCreditsMinor        int64 `json:"service_charge_credits_minor"`
			ServiceChargeCreditsTaxableMinor int64 `json:"service_charge_credits_taxable_minor"`
		} `json:"days"`
	}
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &body))
	require.Len(t, body.Days, 1)
	d := body.Days[0]
	require.Equal(t, []int64{10000, 5000, 5000, 4238, 5000, 4238},
		[]int64{d.SalesMinor, d.CreditsMinor, d.ServiceChargesMinor, d.ServiceChargesTaxableMinor, d.ServiceChargeCreditsMinor, d.ServiceChargeCreditsTaxableMinor},
		"sales and credits stay invoice totals; the service share is reported beside them")
}

func TestAChargeChangedAfterTheQuoteIsRefused(t *testing.T) {
	w := newSaleWorld(t, true)
	p := w.product(t, "TEST-PCM-500", "none")
	w.shelve(t, p, "2028-06-30", 12500, 30)
	delivery := uuid.Must(uuid.NewV7()).String()
	w.declareCharge(t, delivery, ownCharge("DELIVERY", "counter", 5000, "2026-11-01"), "approved")
	quoted := w.chargedCashSale(t, p, 2, 7500, delivery)

	// The same amount under a new treatment, approved after the quote.
	w.declareCharge(t, delivery, strings.Replace(ownCharge("DELIVERY", "counter", 5000, "2026-11-01"), `"rate_ppm":180000`, `"rate_ppm":50000`, 1), "approved")
	code, _, out := w.postCharged(t, quoted)
	require.Equal(t, http.StatusConflict, code, out)
	require.Contains(t, out, "changed since it was priced")

	noVersion := fmt.Sprintf(`{"lines":[{"product_id":%q,"quantity_units":2}],"charges":[{"charge_id":%q}],"tenders":[{"kind":"cash","amount_minor":7500,"tendered_minor":7500}]}`, p, delivery)
	code, _, out = w.postCharged(t, noVersion)
	require.Equal(t, http.StatusBadRequest, code, out)

	// A charged sale replays as itself.
	body := w.chargedCashSale(t, p, 2, 7500, delivery)
	id := uuid.Must(uuid.NewV7()).String()
	first := do(w.r, "PUT", salePath("/"+id), "pharmacist", body)
	require.Equal(t, http.StatusCreated, first.Code, first.Body.String())
	again := do(w.r, "PUT", salePath("/"+id), "pharmacist", body)
	require.Equal(t, http.StatusOK, again.Code, again.Body.String())
	require.JSONEq(t, first.Body.String(), again.Body.String())
}
