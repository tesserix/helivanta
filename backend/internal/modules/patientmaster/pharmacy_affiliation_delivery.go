package patientmaster

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	patientmastercontract "github.com/tesserix/helivanta/internal/modules/patientmaster/contract"
	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// A round's latest decision is re-sent until pharmacy confirms it (#1416).
// The bus dead-letters after five failed deliveries and nothing replays the
// dead-letter subject, so without this a lost revocation would leave
// pharmacy holding a link the hospital has ended.

const (
	affiliationRedeliveryActor = "system:patient-pharmacy-affiliation-redelivery"
	affiliationRedeliveryBatch = 50
	firstAffiliationRedelivery = 2 * time.Minute
	maxAffiliationRedelivery   = time.Hour
)

// affiliationRedeliveryDelay doubles from two minutes and stops growing at
// an hour; a decision is never given up on.
func affiliationRedeliveryDelay(attempts int) time.Duration {
	d := firstAffiliationRedelivery
	for i := 1; i < attempts && d < maxAffiliationRedelivery; i++ {
		d *= 2
	}
	return min(d, maxAffiliationRedelivery)
}

// sendDecision makes decision the round's one to deliver and publishes it,
// in the deciding transaction. A revocation replaces an unconfirmed
// acceptance: pharmacy closes the round from either.
func sendDecision(tx *gorm.DB, tenant string, review pharmacyAffiliationReview, decision pharmacyAffiliationDecision) error {
	if err := tx.Exec(`INSERT INTO patient_pharmacy_affiliation_deliveries (tenant_id,request_id,hospital_id,decision_id,attempts,next_attempt_at) VALUES (?,?,?,?,0,clock_timestamp()+make_interval(secs => ?))
 ON CONFLICT (tenant_id,request_id) DO UPDATE SET decision_id=EXCLUDED.decision_id,attempts=0,next_attempt_at=EXCLUDED.next_attempt_at,updated_at=clock_timestamp()`,
		tenant, review.RequestID, review.HospitalID, decision.ID, firstAffiliationRedelivery.Seconds()).Error; err != nil {
		return err
	}
	return publishDecision(tx, tenant, review, decision)
}

func publishDecision(tx *gorm.DB, tenant string, review pharmacyAffiliationReview, decision pharmacyAffiliationDecision) error {
	data, err := json.Marshal(patientmastercontract.PharmacyAffiliationDecidedData{RequestID: review.RequestID.String(), AffiliationID: review.AffiliationID.String(), HospitalID: review.HospitalID.String(), DecisionID: decision.ID.String(), Outcome: decision.Outcome, DecidedAt: decision.CreatedAt.UTC().Format(time.RFC3339Nano)})
	if err != nil {
		return err
	}
	subject := patientmastercontract.SubjectPharmacyAffiliationDecided
	return events.Enqueue(tx, subject, events.Event{Type: subject, Version: 1, TenantID: tenant, Data: data})
}

// confirmPharmacyStep tells pharmacy this request or withdrawal is handled.
func confirmPharmacyStep(tx *gorm.DB, tenant, request, affiliation, state string) error {
	data, err := json.Marshal(patientmastercontract.PharmacyAffiliationReceivedData{RequestID: request, AffiliationID: affiliation, State: state})
	if err != nil {
		return err
	}
	subject := patientmastercontract.SubjectPharmacyAffiliationReceived
	return events.Enqueue(tx, subject, events.Event{Type: subject, Version: 1, TenantID: tenant, Data: data})
}

// decisionReceiptConsumer stops re-sending a decision pharmacy has handled.
// A confirmation of a superseded decision (an acceptance, once revoked)
// matches no row and changes nothing.
func decisionReceiptConsumer() events.Consumer {
	subject := pharmacycontract.SubjectAffiliationDecisionReceived
	return events.Consumer{Name: "patient-pharmacy-affiliation-decision-receipt", Subject: subject, Handle: func(_ context.Context, tx *gorm.DB, evt events.Event) error {
		var d pharmacycontract.AffiliationDecisionReceivedData
		if evt.Type != subject || evt.Version != 1 || evt.DestinationTenantID != "" || json.Unmarshal(evt.Data, &d) != nil ||
			!validReviewUUID(d.RequestID, true) || !validReviewUUID(d.HospitalID, false) || !validReviewUUID(d.DecisionID, true) {
			return errors.New("invalid affiliation decision receipt event")
		}
		if err := tx.Exec(`SELECT set_config('app.pharmacy_affiliation_hospital',?,true),set_config('app.pharmacy_affiliation_request',?,true)`, d.HospitalID, d.RequestID).Error; err != nil {
			return errors.New("pharmacy affiliation scope unavailable")
		}
		return tx.Exec(`UPDATE patient_pharmacy_affiliation_deliveries SET next_attempt_at=NULL,updated_at=clock_timestamp() WHERE tenant_id=? AND hospital_id=? AND request_id=? AND decision_id=? AND next_attempt_at IS NOT NULL`,
			evt.TenantID, d.HospitalID, d.RequestID, d.DecisionID).Error
	}}
}

