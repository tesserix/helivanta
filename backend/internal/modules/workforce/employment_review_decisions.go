package workforce

import (
	"encoding/json"
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	workforcecontract "github.com/tesserix/helivanta/internal/modules/workforce/contract"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"gorm.io/gorm"
)

func (h *handlers) employmentReviewReviewer(c *gin.Context, p authn.Principal, hospital, staff, id uuid.UUID, fn func(*gorm.DB, IdentitySource, *intakeAuthority) error) error {
	query := c.Request.URL.Query()
	if len(query) != 1 || len(query["employment_review"]) != 1 || query.Get("employment_review") != "v1" {
		return errIntakeProjection
	}
	return h.intakeAuthorityTransaction(c, p, hospital, []authz.Permission{PermEmploymentReview}, true, func(tx *gorm.DB, scoped bool) error {
		value, present := c.Get(intakeAuthorityKey)
		authority, ok := value.(*intakeAuthority)
		if !scoped || !present || !ok || authority == nil || !authority.Scoped {
			return authz.ErrScopedCommandDenied
		}
		if err := tx.Exec(`SELECT set_config('app.workforce_employment_review','review',true),set_config('app.workforce_employment_review_id',?,true),set_config('app.workforce_employment_review_actor',?,true),set_config('app.workforce_employment_review_link',?,true),set_config('app.workforce_identity_source','authorized',true),set_config('app.workforce_identity_staff',?,true)`, id.String(), authority.Actor.Staff.String(), authority.Actor.Link.String(), staff.String()).Error; err != nil {
			return err
		}
		// Verify the selected parent before disclosing a target's identity projection.
		var parent struct{ ID uuid.UUID }
		if err := tx.Table("workforce_employment_reviews").Select("id").Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=?", p.TenantID, hospital, staff, id).Take(&parent).Error; err != nil {
			return err
		}
		source, err := loadIdentitySource(tx, p, hospital, staff)
		if err != nil {
			return err
		}
		return fn(tx, source, authority)
	})
}

func (h *handlers) employmentReviewRead(c *gin.Context, p authn.Principal, hospital, staff, id uuid.UUID, fn func(*gorm.DB, IdentitySource, *intakeAuthority) error) error {
	if _, reviewer := c.Request.URL.Query()["employment_review"]; reviewer {
		return h.employmentReviewReviewer(c, p, hospital, staff, id, fn)
	}
	return h.employmentReviewPreparation(c, p, hospital, staff, id, fn)
}

func employmentReviewIndependent(tx *gorm.DB, p authn.Principal, row EmploymentReview, actor intakeActorReference) (bool, error) {
	if actor.Staff == row.StaffID || row.ProposedBySubject == p.Subject || row.DeclaredSubject == p.Subject {
		return false, nil
	}
	var excluded int64
	err := tx.Raw(`SELECT (SELECT count(*) FROM workforce_employment_review_contributors WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND review_id=? AND subject=?) + (SELECT count(*) FROM workforce_login_links WHERE tenant_id=? AND hospital_id=? AND staff_id=? AND subject=?)`, p.TenantID, row.HospitalID, row.StaffID, row.ID, p.Subject, p.TenantID, row.HospitalID, row.StaffID, p.Subject).Scan(&excluded).Error
	return excluded == 0, err
}

func employmentReviewReceipt(tx *gorm.DB, p authn.Principal, hospital, staff, id, command uuid.UUID, intent string) (employmentReviewRecord, bool, error) {
	row, err := loadEmploymentReview(tx, p, hospital, staff, id, command)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return row, false, nil
	}
	if err == nil && (row.ActorSubject != p.Subject || row.InputHash != intent) {
		err = errEmploymentReviewConflict
	}
	return row, err == nil, err
}

func (h *handlers) employmentReviewChanged(tx *gorm.DB, p authn.Principal, row EmploymentReview) error {
	data, err := json.Marshal(workforcecontract.EmploymentReviewChangedData{HospitalID: row.HospitalID.String(), StaffID: row.StaffID.String(), ReviewID: row.ID.String(), RevisionID: row.RevisionID.String(), Version: row.Version})
	if err != nil {
		return err
	}
	return h.bus.Publish(tx, workforcecontract.SubjectEmploymentReviewChanged, events.Event{Type: workforcecontract.SubjectEmploymentReviewChanged, Version: 1, TenantID: p.TenantID, Data: data})
}

