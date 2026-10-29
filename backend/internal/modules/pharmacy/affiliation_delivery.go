package pharmacy

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

// A round's latest pharmacy step is re-sent until patientmaster confirms it
// (#1416). The bus dead-letters after five failed deliveries and nothing
// replays the dead-letter subject, so without this a lost withdrawal would
// leave the hospital side holding a link the pharmacy has ended.

const (
	redeliveryActor = "system:pharmacy-affiliation-redelivery"
	redeliveryBatch = 50
	// firstRedelivery leaves time for the normal delivery and its confirmation.
	firstRedelivery = 2 * time.Minute
	maxRedelivery   = time.Hour
)

// redeliveryDelay doubles from two minutes and stops growing at an hour;
// a step is never given up on.
func redeliveryDelay(attempts int) time.Duration {
	d := firstRedelivery
	for i := 1; i < attempts && d < maxRedelivery; i++ {
		d *= 2
	}
	return min(d, maxRedelivery)
}

// sendRound records state as the round's step to deliver and publishes it,
// in the caller's transaction.
func sendRound(tx *gorm.DB, tenant string, affiliation, request uuid.UUID, state string) error {
	if err := tx.Exec(`INSERT INTO pharmacy_affiliation_deliveries (tenant_id,request_id,affiliation_id,state,attempts,next_attempt_at) VALUES (?,?,?,?,0,clock_timestamp()+make_interval(secs => ?))
 ON CONFLICT (tenant_id,request_id) DO UPDATE SET state=EXCLUDED.state,attempts=0,next_attempt_at=EXCLUDED.next_attempt_at,updated_at=clock_timestamp()`,
		tenant, request, affiliation, state, firstRedelivery.Seconds()).Error; err != nil {
		return err
	}
	return publishRound(tx, tenant, request, state)
}

// publishRound re-reads the claim from history, so a re-send is the same
// event as the first: the original requester, the declaration as recorded.
func publishRound(tx *gorm.DB, tenant string, request uuid.UUID, state string) error {
	var opened affiliationVerification
	if err := tx.Select(verificationColumns).Where("tenant_id=? AND request_id=? AND kind='requested'", tenant, request).Take(&opened).Error; err != nil {
		return err
	}
	var a hospitalAffiliation
	if err := tx.Select(affiliationColumns).Where("tenant_id=? AND id=?", tenant, opened.AffiliationID).Take(&a).Error; err != nil {
		return err
	}
	var org pharmacyOrganization
	if err := tx.Select(organizationColumns).Where("tenant_id=? AND id=?", tenant, a.PharmacyID).Take(&org).Error; err != nil {
		return err
	}
	subject := pharmacycontract.SubjectAffiliationVerificationRequested
	if state == "withdrawn" {
		subject = pharmacycontract.SubjectAffiliationVerificationWithdrawn
	}
	data, err := json.Marshal(pharmacycontract.AffiliationVerificationData{RequestID: request.String(), AffiliationID: a.ID.String(), PharmacyID: a.PharmacyID.String(), PharmacyName: org.Name, HospitalID: a.HospitalID.String(), Relationship: a.Relationship, RequesterSubject: *opened.ActorSubject})
	if err != nil {
		return err
	}
	return events.Enqueue(tx, subject, events.Event{Type: subject, Version: 1, TenantID: tenant, Data: data})
}

// confirmDecision tells patientmaster this decision is handled.
func confirmDecision(tx *gorm.DB, tenant string, d patientmastercontract.PharmacyAffiliationDecidedData) error {
	var applied int64
	if err := tx.Table("pharmacy_affiliation_verifications").Where("tenant_id=? AND decision_id=?", tenant, d.DecisionID).Count(&applied).Error; err != nil {
		return err
	}
	data, err := json.Marshal(pharmacycontract.AffiliationDecisionReceivedData{RequestID: d.RequestID, HospitalID: d.HospitalID, DecisionID: d.DecisionID, Applied: applied == 1})
	if err != nil {
		return err
	}
	subject := pharmacycontract.SubjectAffiliationDecisionReceived
	return events.Enqueue(tx, subject, events.Event{Type: subject, Version: 1, TenantID: tenant, Data: data})
}

