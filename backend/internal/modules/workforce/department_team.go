package workforce

import (
	"context"
	"errors"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

func (r StaffDepartmentTeamItem) PageKey() (time.Time, uuid.UUID) { return r.CreatedAt, r.PostingID }

// listDepartmentTeam pages approved placements and observes current usability without disclosing private HR reasons.
func (h *handlers) listDepartmentTeam(c *gin.Context, page pagination.Params) ([]StaffDepartmentTeamItem, error) {
	c.Header("Cache-Control", "no-store")
	p, ok := postingAccount(c)
	if !ok {
		return nil, nil
	}

	hospital, err := uuid.Parse(c.Param("hospital"))
	if err != nil || hospital == uuid.Nil {
		respond.NotFound(c, "Staff posting hospital")
		return nil, nil
	}
	if err = h.teamHospital(c.Request.Context(), p, hospital); err != nil {
		respondPostingProposalError(c, err)
		return nil, nil
	}
	if _, available := h.scopeAuthority.(authz.CompoundScopedCommandGuard); !available || h.staffActors == nil || h.db == nil || h.departmentReferences == nil {
		respondPostingProposalError(c, authz.ErrScopedAuthorizationUnavailable)
		return nil, nil
	}
	for key, values := range c.Request.URL.Query() {
		if len(values) != 1 || !contains(key, "limit", "cursor", "department") {
			respond.Error(c, 400, "invalid_request", "Choose one department and a bounded team page.")
			return nil, nil
		}
	}
	department, ok := postingDepartmentHint(c)
	if !ok {
		return nil, nil
	}
	actor, err := h.postingActorForHospital(c, p, hospital)
	if err != nil {
		respondPostingProposalError(c, err)
		return nil, nil
	}
	markScopedPosting(c)
	scope := postingDraftScope{hospital: hospital, staff: actor, department: department}
	rows := []StaffDepartmentTeamItem{}
	err = h.scopedPostingTransaction(c, p, scope, false, PermPostingProposalRead, func(ctx context.Context, tx *gorm.DB, _ EmploymentPeriodSource) error {
		if err := tx.Exec(`SELECT set_config('app.workforce_posting_inbox','read',true),set_config('app.workforce_posting_directory','read',true),set_config('app.workforce_posting_review_mode','read',true)`).Error; err != nil {
			return err
		}
		subtree, err := h.postingInboxSubtree(ctx, tx, p.TenantID, scope)
		if err != nil {
			return err
		}
		q := tx.Table("workforce_posting_proposals p").Joins("JOIN workforce_posting_proposal_revisions r ON (r.tenant_id,r.hospital_id,r.staff_id,r.proposal_id,r.id,r.version)=(p.tenant_id,p.hospital_id,p.staff_id,p.id,p.revision_id,p.version)").Joins("JOIN workforce_staff staff ON (staff.tenant_id,staff.hospital_id,staff.id)=(p.tenant_id,p.hospital_id,p.staff_id)").Select(postingProposalProjection+",staff.employee_code,staff.display_name").Where("p.tenant_id=? AND p.hospital_id=? AND p.status='approved'", p.TenantID, hospital)
		if subtree != nil {
			q = q.Where("r.department_id IN ?", subtree)
		}
		var records []postingInboxRecord
		if err := tenantdb.ApplyKeyset(tx.Table("(?) AS team_rows", q), page).Find(&records).Error; err != nil {
			return err
		}
		path, err := h.postingDepartmentPath(ctx, p, scope)
		if err != nil {
			return err
		}
		guard := postingSourceGuard{db: h.db, departments: h.departmentReferences, actors: h.staffActors}
		for index, record := range records {
			r := record.StaffPostingProposal
			item := StaffDepartmentTeamItem{PostingID: r.ID, RevisionID: r.RevisionID, HospitalID: r.HospitalID, Staff: StaffPostingTarget{ID: r.StaffID, EmployeeCode: record.EmployeeCode, DisplayName: record.DisplayName}, Version: r.Version, DepartmentID: r.DepartmentID, DepartmentRevisionID: r.DepartmentRevisionID, StartsAt: r.StartsAt.UTC(), EndsAt: r.EndsAt, CreatedAt: r.CreatedAt.UTC(), State: "not_current"}
			// The extra keyset probe is trimmed by the route and does not consume source authority.
			if index == page.Limit {
				rows = append(rows, item)
				break
			}
			intent, err := readDepartmentTeamIntent(tx, p, postingDraftScope{hospital: hospital, staff: r.StaffID, draft: r.ID})
			if err != nil {
				return err
			}
			if intent == nil || !validPostingSupervisionIntent(intent) {
				return authz.ErrScopedAuthorizationUnavailable
			}
			item.Responsibility, item.SupervisorStaffID, item.SupervisorPostingID = intent.Kind, intent.SupervisorStaffID, intent.SupervisorProposalID
			if err := tx.Raw("SELECT clock_timestamp()").Row().Scan(&item.CheckedAt); err != nil {
				return err
			}
			item.CheckedAt = item.CheckedAt.UTC()
			err = guard.WithCurrentStaffPosting(ctx, authz.StaffPostingSourceScope{TenantID: p.TenantID, Subject: p.Subject, HospitalID: hospital, StaffID: r.StaffID, PostingID: r.ID, RevisionID: r.RevisionID, DepartmentID: r.DepartmentID, DepartmentRevisionID: r.DepartmentRevisionID, Version: r.Version, DepartmentPath: path}, func(_ context.Context, ref authz.StaffPostingSourceReference) error {
				item.State, item.CheckedAt, item.SourceUntil = "current", ref.CheckedAt, &ref.SourceUntil
				return nil
			})
			if err != nil && !errors.Is(err, authz.ErrScopedCommandDenied) {
				return err
			}
			rows = append(rows, item)
		}
		return postingProposalAuditRecord(tx, p, hospital, hospital, "team_read", 0, map[string]any{"rows": min(len(rows), page.Limit)})
	}, authz.StaffPostingSourceUse)
	if err != nil {
		respondPostingProposalError(c, err)
		return nil, nil
	}
	return rows, nil
}

func readDepartmentTeamIntent(tx *gorm.DB, p authn.Principal, scope postingDraftScope) (intent *StaffPostingSupervisionIntent, err error) {
	restore, err := savePostingSourceFrame(tx, []string{"app.workforce_subject", "app.workforce_hospital", "app.workforce_posting_staff", "app.workforce_posting"})
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, restore()) }()
	if _, err = postingOwnerContext(tx, p, scope, false, true); err != nil {
		return nil, err
	}
	return readPostingSupervisionIntent(tx, p.TenantID, scope)
}

func (h *handlers) teamHospital(ctx context.Context, p authn.Principal, hospital uuid.UUID) error {
	if h.db == nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	return h.db.WithTenant(ctx, p.TenantID, func(tx *gorm.DB) error {
		var row struct{ ID uuid.UUID }
		return tx.Table("workforce_hospitals").Select("id").Where("tenant_id=? AND id=? AND establishment_kind='hospital'", p.TenantID, hospital).Take(&row).Error
	})
}
