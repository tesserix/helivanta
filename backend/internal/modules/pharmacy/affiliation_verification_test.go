package pharmacy_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	patientmastercontract "github.com/tesserix/helivanta/internal/modules/patientmaster/contract"
	"github.com/tesserix/helivanta/internal/modules/pharmacy" //nolint:depguard // module under test
	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func TestAffiliationVerificationLifecycle(t *testing.T) {
	r, db := catalogueHarness(t)
	hospitals := map[string]string{}
	root := "/v1/pharmacy/pharmacies/" + pharmaOrganization + "/hospital-affiliations/"
	declare := func() string {
		id, hospital := uuid.Must(uuid.NewV7()).String(), uuid.NewString()
		hospitals[id] = hospital
		w := do(r, "PUT", root+id, "editor", fmt.Sprintf(`{"hospital_id":%q,"relationship":"owner"}`, hospital))
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), `"verification_status":"pending_validation"`)
		return id
	}
	newID := func() string { return uuid.Must(uuid.NewV7()).String() }
	status := func(affiliation string) map[string]any {
		w := do(r, "GET", "/v1/pharmacy/pharmacies/"+pharmaOrganization+"/hospital-affiliations?limit=100", "editor", "")
		require.Equal(t, 200, w.Code, w.Body.String())
		var page struct{ Data []map[string]any }
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &page))
		for _, item := range page.Data {
			if item["id"] == affiliation {
				return item
			}
		}
		t.Fatalf("affiliation %s not listed in %s", affiliation, w.Body.String())
		return nil
	}
	var decision events.Consumer
	for _, c := range pharmacy.New().Consumers(platform.Deps{}) {
		if c.Name == "pharmacy-affiliation-decision" {
			decision = c
		}
	}
	require.Equal(t, patientmastercontract.SubjectPharmacyAffiliationDecided, decision.Subject)
	decide := func(affiliation, request, outcome string) string {
		id := newID()
		deliverDecision(t, db, decision, patientmastercontract.PharmacyAffiliationDecidedData{RequestID: request, AffiliationID: affiliation, HospitalID: hospitals[affiliation], DecisionID: id, Outcome: outcome, DecidedAt: time.Now().UTC().Format(time.RFC3339Nano)})
		return id
	}
	steps := func(affiliation string) []string {
		var kinds []string
		require.NoError(t, catalogueTx(t, db, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT kind FROM pharmacy_affiliation_verifications WHERE tenant_id=? AND affiliation_id=? ORDER BY sequence`, testutil.TenantA, affiliation).Scan(&kinds).Error
		}))
		return kinds
	}
	published := func(subject, request string) []pharmacycontract.AffiliationVerificationData {
		var raw []string
		require.NoError(t, catalogueTx(t, db, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT (payload->'data')::text FROM outbox_events WHERE subject=? AND payload->'data'->>'request_id'=? ORDER BY created_at,id`, subject, request).Scan(&raw).Error
		}))
		out := make([]pharmacycontract.AffiliationVerificationData, len(raw))
		for i, s := range raw {
			require.NoError(t, json.Unmarshal([]byte(s), &out[i]))
		}
		return out
	}

	t.Run("request, accept, withdraw", func(t *testing.T) {
		affiliation := declare()
		request := newID()
		path := root + affiliation + "/verification-requests/" + request
		require.Equal(t, 403, do(r, "PUT", path, "reader", `{}`).Code)
		w := do(r, "PUT", path, "editor", `{}`)
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), `"verification_status":"requested"`)
		require.Contains(t, w.Body.String(), `"verification_request_id":"`+request+`"`)
		require.JSONEq(t, w.Body.String(), do(r, "PUT", path, "editor", `{}`).Body.String(), "a retry of the round is idempotent")
		require.Equal(t, 409, do(r, "PUT", root+affiliation+"/verification-requests/"+newID(), "editor", `{}`).Code, "one open round at a time")
		other := declare()
		require.Equal(t, 409, do(r, "PUT", root+other+"/verification-requests/"+request, "editor", `{}`).Code, "a round id names one affiliation")

		asked := published(pharmacycontract.SubjectAffiliationVerificationRequested, request)
		require.Len(t, asked, 1, "a retry publishes nothing new")
		require.Equal(t, pharmacycontract.AffiliationVerificationData{RequestID: request, AffiliationID: affiliation, PharmacyID: pharmaOrganization, PharmacyName: asked[0].PharmacyName, HospitalID: hospitals[affiliation], Relationship: "owner", RequesterSubject: "user-editor"}, asked[0])
		require.NotEmpty(t, asked[0].PharmacyName)

		decide(affiliation, newID(), "accepted")
		require.Equal(t, "requested", status(affiliation)["verification_status"], "a decision for another round changes nothing")
		accepted := decide(affiliation, request, "accepted")
		row := status(affiliation)
		require.Equal(t, "verified", row["verification_status"])
		require.NotNil(t, row["verified_at"])
		deliverDecision(t, db, decision, patientmastercontract.PharmacyAffiliationDecidedData{RequestID: request, AffiliationID: affiliation, HospitalID: hospitals[affiliation], DecisionID: accepted, Outcome: "accepted", DecidedAt: time.Now().UTC().Format(time.RFC3339Nano)})
		require.Equal(t, []string{"requested", "accepted"}, steps(affiliation), "redelivery is a no-op")

		withdraw := root + affiliation + "/verification-requests/" + request + "/withdraw"
		require.Equal(t, 400, do(r, "POST", withdraw, "editor", `{"reason":"  "}`).Code)
		require.Equal(t, 403, do(r, "POST", withdraw, "reader", `{"reason":"closing"}`).Code)
		w = do(r, "POST", withdraw, "steward", `{"reason":"Outlet closed"}`)
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), `"verification_status":"withdrawn"`)
		require.Contains(t, w.Body.String(), `"verified_at":null`)
		require.Equal(t, 200, do(r, "POST", withdraw, "editor", `{"reason":"Outlet closed"}`).Code, "a retry is idempotent")
		require.Equal(t, 409, do(r, "POST", withdraw, "editor", `{"reason":"another reason"}`).Code)
		gone := published(pharmacycontract.SubjectAffiliationVerificationWithdrawn, request)
		require.Len(t, gone, 1)
		require.Equal(t, asked[0], gone[0], "the withdrawal carries the request's whole claim")

		decide(affiliation, request, "revoked")
		require.Equal(t, []string{"requested", "accepted", "withdrawn"}, steps(affiliation), "a decision after withdrawal is a no-op")

		history := do(r, "GET", root+affiliation+"/verifications", "editor", "")
		require.Equal(t, 200, history.Code, history.Body.String())
		require.Contains(t, history.Body.String(), `"reason":"Outlet closed"`)
		require.Contains(t, history.Body.String(), `"decision_id":"`+accepted+`"`)
		require.Equal(t, 404, do(r, "GET", root+affiliation+"/verifications", "foreign", "").Code)
	})

	t.Run("decline then ask again; revoke overtaking accept converges", func(t *testing.T) {
		affiliation := declare()
		first := newID()
		require.Equal(t, 200, do(r, "PUT", root+affiliation+"/verification-requests/"+first, "editor", `{}`).Code)
		decide(affiliation, first, "declined")
		require.Equal(t, "declined", status(affiliation)["verification_status"])
		require.Equal(t, 409, do(r, "POST", root+affiliation+"/verification-requests/"+first+"/withdraw", "editor", `{"reason":"nothing open"}`).Code)

		second := newID()
		require.Equal(t, 200, do(r, "PUT", root+affiliation+"/verification-requests/"+second, "editor", `{}`).Code)
		decide(affiliation, second, "revoked")
		decide(affiliation, second, "accepted")
		require.Equal(t, "revoked", status(affiliation)["verification_status"])
		require.Equal(t, []string{"requested", "declined", "requested", "revoked"}, steps(affiliation))
	})

	t.Run("withdraw before acceptance; an old round's retry still answers", func(t *testing.T) {
		affiliation := declare()
		first := newID()
		require.Equal(t, 200, do(r, "PUT", root+affiliation+"/verification-requests/"+first, "editor", `{}`).Code)
		withdraw := root + affiliation + "/verification-requests/" + first + "/withdraw"
		w := do(r, "POST", withdraw, "editor", `{"reason":"Asked too early"}`)
		require.Equal(t, 200, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), `"verification_status":"withdrawn"`)
		decide(affiliation, first, "accepted")
		require.Equal(t, []string{"requested", "withdrawn"}, steps(affiliation), "a late accept for a withdrawn round is a no-op")

		second := newID()
		require.Equal(t, 200, do(r, "PUT", root+affiliation+"/verification-requests/"+second, "editor", `{}`).Code)
		require.Equal(t, 200, do(r, "POST", withdraw, "editor", `{"reason":"Asked too early"}`).Code, "a retried withdrawal of an older round is answered from history")
		require.Equal(t, 409, do(r, "POST", withdraw, "editor", `{"reason":"another"}`).Code)
		require.Equal(t, "requested", status(affiliation)["verification_status"], "the retry does not touch the newer round")

		other := declare()
		w = do(r, "PUT", root+other+"/verification-requests/"+second, "editor", `{}`)
		require.Equal(t, 409, w.Code)
		require.Contains(t, w.Body.String(), "verification_conflict")
	})

	t.Run("a decision naming another hospital is refused", func(t *testing.T) {
		affiliation := declare()
		request := newID()
		require.Equal(t, 200, do(r, "PUT", root+affiliation+"/verification-requests/"+request, "editor", `{}`).Code)
		data, err := json.Marshal(patientmastercontract.PharmacyAffiliationDecidedData{RequestID: request, AffiliationID: affiliation, HospitalID: "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaac", DecisionID: newID(), Outcome: "accepted", DecidedAt: time.Now().UTC().Format(time.RFC3339Nano)})
		require.NoError(t, err)
		require.Error(t, handleDecision(t, db, decision, data))
		require.Equal(t, "requested", status(affiliation)["verification_status"])
	})

	t.Run("the database enforces the lifecycle", func(t *testing.T) {
		affiliation := declare()
		insert := func(tx *gorm.DB, sequence int, kind, request string) error {
			actor, decisionID, reason := any("user-editor"), any(nil), any(nil)
			if kind != "requested" && kind != "withdrawn" {
				actor, decisionID = nil, newID()
			}
			if kind == "withdrawn" {
				reason = "direct"
			}
			return tx.Exec(`INSERT INTO pharmacy_affiliation_verifications (id,tenant_id,affiliation_id,sequence,kind,request_id,decision_id,actor_subject,reason) VALUES (?,?,?,?,?,?,?,?,?)`, newID(), testutil.TenantA, affiliation, sequence, kind, request, decisionID, actor, reason).Error
		}
		write := func(fn func(tx *gorm.DB) error) error { return catalogueTx(t, db, fn) }
		request := newID()
		require.Error(t, write(func(tx *gorm.DB) error { return insert(tx, 1, "accepted", request) }), "nothing to accept")
		require.Error(t, write(func(tx *gorm.DB) error { return insert(tx, 2, "requested", request) }), "numbering starts at one")
		require.NoError(t, write(func(tx *gorm.DB) error { return insert(tx, 1, "requested", request) }))
		require.Error(t, write(func(tx *gorm.DB) error { return insert(tx, 2, "requested", newID()) }), "a round is open")
		require.Error(t, write(func(tx *gorm.DB) error { return insert(tx, 2, "accepted", newID()) }), "the decision must answer the open round")
		require.Error(t, write(func(tx *gorm.DB) error {
			return tx.Exec(`UPDATE pharmacy_affiliation_verifications SET reason='x' WHERE affiliation_id=?`, affiliation).Error
		}), "history is immutable")
		require.Error(t, write(func(tx *gorm.DB) error {
			return tx.Exec(`DELETE FROM pharmacy_affiliation_verifications WHERE affiliation_id=?`, affiliation).Error
		}), "history is retained")

		// Two writers that both read step 1 as the latest: one wins, the
		// other loses on the sequence key, whatever the handler does.
		holding, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
		go func() {
			done <- write(func(tx *gorm.DB) error {
				if err := insert(tx, 2, "accepted", request); err != nil {
					return err
				}
				close(holding)
				<-release
				return nil
			})
		}()
		<-holding
		second := make(chan error, 1)
		go func() { second <- write(func(tx *gorm.DB) error { return insert(tx, 2, "declined", request) }) }()
		time.Sleep(200 * time.Millisecond)
		close(release)
		require.NoError(t, <-done)
		require.Error(t, <-second)
		require.Equal(t, []string{"requested", "accepted"}, steps(affiliation))
		require.Error(t, write(func(tx *gorm.DB) error { return insert(tx, 3, "declined", request) }), "a decided round takes no second decision")

		// Two writers opening a round on a fresh affiliation at once.
		fresh := declare()
		openRound := func(tx *gorm.DB) error {
			return tx.Exec(`INSERT INTO pharmacy_affiliation_verifications (id,tenant_id,affiliation_id,sequence,kind,request_id,actor_subject) VALUES (?,?,?,1,'requested',?,'user-editor')`, newID(), testutil.TenantA, fresh, newID()).Error
		}
		held, let, first := make(chan struct{}), make(chan struct{}), make(chan error, 1)
		go func() {
			first <- write(func(tx *gorm.DB) error {
				if err := openRound(tx); err != nil {
					return err
				}
				close(held)
				<-let
				return nil
			})
		}()
		<-held
		rival := make(chan error, 1)
		go func() { rival <- write(openRound) }()
		time.Sleep(200 * time.Millisecond)
		close(let)
		require.NoError(t, <-first)
		require.Error(t, <-rival, "two requests cannot both open a round")
		require.Equal(t, []string{"requested"}, steps(fresh))
		require.NoError(t, write(func(tx *gorm.DB) error { return insert(tx, 3, "revoked", request) }))
	})
}

func handleDecision(t *testing.T, db *tenantdb.DB, consumer events.Consumer, data []byte) error {
	t.Helper()
	return db.WithTenant(tenantdb.WithActor(t.Context(), "system:"+consumer.Name), testutil.TenantA, func(tx *gorm.DB) error {
		return consumer.Handle(t.Context(), tx, events.Event{Type: consumer.Subject, Version: 1, TenantID: testutil.TenantA, Data: data})
	})
}

func deliverDecision(t *testing.T, db *tenantdb.DB, consumer events.Consumer, d patientmastercontract.PharmacyAffiliationDecidedData) {
	t.Helper()
	data, err := json.Marshal(d)
	require.NoError(t, err)
	require.NoError(t, handleDecision(t, db, consumer, data))
}
