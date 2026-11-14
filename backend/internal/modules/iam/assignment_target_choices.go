package iam

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

//go:embed assignment_target_choices.sql
var assignmentTargetChoicesSchema string

const assignmentTargetReadKey = "iam.assignment_target_read"

type assignmentTargetChoiceRow struct {
	Choice          AssignmentTargetChoice
	Reference       *authz.AssignmentTargetReference
	CreatedAt       time.Time
	MembershipEpoch int64
}

func (row assignmentTargetChoiceRow) MarshalJSON() ([]byte, error) { return json.Marshal(row.Choice) }
func (row assignmentTargetChoiceRow) PageKey() (time.Time, uuid.UUID) {
	return row.CreatedAt, row.Choice.StaffID
}

type assignmentTargetReadState struct {
	Principal authn.Principal
	Hospital  uuid.UUID
	Deadline  time.Time
	Source    assignmentGovernanceProof
	Cancel    context.CancelFunc
}

func earliestGovernanceDeadline(first time.Time, candidates ...time.Time) time.Time {
	for _, candidate := range candidates {
		if candidate.Before(first) {
			first = candidate
		}
	}
	return first
}

func registerAssignmentTargetChoices(r *platform.Router, h *assignmentProposalHandlers) {
	platform.SealedFilteredQueryListRoute(r, "/hospitals/:hospital/assignment-target-choices", PermGovernanceAssignmentManage, "hospital", []platform.QueryFilter{{Key: "status", Default: "eligible"}}, 20, h.assignmentTargetCandidates, h.assignmentTargetPresentation)
}

func assignmentTargetQuery(c *gin.Context) bool {
	query, err := url.ParseQuery(c.Request.URL.RawQuery)
	if err != nil || c.Request.URL.ForceQuery {
		return false
	}
	for key, values := range query {
		if key != "limit" && key != "cursor" && key != "status" || len(values) != 1 || values[0] == "" || key == "status" && values[0] != "eligible" {
			return false
		}
	}
	return true
}

func assignmentTargetPrincipal(c *gin.Context) (authn.Principal, uuid.UUID, bool) {
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return p, uuid.Nil, false
	}
	for header, expected := range map[string]string{"X-Helivanta-Expected-Tenant": p.TenantID, "X-Helivanta-Expected-Subject": p.Subject} {
		values := c.Request.Header.Values(header)
		if len(values) != 1 || values[0] != expected {
			respond.Error(c, 409, "assignment_session_changed", "Reload the current workspace before selecting Staff.")
			return p, uuid.Nil, false
		}
	}
	hospital, err := uuid.Parse(c.Param("hospital"))
	if err != nil || hospital == uuid.Nil || hospital.String() != c.Param("hospital") {
		respond.NotFound(c, "hospital")
		return p, uuid.Nil, false
	}
	if !authn.Refresh(c) {
		return p, uuid.Nil, false
	}
	current, ok := authn.PrincipalFrom(c)
	if !ok || current.TenantID != p.TenantID || current.Subject != p.Subject {
		respond.Error(c, 409, "assignment_session_changed", "Reload the current workspace before selecting Staff.")
		return p, uuid.Nil, false
	}
	permissions, ok := authz.RefreshPermissions(c)
	if !ok {
		return p, uuid.Nil, false
	}
	if !permissions.Has(PermGovernanceAssignmentManage) {
		respond.Forbidden(c, "Current hospital assignment management is required.")
		return p, uuid.Nil, false
	}
	c.Header("Cache-Control", "no-store")
	return current, hospital, true
}

