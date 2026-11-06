package workforce

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/requestid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

const (
	departmentLeaveQueueLimit = 10
	// departmentLeavePostingLimit bounds one requester's live postings; more fails closed rather than truncating.
	departmentLeavePostingLimit = 8
)

// departmentLeaveSession is one ADR-0055 department leave command: the IAM grant, its held department and the locked subtree.
type departmentLeaveSession struct {
	actor   authz.StaffActorReference
	grant   authz.ScopeGrant
	held    uuid.UUID
	subtree []uuid.UUID
}

// leaveDepartmentHint requires exactly one canonical department; a non-v7 id is a hidden reference, not a malformed one.
func leaveDepartmentHint(c *gin.Context) (uuid.UUID, bool) {
	values := c.Request.URL.Query()["department"]
	var id uuid.UUID
	var err error
	if len(values) == 1 {
		id, err = uuid.Parse(values[0])
	}
	if len(values) != 1 || err != nil || id == uuid.Nil || id.String() != values[0] {
		respond.Error(c, 400, "invalid_request", "Choose one of your leave review departments.")
		return uuid.Nil, false
	}
	return id, true
}

// departmentLeaveTransaction runs action inside the guarded department command; only its success admits the response.
func (h *handlers) departmentLeaveTransaction(c *gin.Context, p authn.Principal, hospital, hint uuid.UUID, action func(context.Context, *gorm.DB, departmentLeaveSession) error) error {
	if !authn.Refresh(c) {
		return authz.ErrScopedAuthorizationUnavailable
	}
	current, ok := authn.PrincipalFrom(c)
	if !ok || current.TenantID != p.TenantID || current.Subject != p.Subject {
		return authz.ErrScopedCommandDenied
	}
	p = current
	guard, supported := h.scopeAuthority.(authz.CompoundScopedCommandGuard)
	if !supported || h.staffActors == nil || h.departmentScopes == nil || p.IdleDeadline.IsZero() {
		return authz.ErrScopedAuthorizationUnavailable
	}
	if !postingV7(hint) {
		return authz.ErrDepartmentReferenceHidden
	}
	ctx, cancel := context.WithDeadline(c.Request.Context(), p.IdleDeadline)
	defer cancel()
	ids, err := h.departmentScopes.CurrentPath(ctx, p.TenantID, hospital, hint)
	if errors.Is(err, authz.ErrDepartmentReferenceChanged) {
		return authz.ErrDepartmentReferenceHidden
	}
	if err != nil {
		return err
	}
	if len(ids) == 0 || len(ids) > authz.MaxDepartmentPath || ids[0] != hint {
		return authz.ErrScopedAuthorizationUnavailable
	}
	path := make([]string, len(ids))
	for i, id := range ids {
		path[i] = id.String()
	}
	checks := make([]authz.ScopeCheck, 0, 2)
	var departments []uuid.UUID
	for index, required := range []authz.Permission{PermLeaveApprove, authz.StaffActorUse} {
		request := authz.ScopeRequest{TenantID: p.TenantID, Subject: p.Subject, HospitalID: hospital.String(), Permission: required, DepartmentPath: path}
		grant, err := h.scopeAuthority.AuthorizeScope(ctx, request)
		if err != nil {
			return authz.ErrScopedAuthorizationUnavailable
		}
		if grant.AssignmentID == "" {
			if index == 0 {
				return h.deniedLeaveDepartment(ctx, p, hospital)
			}
			return authz.ErrScopedCommandDenied
		}
		if admitted, exists := authz.HospitalScopeGrant(c, required, hospital.String()); exists && admitted != grant {
			return authz.ErrScopedCommandDenied
		}
		// Hospital-wide reviewers decide through the hospital queue; this route serves department grants only.
		if index == 0 && grant.ScopeKind != "department" {
			return authz.ErrScopedCommandDenied
		}
		if grant.ScopeKind == "department" {
			department, err := uuid.Parse(grant.DepartmentID)
			if err != nil || !slices.Contains(path, grant.DepartmentID) {
				return authz.ErrScopedAuthorizationUnavailable
			}
			if !slices.Contains(departments, department) {
				departments = append(departments, department)
			}
		}
		checks = append(checks, authz.ScopeCheck{Request: request, Grant: grant})
	}
	return guard.WithScopeGrants(ctx, checks, p.AuthTime, func(commandCtx context.Context) error {
		// The supervision gate precedes department locks, matching posting writes so no lock order inverts.
		if err := h.db.WithTenantCommand(commandCtx, p.TenantID, func(tx *gorm.DB) error {
			return tx.Exec(`SELECT workforce_lock_supervision(?::uuid,?::uuid)`, p.TenantID, hospital).Error
		}); err != nil {
			return err
		}
		scope := postingDraftScope{hospital: hospital, department: hint}
		return h.withinPostingDepartments(commandCtx, p.TenantID, scope, departments, func(heldCtx context.Context) error {
			subtree, err := h.departmentScopes.CurrentSubtree(heldCtx, p.TenantID, hospital, []uuid.UUID{hint})
			if err != nil {
				return err
			}
			if len(subtree) == 0 || !slices.Contains(subtree, hint) {
				return authz.ErrDepartmentReferenceChanged
			}
			nodes := make([]string, len(subtree))
			for i, id := range subtree {
				nodes[i] = id.String()
			}
			return h.staffActors.WithCurrentStaffActor(heldCtx, authz.StaffActorScope{TenantID: p.TenantID, Subject: p.Subject, HospitalID: hospital, DepartmentPath: path}, func(actorCtx context.Context, actor authz.StaffActorReference) error {
				return h.transaction(actorCtx, p, hospital, func(tx *gorm.DB) error {
					if err := tx.Exec(`SELECT set_config('app.workforce_leave_department','authorized',true),set_config('app.workforce_leave_department_subtree',?,true)`, strings.Join(nodes, ",")).Error; err != nil {
						return err
					}
					if err := action(actorCtx, tx, departmentLeaveSession{actor: actor, grant: checks[0].Grant, held: departments[0], subtree: subtree}); err != nil {
						return err
					}
					authz.AdmitHandlerScope(actorCtx, c, checks[0].Request)
					return nil
				})
			})
		})
	})
}

