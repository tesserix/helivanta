package pharmacy

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"

	patientmastercontract "github.com/tesserix/helivanta/internal/modules/patientmaster/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// affiliationVerification is one step of an affiliation's verification
// history (#1416). The hospital's reviewer is recorded in patientmaster,
// behind decision_id; pharmacy keeps only that receipt.
type affiliationVerification struct {
	ID            uuid.UUID  `json:"id"`
	TenantID      uuid.UUID  `json:"-"`
	AffiliationID uuid.UUID  `json:"affiliation_id"`
	Sequence      int        `json:"sequence"`
	Kind          string     `json:"kind"`
	RequestID     uuid.UUID  `json:"request_id"`
	DecisionID    *uuid.UUID `json:"decision_id"`
	ActorSubject  *string    `json:"actor_subject"`
	Reason        *string    `json:"reason"`
	CreatedAt     time.Time  `json:"created_at" gorm:"autoCreateTime:false;default:clock_timestamp()"`
}

func (affiliationVerification) TableName() string                 { return "pharmacy_affiliation_verifications" }
func (r affiliationVerification) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

const verificationColumns = "id,tenant_id,affiliation_id,sequence,kind,request_id,decision_id,actor_subject,reason,created_at"

// verificationStatus names the affiliation's state after its latest step.
var verificationStatus = map[string]string{"requested": "requested", "accepted": "verified", "declined": "declined", "revoked": "revoked", "withdrawn": "withdrawn"}

var (
	errVerificationConflict = errors.New("affiliation verification state conflicts")
	errWithdrawReason       = errors.New("a withdrawal reason of 1 to 500 characters is required")
)

func affiliationVerificationRoutes(g *platform.Router, h *catalogueHandlers) {
	a := "/pharmacies/:pharmacy/hospital-affiliations/:id"
	g.PUT(a+"/verification-requests/:request", PermAccessManage, h.requestVerification)
	g.POST(a+"/verification-requests/:request/withdraw", PermAccessManage, h.withdrawVerification)
	platform.ListRoute(g, a+"/verifications", PermCatalogueRead, h.listVerifications)
}

// latestVerification is nil for an affiliation never put up for review.
func latestVerification(tx *gorm.DB, tenant string, affiliation uuid.UUID) (*affiliationVerification, error) {
	var rows []affiliationVerification
	if err := tx.Select(verificationColumns).Where("tenant_id=? AND affiliation_id=?", tenant, affiliation).Order("sequence DESC").Limit(1).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return &rows[0], nil
}

// fillVerification replaces the declaration-time status with the current
// one. The declaration column never changes; history decides.
func fillVerification(tx *gorm.DB, tenant string, rows []hospitalAffiliation) error {
	if len(rows) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(rows))
	for i := range rows {
		ids[i] = rows[i].ID
	}
	var latest []affiliationVerification
	if err := tx.Raw(`SELECT DISTINCT ON (affiliation_id) `+verificationColumns+` FROM pharmacy_affiliation_verifications WHERE tenant_id=? AND affiliation_id IN ? ORDER BY affiliation_id, sequence DESC`, tenant, ids).Scan(&latest).Error; err != nil {
		return err
	}
	by := make(map[uuid.UUID]affiliationVerification, len(latest))
	for _, v := range latest {
		by[v.AffiliationID] = v
	}
	for i := range rows {
		v, ok := by[rows[i].ID]
		if !ok {
			rows[i].VerificationStatus = "pending_validation"
			continue
		}
		rows[i].VerificationStatus = verificationStatus[v.Kind]
		request := v.RequestID
		rows[i].VerificationRequestID = &request
		if v.Kind == "accepted" {
			// When pharmacy recorded the acceptance; the hospital's own
			// decision time stays in patientmaster's history.
			at := v.CreatedAt
			rows[i].VerifiedAt = &at
		}
	}
	return nil
}

func verificationTarget(c *gin.Context) (pharmacy, affiliation, request uuid.UUID, ok bool) {
	if pharmacy, ok = registryID(c, "pharmacy"); !ok {
		return
	}
	if affiliation, ok = registryID(c, "id"); !ok {
		return
	}
	request, ok = registryID(c, "request")
	return
}