// affiliationReceiptConsumer stops re-sending a step patientmaster has
// handled. A confirmation of a superseded step (a request, once withdrawn)
// matches no row and changes nothing.
func affiliationReceiptConsumer() events.Consumer {
	subject := patientmastercontract.SubjectPharmacyAffiliationReceived
	return events.Consumer{Name: "pharmacy-affiliation-receipt", Subject: subject, Handle: func(_ context.Context, tx *gorm.DB, evt events.Event) error {
		var d patientmastercontract.PharmacyAffiliationReceivedData
		if evt.Type != subject || evt.Version != 1 || evt.DestinationTenantID != "" || json.Unmarshal(evt.Data, &d) != nil || (d.State != "requested" && d.State != "withdrawn") {
			return errors.New("invalid affiliation receipt event")
		}
		request, err1 := uuid.Parse(d.RequestID)
		affiliation, err2 := uuid.Parse(d.AffiliationID)
		if err1 != nil || err2 != nil {
			return errors.New("invalid affiliation receipt event")
		}
		return tx.Exec(`UPDATE pharmacy_affiliation_deliveries SET next_attempt_at=NULL,updated_at=clock_timestamp() WHERE tenant_id=? AND request_id=? AND affiliation_id=? AND state=? AND next_attempt_at IS NOT NULL`,
			evt.TenantID, request, affiliation, d.State).Error
	}}
}

// RunAffiliationRedelivery re-sends unconfirmed steps until ctx ends.
func RunAffiliationRedelivery(ctx context.Context, deps platform.Deps) {
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if err := RedeliverAffiliationsOnce(ctx, deps.DB); err != nil {
				slog.WarnContext(ctx, "pharmacy affiliation redelivery pass failed", "error", err)
			}
		}
	}
}

// RedeliverAffiliationsOnce runs one pass; tests drive it instead of the ticker.
func RedeliverAffiliationsOnce(ctx context.Context, db *tenantdb.DB) error {
	var due []struct {
		TenantID  uuid.UUID
		RequestID uuid.UUID
	}
	// The one cross-tenant read: ids of due rounds, nothing else (the system
	// role holds SELECT on just these columns).
	if err := db.WithAllTenants(ctx, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT tenant_id,request_id FROM pharmacy_affiliation_deliveries WHERE next_attempt_at <= now() ORDER BY next_attempt_at LIMIT ?`, redeliveryBatch).Scan(&due).Error
	}); err != nil {
		return err
	}
	ctx = tenantdb.WithActor(ctx, redeliveryActor)
	for _, d := range due {
		if err := db.WithTenant(ctx, d.TenantID.String(), func(tx *gorm.DB) error {
			var row struct {
				State    string
				Attempts int
			}
			// Locked and re-checked: a confirmation may have landed since the scan.
			err := tx.Table("pharmacy_affiliation_deliveries").Select("state,attempts").Clauses(clause.Locking{Strength: "UPDATE"}).
				Where("tenant_id=? AND request_id=? AND next_attempt_at <= now()", d.TenantID, d.RequestID).Take(&row).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			if err != nil {
				return err
			}
			if err := publishRound(tx, d.TenantID.String(), d.RequestID, row.State); err != nil {
				return err
			}
			slog.WarnContext(ctx, "re-sent unconfirmed pharmacy affiliation step", "tenant_id", d.TenantID, "request_id", d.RequestID, "state", row.State, "attempt", row.Attempts+1)
			return tx.Exec(`UPDATE pharmacy_affiliation_deliveries SET attempts=attempts+1,next_attempt_at=clock_timestamp()+make_interval(secs => ?),updated_at=clock_timestamp() WHERE tenant_id=? AND request_id=?`,
				redeliveryDelay(row.Attempts+1).Seconds(), d.TenantID, d.RequestID).Error
		}); err != nil {
			slog.WarnContext(ctx, "pharmacy affiliation redelivery failed; retrying later", "tenant_id", d.TenantID, "request_id", d.RequestID, "error", err)
		}
	}
	return nil
}