func (h *assignmentProposalHandlers) withAssignmentTargetSource(ctx context.Context, p authn.Principal, hospital uuid.UUID, action func(context.Context, authz.GovernanceSourceEvidence, assignmentGovernanceProof) error) error {
	if !h.native || h.db == nil || h.sources == nil || h.targets == nil || h.identities == nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	request := authz.GovernanceSourceRequest{Principal: p, Capability: authz.GovernanceAssignmentManage, Scope: authz.GovernanceEstablishmentScope{TenantID: p.TenantID, Kind: "hospital", ID: &hospital}}
	calls := 0
	var callbackErr error
	err := h.sources.WithGovernanceSource(ctx, request, func(commandCtx context.Context, source authz.GovernanceSourceEvidence) error {
		calls++
		if calls != 1 || !h.db.GuardedCommandActive(commandCtx) || tenantdb.ActorFrom(commandCtx) != p.Subject {
			callbackErr = authz.ErrScopedAuthorizationUnavailable
			return callbackErr
		}
		proof, err := assignmentProof(p, hospital, authz.GovernanceAssignmentManage, source)
		if err != nil {
			callbackErr = err
			return err
		}
		callbackErr = action(commandCtx, source, proof)
		return callbackErr
	})
	if callbackErr != nil {
		return callbackErr
	}
	if err == nil && calls != 1 {
		return authz.ErrScopedAuthorizationUnavailable
	}
	return err
}

