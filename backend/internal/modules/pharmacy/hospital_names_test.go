package pharmacy_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	patientmastercontract "github.com/tesserix/helivanta/internal/modules/patientmaster/contract"
	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // module under test
	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract"
	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func consumerNamed(t *testing.T, name string) events.Consumer {
	t.Helper()
	for _, c := range pharmacy.New().Consumers(platform.Deps{}) {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no consumer named %s", name)
	return events.Consumer{}
}

func deliverName(db *tenantdb.DB, t *testing.T, c events.Consumer, tenant string, d workforcecontract.HospitalNamedData) error {
	t.Helper()
	data, err := json.Marshal(d)
	require.NoError(t, err)
	return db.WithTenant(tenantdb.WithActor(t.Context(), "system:"+c.Name), tenant, func(tx *gorm.DB) error {
		return c.Handle(t.Context(), tx, events.Event{Type: c.Subject, Version: 1, TenantID: tenant, Data: data})
	})
}

func TestHospitalNamesKeepTheNewestVersion(t *testing.T) {
	_, db := catalogueHarness(t)
	c := consumerNamed(t, "pharmacy-hospital-names")
	require.Equal(t, workforcecontract.SubjectHospitalNamed, c.Subject)
	h := uuid.NewString()
	stored := func() (string, int64) {
		var rows []struct {
			Name        string
			NameVersion int64
		}
		require.NoError(t, catalogueTx(t, db, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT name, name_version FROM pharmacy_hospital_names WHERE tenant_id=? AND hospital_id=?`, testutil.TenantA, h).Scan(&rows).Error
		}))
		require.Len(t, rows, 1)
		return rows[0].Name, rows[0].NameVersion
	}
	for i, d := range []workforcecontract.HospitalNamedData{{HospitalID: h, Name: "B", NameVersion: 2}, {HospitalID: h, Name: "A", NameVersion: 1}, {HospitalID: h, Name: "B", NameVersion: 2}} {
		require.NoError(t, deliverName(db, t, c, testutil.TenantA, d))
		name, version := stored() // checked after every delivery: the stale v1 must not land even briefly
		require.Equal(t, "B", name, "after delivery %d", i)
		require.EqualValues(t, 2, version)
	}
	for _, bad := range []workforcecontract.HospitalNamedData{{Name: "X", NameVersion: 1}, {HospitalID: h, Name: "  ", NameVersion: 1}, {HospitalID: h, Name: "X"}} {
		require.Error(t, deliverName(db, t, c, testutil.TenantA, bad), "%+v", bad)
	}
}

func TestHospitalNameShownOnlyForAVerifiedAffiliation(t *testing.T) {
	r, db := catalogueHarness(t)
	c := consumerNamed(t, "pharmacy-hospital-names")
	decision := consumerNamed(t, "pharmacy-affiliation-decision")
	root := "/v1/pharmacy/pharmacies/" + pharmaOrganization + "/hospital-affiliations/"
	hospitals := map[string]string{}
	require.NoError(t, deliverName(db, t, c, testutil.TenantA, workforcecontract.HospitalNamedData{HospitalID: uuid.NewString(), Name: "Unaffiliated", NameVersion: 1}))
	list := func() map[string]any {
		w := do(r, "GET", "/v1/pharmacy/pharmacies/"+pharmaOrganization+"/hospital-affiliations?limit=100", "editor", "")
		require.Equal(t, 200, w.Code, w.Body.String())
		require.NotContains(t, w.Body.String(), "Unaffiliated")
		var page struct{ Data []map[string]any }
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
		out := map[string]any{}
		for _, item := range page.Data {
			out[item["id"].(string)] = item["hospital_name"]
		}
		return out
	}
	declare := func() string {
		id, hospital := uuid.Must(uuid.NewV7()).String(), uuid.NewString()
		hospitals[id] = hospital
		require.NoError(t, deliverName(db, t, c, testutil.TenantA, workforcecontract.HospitalNamedData{HospitalID: hospital, Name: "City General", NameVersion: 1}))
		w := do(r, "PUT", root+id, "editor", fmt.Sprintf(`{"hospital_id":%q,"relationship":"owner"}`, hospital))
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), `"hospital_name":null`)
		return id
	}
	ask := func(a string) string {
		req := uuid.Must(uuid.NewV7()).String()
		w := do(r, "PUT", root+a+"/verification-requests/"+req, "editor", `{}`)
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), `"hospital_name":null`, "a requested affiliation is not verified")
		return req
	}
	decide := func(a, req, outcome string) {
		deliverDecision(t, db, decision, patientmastercontract.PharmacyAffiliationDecidedData{RequestID: req, AffiliationID: a, HospitalID: hospitals[a], DecisionID: uuid.Must(uuid.NewV7()).String(), Outcome: outcome, DecidedAt: time.Now().UTC().Format(time.RFC3339Nano)})
	}

	a := declare()
	requireNoName(t, list(), a)
	req := ask(a)
	requireNoName(t, list(), a)
	decide(a, req, "accepted")
	require.Equal(t, "City General", list()[a])
	decide(a, req, "revoked")
	requireNoName(t, list(), a)

	d := declare()
	decide(d, ask(d), "declined")
	requireNoName(t, list(), d)
	w := declare()
	wreq := ask(w)
	require.Equal(t, 200, do(r, "POST", root+w+"/verification-requests/"+wreq+"/withdraw", "editor", `{"reason":"closing"}`).Code)
	requireNoName(t, list(), w)
}

func TestHospitalNamesStayInTheirTenant(t *testing.T) {
	r, db := catalogueHarness(t)
	c := consumerNamed(t, "pharmacy-hospital-names")
	decision := consumerNamed(t, "pharmacy-affiliation-decision")
	hospital := uuid.NewString()
	require.NoError(t, deliverName(db, t, c, testutil.TenantB, workforcecontract.HospitalNamedData{HospitalID: hospital, Name: "Other Tenant Hospital", NameVersion: 1}))
	root := "/v1/pharmacy/pharmacies/" + pharmaOrganization + "/hospital-affiliations/"
	a, req := uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, do(r, "PUT", root+a, "editor", fmt.Sprintf(`{"hospital_id":%q,"relationship":"owner"}`, hospital)).Code)
	require.Equal(t, 200, do(r, "PUT", root+a+"/verification-requests/"+req, "editor", `{}`).Code)
	deliverDecision(t, db, decision, patientmastercontract.PharmacyAffiliationDecidedData{RequestID: req, AffiliationID: a, HospitalID: hospital, DecisionID: uuid.Must(uuid.NewV7()).String(), Outcome: "accepted", DecidedAt: time.Now().UTC().Format(time.RFC3339Nano)})
	w := do(r, "GET", strings.TrimSuffix(root, "/")+"?limit=100", "editor", "")
	require.Equal(t, 200, w.Code, w.Body.String())
	require.Contains(t, w.Body.String(), `"verification_status":"verified"`)
	require.Contains(t, w.Body.String(), `"hospital_name":null`)
	require.NotContains(t, w.Body.String(), "Other Tenant Hospital")
}

// requireNoName asserts the affiliation is listed and its hospital_name is null; a missing row must not pass as "no name".
func requireNoName(t *testing.T, names map[string]any, id string) {
	t.Helper()
	v, ok := names[id]
	require.True(t, ok, "affiliation %s must be listed", id)
	require.Nil(t, v)
}

// receipts returns the confirmations enqueued for a hospital, oldest first,
// and asserts each payload carries exactly hospital_id and name_version.
func receipts(t *testing.T, db *tenantdb.DB, hospital string) []pharmacycontract.HospitalNameReceivedData {
	t.Helper()
	var raw []string
	require.NoError(t, catalogueTx(t, db, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT (payload->'data')::text FROM outbox_events WHERE subject=? AND payload->'data'->>'hospital_id'=? ORDER BY created_at,id`, pharmacycontract.SubjectHospitalNameReceived, hospital).Scan(&raw).Error
	}))
	var out []pharmacycontract.HospitalNameReceivedData
	for _, r := range raw {
		var keys map[string]any
		require.NoError(t, json.Unmarshal([]byte(r), &keys))
		require.Len(t, keys, 2, "a confirmation carries no name")
		var d pharmacycontract.HospitalNameReceivedData
		require.NoError(t, json.Unmarshal([]byte(r), &d))
		out = append(out, d)
	}
	return out
}