func (h *handlers) decideEmploymentReview(c *gin.Context) {
	p, hospital, staff, id, ok := personReviewParams(c)
	if !ok {
		return
	}
	var in EmploymentReviewDecisionInput
	if !decodeSized(c, &in, 4096) {
		return
	}
	if in.CommandID.Version() != 7 || in.ExpectedVersion == nil || *in.ExpectedVersion < 1 || *in.ExpectedVersion >= 9007199254740990 || !contains(in.Decision, "approve", "reject") || !personReviewText(&in.Reason, 500, true) || !in.IndependentPersonConfirmed || !in.TermsExamined {
		respond.Error(c, 400, "invalid_request", "Enter the current review version, decision reason and human examination and independence attestations.")
		return
	}
	intent, err := personReviewIntent("decide-employment-review", in)
	if err != nil {
		employmentReviewError(c, err)
		return
	}
	var result EmploymentReview
	err = h.employmentReviewReviewer(c, p, hospital, staff, id, func(tx *gorm.DB, source IdentitySource, authority *intakeAuthority) error {
		row, err := loadEmploymentReview(tx, p, hospital, staff, id, uuid.Nil)
		if err != nil {
			return err
		}
		independent, err := employmentReviewIndependent(tx, p, row.EmploymentReview, authority.Actor)
		if err != nil {
			return err
		}
		if !independent {
			return errPersonReviewIndependence
		}
		receipt, found, err := employmentReviewReceipt(tx, p, hospital, staff, id, in.CommandID, intent)
		if err != nil {
			return err
		}
		if found {
			result = receipt.EmploymentReview
			return employmentReviewAudit(tx, p, result, "workforce.employment_review.receipt.read", authority)
		}
		if row.Status != "pending" || row.Version != *in.ExpectedVersion {
			return errEmploymentReviewConflict
		}
		status := "rejected"
		if in.Decision == "approve" {
			state, err := employmentReviewState(tx, p, source, row.EmploymentReview)
			if err != nil {
				return err
			}
			if state != "pending" {
				return errEmploymentReviewConflict
			}
			status = "approved"
		}
		updated := tx.Table("workforce_employment_reviews").Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=? AND version=? AND status='pending'", p.TenantID, hospital, staff, id, *in.ExpectedVersion).Updates(map[string]any{"status": status, "revision_id": in.CommandID, "version": row.Version + 1, "actor_subject": p.Subject, "decision_reason": in.Reason, "reviewer_terms_examined": in.TermsExamined, "independent_person_confirmed": in.IndependentPersonConfirmed, "input_hash": intent})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return errEmploymentReviewConflict
		}
		saved, err := loadEmploymentReview(tx, p, hospital, staff, id, uuid.Nil)
		if err != nil {
			return err
		}
		result = saved.EmploymentReview
		if err := employmentReviewAudit(tx, p, result, "workforce.employment_review.decided", authority); err != nil {
			return err
		}
		return h.employmentReviewChanged(tx, p, result)
	})
	if err != nil {
		employmentReviewError(c, err)
		return
	}
	respond.OK(c, result)
}

func (h *handlers) withdrawEmploymentReview(c *gin.Context) { h.endEmploymentReview(c, true) }
func (h *handlers) revokeEmploymentReview(c *gin.Context)   { h.endEmploymentReview(c, false) }

func (h *handlers) endEmploymentReview(c *gin.Context, withdraw bool) {
	p, hospital, staff, id, ok := personReviewParams(c)
	if !ok {
		return
	}
	var in EmploymentReviewActionInput
	if !decodeSized(c, &in, 4096) {
		return
	}
	if in.CommandID.Version() != 7 || in.ExpectedVersion == nil || *in.ExpectedVersion < 1 || *in.ExpectedVersion >= 9007199254740990 || !personReviewText(&in.Reason, 500, true) {
		respond.Error(c, 400, "invalid_request", "Enter the current review version and a reason.")
		return
	}
	transaction, before, after := h.employmentReviewReviewer, "approved", "revoked"
	if withdraw {
		transaction, before, after = h.employmentReviewPreparation, "pending", "withdrawn"
	}
	intent, err := personReviewIntent(after+"-employment-review", in)
	if err != nil {
		employmentReviewError(c, err)
		return
	}
	var result EmploymentReview
	err = transaction(c, p, hospital, staff, id, func(tx *gorm.DB, _ IdentitySource, authority *intakeAuthority) error {
		row, err := loadEmploymentReview(tx, p, hospital, staff, id, uuid.Nil)
		if err != nil {
			return err
		}
		if withdraw {
			if row.ProposedBySubject != p.Subject {
				return authz.ErrScopedCommandDenied
			}
		} else {
			independent, err := employmentReviewIndependent(tx, p, row.EmploymentReview, authority.Actor)
			if err != nil {
				return err
			}
			if !independent {
				return errPersonReviewIndependence
			}
		}
		receipt, found, err := employmentReviewReceipt(tx, p, hospital, staff, id, in.CommandID, intent)
		if err != nil {
			return err
		}
		if found {
			result = receipt.EmploymentReview
			return employmentReviewAudit(tx, p, result, "workforce.employment_review.receipt.read", authority)
		}
		if row.Status != before || row.Version != *in.ExpectedVersion {
			return errEmploymentReviewConflict
		}
		updated := tx.Table("workforce_employment_reviews").Where("tenant_id=? AND hospital_id=? AND staff_id=? AND id=? AND version=? AND status=?", p.TenantID, hospital, staff, id, *in.ExpectedVersion, before).Updates(map[string]any{"status": after, "revision_id": in.CommandID, "version": row.Version + 1, "actor_subject": p.Subject, "decision_reason": in.Reason, "reviewer_terms_examined": false, "independent_person_confirmed": false, "input_hash": intent})
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return errEmploymentReviewConflict
		}
		saved, err := loadEmploymentReview(tx, p, hospital, staff, id, uuid.Nil)
		if err != nil {
			return err
		}
		result = saved.EmploymentReview
		if err := employmentReviewAudit(tx, p, result, "workforce.employment_review."+after, authority); err != nil {
			return err
		}
		return h.employmentReviewChanged(tx, p, result)
	})
	if err != nil {
		employmentReviewError(c, err)
		return
	}
	respond.OK(c, result)
}