// verificationCommand runs one step under the affiliation's lock and answers
// with the affiliation's resulting status, read in the same transaction.
func (h *catalogueHandlers) verificationCommand(c *gin.Context, in any, step func(tx *gorm.DB, p authn.Principal, a hospitalAffiliation, org pharmacyOrganization, request uuid.UUID, prev *affiliationVerification) error) {
	p, _, ok := pharmacyPrincipal(c)
	if !ok {
		return
	}
	pharmacy, id, request, ok := verificationTarget(c)
	if !ok || !decodeCatalogue(c, in) {
		return
	}
	var result hospitalAffiliation
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if err := stockLock(tx, p.TenantID+":affiliation:"+id.String()); err != nil {
			return err
		}
		if err := tx.Select(affiliationColumns).Where("tenant_id=? AND pharmacy_id=? AND id=?", p.TenantID, pharmacy, id).Take(&result).Error; err != nil {
			return err
		}
		var org pharmacyOrganization
		if err := tx.Select(organizationColumns).Where("tenant_id=? AND id=?", p.TenantID, pharmacy).Take(&org).Error; err != nil {
			return err
		}
		prev, err := latestVerification(tx, p.TenantID, id)
		if err != nil {
			return err
		}
		if err := step(tx, p, result, org, request, prev); err != nil {
			return err
		}
		rows := []hospitalAffiliation{result}
		if err := fillVerification(tx, p.TenantID, rows); err != nil {
			return err
		}
		result = rows[0]
		return nil
	})
	var pg *pgconn.PgError
	if errors.As(err, &pg) && (pg.Code == "23505" || pg.Code == "23514") {
		// A racing step lost on the sequence or round-id key, or the
		// transition trigger refused it: the state moved under the caller.
		err = errVerificationConflict
	}
	switch {
	case errors.Is(err, errWithdrawReason):
		respond.Error(c, http.StatusBadRequest, "invalid_request", errWithdrawReason.Error())
	case errors.Is(err, errVerificationConflict):
		respond.Error(c, http.StatusConflict, "verification_conflict", "the affiliation's verification state does not allow this; reload and try again")
	case err != nil:
		stockError(c, err)
	default:
		respond.OK(c, result)
	}
}

func (h *catalogueHandlers) requestVerification(c *gin.Context) {
	var in struct{}
	h.verificationCommand(c, &in, func(tx *gorm.DB, p authn.Principal, a hospitalAffiliation, org pharmacyOrganization, request uuid.UUID, prev *affiliationVerification) error {
		var replay []affiliationVerification
		if err := tx.Select(verificationColumns).Where("tenant_id=? AND request_id=? AND kind='requested'", p.TenantID, request).Limit(1).Find(&replay).Error; err != nil {
			return err
		}
		if len(replay) == 1 {
			// A retry of this round is answered with the current state; the
			// same id on another affiliation is a different command.
			if replay[0].AffiliationID != a.ID {
				return errVerificationConflict
			}
			return nil
		}
		sequence := 1
		if prev != nil {
			if prev.Kind == "requested" || prev.Kind == "accepted" {
				return errVerificationConflict
			}
			sequence = prev.Sequence + 1
		}
		subject := p.Subject
		if err := tx.Create(&affiliationVerification{ID: uuid.Must(uuid.NewV7()), TenantID: a.TenantID, AffiliationID: a.ID, Sequence: sequence, Kind: "requested", RequestID: request, ActorSubject: &subject}).Error; err != nil {
			return err
		}
		return sendRound(tx, p.TenantID, a.ID, request, "requested")
	})
}

func (h *catalogueHandlers) withdrawVerification(c *gin.Context) {
	var in struct {
		Reason string `json:"reason"`
	}
	h.verificationCommand(c, &in, func(tx *gorm.DB, p authn.Principal, a hospitalAffiliation, org pharmacyOrganization, request uuid.UUID, prev *affiliationVerification) error {
		reason := strings.TrimSpace(in.Reason)
		if reason == "" || len([]rune(reason)) > 500 {
			return errWithdrawReason
		}
		// A retry is answered from that round's own withdrawal, even after a
		// newer round has opened; a different reason is a different command.
		var done []affiliationVerification
		if err := tx.Select(verificationColumns).Where("tenant_id=? AND affiliation_id=? AND request_id=? AND kind='withdrawn'", p.TenantID, a.ID, request).Limit(1).Find(&done).Error; err != nil {
			return err
		}
		if len(done) == 1 {
			if done[0].Reason == nil || *done[0].Reason != reason {
				return errVerificationConflict
			}
			return nil
		}
		if prev == nil || prev.RequestID != request {
			return errVerificationConflict
		}
		if prev.Kind != "requested" && prev.Kind != "accepted" {
			return errVerificationConflict
		}
		subject := p.Subject
		if err := tx.Create(&affiliationVerification{ID: uuid.Must(uuid.NewV7()), TenantID: a.TenantID, AffiliationID: a.ID, Sequence: prev.Sequence + 1, Kind: "withdrawn", RequestID: request, ActorSubject: &subject, Reason: &reason}).Error; err != nil {
			return err
		}
		return sendRound(tx, p.TenantID, a.ID, request, "withdrawn")
	})
}

