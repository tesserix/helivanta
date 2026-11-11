package workforce

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/tesserix/helivanta/internal/platform/respond"
)

// pharmacyEmployment admits a pharmacy's employment terms only from the caller's current account and workspace (ADR-0054 slice 4b).
func (h *handlers) pharmacyEmployment(c *gin.Context) (employmentRecord, bool) {
	p, pharmacy, ok := pharmacyStaffScope(c)
	if !ok || !enrollmentSession(c) {
		return employmentRecord{}, false
	}
	r, ok := employmentIDs(c, p, pharmacy.establishment())
	r.authority = h.pharmacyAuthority(c, p, pharmacy)
	return r, ok
}

// pharmacyEmploymentLifecycle requires the lifecycle view, because no pharmacy client predates withdrawal.
func (h *handlers) pharmacyEmploymentLifecycle(c *gin.Context) (employmentRecord, bool) {
	r, ok := h.pharmacyEmployment(c)
	if _, present := c.Request.URL.Query()["lifecycle"]; ok && !present {
		respond.Error(c, 400, "invalid_request", employmentLifecycleUnsupported)
		return r, false
	}
	return r, ok
}

func (h *handlers) pharmacyEmploymentEvidence(c *gin.Context) (employmentEvidenceScope, bool) {
	r, ok := h.pharmacyEmployment(c)
	if !ok {
		return employmentEvidenceScope{employmentRecord: r}, false
	}
	return employmentEvidenceParams(c, r)
}

func (h *handlers) downloadPharmacyEmploymentEvidence(c *gin.Context) {
	if s, ok := h.pharmacyEmploymentEvidence(c); ok {
		h.sendEmploymentEvidence(c, s)
	}
}

type pharmacyEmploymentPeriodRevision struct{ pharmacyEmploymentPeriodDraft }

type pharmacyEmploymentEvidenceRevision struct {
	pharmacyEmploymentEvidenceBinding
}

func (r pharmacyEmploymentPeriodDraft) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.ID }

func (r pharmacyEmploymentPeriodRevision) PageKey() (time.Time, uuid.UUID) {
	return r.UpdatedAt, r.ID
}

func (r pharmacyEmploymentEvidenceBinding) PageKey() (time.Time, uuid.UUID) {
	return r.CreatedAt, r.ID
}

func (r pharmacyEmploymentEvidenceRevision) PageKey() (time.Time, uuid.UUID) {
	return r.UpdatedAt, r.ID
}

func pharmacyEmploymentPeriodRevisionView(r employmentPeriodRevision) pharmacyEmploymentPeriodRevision {
	return pharmacyEmploymentPeriodRevision{pharmacyEmploymentPeriodDraftView(r.EmploymentPeriodDraft)}
}

func pharmacyEmploymentEvidenceRevisionView(r employmentEvidenceRevision) pharmacyEmploymentEvidenceRevision {
	return pharmacyEmploymentEvidenceRevision{pharmacyEmploymentEvidenceBindingView(r.EmploymentEvidenceBinding)}
}