// deniedLeaveDepartment hides departments outside every held subtree (404) and refuses callers holding none (403).
func (h *handlers) deniedLeaveDepartment(ctx context.Context, p authn.Principal, hospital uuid.UUID) error {
	lister, ok := h.scopeAuthority.(authz.DepartmentScopeLister)
	if !ok {
		return authz.ErrScopedAuthorizationUnavailable
	}
	grants, err := lister.DepartmentScopes(ctx, authz.ScopeRequest{TenantID: p.TenantID, Subject: p.Subject, HospitalID: hospital.String(), Permission: PermLeaveApprove})
	if err != nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	if len(grants) > 0 {
		return authz.ErrDepartmentReferenceHidden
	}
	return authz.ErrScopedCommandDenied
}

// reviewer locks the leave actor row and requires it to be the Staff actor the department command proved.
func (s departmentLeaveSession) reviewer(tx *gorm.DB, p authn.Principal, hospital uuid.UUID) (reviewedLeaveActor, error) {
	actor, err := lockReviewedLeaveActor(tx, p, hospital)
	if err != nil {
		return actor, err
	}
	if actor.StaffID != s.actor.StaffID || actor.ID != s.actor.LinkID || actor.GenerationID != s.actor.GenerationID {
		return actor, authz.ErrScopedCommandDenied
	}
	return actor, nil
}

func (s departmentLeaveSession) evidence(departments []uuid.UUID) map[string]any {
	evidence := map[string]any{"scope_kind": "department", "department_id": s.held}
	if departments != nil {
		evidence["requester_departments"] = departments
	}
	return evidence
}

var departmentLeavePostingFrame = []string{"app.workforce_subject", "app.workforce_hospital", "app.workforce_posting_staff", "app.workforce_posting", "app.workforce_posting_review_mode", "app.workforce_posting_permission", "app.workforce_posting_scoped", "app.workforce_posting_actor", "app.workforce_posting_link", "app.workforce_posting_generation", "app.workforce_posting_assignment", "app.workforce_posting_assignment_version", "app.workforce_posting_scope_epoch", "app.workforce_posting_membership_epoch", "app.workforce_posting_scope_kind"}