// RunPharmacyAffiliationRedelivery re-sends unconfirmed decisions until ctx ends.
func RunPharmacyAffiliationRedelivery(ctx context.Context, deps platform.Deps) {
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if err := RedeliverPharmacyAffiliationsOnce(ctx, deps.DB); err != nil {
				slog.WarnContext(ctx, "pharmacy affiliation decision redelivery pass failed", "error", err)
			}
		}
	}
}

// RedeliverPharmacyAffiliationsOnce runs one pass; tests drive it instead of the ticker.
func RedeliverPharmacyAffiliationsOnce(ctx context.Context, db *tenantdb.DB) error {
	var due []struct {
		TenantID   uuid.UUID
		RequestID  uuid.UUID
		HospitalID uuid.UUID
	}
	// The one cross-tenant read: ids of due rounds and their hospital,
	// nothing else (the system role holds SELECT on just these columns).
	if err := db.WithAllTenants(ctx, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT tenant_id,request_id,hospital_id FROM patient_pharmacy_affiliation_deliveries WHERE next_attempt_at <= now() ORDER BY next_attempt_at LIMIT ?`, affiliationRedeliveryBatch).Scan(&due).Error
	}); err != nil {
		return err
	}
	ctx = tenantdb.WithActor(ctx, affiliationRedeliveryActor)
	for _, d := range due {
		if err := db.WithTenant(ctx, d.TenantID.String(), func(tx *gorm.DB) error {
			if err := tx.Exec(`SELECT set_config('app.pharmacy_affiliation_hospital',?,true),set_config('app.pharmacy_affiliation_request',?,true)`, d.HospitalID.String(), d.RequestID.String()).Error; err != nil {
				return err
			}
			var row struct {
				DecisionID uuid.UUID
				Attempts   int
			}
			// Locked and re-checked: a confirmation may have landed since the scan.
			err := tx.Table("patient_pharmacy_affiliation_deliveries").Select("decision_id,attempts").Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("tenant_id=? AND request_id=? AND next_attempt_at <= now()", d.TenantID, d.RequestID).Take(&row).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			if err != nil {
				return err
			}
			var review pharmacyAffiliationReview
			if err := tx.Select(pharmacyAffiliationColumns).Where("tenant_id=? AND request_id=?", d.TenantID, d.RequestID).Take(&review).Error; err != nil {
				return err
			}
			var decision pharmacyAffiliationDecision
			if err := tx.Select(pharmacyDecisionColumns).Where("tenant_id=? AND id=?", d.TenantID, row.DecisionID).Take(&decision).Error; err != nil {
				return err
			}
			if err := publishDecision(tx, d.TenantID.String(), review, decision); err != nil {
				return err
			}
			slog.WarnContext(ctx, "re-sent unconfirmed pharmacy affiliation decision", "tenant_id", d.TenantID, "request_id", d.RequestID, "decision_id", row.DecisionID, "attempt", row.Attempts+1)
			return tx.Exec(`UPDATE patient_pharmacy_affiliation_deliveries SET attempts=attempts+1,next_attempt_at=clock_timestamp()+make_interval(secs => ?),updated_at=clock_timestamp() WHERE tenant_id=? AND request_id=?`,
				affiliationRedeliveryDelay(row.Attempts+1).Seconds(), d.TenantID, d.RequestID).Error
		}); err != nil {
			slog.WarnContext(ctx, "pharmacy affiliation decision redelivery failed; retrying later", "tenant_id", d.TenantID, "request_id", d.RequestID, "error", err)
		}
	}
	return nil
}
