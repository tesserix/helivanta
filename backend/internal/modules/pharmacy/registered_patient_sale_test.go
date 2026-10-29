package pharmacy_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	patientmastercontract "github.com/tesserix/helivanta/internal/modules/patientmaster/contract"
	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // module under test
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// #1386 B-2c: a sale to a registered patient of a hospital that verified the pharmacy.

func TestARegisteredPatientSaleNeedsAVerifiedLink(t *testing.T) {
	w := newSaleWorld(t, true)
	p := w.product(t, "TEST-PCM-500", "none")
	w.shelve(t, p, "2028-06-30", 12500, 30)
	consumers := map[string]events.Consumer{}
	for _, c := range pharmacy.New().Consumers(platform.Deps{}) {
		consumers[c.Name] = c
	}
	handle := func(name string, v any) {
		data, err := json.Marshal(v)
		require.NoError(t, err)
		c := consumers[name]
		eventType := c.Subject
		if name == "pharmacy-patient-registered" {
			eventType = patientmastercontract.TypePatientRegistered
		}
		require.NoError(t, w.db.WithTenant(tenantdb.WithActor(t.Context(), "system:"+name), testutil.TenantA, func(tx *gorm.DB) error {
			return c.Handle(t.Context(), tx, events.Event{Type: eventType, Version: 1, TenantID: testutil.TenantA, Data: data})
		}))
	}
	register := func(hospital string) string {
		patient := uuid.NewString()
		handle("pharmacy-patient-registered", patientmastercontract.PatientRegisteredData{PatientID: patient, HospitalID: hospital})
		return patient
	}
	root := "/v1/pharmacy/pharmacies/" + pharmaOrganization + "/hospital-affiliations/"
	link := func(hospital string) (affiliation string) {
		affiliation = uuid.Must(uuid.NewV7()).String()
		res := do(w.r, "PUT", root+affiliation, "editor", fmt.Sprintf(`{"hospital_id":%q,"relationship":"serving"}`, hospital))
		require.Equal(t, http.StatusOK, res.Code, res.Body.String())
		return affiliation
	}
	request := func(affiliation string) string {
		round := uuid.Must(uuid.NewV7()).String()
		res := do(w.r, "PUT", root+affiliation+"/verification-requests/"+round, "editor", `{}`)
		require.Equal(t, http.StatusOK, res.Code, res.Body.String())
		return round
	}
	decide := func(affiliation, hospital, round, outcome string) {
		handle("pharmacy-affiliation-decision", patientmastercontract.PharmacyAffiliationDecidedData{RequestID: round, AffiliationID: affiliation, HospitalID: hospital,
			DecisionID: uuid.Must(uuid.NewV7()).String(), Outcome: outcome, DecidedAt: time.Now().UTC().Format(time.RFC3339Nano)})
	}
	sale := func(patient, hospital string) string {
		return fmt.Sprintf(`{"lines":[{"product_id":%q,"quantity_units":1}],"tenders":[{"kind":"cash","amount_minor":1250,"tendered_minor":1250}],"patient":{"patient_id":%q,"hospital_id":%q}}`, p, patient, hospital)
	}

	hospital := uuid.NewString()
	patient := register(hospital)
	affiliation := link(hospital)

	code, _, out := w.sell(t, sale(patient, hospital))
	require.Equal(t, http.StatusConflict, code, out)
	require.Contains(t, out, "has not verified this pharmacy", "a declared link is not enough")
	round := request(affiliation)
	code, _, out = w.sell(t, sale(patient, hospital))
	require.Equal(t, http.StatusConflict, code, out, "nor a requested one")

	decide(affiliation, hospital, round, "accepted")
	id := uuid.Must(uuid.NewV7()).String()
	res := do(w.r, "PUT", salePath("/"+id), "pharmacist", sale(patient, hospital))
	require.Equal(t, http.StatusCreated, res.Code, res.Body.String())
	require.Contains(t, res.Body.String(), `"patient_id":"`+patient+`"`)
	require.Contains(t, res.Body.String(), `"patient_hospital_id":"`+hospital+`"`)
	require.Contains(t, res.Body.String(), `"buyer_name":null`)
	posted := res.Body.String()

	var payload string
	require.NoError(t, w.db.WithTenant(t.Context(), testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT payload::text FROM outbox_events WHERE subject='helivanta.in.pharmacy.sale_posted.v1' AND payload->'data'->>'sale_id'=?`, id).Scan(&payload).Error
	}))
	require.NotEmpty(t, payload)
	require.NotContains(t, payload, patient, "the patient never leaves pharmacy")

	code, _, out = w.sell(t, sale(uuid.NewString(), hospital))
	require.Equal(t, http.StatusConflict, code, out)
	require.Contains(t, out, "not a registration", "an id pharmacy never received")
	other := uuid.NewString()
	code, _, out = w.sell(t, sale(register(other), hospital))
	require.Equal(t, http.StatusConflict, code, out, "a patient of another hospital under this hospital's id")
	require.Contains(t, out, "not a registration")
	code, _, out = w.sell(t, sale(patient, other))
	require.Equal(t, http.StatusConflict, code, out)
	code, _, out = w.sell(t, sale(register(other), other))
	require.Equal(t, http.StatusConflict, code, out)
	require.Contains(t, out, "has not verified this pharmacy", "a real patient of a hospital the pharmacy is not verified with")
	changed := do(w.r, "PUT", salePath("/"+id), "pharmacist", sale(register(hospital), hospital))
	require.Equal(t, http.StatusConflict, changed.Code, "the same sale id naming another patient is a different sale")
	require.Contains(t, changed.Body.String(), "already exists with different details")

	both := strings.Replace(sale(patient, hospital), `"patient":`, `"buyer":{"name":"TEST Walk In"},"patient":`, 1)
	code, _, out = w.sell(t, both)
	require.Equal(t, http.StatusBadRequest, code, out)
	require.Contains(t, out, "a registered patient, an admission, or a walk-in buyer: one of them")

	decide(affiliation, hospital, round, "revoked")
	code, _, out = w.sell(t, sale(patient, hospital))
	require.Equal(t, http.StatusConflict, code, out, "a revoked link sells nothing")
	replay := do(w.r, "PUT", salePath("/"+id), "pharmacist", sale(patient, hospital))
	require.Equal(t, http.StatusOK, replay.Code, replay.Body.String())
	require.JSONEq(t, posted, replay.Body.String(), "a replay answers the posted sale, not a new one")

	again := request(affiliation)
	decide(affiliation, hospital, again, "accepted")
	code, _, out = w.sell(t, sale(patient, hospital))
	require.Equal(t, http.StatusCreated, code, out, "re-verified")
	// With the link verified, only the pair and exclusion constraints refuse.
	for name, change := range map[string]string{
		"pharmacy_sale_patient_or_buyer": `'buyer_name', 'TEST Walk In'`,
		"pharmacy_sale_patient_pair":     `'patient_id', null`,
	} {
		err := catalogueTx(t, w.db, func(tx *gorm.DB) error {
			return tx.Exec(`INSERT INTO pharmacy_sales SELECT (jsonb_populate_record(NULL::pharmacy_sales, to_jsonb(s) || jsonb_build_object(
 'id', ?::text, 'sequence_number', s.sequence_number+2000, 'invoice_number', regexp_replace(s.invoice_number,'[0-9]+$',(s.sequence_number+2000)::text),
 'intent_sha256', repeat('c',64), 'actor_subject', 'user-editor', `+change+`))).* FROM pharmacy_sales s WHERE s.id=?`, uuid.Must(uuid.NewV7()).String(), id).Error
		})
		require.ErrorContains(t, err, name)
	}
	res = do(w.r, "POST", root+affiliation+"/verification-requests/"+again+"/withdraw", "editor", `{"reason":"Closing"}`)
	require.Equal(t, http.StatusOK, res.Code, res.Body.String())
	code, _, out = w.sell(t, sale(patient, hospital))
	require.Equal(t, http.StatusConflict, code, out, "a withdrawn link sells nothing")

	// The database refuses without the handler: a copy of the posted sale,
	// renumbered, naming the patient while the link stands withdrawn.
	err := catalogueTx(t, w.db, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO pharmacy_sales SELECT (jsonb_populate_record(NULL::pharmacy_sales, to_jsonb(s) || jsonb_build_object(
 'id', ?::text, 'sequence_number', s.sequence_number+1000, 'invoice_number', regexp_replace(s.invoice_number,'[0-9]+$',(s.sequence_number+1000)::text),
 'intent_sha256', repeat('b',64)))).* FROM pharmacy_sales s WHERE s.id=?`, uuid.Must(uuid.NewV7()).String(), id).Error
	})
	require.ErrorContains(t, err, "has not verified this pharmacy")
}

