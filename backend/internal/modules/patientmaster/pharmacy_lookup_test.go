package patientmaster_test

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/patientmaster" //nolint:depguard // module under test
	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func TestPharmacyPatientLookup(t *testing.T) {
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"tokAdmin": testutil.TenantA, "tokPharm": testutil.TenantA, "tokOther": testutil.TenantA, "tokB": testutil.TenantB},
		Perms: map[string][]authz.Permission{
			"tokAdmin": {patientmaster.PermPharmacyAffiliationReview, patientmaster.PermPatientRegister, patientmaster.PermPatientRead},
			// Pharmacists hold patient.read in the real role set; the lookup grant must not open the registry.
			"tokPharm": {patientmaster.PermPharmacyLookup, patientmaster.PermPatientRead},
			"tokOther": {patientmaster.PermPharmacyLookup},
			"tokB":     {patientmaster.PermPharmacyLookup},
		},
		Modules: []platform.Module{patientmaster.New()},
	})
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO patient_hospital_access (tenant_id,hospital_id,subject,level) VALUES (?,?,'user-tokAdmin','registrar')`, testutil.TenantA, testHospital).Error
	}))
	byName := map[string]events.Consumer{}
	for _, c := range patientmaster.New().Consumers(platform.Deps{}) {
		byName[c.Name] = c
	}
	deliver := func(name string, v any) {
		consumer := byName[name]
		encoded, err := json.Marshal(v)
		require.NoError(t, err)
		require.NoError(t, db.WithTenant(tenantdb.WithActor(ctx, "system:"+consumer.Name), testutil.TenantA, func(tx *gorm.DB) error {
			return consumer.Handle(ctx, tx, events.Event{Type: consumer.Subject, Version: 1, TenantID: testutil.TenantA, Data: encoded})
		}))
	}
	newID := func() string { return uuid.Must(uuid.NewV7()).String() }

	w := scopedDo(r, "POST", "/v1/patients", "tokAdmin", registerBody("Synthetic", "Lookup", "1990-05-10", "", false, ""))
	require.Equal(t, 201, w.Code, w.Body.String())
	var registered struct {
		Patient struct {
			ID  string `json:"id"`
			MRN string `json:"mrn"`
		} `json:"patient"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &registered))
	patient, mrn := registered.Patient.ID, registered.Patient.MRN
	require.NotEmpty(t, mrn)

	pharmacy := newID()
	claim := func() pharmacycontract.AffiliationVerificationData {
		return pharmacycontract.AffiliationVerificationData{RequestID: newID(), AffiliationID: newID(), PharmacyID: pharmacy, PharmacyName: "Synthetic Ward Pharmacy", HospitalID: testHospital, Relationship: "owner", RequesterSubject: "user-asker"}
	}
	accept := func(data pharmacycontract.AffiliationVerificationData) string {
		decision := newID()
		w := scopedDo(r, "PUT", "/v1/patients/pharmacy-affiliations/"+data.RequestID+"/decision/"+decision, "tokAdmin", `{"outcome":"accepted","reason":"Owned"}`)
		require.Equal(t, 200, w.Code, w.Body.String())
		return decision
	}
	confirm := func(data pharmacycontract.AffiliationVerificationData, decision string) {
		deliver("patient-pharmacy-affiliation-decision-receipt", pharmacycontract.AffiliationDecisionReceivedData{RequestID: data.RequestID, HospitalID: testHospital, DecisionID: decision, Applied: true})
	}
	grantPath := func(subject string) string {
		return "/v1/patients/hospitals/" + testHospital + "/pharmacy-lookup-access/" + pharmacy + "/" + subject
	}
	lookup := func(token, pharmacyID, value string) (int, string) {
		w := do(r, "GET", "/v1/patients/pharmacy-lookup?hospital_id="+testHospital+"&pharmacy_id="+pharmacyID+"&mrn="+url.QueryEscape(value), token, "")
		return w.Code, w.Body.String()
	}
	audited := func() []string {
		var rows []string
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT resource_ids::text FROM patient_access_audit WHERE action='pharmacy_lookup' AND actor_subject='user-tokPharm' AND pharmacy_id=? ORDER BY created_at,id`, pharmacy).Scan(&rows).Error
		}))
		return rows
	}

	first := claim()
	deliver("patient-pharmacy-affiliation-request", first)
	require.Equal(t, 409, do(r, "PUT", grantPath("user-tokPharm"), "tokAdmin", `{}`).Code, "no grant before the hospital accepts")
	decision := accept(first)
	require.Equal(t, 409, do(r, "PUT", grantPath("user-tokPharm"), "tokAdmin", `{}`).Code, "nor before pharmacy confirms the acceptance")
	confirm(first, decision)

	require.Equal(t, 403, do(r, "PUT", grantPath("user-tokPharm"), "tokPharm", `{}`).Code)
	w = do(r, "PUT", grantPath("user-tokPharm"), "tokAdmin", `{}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	list := do(r, "GET", "/v1/patients/hospitals/"+testHospital+"/pharmacy-lookup-access?pharmacy_id="+pharmacy, "tokAdmin", "")
	require.Equal(t, 200, list.Code, list.Body.String())
	require.Contains(t, list.Body.String(), `"subject":"user-tokPharm"`)
	require.Contains(t, list.Body.String(), `"granted_by":"user-tokAdmin"`)

	code, body := lookup("tokPharm", pharmacy, "  "+strings.ToLower(mrn)+" ")
	require.Equal(t, 200, code, body)
	var found map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &found))
	require.Equal(t, patient, found["patient_id"])
	require.Equal(t, "Synthetic Lookup", found["name"])
	require.Equal(t, false, found["age_estimated"])
	require.GreaterOrEqual(t, found["age_years"], float64(35))
	require.ElementsMatch(t, []string{"patient_id", "name", "age_years", "age_estimated", "sex"}, keys(found), "nothing beyond the minimum")

	_, refusal := lookup("tokPharm", pharmacy, "NO-SUCH-MRN")
	for name, attempt := range map[string]func() (int, string){
		"no match":       func() (int, string) { return lookup("tokPharm", pharmacy, "NO-SUCH-MRN") },
		"not granted":    func() (int, string) { return lookup("tokOther", pharmacy, mrn) },
		"other pharmacy": func() (int, string) { return lookup("tokPharm", newID(), mrn) },
		"other tenant":   func() (int, string) { return lookup("tokB", pharmacy, mrn) },
	} {
		code, body := attempt()
		require.Equal(t, 404, code, name)
		require.JSONEq(t, refusal, body, "%s answers exactly like a miss", name)
	}
	require.Equal(t, 400, do(r, "GET", "/v1/patients/pharmacy-lookup?hospital_id="+testHospital+"&pharmacy_id="+pharmacy, "tokPharm", "").Code)

	require.Equal(t, 404, scopedDo(r, "GET", "/v1/patients/"+patient, "tokPharm", "").Code, "a lookup grant opens no registry read")
	require.Equal(t, 404, scopedDo(r, "GET", "/v1/patients", "tokPharm", "").Code)

	require.Equal(t, []string{`["` + patient + `"]`, "[]", "[]"}, audited(), "every lookup by the pharmacist for this pharmacy is audited, a miss included")

	// The hospital revokes: lookups stop at once.
	w = scopedDo(r, "PUT", "/v1/patients/pharmacy-affiliations/"+first.RequestID+"/decision/"+newID(), "tokAdmin", `{"outcome":"revoked","reason":"Sold"}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	code, _ = lookup("tokPharm", pharmacy, mrn)
	require.Equal(t, 404, code, "a revocation ends lookups in the same transaction")
	confirm(first, decision)
	code, _ = lookup("tokPharm", pharmacy, mrn)
	require.Equal(t, 404, code, "a late confirmation of the revoked acceptance does not reopen the link")

	// Re-verified in a new round: the old grant does not come back.
	second := claim()
	deliver("patient-pharmacy-affiliation-request", second)
	confirm(second, accept(second))
	code, _ = lookup("tokPharm", pharmacy, mrn)
	require.Equal(t, 404, code, "a grant from an ended round is inert")
	require.NotContains(t, do(r, "GET", "/v1/patients/hospitals/"+testHospital+"/pharmacy-lookup-access?pharmacy_id="+pharmacy, "tokAdmin", "").Body.String(), "user-tokPharm")
	require.Equal(t, 200, do(r, "PUT", grantPath("user-tokPharm"), "tokAdmin", `{}`).Code)
	code, _ = lookup("tokPharm", pharmacy, mrn)
	require.Equal(t, 200, code)

	// The pharmacy withdraws: lookups stop when patientmaster hears of it.
	deliver("patient-pharmacy-affiliation-withdrawal", second)
	code, _ = lookup("tokPharm", pharmacy, mrn)
	require.Equal(t, 404, code, "a withdrawal ends lookups")

	t.Run("revoking a grant ends that pharmacist's lookups", func(t *testing.T) {
		third := claim()
		deliver("patient-pharmacy-affiliation-request", third)
		confirm(third, accept(third))
		require.Equal(t, 200, do(r, "PUT", grantPath("user-tokPharm"), "tokAdmin", `{}`).Code)
		require.Equal(t, 200, do(r, "PUT", grantPath("user-tokPharm"), "tokAdmin", `{}`).Code, "a repeated grant is answered the same")
		code, _ := lookup("tokPharm", pharmacy, mrn)
		require.Equal(t, 200, code)
		require.Equal(t, 200, do(r, "DELETE", grantPath("user-tokPharm"), "tokAdmin", "").Code)
		require.Equal(t, 200, do(r, "DELETE", grantPath("user-tokPharm"), "tokAdmin", "").Code, "a repeated revoke is answered the same")
		code, _ = lookup("tokPharm", pharmacy, mrn)
		require.Equal(t, 404, code)
		var actions []string
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT action FROM patient_access_audit WHERE access_level='pharmacy_lookup' AND target_subject='user-tokPharm' AND pharmacy_id=? ORDER BY created_at,id`, pharmacy).Scan(&actions).Error
		}))
		require.Equal(t, []string{"grant", "grant", "grant", "revoke"}, actions, "repeats record nothing")
		deliver("patient-pharmacy-affiliation-withdrawal", third)
	})

	t.Run("the database holds the rules", func(t *testing.T) {
		require.Error(t, db.WithTenant(tenantdb.WithActor(ctx, "user-tokPharm"), testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Exec(`INSERT INTO patient_pharmacy_lookup_access (tenant_id,hospital_id,pharmacy_id,request_id,subject,granted_by) VALUES (?,?,?,?,'user-tokPharm','user-tokPharm')`, testutil.TenantA, testHospital, pharmacy, first.RequestID).Error
		}), "only a registrar grants")
		require.Error(t, db.WithTenant(tenantdb.WithActor(ctx, "user-tokPharm"), testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Exec(`INSERT INTO patient_pharmacy_links (tenant_id,hospital_id,pharmacy_id,request_id) VALUES (?,?,?,?)`, testutil.TenantA, testHospital, newID(), first.RequestID).Error
		}), "only a confirmed acceptance opens a link")
		require.NoError(t, db.WithTenant(tenantdb.WithActor(ctx, "user-tokPharm"), testutil.TenantA, func(tx *gorm.DB) error {
			res := tx.Exec(`DELETE FROM patient_pharmacy_links WHERE tenant_id=?`, testutil.TenantA)
			if res.Error == nil && res.RowsAffected != 0 {
				return fmt.Errorf("deleted %d links", res.RowsAffected)
			}
			return res.Error
		}), "a non-registrar cannot end a link")
		var withLookupSession int64
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.pharmacy_lookup_subject','user-tokOther',true),set_config('app.pharmacy_lookup_hospital',?,true),set_config('app.pharmacy_lookup_pharmacy',?,true)`, testHospital, pharmacy).Error; err != nil {
				return err
			}
			return tx.Raw(`SELECT count(*) FROM patients`).Scan(&withLookupSession).Error
		}))
		require.Zero(t, withLookupSession, "a session without a grant sees no patient")
	})

	t.Run("row security alone admits only the granted subject in the current round", func(t *testing.T) {
		fourth := claim()
		deliver("patient-pharmacy-affiliation-request", fourth)
		confirm(fourth, accept(fourth))
		require.Equal(t, 200, do(r, "PUT", grantPath("user-tokPharm"), "tokAdmin", `{}`).Code)
		// A grant left over from the first, ended round.
		require.NoError(t, db.WithTenant(tenantdb.WithActor(ctx, "user-tokAdmin"), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.patient_subject','user-tokAdmin',true),set_config('app.patient_hospital',?,true)`, testHospital).Error; err != nil {
				return err
			}
			return tx.Exec(`INSERT INTO patient_pharmacy_lookup_access (tenant_id,hospital_id,pharmacy_id,request_id,subject,granted_by) VALUES (?,?,?,?,'user-stale','user-tokAdmin')`, testutil.TenantA, testHospital, pharmacy, first.RequestID).Error
		}))
		seen := func(subject, pharmacyID string) int64 {
			var n int64
			require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
				if err := tx.Exec(`SELECT set_config('app.pharmacy_lookup_subject',?,true),set_config('app.pharmacy_lookup_hospital',?,true),set_config('app.pharmacy_lookup_pharmacy',?,true),set_config('app.pharmacy_lookup_mrn',?,true)`, subject, testHospital, pharmacyID, mrn).Error; err != nil {
					return err
				}
				return tx.Raw(`SELECT count(*) FROM patients WHERE id=?`, patient).Scan(&n).Error
			}))
			return n
		}
		require.EqualValues(t, 1, seen("user-tokPharm", pharmacy))
		other := scopedDo(r, "POST", "/v1/patients", "tokAdmin", registerBody("Second", "Synthetic", "1985-01-02", "", false, ""))
		require.Equal(t, 201, other.Code, other.Body.String())
		// The session may see the one patient its MRN names, and nothing of
		// that patient beyond the patients row: no identifiers, consents or
		// blood group.
		require.NoError(t, db.WithTenant(tenantdb.WithActor(ctx, "user-tokAdmin"), testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.patient_subject','user-tokAdmin',true),set_config('app.patient_hospital',?,true)`, testHospital).Error; err != nil {
				return err
			}
			return tx.Exec(`INSERT INTO patient_identifiers (tenant_id,patient_id,kind,value) VALUES (?,?,'abha_number','91-0000-0000-0001')`, testutil.TenantA, patient).Error
		}))
		var scope struct{ Patients, Identifiers, Consents, Groups int64 }
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.pharmacy_lookup_subject','user-tokPharm',true),set_config('app.pharmacy_lookup_hospital',?,true),set_config('app.pharmacy_lookup_pharmacy',?,true),set_config('app.pharmacy_lookup_mrn',?,true)`, testHospital, pharmacy, mrn).Error; err != nil {
				return err
			}
			return tx.Raw(`SELECT (SELECT count(*) FROM patients) AS patients,(SELECT count(*) FROM patient_identifiers) AS identifiers,(SELECT count(*) FROM patient_consents) AS consents,(SELECT count(*) FROM patient_reported_blood_groups) AS groups`).Scan(&scope).Error
		}))
		require.EqualValues(t, 1, scope.Patients, "only the patient the MRN names")
		require.Zero(t, scope.Identifiers, "no identifiers")
		require.Zero(t, scope.Consents, "no consent receipts")
		require.Zero(t, scope.Groups)
		require.Zero(t, seen("user-tokOther", pharmacy), "another subject")
		require.Zero(t, seen("user-stale", pharmacy), "a grant from an ended round")
		require.Zero(t, seen("user-tokPharm", newID()), "another pharmacy")
	})

	t.Run("no audit, no answer", func(t *testing.T) {
		require.NoError(t, db.Migrate(ctx, []tenantdb.Migration{{ID: "test_pharmacy_lookup_audit_failure", SQL: `CREATE FUNCTION reject_pharmacy_lookup_audit() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.action='pharmacy_lookup' THEN RAISE EXCEPTION 'synthetic audit outage'; END IF; RETURN NEW; END $$; CREATE TRIGGER reject_pharmacy_lookup_audit BEFORE INSERT ON patient_access_audit FOR EACH ROW EXECUTE FUNCTION reject_pharmacy_lookup_audit();`}}))
		code, body := lookup("tokPharm", pharmacy, mrn)
		require.Equal(t, 500, code)
		require.NotContains(t, body, "Synthetic")
		require.NotContains(t, body, patient)
	})
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestPharmacyLinkActiveOnlyOnceBothSidesAgree(t *testing.T) {
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"tokAdmin": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"tokAdmin": {patientmaster.PermPharmacyAffiliationReview}},
		Modules: []platform.Module{patientmaster.New()},
	})
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO patient_hospital_access (tenant_id,hospital_id,subject,level) VALUES (?,?,'user-tokAdmin','registrar')`, testutil.TenantA, testHospital).Error
	}))
	byName := map[string]events.Consumer{}
	for _, c := range patientmaster.New().Consumers(platform.Deps{}) {
		byName[c.Name] = c
	}
	handle := func(tx *gorm.DB, name string, v any) error {
		consumer := byName[name]
		encoded, err := json.Marshal(v)
		require.NoError(t, err)
		if err := tx.Exec(`SELECT set_config('app.actor_subject',?,true)`, "system:"+consumer.Name).Error; err != nil {
			return err
		}
		return consumer.Handle(ctx, tx, events.Event{Type: consumer.Subject, Version: 1, TenantID: testutil.TenantA, Data: encoded})
	}
	deliver := func(name string, v any) error {
		return db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error { return handle(tx, name, v) })
	}
	newID := func() string { return uuid.Must(uuid.NewV7()).String() }
	pharmacy := newID()
	claim := func() pharmacycontract.AffiliationVerificationData {
		return pharmacycontract.AffiliationVerificationData{RequestID: newID(), AffiliationID: newID(), PharmacyID: pharmacy, PharmacyName: "Synthetic Ward Pharmacy", HospitalID: testHospital, Relationship: "owner", RequesterSubject: "user-asker"}
	}
	decide := func(data pharmacycontract.AffiliationVerificationData, outcome string) string {
		decision := newID()
		w := scopedDo(r, "PUT", "/v1/patients/pharmacy-affiliations/"+data.RequestID+"/decision/"+decision, "tokAdmin", fmt.Sprintf(`{"outcome":%q,"reason":"test"}`, outcome))
		require.Equal(t, 200, w.Code, w.Body.String())
		return decision
	}
	receipt := func(data pharmacycontract.AffiliationVerificationData, decision string, applied bool) pharmacycontract.AffiliationDecisionReceivedData {
		return pharmacycontract.AffiliationDecisionReceivedData{RequestID: data.RequestID, HospitalID: testHospital, DecisionID: decision, Applied: applied}
	}
	link := func() string {
		var round []string
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT request_id::text FROM patient_pharmacy_links WHERE tenant_id=? AND hospital_id=? AND pharmacy_id=?`, testutil.TenantA, testHospital, pharmacy).Scan(&round).Error
		}))
		if len(round) == 0 {
			return ""
		}
		return round[0]
	}
	// race holds `first` open after its work, starts `second`, then commits.
	race := func(first func(tx *gorm.DB) error, second func() error) (error, error) {
		held, let, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
		go func() {
			done <- db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
				if err := first(tx); err != nil {
					close(held)
					return err
				}
				close(held)
				<-let
				return nil
			})
		}()
		<-held
		after := make(chan error, 1)
		go func() { after <- second() }()
		time.Sleep(300 * time.Millisecond)
		close(let)
		return <-done, <-after
	}

	t.Run("a revocation racing the confirmation leaves no link", func(t *testing.T) {
		data := claim()
		require.NoError(t, deliver("patient-pharmacy-affiliation-request", data))
		accept := decide(data, "accepted")
		revokeErr, receiptErr := race(func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.actor_subject','user-tokAdmin',true),set_config('app.patient_subject','user-tokAdmin',true),set_config('app.patient_hospital',?,true)`, testHospital).Error; err != nil {
				return err
			}
			if err := tx.Exec(`SELECT 1 FROM patient_pharmacy_affiliation_reviews WHERE tenant_id=? AND request_id=? FOR UPDATE`, testutil.TenantA, data.RequestID).Error; err != nil {
				return err
			}
			if err := tx.Exec(`INSERT INTO patient_pharmacy_affiliation_decisions(id,tenant_id,hospital_id,request_id,outcome,reason,fingerprint,reviewer_subject) VALUES (?,?,?,?,'revoked','race',repeat('a',64),'user-tokAdmin')`, newID(), testutil.TenantA, testHospital, data.RequestID).Error; err != nil {
				return err
			}
			return tx.Exec(`DELETE FROM patient_pharmacy_links WHERE tenant_id=? AND request_id=?`, testutil.TenantA, data.RequestID).Error
		}, func() error {
			return deliver("patient-pharmacy-affiliation-decision-receipt", receipt(data, accept, true))
		})
		require.NoError(t, revokeErr)
		require.NoError(t, receiptErr)
		require.Empty(t, link(), "the confirmation lost to the revocation")
	})

	t.Run("a withdrawal racing the confirmation leaves no link", func(t *testing.T) {
		data := claim()
		require.NoError(t, deliver("patient-pharmacy-affiliation-request", data))
		accept := decide(data, "accepted")
		withdrawErr, receiptErr := race(func(tx *gorm.DB) error {
			return handle(tx, "patient-pharmacy-affiliation-withdrawal", data)
		}, func() error {
			return deliver("patient-pharmacy-affiliation-decision-receipt", receipt(data, accept, true))
		})
		require.NoError(t, withdrawErr)
		require.NoError(t, receiptErr)
		require.Empty(t, link())
	})

	t.Run("a confirmation of a decision pharmacy skipped opens nothing", func(t *testing.T) {
		data := claim()
		require.NoError(t, deliver("patient-pharmacy-affiliation-request", data))
		accept := decide(data, "accepted")
		require.NoError(t, deliver("patient-pharmacy-affiliation-decision-receipt", receipt(data, accept, false)))
		require.Empty(t, link())
		var due []string
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.patient_subject','user-tokAdmin',true),set_config('app.patient_hospital',?,true)`, testHospital).Error; err != nil {
				return err
			}
			return tx.Raw(`SELECT request_id::text FROM patient_pharmacy_affiliation_deliveries WHERE tenant_id=? AND request_id=? AND next_attempt_at IS NULL`, testutil.TenantA, data.RequestID).Scan(&due).Error
		}))
		require.Len(t, due, 1, "it still stops the re-sends")
		require.NoError(t, deliver("patient-pharmacy-affiliation-withdrawal", data))
	})

	t.Run("revoked or withdrawn before confirmation: no link", func(t *testing.T) {
		revoked := claim()
		require.NoError(t, deliver("patient-pharmacy-affiliation-request", revoked))
		accept := decide(revoked, "accepted")
		decide(revoked, "revoked")
		require.NoError(t, deliver("patient-pharmacy-affiliation-decision-receipt", receipt(revoked, accept, true)))
		require.Empty(t, link())

		withdrawn := claim()
		require.NoError(t, deliver("patient-pharmacy-affiliation-request", withdrawn))
		accept = decide(withdrawn, "accepted")
		require.NoError(t, deliver("patient-pharmacy-affiliation-withdrawal", withdrawn))
		require.NoError(t, deliver("patient-pharmacy-affiliation-decision-receipt", receipt(withdrawn, accept, true)))
		require.Empty(t, link())
	})

	t.Run("a new round waits for the old round's link to end", func(t *testing.T) {
		older := claim()
		require.NoError(t, deliver("patient-pharmacy-affiliation-request", older))
		require.NoError(t, deliver("patient-pharmacy-affiliation-decision-receipt", receipt(older, decide(older, "accepted"), true)))
		require.Equal(t, older.RequestID, link())

		newer := claim()
		require.NoError(t, deliver("patient-pharmacy-affiliation-request", newer))
		accept := decide(newer, "accepted")
		require.Error(t, deliver("patient-pharmacy-affiliation-decision-receipt", receipt(newer, accept, true)), "refused, and so re-sent, while the older link stands")
		require.Equal(t, older.RequestID, link())
		require.NoError(t, deliver("patient-pharmacy-affiliation-withdrawal", older))
		require.Empty(t, link())
		require.NoError(t, deliver("patient-pharmacy-affiliation-decision-receipt", receipt(newer, accept, true)))
		require.Equal(t, newer.RequestID, link(), "the re-sent confirmation opens the new round's link")
		require.NoError(t, deliver("patient-pharmacy-affiliation-decision-receipt", receipt(newer, accept, true)), "a duplicate stays idempotent")
	})
}