func storedNames(t *testing.T, db *tenantdb.DB, hospital string) []string {
	t.Helper()
	var names []string
	require.NoError(t, catalogueTx(t, db, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT name FROM pharmacy_hospital_names WHERE tenant_id=? AND hospital_id=?`, testutil.TenantA, hospital).Scan(&names).Error
	}))
	return names
}

func TestHospitalNameIsConfirmed(t *testing.T) {
	_, db := catalogueHarness(t)
	c := consumerNamed(t, "pharmacy-hospital-names")
	h := uuid.NewString()
	require.NoError(t, deliverName(db, t, c, testutil.TenantA, workforcecontract.HospitalNamedData{HospitalID: h, Name: "City", NameVersion: 1}))
	require.Equal(t, []pharmacycontract.HospitalNameReceivedData{{HospitalID: h, NameVersion: 1}}, receipts(t, db, h))
	require.Equal(t, []string{"City"}, storedNames(t, db, h))
}

func TestStaleHospitalNameIsStillConfirmed(t *testing.T) {
	_, db := catalogueHarness(t)
	c := consumerNamed(t, "pharmacy-hospital-names")
	h := uuid.NewString()
	require.NoError(t, deliverName(db, t, c, testutil.TenantA, workforcecontract.HospitalNamedData{HospitalID: h, Name: "New", NameVersion: 2}))
	require.NoError(t, deliverName(db, t, c, testutil.TenantA, workforcecontract.HospitalNamedData{HospitalID: h, Name: "Old", NameVersion: 1}))
	require.Equal(t, []pharmacycontract.HospitalNameReceivedData{{HospitalID: h, NameVersion: 2}, {HospitalID: h, NameVersion: 1}}, receipts(t, db, h))
	require.Equal(t, []string{"New"}, storedNames(t, db, h))
}

func TestRejectedHospitalNameIsNotConfirmed(t *testing.T) {
	_, db := catalogueHarness(t)
	c := consumerNamed(t, "pharmacy-hospital-names")
	h := uuid.NewString()
	for _, bad := range []workforcecontract.HospitalNamedData{{HospitalID: h, Name: "", NameVersion: 1}, {HospitalID: uuid.Nil.String(), Name: "X", NameVersion: 1}, {HospitalID: h, Name: "X", NameVersion: 0}} {
		require.Error(t, deliverName(db, t, c, testutil.TenantA, bad), "%+v", bad)
	}
	require.Empty(t, receipts(t, db, h))
	require.Empty(t, receipts(t, db, uuid.Nil.String()))
	require.Empty(t, storedNames(t, db, h))
}

func TestHospitalNameAndConfirmationCommitTogether(t *testing.T) {
	_, db := catalogueHarness(t)
	c := consumerNamed(t, "pharmacy-hospital-names")
	h := uuid.NewString()
	data, err := json.Marshal(workforcecontract.HospitalNamedData{HospitalID: h, Name: "City", NameVersion: 1})
	require.NoError(t, err)
	boom := fmt.Errorf("after the handler")
	err = db.WithTenant(tenantdb.WithActor(t.Context(), "system:"+c.Name), testutil.TenantA, func(tx *gorm.DB) error {
		require.NoError(t, c.Handle(t.Context(), tx, events.Event{Type: c.Subject, Version: 1, TenantID: testutil.TenantA, Data: data}))
		return boom
	})
	require.ErrorIs(t, err, boom)
	require.Empty(t, storedNames(t, db, h))
	require.Empty(t, receipts(t, db, h))
}
