package patientmaster_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/patientmaster" //nolint:depguard // module under test
	patientmastercontract "github.com/tesserix/helivanta/internal/modules/patientmaster/contract"
	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func TestPharmacyAffiliationReview(t *testing.T) {
	review := patientmaster.PermPharmacyAffiliationReview
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"tokA": testutil.TenantA, "tokReader": testutil.TenantA, "tokNoPerm": testutil.TenantA, "tokAsker": testutil.TenantA, "tokB": testutil.TenantB},
		Perms: map[string][]authz.Permission{
			"tokA": {review}, "tokReader": {review}, "tokAsker": {review},
			"tokNoPerm": {patientmaster.PermPatientRead}, "tokB": {review},
		},
		Modules: []platform.Module{patientmaster.New()},
	})
	for subject, level := range map[string]string{"user-tokA": "registrar", "user-tokReader": "reader", "user-tokNoPerm": "registrar", "user-tokAsker": "registrar"} {
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Exec(`INSERT INTO patient_hospital_access (tenant_id,hospital_id,subject,level) VALUES (?,?,?,?)`, testutil.TenantA, testHospital, subject, level).Error
		}))
	}
	require.NoError(t, db.WithTenant(ctx, testutil.TenantB, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO patient_hospital_access (tenant_id,hospital_id,subject,level) VALUES (?,?,'user-tokB','registrar')`, testutil.TenantB, testHospital).Error
	}))
	consumers := patientmaster.New().Consumers(platform.Deps{})
	byName := map[string]events.Consumer{}
	for _, c := range consumers {
		byName[c.Name] = c
	}
	request, withdrawal := byName["patient-pharmacy-affiliation-request"], byName["patient-pharmacy-affiliation-withdrawal"]
	require.NotEmpty(t, request.Name)
	require.NotEmpty(t, withdrawal.Name)
	deliver := func(consumer events.Consumer, data pharmacycontract.AffiliationVerificationData) error {
		encoded, err := json.Marshal(data)
		require.NoError(t, err)
		return db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.actor_subject',?,true)`, "system:"+consumer.Name).Error; err != nil {
				return err
			}
			return consumer.Handle(ctx, tx, events.Event{Type: consumer.Subject, Version: 1, TenantID: testutil.TenantA, Data: encoded})
		})
	}
	claim := func() pharmacycontract.AffiliationVerificationData {
		return pharmacycontract.AffiliationVerificationData{RequestID: uuid.Must(uuid.NewV7()).String(), AffiliationID: uuid.Must(uuid.NewV7()).String(), PharmacyID: uuid.Must(uuid.NewV7()).String(), PharmacyName: "Synthetic Ward Pharmacy", HospitalID: testHospital, Relationship: "owner", RequesterSubject: "user-tokAsker"}
	}
	decide := func(token string, data pharmacycontract.AffiliationVerificationData, decision, outcome, reason string) (int, map[string]any) {
		w := scopedDo(r, "PUT", "/v1/patients/pharmacy-affiliations/"+data.RequestID+"/decision/"+decision, token, fmt.Sprintf(`{"outcome":%q,"reason":%q}`, outcome, reason))
		var body map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &body)
		return w.Code, body
	}
	decided := func(data pharmacycontract.AffiliationVerificationData) []pharmacyDecided {
		var rows []pharmacyDecided
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT payload->'data'->>'outcome' AS outcome, payload->'data'->>'decision_id' AS decision_id, payload->'data'->>'affiliation_id' AS affiliation_id FROM outbox_events WHERE subject=? AND payload->'data'->>'request_id'=? ORDER BY created_at,id`, "helivanta.in.patientmaster.pharmacy_affiliation_decided.v1", data.RequestID).Scan(&rows).Error
		}))
		return rows
	}
	newID := func() string { return uuid.Must(uuid.NewV7()).String() }

	t.Run("accept then revoke, attributed and published", func(t *testing.T) {
		data := claim()
		require.NoError(t, deliver(request, data))
		require.NoError(t, deliver(request, data), "redelivery is idempotent")
		changed := data
		changed.Relationship = "partner"
		require.Error(t, deliver(request, changed), "a different claim under the same request id is refused")

		list := scopedDo(r, "GET", "/v1/patients/pharmacy-affiliations", "tokA", "")
		require.Equal(t, 200, list.Code, list.Body.String())
		require.Contains(t, list.Body.String(), data.RequestID)
		require.Contains(t, list.Body.String(), "Synthetic Ward Pharmacy")
		require.NotContains(t, list.Body.String(), "user-tokAsker", "the requester's identity is not shown to the queue")

		accept := newID()
		code, body := decide("tokA", data, accept, "accepted", "Our hospital owns this pharmacy")
		require.Equal(t, 200, code, body)
		require.Equal(t, "accepted", body["status"])
		again, replay := decide("tokA", data, accept, "accepted", "Our hospital owns this pharmacy")
		require.Equal(t, 200, again)
		require.Equal(t, body, replay)
		code, _ = decide("tokA", data, accept, "accepted", "a different reason")
		require.Equal(t, 409, code, "the same decision id with other input is a different command")
		code, _ = decide("tokA", data, newID(), "declined", "too late")
		require.Equal(t, 409, code, "an accepted request cannot be declined")

		code, body = decide("tokA", data, newID(), "revoked", "Pharmacy sold")
		require.Equal(t, 200, code, body)
		require.Equal(t, "revoked", body["status"])
		decisions, ok := body["decisions"].([]any)
		require.True(t, ok)
		require.Len(t, decisions, 2)
		require.Equal(t, "user-tokA", decisions[0].(map[string]any)["reviewer_subject"])
		require.Equal(t, "Pharmacy sold", decisions[1].(map[string]any)["reason"])

		out := decided(data)
		require.Len(t, out, 2)
		require.Equal(t, "accepted", out[0].Outcome)
		require.Equal(t, accept, out[0].DecisionID)
		require.Equal(t, data.AffiliationID, out[0].AffiliationID)
		require.Equal(t, "revoked", out[1].Outcome)

		require.NoError(t, deliver(withdrawal, data))
		require.Contains(t, scopedDo(r, "GET", "/v1/patients/pharmacy-affiliations", "tokA", "").Body.String(), `"status":"revoked"`, "a withdrawal after revocation changes nothing")
	})

	t.Run("the requester cannot accept their own request", func(t *testing.T) {
		data := claim()
		require.NoError(t, deliver(request, data))
		code, _ := decide("tokAsker", data, newID(), "accepted", "self")
		require.Equal(t, 409, code)
		code, body := decide("tokAsker", data, newID(), "declined", "declining is not a grant")
		require.Equal(t, 200, code, body)
		require.Len(t, decided(data), 1, "only the decline is published")
	})

	t.Run("authority", func(t *testing.T) {
		data := claim()
		require.NoError(t, deliver(request, data))
		code, _ := decide("tokReader", data, newID(), "accepted", "reader grant")
		require.Equal(t, 404, code, "a reader grant is not a registrar grant")
		require.Equal(t, 404, scopedDo(r, "GET", "/v1/patients/pharmacy-affiliations", "tokReader", "").Code)
		code, _ = decide("tokNoPerm", data, newID(), "accepted", "no permission")
		require.Equal(t, 403, code)
		code, _ = decide("tokB", data, newID(), "accepted", "other tenant")
		require.Equal(t, 404, code)
		require.NotContains(t, scopedDo(r, "GET", "/v1/patients/pharmacy-affiliations", "tokB", "").Body.String(), data.RequestID)
		code, _ = decide("tokA", data, newID(), "accepted", "")
		require.Equal(t, 400, code, "a reason is required")
		code, _ = decide("tokA", data, newID(), "revoked", "nothing to revoke")
		require.Equal(t, 409, code)
		require.Empty(t, decided(data))
	})

	t.Run("withdrawal before its request leaves nothing to decide", func(t *testing.T) {
		data := claim()
		require.NoError(t, deliver(withdrawal, data))
		require.NoError(t, deliver(request, data))
		code, _ := decide("tokA", data, newID(), "accepted", "late")
		require.Equal(t, 409, code)
		require.Empty(t, decided(data))
	})

	t.Run("withdrawal of a pending request closes it", func(t *testing.T) {
		data := claim()
		require.NoError(t, deliver(request, data))
		require.NoError(t, deliver(withdrawal, data))
		require.Contains(t, scopedDo(r, "GET", "/v1/patients/pharmacy-affiliations", "tokA", "").Body.String(), `"request_id":"`+data.RequestID+`","affiliation_id":"`+data.AffiliationID+`","pharmacy_id":"`+data.PharmacyID+`","pharmacy_name":"Synthetic Ward Pharmacy","hospital_id":"`+testHospital+`","relationship":"owner","status":"withdrawn"`)
		code, _ := decide("tokA", data, newID(), "accepted", "late")
		require.Equal(t, 409, code)
		require.Empty(t, decided(data))
	})

	t.Run("withdrawal ends an accepted link", func(t *testing.T) {
		data := claim()
		require.NoError(t, deliver(request, data))
		code, _ := decide("tokA", data, newID(), "accepted", "ok")
		require.Equal(t, 200, code)
		require.NoError(t, deliver(withdrawal, data))
		code, _ = decide("tokA", data, newID(), "revoked", "already gone")
		require.Equal(t, 409, code)
	})

	t.Run("the database enforces the rules without the handler", func(t *testing.T) {
		data := claim()
		require.NoError(t, deliver(request, data))
		asReviewer := func(subject string, fn func(tx *gorm.DB) error) error {
			return db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
				if err := tx.Exec(`SELECT set_config('app.actor_subject',?,true),set_config('app.patient_subject',?,true),set_config('app.patient_hospital',?,true)`, subject, subject, testHospital).Error; err != nil {
					return err
				}
				return fn(tx)
			})
		}
		insert := func(subject, outcome string) error {
			return asReviewer(subject, func(tx *gorm.DB) error {
				return tx.Exec(`INSERT INTO patient_pharmacy_affiliation_decisions(id,tenant_id,hospital_id,request_id,outcome,reason,fingerprint,reviewer_subject) VALUES (?,?,?,?,?,'direct',repeat('a',64),?)`, newID(), testutil.TenantA, testHospital, data.RequestID, outcome, subject).Error
			})
		}
		require.Error(t, insert("user-tokAsker", "accepted"), "self-acceptance")
		require.Error(t, insert("user-tokReader", "accepted"), "reader grant")
		require.Error(t, insert("user-tokA", "revoked"), "revoke before accept")
		require.Error(t, asReviewer("user-tokA", func(tx *gorm.DB) error {
			return tx.Exec(`UPDATE patient_pharmacy_affiliation_reviews SET status='accepted' WHERE tenant_id=? AND request_id=?`, testutil.TenantA, data.RequestID).Error
		}), "status cannot move without its decision")
		// Two reviewers deciding at once: the second waits on the first and
		// then finds the request no longer pending.
		race := claim()
		require.NoError(t, deliver(request, race))
		decideRaw := func(tx *gorm.DB, outcome string) error {
			return tx.Exec(`INSERT INTO patient_pharmacy_affiliation_decisions(id,tenant_id,hospital_id,request_id,outcome,reason,fingerprint,reviewer_subject) VALUES (?,?,?,?,?,'race',repeat('a',64),'user-tokA')`, newID(), testutil.TenantA, testHospital, race.RequestID, outcome).Error
		}
		held, let, first := make(chan struct{}), make(chan struct{}), make(chan error, 1)
		go func() {
			first <- asReviewer("user-tokA", func(tx *gorm.DB) error {
				if err := decideRaw(tx, "accepted"); err != nil {
					return err
				}
				close(held)
				<-let
				return nil
			})
		}()
		<-held
		rival := make(chan error, 1)
		go func() {
			rival <- asReviewer("user-tokA", func(tx *gorm.DB) error { return decideRaw(tx, "declined") })
		}()
		time.Sleep(200 * time.Millisecond)
		close(let)
		require.NoError(t, <-first)
		require.Error(t, <-rival, "a request takes one decision")

		require.NoError(t, insert("user-tokA", "accepted"))
		require.Error(t, asReviewer("user-tokA", func(tx *gorm.DB) error {
			return tx.Exec(`UPDATE patient_pharmacy_affiliation_decisions SET reason='rewritten'`).Error
		}), "decisions are append-only")
		require.Error(t, asReviewer("user-tokA", func(tx *gorm.DB) error {
			return tx.Exec(`UPDATE patient_pharmacy_affiliation_reviews SET pharmacy_name='renamed' WHERE tenant_id=? AND request_id=?`, testutil.TenantA, data.RequestID).Error
		}), "the claim is fixed")
		require.Error(t, asReviewer("user-tokA", func(tx *gorm.DB) error {
			return tx.Exec(`DELETE FROM patient_pharmacy_affiliation_reviews WHERE tenant_id=? AND request_id=?`, testutil.TenantA, data.RequestID).Error
		}), "reviews are retained")
	})
}

func TestPharmacyAffiliationDecisionsAreResentUntilConfirmed(t *testing.T) {
	r, db, _, ctx := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:  map[string]string{"tokA": testutil.TenantA},
		Perms:   map[string][]authz.Permission{"tokA": {patientmaster.PermPharmacyAffiliationReview}},
		Modules: []platform.Module{patientmaster.New()},
	})
	require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
		return tx.Exec(`INSERT INTO patient_hospital_access (tenant_id,hospital_id,subject,level) VALUES (?,?,'user-tokA','registrar')`, testutil.TenantA, testHospital).Error
	}))
	byName := map[string]events.Consumer{}
	for _, c := range patientmaster.New().Consumers(platform.Deps{}) {
		byName[c.Name] = c
	}
	deliver := func(consumer events.Consumer, v any) error {
		encoded, err := json.Marshal(v)
		require.NoError(t, err)
		return db.WithTenant(tenantdb.WithActor(ctx, "system:"+consumer.Name), testutil.TenantA, func(tx *gorm.DB) error {
			return consumer.Handle(ctx, tx, events.Event{Type: consumer.Subject, Version: 1, TenantID: testutil.TenantA, Data: encoded})
		})
	}
	newID := func() string { return uuid.Must(uuid.NewV7()).String() }
	data := pharmacycontract.AffiliationVerificationData{RequestID: newID(), AffiliationID: newID(), PharmacyID: newID(), PharmacyName: "Synthetic Ward Pharmacy", HospitalID: testHospital, Relationship: "owner", RequesterSubject: "user-asker"}
	outbox := func(subject string) []map[string]string {
		var raw []string
		require.NoError(t, db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT (payload->'data')::text FROM outbox_events WHERE subject=? AND payload->'data'->>'request_id'=? ORDER BY created_at,id`, subject, data.RequestID).Scan(&raw).Error
		}))
		out := make([]map[string]string, len(raw))
		for i, s := range raw {
			require.NoError(t, json.Unmarshal([]byte(s), &out[i]))
		}
		return out
	}
	asRegistrar := func(fn func(tx *gorm.DB) error) error {
		return db.WithTenant(ctx, testutil.TenantA, func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.patient_subject','user-tokA',true),set_config('app.patient_hospital',?,true)`, testHospital).Error; err != nil {
				return err
			}
			return fn(tx)
		})
	}
	delivery := func() (decision string, attempts int, due *float64) {
		var row struct {
			DecisionID string
			Attempts   int
			Wait       *float64
		}
		require.NoError(t, asRegistrar(func(tx *gorm.DB) error {
			return tx.Raw(`SELECT decision_id::text,attempts,EXTRACT(EPOCH FROM next_attempt_at-clock_timestamp()) AS wait FROM patient_pharmacy_affiliation_deliveries WHERE tenant_id=? AND request_id=?`, testutil.TenantA, data.RequestID).Scan(&row).Error
		}))
		return row.DecisionID, row.Attempts, row.Wait
	}
	lapse := func() {
		require.NoError(t, asRegistrar(func(tx *gorm.DB) error {
			return tx.Exec(`UPDATE patient_pharmacy_affiliation_deliveries SET next_attempt_at=clock_timestamp()-interval '1 second' WHERE tenant_id=? AND request_id=? AND next_attempt_at IS NOT NULL`, testutil.TenantA, data.RequestID).Error
		}))
	}
	redeliver := func() { require.NoError(t, patientmaster.RedeliverPharmacyAffiliationsOnce(ctx, db)) }
	received := patientmastercontract.SubjectPharmacyAffiliationReceived
	decided := patientmastercontract.SubjectPharmacyAffiliationDecided

	// Each pharmacy step handled is confirmed, a duplicate included.
	require.NoError(t, deliver(byName["patient-pharmacy-affiliation-request"], data))
	require.NoError(t, deliver(byName["patient-pharmacy-affiliation-request"], data))
	confirmations := outbox(received)
	require.Len(t, confirmations, 2)
	require.Equal(t, map[string]string{"request_id": data.RequestID, "affiliation_id": data.AffiliationID, "state": "requested"}, confirmations[0])

	accept := newID()
	w := scopedDo(r, "PUT", "/v1/patients/pharmacy-affiliations/"+data.RequestID+"/decision/"+accept, "tokA", `{"outcome":"accepted","reason":"Owned"}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	current, attempts, due := delivery()
	require.Equal(t, accept, current)
	require.Zero(t, attempts)
	require.NotNil(t, due)
	require.InDelta(t, 120, *due, 5)
	redeliver()
	require.Len(t, outbox(decided), 1, "nothing is re-sent before it is due")
	lapse()
	redeliver()
	require.Len(t, outbox(decided), 2, "an unconfirmed acceptance is re-sent")
	require.Equal(t, outbox(decided)[0], outbox(decided)[1])
	_, attempts, _ = delivery()
	require.Equal(t, 1, attempts)

	revoke := newID()
	w = scopedDo(r, "PUT", "/v1/patients/pharmacy-affiliations/"+data.RequestID+"/decision/"+revoke, "tokA", `{"outcome":"revoked","reason":"Sold"}`)
	require.Equal(t, 200, w.Code, w.Body.String())
	current, attempts, _ = delivery()
	require.Equal(t, revoke, current, "the revocation replaces the unconfirmed acceptance")
	require.Zero(t, attempts)
	receipt := byName["patient-pharmacy-affiliation-decision-receipt"]
	require.NoError(t, deliver(receipt, pharmacycontract.AffiliationDecisionReceivedData{RequestID: data.RequestID, HospitalID: testHospital, DecisionID: accept, Applied: true}))
	_, _, due = delivery()
	require.NotNil(t, due, "confirming the superseded acceptance does not confirm the revocation")
	lapse()
	redeliver()
	sentDecisions := outbox(decided)
	require.Len(t, sentDecisions, 4)
	require.Equal(t, revoke, sentDecisions[3]["decision_id"])
	require.Equal(t, "revoked", sentDecisions[3]["outcome"])
	require.NoError(t, deliver(receipt, pharmacycontract.AffiliationDecisionReceivedData{RequestID: data.RequestID, HospitalID: testHospital, DecisionID: revoke, Applied: true}))
	_, _, due = delivery()
	require.Nil(t, due, "a confirmed decision is no longer due")
	redeliver()
	require.Len(t, outbox(decided), 4)

	require.NoError(t, deliver(byName["patient-pharmacy-affiliation-withdrawal"], data))
	confirmations = outbox(received)
	require.Len(t, confirmations, 3)
	require.Equal(t, "withdrawn", confirmations[2]["state"], "a withdrawal is confirmed even when the link was already revoked")
}

type pharmacyDecided struct {
	Outcome       string
	DecisionID    string
	AffiliationID string
}