func (h *catalogueHandlers) listVerifications(c *gin.Context, params pagination.Params) ([]affiliationVerification, error) {
	p, _, ok := pharmacyPrincipal(c)
	if !ok {
		return nil, nil
	}
	pharmacy, ok := registryID(c, "pharmacy")
	if !ok {
		return nil, nil
	}
	id, ok := registryID(c, "id")
	if !ok {
		return nil, nil
	}
	var rows []affiliationVerification
	err := h.db.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
		if err := pharmacyVisible(tx, p, pharmacy, registryManager(c)); err != nil {
			return err
		}
		var n int64
		if err := tx.Table("pharmacy_hospital_affiliations").Where("tenant_id=? AND pharmacy_id=? AND id=?", p.TenantID, pharmacy, id).Count(&n).Error; err != nil {
			return err
		}
		if n != 1 {
			return gorm.ErrRecordNotFound
		}
		return tenantdb.ApplyKeyset(tx, params).Select(verificationColumns).Where("tenant_id=? AND affiliation_id=?", p.TenantID, id).Find(&rows).Error
	})
	if err != nil {
		catalogueError(c, err)
	}
	return rows, nil
}

// affiliationDecisionConsumer records the hospital's decision against the
// round it answers. A decision for a round that is no longer open (withdrawn,
// or already decided) is a no-op, so redelivery and reordering converge: a
// revoke that overtakes its accept closes the round, and the accept that
// follows finds it closed.
func affiliationDecisionConsumer() events.Consumer {
	subject := patientmastercontract.SubjectPharmacyAffiliationDecided
	return events.Consumer{Name: "pharmacy-affiliation-decision", Subject: subject, Handle: func(_ context.Context, tx *gorm.DB, evt events.Event) error {
		var d patientmastercontract.PharmacyAffiliationDecidedData
		if evt.Type != subject || evt.Version != 1 || evt.DestinationTenantID != "" || json.Unmarshal(evt.Data, &d) != nil {
			return errors.New("invalid affiliation decision event")
		}
		request, err1 := uuid.Parse(d.RequestID)
		affiliation, err2 := uuid.Parse(d.AffiliationID)
		decision, err3 := uuid.Parse(d.DecisionID)
		if err1 != nil || err2 != nil || err3 != nil || decision.Version() != 7 || request.Version() != 7 {
			return errors.New("invalid affiliation decision event")
		}
		switch d.Outcome {
		case "accepted", "declined", "revoked":
		default:
			return errors.New("invalid affiliation decision outcome")
		}
		if err := stockLock(tx, evt.TenantID+":affiliation:"+affiliation.String()); err != nil {
			return err
		}
		var seen int64
		if err := tx.Table("pharmacy_affiliation_verifications").Where("tenant_id=? AND decision_id=?", evt.TenantID, decision).Count(&seen).Error; err != nil {
			return err
		}
		if seen != 0 {
			return confirmDecision(tx, evt.TenantID, d)
		}
		var a hospitalAffiliation
		if err := tx.Select(affiliationColumns).Where("tenant_id=? AND id=?", evt.TenantID, affiliation).Take(&a).Error; err != nil {
			return err
		}
		if a.HospitalID.String() != d.HospitalID {
			return errors.New("affiliation decision names a different hospital")
		}
		prev, err := latestVerification(tx, evt.TenantID, affiliation)
		if err != nil {
			return err
		}
		open := prev != nil && prev.RequestID == request && (prev.Kind == "requested" || (prev.Kind == "accepted" && d.Outcome == "revoked"))
		if open {
			step := affiliationVerification{ID: uuid.Must(uuid.NewV7()), TenantID: a.TenantID, AffiliationID: affiliation, Sequence: prev.Sequence + 1, Kind: d.Outcome, RequestID: request, DecisionID: &decision}
			if err := tx.Create(&step).Error; err != nil {
				return err
			}
		}
		// A no-op is still handled: patientmaster stops re-sending either way.
		return confirmDecision(tx, evt.TenantID, d)
	}}
}
