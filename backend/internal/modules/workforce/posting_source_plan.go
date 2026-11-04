package workforce

import (
	"time"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authz"
)

type postingSourcePlan struct {
	Sources          []postingChainSource
	References       []authz.DepartmentReference
	EligibilityUntil time.Time
}

func collectPostingSourcePlan(tenant string, hospital uuid.UUID, row StaffPostingProposal, intent StaffPostingSupervisionIntent, read func(postingDraftScope, uuid.UUID) (postingChainSource, error)) (postingSourcePlan, error) {
	plan := postingSourcePlan{Sources: make([]postingChainSource, 0, postingChainLimit-1), EligibilityUntil: row.EligibilityUntil}
	if row.HospitalID != hospital || !validPostingSupervisionIntent(&intent) || !row.StartsAt.Before(row.EligibilityUntil) {
		return plan, errPostingProposalConflict
	}
	base := authz.DepartmentReference{TenantID: tenant, HospitalID: hospital, DepartmentID: row.DepartmentID, DepartmentRevision: row.DepartmentRevisionID}
	if intent.Kind == "hospital_management" {
		base.AncestorID, base.AncestorRevision = row.DepartmentID, row.DepartmentRevisionID
		plan.References = []authz.DepartmentReference{base}
		return plan, nil
	}
	if read == nil {
		return plan, authz.ErrScopedAuthorizationUnavailable
	}
	if intent.Kind != "department_supervisor" && intent.Kind != "department_member" {
		return plan, errPostingProposalConflict
	}
	plan.References = []authz.DepartmentReference{base}
	seenStaff, seenPostings := map[uuid.UUID]bool{row.StaffID: true}, map[uuid.UUID]bool{row.ID: true}
	current := row
	for len(plan.Sources) < postingChainLimit-1 {
		if !validPostingSupervisionIntent(&intent) || intent.SupervisorStaffID == nil || intent.SupervisorProposalID == nil || intent.SupervisorRevisionID == nil {
			return plan, errPostingProposalConflict
		}
		staff, proposal := *intent.SupervisorStaffID, *intent.SupervisorProposalID
		if seenStaff[staff] || seenPostings[proposal] {
			return plan, errPostingProposalConflict
		}
		seenStaff[staff], seenPostings[proposal] = true, true
		ancestor, err := read(postingDraftScope{hospital: hospital, staff: staff, draft: proposal}, *intent.SupervisorRevisionID)
		if err != nil {
			return plan, err
		}
		parent := ancestor.Posting
		if parent.HospitalID != hospital || parent.StaffID != staff || parent.ID != proposal || parent.RevisionID != *intent.SupervisorRevisionID || parent.Status != "approved" ||
			ancestor.Intent.Kind != "hospital_management" && ancestor.Intent.Kind != "department_supervisor" ||
			current.StartsAt.Before(parent.StartsAt) || parent.EndsAt != nil && (current.EndsAt == nil || current.EndsAt.After(*parent.EndsAt)) {
			return plan, errPostingProposalConflict
		}
		if parent.EligibilityUntil.Before(plan.EligibilityUntil) {
			plan.EligibilityUntil = parent.EligibilityUntil
		}
		ref := authz.DepartmentReference{TenantID: tenant, HospitalID: hospital, DepartmentID: parent.DepartmentID, DepartmentRevision: parent.DepartmentRevisionID}
		if ancestor.Intent.Kind == "department_supervisor" {
			ref = authz.DepartmentReference{TenantID: tenant, HospitalID: hospital, DepartmentID: current.DepartmentID, DepartmentRevision: current.DepartmentRevisionID, AncestorID: parent.DepartmentID, AncestorRevision: parent.DepartmentRevisionID}
		}
		plan.References = append(plan.References, ref)
		plan.Sources = append(plan.Sources, ancestor)
		if ancestor.Intent.Kind == "hospital_management" {
			if !row.StartsAt.Before(plan.EligibilityUntil) {
				return plan, errPostingProposalConflict
			}
			return plan, nil
		}
		current, intent = parent, ancestor.Intent
	}
	return plan, errPostingProposalConflict
}
