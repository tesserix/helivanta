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
)

func TestAffiliationStepsAreResentUntilConfirmed(t *testing.T) {
	r, db := catalogueHarness(t)
	consumers := map[string]events.Consumer{}
	for _, c := range pharmacy.New().Consumers(platform.Deps{}) {
		consumers[c.Name] = c
	}
	receipt, decision := consumers["pharmacy-affiliation-receipt"], consumers["pharmacy-affiliation-decision"]
	require.Equal(t, patientmastercontract.SubjectPharmacyAffiliationReceived, receipt.Subject)
	hospital := uuid.NewString()
	affiliation, request := uuid.Must(uuid.NewV7()).String(), uuid.Must(uuid.NewV7()).String()
	root := "/v1/pharmacy/pharmacies/" + pharmaOrganization + "/hospital-affiliations/" + affiliation
	require.Equal(t, 200, do(r, "PUT", root, "editor", fmt.Sprintf(`{"hospital_id":%q,"relationship":"serving"}`, hospital)).Code)
	require.Equal(t, 200, do(r, "PUT", root+"/verification-requests/"+request, "editor", `{}`).Code)

	sent := func(subject string) []string {
		var rows []string
		require.NoError(t, catalogueTx(t, db, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT (payload->'data')::text FROM outbox_events WHERE subject=? AND payload->'data'->>'request_id'=? ORDER BY created_at,id`, subject, request).Scan(&rows).Error
		}))
		return rows
	}
	delivery := func() (state string, attempts int, due *time.Duration) {
		var row struct {
			State    string
			Attempts int
			Wait     *float64
		}
		require.NoError(t, catalogueTx(t, db, func(tx *gorm.DB) error {
			return tx.Raw(`SELECT state,attempts,EXTRACT(EPOCH FROM next_attempt_at-clock_timestamp()) AS wait FROM pharmacy_affiliation_deliveries WHERE tenant_id=? AND request_id=?`, testutil.TenantA, request).Scan(&row).Error
		}))
		if row.Wait != nil {
			d := time.Duration(*row.Wait * float64(time.Second))
			due = &d
		}
		return row.State, row.Attempts, due
	}
	lapse := func() {
		require.NoError(t, catalogueTx(t, db, func(tx *gorm.DB) error {
			return tx.Exec(`UPDATE pharmacy_affiliation_deliveries SET next_attempt_at=clock_timestamp()-interval '1 second' WHERE tenant_id=? AND request_id=? AND next_attempt_at IS NOT NULL`, testutil.TenantA, request).Error
		}))
	}
	redeliver := func() { require.NoError(t, pharmacy.RedeliverAffiliationsOnce(t.Context(), db)) }
	confirm := func(state string) {
		data, err := json.Marshal(patientmastercontract.PharmacyAffiliationReceivedData{RequestID: request, AffiliationID: affiliation, State: state})
		require.NoError(t, err)
		require.NoError(t, handleDecision(t, db, receipt, data))
	}
	requested := pharmacycontract.SubjectAffiliationVerificationRequested
	withdrawn := pharmacycontract.SubjectAffiliationVerificationWithdrawn

	state, attempts, due := delivery()
	require.Equal(t, "requested", state)
	require.Zero(t, attempts)
	require.NotNil(t, due)
	require.InDelta(t, (2 * time.Minute).Seconds(), due.Seconds(), 5, "the first re-send waits for normal delivery")
	redeliver()
	require.Len(t, sent(requested), 1, "nothing is re-sent before it is due")

	lapse()
	redeliver()
	require.Len(t, sent(requested), 2, "an unconfirmed request is re-sent")
	require.JSONEq(t, sent(requested)[0], sent(requested)[1], "the re-send is the same claim")
	lapse()
	redeliver()
	lapse()
	redeliver()
	_, attempts, due = delivery()
	require.Equal(t, 3, attempts)
	require.InDelta(t, (8 * time.Minute).Seconds(), due.Seconds(), 5, "re-sends back off exponentially")

	confirm("requested")
	_, _, due = delivery()
	require.Nil(t, due, "a confirmed step is no longer due")
	redeliver()
	require.Len(t, sent(requested), 4)

	require.Equal(t, 200, do(r, "POST", root+"/verification-requests/"+request+"/withdraw", "editor", `{"reason":"Closed"}`).Code)
	state, attempts, due = delivery()
	require.Equal(t, "withdrawn", state)
	require.Zero(t, attempts)
	require.NotNil(t, due, "the withdrawal is owed a confirmation of its own")
	confirm("requested")
	_, _, due = delivery()
	require.NotNil(t, due, "confirming the superseded request does not confirm the withdrawal")
	lapse()
	redeliver()
	require.Len(t, sent(withdrawn), 2, "an unconfirmed withdrawal is re-sent")
	require.JSONEq(t, sent(withdrawn)[0], sent(withdrawn)[1])
	confirm("withdrawn")
	_, _, due = delivery()
	require.Nil(t, due)

	// Every decision handled is confirmed: one applied, its duplicate, and a
	// no-op for a closed round.
	second := uuid.Must(uuid.NewV7()).String()
	require.Equal(t, 200, do(r, "PUT", root+"/verification-requests/"+second, "editor", `{}`).Code)
	applied := patientmastercontract.PharmacyAffiliationDecidedData{RequestID: second, AffiliationID: affiliation, HospitalID: hospital, DecisionID: uuid.Must(uuid.NewV7()).String(), Outcome: "accepted", DecidedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	deliverDecision(t, db, decision, applied)
	deliverDecision(t, db, decision, applied)
	var twice int64
	require.NoError(t, catalogueTx(t, db, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT count(*) FROM outbox_events WHERE subject=? AND payload->'data'->>'decision_id'=?`, pharmacycontract.SubjectAffiliationDecisionReceived, applied.DecisionID).Scan(&twice).Error
	}))
	require.EqualValues(t, 2, twice, "a redelivered, already-applied decision is confirmed again")
	var appliedFlags []bool
	require.NoError(t, catalogueTx(t, db, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT (payload->'data'->>'applied')::boolean FROM outbox_events WHERE subject=? AND payload->'data'->>'decision_id'=? ORDER BY created_at,id`, pharmacycontract.SubjectAffiliationDecisionReceived, applied.DecisionID).Scan(&appliedFlags).Error
	}))
	require.Equal(t, []bool{true, true}, appliedFlags, "an applied decision is confirmed as applied, redelivered or not")
	decided := patientmastercontract.PharmacyAffiliationDecidedData{RequestID: request, AffiliationID: affiliation, HospitalID: hospital, DecisionID: uuid.Must(uuid.NewV7()).String(), Outcome: "accepted", DecidedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	deliverDecision(t, db, decision, decided)
	deliverDecision(t, db, decision, decided)
	confirmations := sent(pharmacycontract.SubjectAffiliationDecisionReceived)
	require.Len(t, confirmations, 2)
	var got pharmacycontract.AffiliationDecisionReceivedData
	require.NoError(t, json.Unmarshal([]byte(confirmations[0]), &got))
	require.Equal(t, pharmacycontract.AffiliationDecisionReceivedData{RequestID: request, HospitalID: hospital, DecisionID: decided.DecisionID, Applied: false}, got, "a decision for a withdrawn round is confirmed, not applied")
}