func (h *assignmentProposalHandlers) assignmentTargetSelection(ctx context.Context, request authz.GovernedAssignmentSelectionRequest, subjects []string, action func(*gorm.DB) error) error {
	deadline, bounded := ctx.Deadline()
	now := time.Now()
	if len(subjects) > 21 || !bounded || !deadline.After(now) || deadline.After(now.Add(5*time.Second)) || !h.db.GuardedCommandActive(ctx) || tenantdb.ActorFrom(ctx) != request.Principal.Subject {
		return authz.ErrScopedAuthorizationUnavailable
	}
	proof, err := authz.AssignmentTargetChoiceProof(request, now)
	if err != nil {
		return err
	}
	deadline = earliestGovernanceDeadline(deadline, request.Source.ExpiresAt, request.Principal.IdleDeadline)
	proof["read_expires_at"] = deadline
	raw, err := json.Marshal(proof)
	if err != nil {
		return err
	}
	selected, err := json.Marshal(subjects)
	if err != nil {
		return err
	}
	if err := h.db.ConstrainGuardedCommand(ctx, request.Principal.TenantID, deadline); err != nil {
		return err
	}
	return h.db.WithTenantCommand(ctx, request.Principal.TenantID, func(tx *gorm.DB) (err error) {
		var supported bool
		if err = tx.Raw(`SELECT CASE WHEN to_regprocedure('public.iam_staff_selection_support()')IS NULL THEN false ELSE public.iam_staff_selection_support()END`).Scan(&supported).Error; err != nil {
			return err
		}
		if !supported {
			return authz.ErrScopedAuthorizationUnavailable
		}
		var previous string
		if err = tx.Raw(`SELECT jsonb_object_agg(key,COALESCE(current_setting(key,true),''))::text FROM unnest(ARRAY['app.iam_staff_selection','app.iam_staff_selection_subjects'])AS key`).Scan(&previous).Error; err != nil {
			return err
		}
		defer func() {
			err = errors.Join(err, tx.Exec(`SELECT set_config(key,value,true)FROM jsonb_each_text(?::jsonb)`, previous).Error)
		}()
		if err = tx.Exec(`SELECT set_config('app.iam_staff_selection',?,true),set_config('app.iam_staff_selection_subjects',?,true)`, string(raw), string(selected)).Error; err != nil {
			return err
		}
		if err = action(tx); err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,'iam.assignment.target_choices.read','workforce_hospitals',?)`, request.Principal.TenantID, request.Principal.Subject, request.HospitalID).Error
	})
}

func (h *assignmentProposalHandlers) assignmentTargetCandidates(c *gin.Context, page pagination.Params) ([]assignmentTargetChoiceRow, error) {
	requestCtx, requestCancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	c.Request = c.Request.WithContext(requestCtx)
	retained := false
	defer func() {
		if !retained {
			requestCancel()
		}
	}()
	p, hospital, ok := assignmentTargetPrincipal(c)
	if !ok {
		return nil, nil
	}
	if !assignmentTargetQuery(c) || page.Limit < 1 || page.Limit > 20 {
		respond.Error(c, 400, "invalid_request", "Use one bounded Staff choice page without identity filters.")
		return nil, nil
	}
	deadline, _ := requestCtx.Deadline()
	state := assignmentTargetReadState{Principal: p, Hospital: hospital, Deadline: earliestGovernanceDeadline(deadline, p.IdleDeadline), Cancel: requestCancel}
	ctx, cancel := context.WithDeadline(c.Request.Context(), state.Deadline)
	defer cancel()
	rows := []assignmentTargetChoiceRow{}
	err := h.withAssignmentTargetSource(ctx, p, hospital, func(commandCtx context.Context, source authz.GovernanceSourceEvidence, proof assignmentGovernanceProof) error {
		state.Source = proof
		request := authz.GovernedAssignmentSelectionRequest{Principal: p, HospitalID: hospital, Capability: authz.GovernanceAssignmentManage, Source: source}
		candidates, err := h.targets.ReadAssignmentTargetCandidates(commandCtx, request, page)
		if err != nil {
			return err
		}
		if len(candidates) > page.Limit+1 {
			return authz.ErrScopedAuthorizationUnavailable
		}
		subjects := []string{}
		for _, candidate := range candidates {
			if candidate.StaffID == uuid.Nil || candidate.CreatedAt.IsZero() {
				return authz.ErrScopedAuthorizationUnavailable
			}
			rows = append(rows, assignmentTargetChoiceRow{Choice: AssignmentTargetChoice{HospitalID: hospital, StaffID: candidate.StaffID}, Reference: candidate.Reference, CreatedAt: candidate.CreatedAt})
			if candidate.Reference != nil {
				if candidate.Reference.StaffID != candidate.StaffID || !proposalSubject(candidate.Reference.Subject) || slices.Contains(subjects, candidate.Reference.Subject) {
					return authz.ErrScopedAuthorizationUnavailable
				}
				subjects = append(subjects, candidate.Reference.Subject)
			}
		}
		return h.assignmentTargetSelection(commandCtx, request, subjects, func(tx *gorm.DB) error {
			for index := range rows {
				row := &rows[index]
				if row.Reference == nil {
					continue
				}
				epoch, member, err := assignmentTargetMembership(tx, p.TenantID, hospital, *row.Reference, false)
				if err != nil {
					return err
				}
				if member {
					row.MembershipEpoch = epoch
					row.Choice.Subject = row.Reference.Subject
				} else {
					row.Reference = nil
				}
			}
			return nil
		})
	})
	if err != nil {
		assignmentTargetError(c, err)
		return nil, nil
	}
	c.Set(assignmentTargetReadKey, state)
	retained = true
	return rows, nil
}

func assignmentTargetMembership(tx *gorm.DB, tenant string, hospital uuid.UUID, ref authz.AssignmentTargetReference, lock bool) (int64, bool, error) {
	locked := func(query *gorm.DB) *gorm.DB {
		if lock {
			return query.Clauses(clause.Locking{Strength: "SHARE"})
		}
		return query
	}
	var epoch struct{ Epoch int64 }
	err := locked(tx.Table("iam_subject_membership_epochs").Select("epoch").Where("tenant_id=? AND subject=? AND member_count>0", tenant, ref.Subject)).Take(&epoch).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	var members []struct{ RoleKey authz.Role }
	if err := locked(tx.Table("iam_members").Select("role_key").Where("tenant_id=? AND subject=?", tenant, ref.Subject).Order("role_key").Limit(3)).Find(&members).Error; err != nil {
		return 0, false, err
	}
	roles := make([]authz.Role, len(members))
	for index, member := range members {
		roles[index] = member.RoleKey
	}
	if !employmentMembershipRoles(roles) || epoch.Epoch < 1 || epoch.Epoch > 9007199254740991 {
		return 0, false, nil
	}
	var basis struct {
		StaffID, GenerationID uuid.UUID
		EmploymentEpoch       int64
		ValidUntil            time.Time
	}
	err = locked(tx.Table("iam_employment_bases").Select("staff_id,generation_id,employment_epoch,valid_until").Where("tenant_id=? AND hospital_id=? AND subject=?", tenant, hospital, ref.Subject)).Take(&basis).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return epoch.Epoch, basis.StaffID == ref.StaffID && basis.GenerationID == ref.GenerationID && basis.EmploymentEpoch == ref.EmploymentEpoch && basis.ValidUntil.Equal(ref.EligibleUntil) && basis.ValidUntil.After(time.Now()), nil
}

func (h *assignmentProposalHandlers) assignmentTargetPresentation(c *gin.Context, rows []assignmentTargetChoiceRow) ([]assignmentTargetChoiceRow, error) {
	value, present := c.Get(assignmentTargetReadKey)
	state, valid := value.(assignmentTargetReadState)
	if !present || !valid {
		assignmentTargetError(c, authz.ErrScopedAuthorizationUnavailable)
		return nil, nil
	}
	defer state.Cancel()
	ctx, cancel := context.WithDeadline(c.Request.Context(), state.Deadline)
	defer cancel()
	group, lookupCtx := errgroup.WithContext(ctx)
	group.SetLimit(4)
	for index := range rows {
		if rows[index].Reference == nil {
			continue
		}
		group.Go(func() error {
			row := &rows[index]
			identity, err := h.identities.AssignmentCandidateIdentity(lookupCtx, state.Principal.TenantID, row.Reference.Subject)
			if err != nil || identity != nil && identity.Validate() != nil {
				return authz.ErrScopedAuthorizationUnavailable
			}
			if identity != nil {
				row.Choice.Name, row.Choice.Email = identity.DisplayName, identity.Email
			}
			return nil
		})
	}
	if err := group.Wait(); err != nil {
		assignmentTargetError(c, err)
		return nil, nil
	}
	p, hospital, ok := assignmentTargetPrincipal(c)
	if !ok {
		return nil, nil
	}
	if hospital != state.Hospital || p.TenantID != state.Principal.TenantID || p.Subject != state.Principal.Subject || !p.AuthTime.Equal(state.Principal.AuthTime) {
		assignmentTargetError(c, authz.ErrAssignmentTargetChanged)
		return nil, nil
	}
	refs, subjects := []authz.AssignmentTargetReference{}, []string{}
	for _, row := range rows {
		if row.Reference != nil {
			refs = append(refs, *row.Reference)
			subjects = append(subjects, row.Reference.Subject)
		}
	}
	slices.Sort(subjects)
	err := h.withAssignmentTargetSource(ctx, p, hospital, func(commandCtx context.Context, source authz.GovernanceSourceEvidence, proof assignmentGovernanceProof) error {
		if !sameAssignmentSource(state.Source, proof) {
			return authz.ErrAssignmentTargetChanged
		}
		request := authz.GovernedAssignmentSelectionRequest{Principal: p, HospitalID: hospital, Capability: authz.GovernanceAssignmentManage, Source: source}
		return h.assignmentTargetSelection(commandCtx, request, subjects, func(tx *gorm.DB) error {
			for _, subject := range subjects {
				for _, row := range rows {
					if row.Reference == nil || row.Reference.Subject != subject {
						continue
					}
					epoch, member, err := assignmentTargetMembership(tx, p.TenantID, hospital, *row.Reference, true)
					if err != nil {
						return err
					}
					if !member || epoch != row.MembershipEpoch {
						return authz.ErrAssignmentTargetChanged
					}
				}
			}
			return h.targets.RecheckAssignmentTargets(commandCtx, request, refs)
		})
	})
	if err != nil || ctx.Err() != nil {
		assignmentTargetError(c, errors.Join(err, ctx.Err()))
		return nil, nil
	}
	allowed := make([]assignmentTargetChoiceRow, 0, len(rows))
	for _, row := range rows {
		if row.Reference != nil && row.Choice.Name != "" && row.Choice.Email != "" {
			allowed = append(allowed, row)
		}
	}
	return allowed, nil
}

func assignmentTargetError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, authz.ErrScopedCredentialRevoked):
		respond.Error(c, 401, "credentials_revoked", "Sign in again before selecting Staff.")
	case errors.Is(err, authz.ErrGovernanceScopeHidden), errors.Is(err, authz.ErrScopedCommandDenied), errors.Is(err, gorm.ErrRecordNotFound):
		respond.NotFound(c, "hospital Staff choices")
	case errors.Is(err, authz.ErrAssignmentTargetChanged):
		respond.Error(c, 409, "assignment_target_changed", "Staff or account facts changed. Refresh the choices before continuing.")
	default:
		respond.Error(c, 503, "assignment_targets_unavailable", "Current Staff, account identity or read audit could not be confirmed.")
	}
}