// A sale waits for a verification step being applied to the hospital's link,
// then sees it: a revocation in flight is never sold through.
func TestARegisteredPatientSaleWaitsForAVerificationStep(t *testing.T) {
	w := newSaleWorld(t, true)
	p := w.product(t, "TEST-PCM-500", "none")
	w.shelve(t, p, "2028-06-30", 12500, 30)
	consumers := map[string]events.Consumer{}
	for _, c := range pharmacy.New().Consumers(platform.Deps{}) {
		consumers[c.Name] = c
	}
	apply := func(tx *gorm.DB, name string, v any) error {
		data, err := json.Marshal(v)
		require.NoError(t, err)
		c := consumers[name]
		eventType := c.Subject
		if name == "pharmacy-patient-registered" {
			eventType = patientmastercontract.TypePatientRegistered
		}
		return c.Handle(t.Context(), tx, events.Event{Type: eventType, Version: 1, TenantID: testutil.TenantA, Data: data})
	}
	inTenant := func(name string, fn func(tx *gorm.DB) error) error {
		return w.db.WithTenant(tenantdb.WithActor(t.Context(), "system:"+name), testutil.TenantA, fn)
	}
	hospital, patient := uuid.NewString(), uuid.NewString()
	require.NoError(t, inTenant("pharmacy-patient-registered", func(tx *gorm.DB) error {
		return apply(tx, "pharmacy-patient-registered", patientmastercontract.PatientRegisteredData{PatientID: patient, HospitalID: hospital})
	}))
	root := "/v1/pharmacy/pharmacies/" + pharmaOrganization + "/hospital-affiliations/"
	affiliation, round := uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
	require.Equal(t, http.StatusOK, do(w.r, "PUT", root+affiliation, "editor", fmt.Sprintf(`{"hospital_id":%q,"relationship":"owner"}`, hospital)).Code)
	require.Equal(t, http.StatusOK, do(w.r, "PUT", root+affiliation+"/verification-requests/"+round, "editor", `{}`).Code)
	decision := func(outcome string) patientmastercontract.PharmacyAffiliationDecidedData {
		return patientmastercontract.PharmacyAffiliationDecidedData{RequestID: round, AffiliationID: affiliation, HospitalID: hospital,
			DecisionID: uuid.Must(uuid.NewV7()).String(), Outcome: outcome, DecidedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	}
	require.NoError(t, inTenant("pharmacy-affiliation-decision", func(tx *gorm.DB) error {
		return apply(tx, "pharmacy-affiliation-decision", decision("accepted"))
	}))

	held, let, revoked := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		revoked <- inTenant("pharmacy-affiliation-decision", func(tx *gorm.DB) error {
			if err := apply(tx, "pharmacy-affiliation-decision", decision("revoked")); err != nil {
				return err
			}
			close(held)
			<-let
			return nil
		})
	}()
	<-held
	sold := make(chan string, 1)
	go func() {
		_, _, out := w.sell(t, fmt.Sprintf(`{"lines":[{"product_id":%q,"quantity_units":1}],"tenders":[{"kind":"cash","amount_minor":1250,"tendered_minor":1250}],"patient":{"patient_id":%q,"hospital_id":%q}}`, p, patient, hospital))
		sold <- out
	}()
	select {
	case out := <-sold:
		t.Fatalf("the sale did not wait for the revocation being applied: %s", out)
	case <-time.After(400 * time.Millisecond):
	}
	close(let)
	require.NoError(t, <-revoked)
	require.Contains(t, <-sold, "has not verified this pharmacy", "the sale sees the revocation it waited for")
}