// departmentLeaveRequester proves every live approved posting of staff lies in the session subtree and returns those departments.
func departmentLeaveRequester(tx *gorm.DB, p authn.Principal, hospital, staff uuid.UUID, s departmentLeaveSession) (departments []uuid.UUID, until time.Time, err error) {
	restore, err := savePostingSourceFrame(tx, departmentLeavePostingFrame)
	if err != nil {
		return nil, until, err
	}
	defer func() { err = errors.Join(err, restore()) }()
	if err = tx.Exec(`SELECT set_config('app.workforce_posting_review_mode','leave_department',true),set_config('app.workforce_posting_permission',?,true),set_config('app.workforce_posting_scoped','read',true),set_config('app.workforce_posting_actor',?,true),set_config('app.workforce_posting_link',?,true),set_config('app.workforce_posting_generation',?,true),set_config('app.workforce_posting_assignment',?,true),set_config('app.workforce_posting_assignment_version',?,true),set_config('app.workforce_posting_scope_epoch',?,true),set_config('app.workforce_posting_membership_epoch',?,true),set_config('app.workforce_posting_scope_kind','',true)`, string(PermLeaveApprove), s.actor.StaffID.String(), s.actor.LinkID.String(), s.actor.GenerationID.String(), s.grant.AssignmentID, strconv.FormatInt(s.grant.Version, 10), strconv.FormatInt(s.grant.ScopeEpoch, 10), strconv.FormatInt(s.grant.MembershipEpoch, 10)).Error; err != nil {
		return nil, until, err
	}
	lifecycle, err := newPostingLifecycleReview(tx)
	if err != nil {
		return nil, until, err
	}
	scope := postingDraftScope{hospital: hospital, staff: staff}
	source, err := postingOwnerContext(tx, p, scope, false, true)
	if err != nil {
		return nil, until, err
	}
	approved := func() *gorm.DB {
		return tx.Table("workforce_posting_proposals p").
			Joins("JOIN workforce_posting_proposal_revisions r ON (r.tenant_id,r.id)=(p.tenant_id,p.revision_id)").
			Where("p.tenant_id=? AND p.hospital_id=? AND p.staff_id=? AND p.status='approved'", p.TenantID, hospital, staff)
	}
	var ids []uuid.UUID
	if err = approved().Where("r.starts_at<=? AND (r.ends_at IS NULL OR r.ends_at>?) AND r.eligibility_until>?", lifecycle.CheckedAt, lifecycle.CheckedAt, lifecycle.CheckedAt).
		Order("p.id").Limit(departmentLeavePostingLimit+1).Pluck("p.id", &ids).Error; err != nil {
		return nil, until, err
	}
	if len(ids) > departmentLeavePostingLimit {
		return nil, until, authz.ErrScopedAuthorizationUnavailable
	}
	for _, id := range ids {
		posting := scope
		posting.draft = id
		var row StaffPostingProposal
		row, err = postingProposalResult(tx, p, posting, uuid.Nil)
		if err != nil {
			return nil, until, err
		}
		live, e := livePostingUntil(tx, p, posting, row, source, lifecycle)
		if e != nil {
			return nil, until, e
		}
		if !live {
			continue
		}
		if !slices.Contains(s.subtree, row.DepartmentID) {
			// One live posting outside the subtree places the requester outside this department's authority.
			return nil, until, gorm.ErrRecordNotFound
		}
		if !slices.Contains(departments, row.DepartmentID) {
			departments = append(departments, row.DepartmentID)
		}
	}
	if len(departments) == 0 {
		return nil, until, gorm.ErrRecordNotFound
	}
	// A posting that starts later can move the requester, so the authority also ends at the next start.
	var next struct{ Next *time.Time }
	if err = approved().Where("r.starts_at>?", lifecycle.CheckedAt).Select("min(r.starts_at) AS next").Scan(&next).Error; err != nil {
		return nil, until, err
	}
	until = lifecycle.EffectiveUntil
	if next.Next != nil && next.Next.Before(until) {
		until = next.Next.UTC()
	}
	slices.SortFunc(departments, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	return departments, until, nil
}

// livePostingUntil reports whether row is a current posting, narrowing the lifecycle deadline when it is.
func livePostingUntil(tx *gorm.DB, p authn.Principal, s postingDraftScope, row StaffPostingProposal, source EmploymentPeriodSource, lifecycle *postingLifecycleReview) (bool, error) {
	if row.Status != "approved" {
		return false, nil
	}
	generation, state, generationUntil, err := readPostingTargetGeneration(tx, p, s, row.GenerationID)
	if err != nil {
		return false, err
	}
	until, err := currentPostingUseDeadline(row, source, generation, state, generationUntil, lifecycle.CheckedAt)
	if errors.Is(err, authz.ErrScopedCommandDenied) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	cutoff, err := readPostingRetainedCutoff(tx, p, s)
	if err != nil {
		return false, err
	}
	if err = lifecycle.observe(row, cutoff); errors.Is(err, errPostingProposalConflict) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if until.Before(lifecycle.EffectiveUntil) {
		lifecycle.EffectiveUntil = until
	}
	return true, nil
}

func departmentLeaveAudit(tx *gorm.DB, p authn.Principal, hospital, resource uuid.UUID, action string, actor reviewedLeaveActor, s departmentLeaveSession, departments []uuid.UUID) error {
	evidence, err := json.Marshal(s.evidence(departments))
	if err != nil {
		return err
	}
	grant := s.grant
	return tx.Exec(`INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(?,?,?,?,?,jsonb_build_object('assignment_id',?::text,'assignment_version',?::bigint,'scope_epoch',?::bigint,'membership_epoch',?::bigint,'actor_link_id',?::text,'actor_link_version',?::bigint)||?::jsonb)`, p.TenantID, hospital, p.Subject, action, resource, grant.AssignmentID, grant.Version, grant.ScopeEpoch, grant.MembershipEpoch, actor.ID.String(), actor.Version, string(evidence)).Error
}

func departmentLeaveReadContext(tx *gorm.DB, kind string, staffID, requestID uuid.UUID) error {
	return tx.Exec(`SELECT set_config('app.workforce_leave_review_read','authorized',true),set_config('app.workforce_leave_review_read_kind',?,true),set_config('app.workforce_leave_review_read_staff',?,true),set_config('app.workforce_leave_review_read_request',?,true)`, kind, staffID.String(), requestID.String()).Error
}

func (h *handlers) listDepartmentLeaveReviewRequests(c *gin.Context, page pagination.Params) ([]platform.Filtered[leaveReviewRequest], error) {
	p, hospital, ok := scope(c)
	if !ok {
		return nil, nil
	}
	hint, ok := leaveDepartmentHint(c)
	if !ok {
		return nil, nil
	}
	rows := []platform.Filtered[leaveReviewRequest]{}
	err := h.departmentLeaveTransaction(c, p, hospital, hint, func(_ context.Context, tx *gorm.DB, s departmentLeaveSession) error {
		actor, err := s.reviewer(tx, p, hospital)
		if err != nil {
			return err
		}
		if err := departmentLeaveReadContext(tx, "queue", uuid.Nil, uuid.Nil); err != nil {
			return err
		}
		var candidates []leaveReviewRequest
		if err := tenantdb.ApplyKeyset(tx, page).Table(leaveRequestView).Select("id,staff_id,employee_code,display_name,code,name,leave_year,unit,start_on,end_on,session,minutes,half_days,status,version,created_at,(SELECT s.level::int FROM workforce_leave_route_steps s WHERE (s.tenant_id,s.hospital_id,s.request_id)=(v.tenant_id,v.hospital_id,v.id) AND s.staff_id=?) AS route_level", actor.StaffID).Where("tenant_id=? AND hospital_id=? AND status='pending' AND staff_id<>? AND requested_by_subject<>?", p.TenantID, hospital, actor.StaffID, p.Subject).Find(&candidates).Error; err != nil {
			return err
		}
		requesters := map[uuid.UUID][]uuid.UUID{}
		for index, row := range candidates {
			departments, seen := requesters[row.StaffID]
			if !seen {
				departments, _, err = departmentLeaveRequester(tx, p, hospital, row.StaffID, s)
				if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
					return err
				}
				requesters[row.StaffID] = departments
			}
			rows = append(rows, platform.Filtered[leaveReviewRequest]{Row: row, Visible: departments != nil})
			if departments != nil && index < page.Limit {
				if err := departmentLeaveAudit(tx, p, hospital, row.ID, "workforce.leave_review.queue_read", actor, s, departments); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		respondDepartmentLeaveError(c, err)
		return nil, nil
	}
	return rows, nil
}

func (h *handlers) getDepartmentLeaveReviewRequest(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	staffID, requestID, ok := leaveRequestIDs(c)
	if !ok {
		return
	}
	hint, ok := leaveDepartmentHint(c)
	if !ok {
		return
	}
	var result leaveRequestDetail
	err := h.departmentLeaveTransaction(c, p, hospital, hint, func(_ context.Context, tx *gorm.DB, s departmentLeaveSession) error {
		actor, err := s.reviewer(tx, p, hospital)
		if err != nil {
			return err
		}
		if err := departmentLeaveReadContext(tx, "detail", staffID, requestID); err != nil {
			return err
		}
		var request struct{ ID uuid.UUID }
		if err := tx.Table("workforce_leave_requests").Select("id").Clauses(clause.Locking{Strength: "SHARE"}).Where("tenant_id=? AND hospital_id=? AND id=? AND staff_id=? AND status='pending' AND staff_id<>? AND requested_by_subject<>?", p.TenantID, hospital, requestID, staffID, actor.StaffID, p.Subject).Take(&request).Error; err != nil {
			return err
		}
		departments, _, err := departmentLeaveRequester(tx, p, hospital, staffID, s)
		if err != nil {
			return err
		}
		if result, err = loadLeaveRequestDetail(tx, p.TenantID, hospital, staffID, requestID); err != nil {
			return err
		}
		return departmentLeaveAudit(tx, p, hospital, requestID, "workforce.leave_review.detail_read", actor, s, departments)
	})
	if err != nil {
		respondDepartmentLeaveError(c, err)
		return
	}
	respond.OK(c, result)
}

// decideDepartmentLeaveRequest is the hospital decision body under a department grant held through commit (ADR-0055).
func (h *handlers) decideDepartmentLeaveRequest(c *gin.Context) {
	p, hospital, ok := scope(c)
	if !ok {
		return
	}
	staffID, id, ok := leaveRequestIDs(c)
	if !ok {
		return
	}
	hint, ok := leaveDepartmentHint(c)
	if !ok {
		return
	}
	var in leaveDecisionInput
	if !decode(c, &in) {
		return
	}
	t := leaveTransition{from: "pending", to: "approved", separate: true, decides: true}
	if in.Decision == "reject" {
		t.to = "rejected"
	}
	note, ok := leaveTransitionNote(c, t, in.Note)
	if !ok {
		return
	}
	var result leaveRequestDetail
	err := h.departmentLeaveTransaction(c, p, hospital, hint, func(ctx context.Context, tx *gorm.DB, s departmentLeaveSession) error {
		routed, err := prepareScopedLeaveReview(tx, p, hospital, staffID, id, s.grant)
		if err != nil {
			return err
		}
		if _, err := s.reviewer(tx, p, hospital); err != nil {
			return err
		}
		departments, until, err := departmentLeaveRequester(tx, p, hospital, staffID, s)
		if err != nil {
			return err
		}
		if err := h.db.ConstrainGuardedCommand(ctx, p.TenantID, until); err != nil {
			return err
		}
		evidence := map[string]any{}
		if err := json.Unmarshal(routed, &evidence); err != nil {
			return err
		}
		for key, value := range s.evidence(departments) {
			evidence[key] = value
		}
		evidence["actor_generation_id"] = s.actor.GenerationID
		encoded, err := json.Marshal(evidence)
		if err != nil {
			return err
		}
		if err := h.applyLeaveTransition(tx, c, p, leaveTransitionCommand{hospital: hospital, staffID: staffID, id: id, transition: t, expected: *in.ExpectedVersion, note: note, evidence: encoded, department: true}); err != nil {
			return err
		}
		result, err = loadLeaveRequestDetail(tx, p.TenantID, hospital, staffID, id)
		return err
	})
	if err != nil {
		respondDepartmentLeaveError(c, err)
		return
	}
	respond.OK(c, result)
}

func respondDepartmentLeaveError(c *gin.Context, err error) {
	if c.IsAborted() {
		return
	}
	switch {
	case errors.Is(err, errDepartmentScopeChanged), errors.Is(err, authz.ErrDepartmentReferenceChanged):
		respond.Error(c, 409, "department_scope_changed", "The department structure changed. Reload the department leave queue.")
	case errors.Is(err, authz.ErrDepartmentSubtreeLimit):
		requestid.Logger(c).WarnContext(c.Request.Context(), "department leave review unavailable", "reason", "department_subtree_limit")
		respondLeaveRequestError(c, authz.ErrScopedAuthorizationUnavailable)
	case errors.Is(err, authz.ErrDepartmentReferenceHidden), errors.Is(err, gorm.ErrRecordNotFound), errors.Is(err, tenantdb.ErrGuardedCommandExpired):
		respond.NotFound(c, "Leave request")
	case errors.Is(err, context.DeadlineExceeded):
		respondLeaveRequestError(c, authz.ErrScopedAuthorizationUnavailable)
	default:
		respondLeaveRequestError(c, err)
	}
}
