package patientmaster

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	patientmastercontract "github.com/tesserix/helivanta/internal/modules/patientmaster/contract"
	pharmacycontract "github.com/tesserix/helivanta/internal/modules/pharmacy/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// pharmacyAffiliationReview is a pharmacy's request that this hospital
// verify its affiliation (#1416). The requester is kept only to refuse
// self-acceptance and is never shown; each decision shows its reviewer.
type pharmacyAffiliationReview struct {
	RequestID        uuid.UUID                     `json:"request_id"`
	AffiliationID    uuid.UUID                     `json:"affiliation_id"`
	PharmacyID       uuid.UUID                     `json:"pharmacy_id"`
	PharmacyName     string                        `json:"pharmacy_name"`
	HospitalID       uuid.UUID                     `json:"hospital_id"`
	Relationship     string                        `json:"relationship"`
	RequesterSubject string                        `json:"-"`
	Status           string                        `json:"status"`
	CreatedAt        time.Time                     `json:"created_at"`
	UpdatedAt        time.Time                     `json:"updated_at"`
	Decisions        []pharmacyAffiliationDecision `json:"decisions" gorm:"-"`
}

func (pharmacyAffiliationReview) TableName() string { return "patient_pharmacy_affiliation_reviews" }
func (r pharmacyAffiliationReview) PageKey() (time.Time, uuid.UUID) {
	return r.CreatedAt, r.RequestID
}

type pharmacyAffiliationDecision struct {
	ID              uuid.UUID `json:"id"`
	RequestID       uuid.UUID `json:"-"`
	Outcome         string    `json:"outcome"`
	Reason          string    `json:"reason"`
	Fingerprint     string    `json:"-"`
	ReviewerSubject string    `json:"reviewer_subject"`
	CreatedAt       time.Time `json:"created_at"`
}

func (pharmacyAffiliationDecision) TableName() string {
	return "patient_pharmacy_affiliation_decisions"
}

const pharmacyAffiliationColumns = "request_id,affiliation_id,pharmacy_id,pharmacy_name,hospital_id,relationship,requester_subject,status,created_at,updated_at"
const pharmacyDecisionColumns = "id,request_id,outcome,reason,fingerprint,reviewer_subject,created_at"

var errAffiliationConflict = errors.New("pharmacy affiliation review conflicts")

// pharmacyAffiliationConsumer queues a request, or records its withdrawal.
// Either may arrive first: both carry the whole claim, and a withdrawal
// that overtakes its request is stored as an already-withdrawn review.
func pharmacyAffiliationConsumer(subject, name string) events.Consumer {
	return events.Consumer{Name: name, Subject: subject, Handle: func(_ context.Context, tx *gorm.DB, evt events.Event) error {
		var d pharmacycontract.AffiliationVerificationData
		if evt.Type != subject || evt.Version != 1 || evt.DestinationTenantID != "" || !validReviewUUID(evt.TenantID, false) || json.Unmarshal(evt.Data, &d) != nil ||
			!validReviewUUID(d.RequestID, true) || !validReviewUUID(d.AffiliationID, true) || !validReviewUUID(d.PharmacyID, true) || !validReviewUUID(d.HospitalID, false) ||
			strings.TrimSpace(d.RequesterSubject) != d.RequesterSubject || len(d.RequesterSubject) < 1 || len(d.RequesterSubject) > 200 ||
			strings.TrimSpace(d.PharmacyName) == "" || len([]rune(d.PharmacyName)) > 200 {
			return errors.New("invalid pharmacy affiliation event")
		}
		switch d.Relationship {
		case "owner", "serving", "partner":
		default:
			return errors.New("invalid pharmacy affiliation relationship")
		}
		if err := tx.Exec(`SELECT set_config('app.pharmacy_affiliation_hospital',?,true),set_config('app.pharmacy_affiliation_request',?,true)`, d.HospitalID, d.RequestID).Error; err != nil {
			return errors.New("pharmacy affiliation scope unavailable")
		}
		status := "pending"
		if subject == pharmacycontract.SubjectAffiliationVerificationWithdrawn {
			status = "withdrawn"
		}
		if err := tx.Exec(`INSERT INTO patient_pharmacy_affiliation_reviews(tenant_id,request_id,affiliation_id,pharmacy_id,pharmacy_name,hospital_id,relationship,requester_subject,status) VALUES (?,?,?,?,?,?,?,?,?) ON CONFLICT(tenant_id,request_id) DO NOTHING`,
			evt.TenantID, d.RequestID, d.AffiliationID, d.PharmacyID, d.PharmacyName, d.HospitalID, d.Relationship, d.RequesterSubject, status).Error; err != nil {
			return errors.New("pharmacy affiliation ingestion unavailable")
		}
		var row pharmacyAffiliationReview
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select(pharmacyAffiliationColumns).Where("tenant_id=? AND request_id=?", evt.TenantID, d.RequestID).Take(&row).Error; err != nil {
			return errors.New("pharmacy affiliation receipt unavailable")
		}
		if row.AffiliationID.String() != d.AffiliationID || row.PharmacyID.String() != d.PharmacyID || row.PharmacyName != d.PharmacyName || row.HospitalID.String() != d.HospitalID || row.Relationship != d.Relationship || row.RequesterSubject != d.RequesterSubject {
			return errors.New("pharmacy affiliation claim mismatch")
		}
		if status == "withdrawn" && (row.Status == "pending" || row.Status == "accepted") {
			if err := tx.Exec(`UPDATE patient_pharmacy_affiliation_reviews SET status='withdrawn',updated_at=clock_timestamp() WHERE tenant_id=? AND request_id=?`, evt.TenantID, d.RequestID).Error; err != nil {
				return errors.New("pharmacy affiliation withdrawal unavailable")
			}
		}
		return nil
	}}
}

