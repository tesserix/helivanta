package pharmacy_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
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
	ID      string             `json:"id"`
	Code    string             `json:"code"`
	InForce *chargeVersionResp `json:"in_force"`
	Latest  *chargeVersionResp `json:"latest"`
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