func pharmacyAffiliationReadAudit(tx *gorm.DB, p authn.Principal, rows []pharmacyAffiliationReview) error {
	ids := make([]uuid.UUID, len(rows))
	for i, row := range rows {
		ids[i] = row.RequestID
	}
	encoded, err := json.Marshal(ids)
	if err != nil {
		return err
	}
	return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) SELECT ?,?,'read','patient_pharmacy_affiliation_reviews',value::uuid FROM jsonb_array_elements_text(?::jsonb)`, p.TenantID, p.Subject, string(encoded)).Error
}

func withAffiliationDecisions(tx *gorm.DB, tenant string, rows []pharmacyAffiliationReview) error {
	if len(rows) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(rows))
	for i := range rows {
		ids[i] = rows[i].RequestID
	}
	var decisions []pharmacyAffiliationDecision
	if err := tx.Select(pharmacyDecisionColumns).Where("tenant_id=? AND request_id IN ?", tenant, ids).Order("created_at,id").Find(&decisions).Error; err != nil {
		return err
	}
	by := make(map[uuid.UUID][]pharmacyAffiliationDecision, len(rows))
	for _, d := range decisions {
		by[d.RequestID] = append(by[d.RequestID], d)
	}
	for i := range rows {
		rows[i].Decisions = append([]pharmacyAffiliationDecision{}, by[rows[i].RequestID]...)
	}
	return nil
}

func (h *patientHandlers) listPharmacyAffiliations(c *gin.Context, params pagination.Params) ([]pharmacyAffiliationReview, error) {
	c.Header("Cache-Control", "no-store")
	hospital, ok := hospitalScope(c)
	if !ok {
		return nil, nil
	}
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return nil, nil
	}
	var rows []pharmacyAffiliationReview
	err := h.hospitalTransaction(c.Request.Context(), p, hospital, true, func(tx *gorm.DB) error {
		if err := tenantdb.ApplyKeyset(tx, params).Select(pharmacyAffiliationColumns).Where("tenant_id=? AND hospital_id=?", p.TenantID, hospital).Find(&rows).Error; err != nil {
			return err
		}
		if err := withAffiliationDecisions(tx, p.TenantID, rows); err != nil {
			return err
		}
		return pharmacyAffiliationReadAudit(tx, p, rows)
	})
	if err != nil {
		patientError(c, err)
		return nil, nil
	}
	return rows, nil
}

type pharmacyAffiliationDecisionInput struct {
	Outcome string `json:"outcome"`
	Reason  string `json:"reason"`
}

func (h *patientHandlers) decidePharmacyAffiliation(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	hospital, ok := hospitalScope(c)
	if !ok {
		return
	}
	p, _, ok := authn.ExpectedTenantPrincipal(c)
	if !ok {
		return
	}
	if !validReviewUUID(c.Param("request"), true) || !validReviewUUID(c.Param("decision"), true) {
		respond.NotFound(c, "review")
		return
	}
	var input pharmacyAffiliationDecisionInput
	if !decodePatientInput(c, &input) {
		return
	}
	input.Reason = strings.TrimSpace(input.Reason)
	if (input.Outcome != "accepted" && input.Outcome != "declined" && input.Outcome != "revoked") || input.Reason == "" || len([]rune(input.Reason)) > 500 {
		respond.BadRequest(c, errors.New("outcome must be accepted, declined or revoked, with a reason of 1 to 500 characters"))
		return
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		respond.InternalErr(c, err, "could not process review decision")
		return
	}
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(encoded))
	var row pharmacyAffiliationReview
	err = h.hospitalTransaction(c.Request.Context(), p, hospital, true, func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select(pharmacyAffiliationColumns).Where("tenant_id=? AND hospital_id=? AND request_id=?", p.TenantID, hospital, c.Param("request")).Take(&row).Error; err != nil {
			return err
		}
		var prior []pharmacyAffiliationDecision
		if err := tx.Select(pharmacyDecisionColumns).Where("tenant_id=? AND id=?", p.TenantID, c.Param("decision")).Find(&prior).Error; err != nil {
			return err
		}
		if len(prior) == 1 {
			// A retry is answered from history; the same id with other input,
			// or for another request, is a different command.
			if prior[0].RequestID != row.RequestID || prior[0].Fingerprint != fingerprint || prior[0].ReviewerSubject != p.Subject {
				return errAffiliationConflict
			}
			return h.readPharmacyAffiliation(tx, p, &row)
		}
		// Self-acceptance and the wrong state are refused here for a clear
		// answer, and again by the insert trigger whatever the code path.
		if (input.Outcome == "accepted" && row.RequesterSubject == p.Subject) ||
			(input.Outcome != "revoked" && row.Status != "pending") || (input.Outcome == "revoked" && row.Status != "accepted") {
			return errAffiliationConflict
		}
		decision := pharmacyAffiliationDecision{ID: uuid.MustParse(c.Param("decision")), RequestID: row.RequestID, Outcome: input.Outcome, Reason: input.Reason, Fingerprint: fingerprint, ReviewerSubject: p.Subject}
		if err := tx.Exec(`INSERT INTO patient_pharmacy_affiliation_decisions(id,tenant_id,hospital_id,request_id,outcome,reason,fingerprint,reviewer_subject) VALUES (?,?,?,?,?,?,?,?)`,
			decision.ID, p.TenantID, hospital, row.RequestID, decision.Outcome, decision.Reason, decision.Fingerprint, decision.ReviewerSubject).Error; err != nil {
			return err
		}
		if err := tx.Select(pharmacyDecisionColumns).Where("tenant_id=? AND id=?", p.TenantID, decision.ID).Take(&decision).Error; err != nil {
			return err
		}
		data, err := json.Marshal(patientmastercontract.PharmacyAffiliationDecidedData{RequestID: row.RequestID.String(), AffiliationID: row.AffiliationID.String(), HospitalID: row.HospitalID.String(), DecisionID: decision.ID.String(), Outcome: decision.Outcome, DecidedAt: decision.CreatedAt.UTC().Format(time.RFC3339Nano)})
		if err != nil {
			return err
		}
		if err := h.bus.Publish(tx, patientmastercontract.SubjectPharmacyAffiliationDecided, events.Event{Type: patientmastercontract.SubjectPharmacyAffiliationDecided, Version: 1, TenantID: p.TenantID, Data: data}); err != nil {
			return err
		}
		return h.readPharmacyAffiliation(tx, p, &row)
	})
	var pg *pgconn.PgError
	if errors.As(err, &pg) && (pg.Code == "23505" || pg.Code == "23514") {
		// A concurrent decision won the round, or the trigger refused it.
		err = errAffiliationConflict
	}
	if errors.Is(err, errAffiliationConflict) {
		respond.Error(c, 409, "review_conflict", "The request's state or this decision's input changed")
		return
	}
	if err != nil {
		patientError(c, err)
		return
	}
	respond.OK(c, row)
}

func (h *patientHandlers) readPharmacyAffiliation(tx *gorm.DB, p authn.Principal, row *pharmacyAffiliationReview) error {
	if err := tx.Select(pharmacyAffiliationColumns).Where("tenant_id=? AND request_id=?", p.TenantID, row.RequestID).Take(row).Error; err != nil {
		return err
	}
	rows := []pharmacyAffiliationReview{*row}
	if err := withAffiliationDecisions(tx, p.TenantID, rows); err != nil {
		return err
	}
	*row = rows[0]
	return pharmacyAffiliationReadAudit(tx, p, rows)
}
