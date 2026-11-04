package workforce

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

type postingSourceGuard struct {
	db          *tenantdb.DB
	departments authz.DepartmentReferenceGuard
	actors      authz.CurrentStaffActorGuard
}

func (*Module) PostingSources(db *tenantdb.DB, departments authz.DepartmentReferenceGuard, actors authz.CurrentStaffActorGuard) authz.CurrentStaffPostingSourceGuard {
	return &postingSourceGuard{db: db, departments: departments, actors: actors}
}

type postingUseObservation struct {
	StaffID              uuid.UUID                     `json:"staff_id"`
	PostingID            uuid.UUID                     `json:"posting_id"`
	RevisionID           uuid.UUID                     `json:"revision_id"`
	GenerationID         uuid.UUID                     `json:"generation_id"`
	StaffVersion         int64                         `json:"staff_version"`
	EmploymentEpoch      int64                         `json:"employment_epoch"`
	DepartmentID         uuid.UUID                     `json:"department_id"`
	DepartmentRevisionID uuid.UUID                     `json:"department_revision_id"`
	NominalUntil         time.Time                     `json:"nominal_until"`
	CurrentUntil         time.Time                     `json:"current_until"`
	Cutoff               StaffPostingCutoffObservation `json:"cutoff"`
}

func validPostingSourceScope(scope authz.StaffPostingSourceScope) bool {
	tenant, err := uuid.Parse(scope.TenantID)
	return err == nil && tenant != uuid.Nil && tenant.String() == scope.TenantID && validLoginSubject(scope.Subject) && scope.HospitalID != uuid.Nil && scope.StaffID != uuid.Nil &&
		postingV7(scope.PostingID) && postingV7(scope.RevisionID) && postingV7(scope.DepartmentID) && postingV7(scope.DepartmentRevisionID) && scope.Version >= 2 && scope.Version <= 9007199254740990
}

func (g *postingSourceGuard) WithCurrentStaffPosting(ctx context.Context, scope authz.StaffPostingSourceScope, action func(context.Context, authz.StaffPostingSourceReference) error) error {
	if g.db == nil || g.departments == nil || g.actors == nil || action == nil || !validPostingSourceScope(scope) || !g.db.GuardedCommandActive(ctx) {
		return authz.ErrScopedAuthorizationUnavailable
	}
	request := authz.ScopeRequest{TenantID: scope.TenantID, Subject: scope.Subject, HospitalID: scope.HospitalID.String(), Permission: authz.StaffPostingSourceUse}
	grant, ok := authz.ScopedCommandGrant(ctx, request)
	if !ok || grant.AssignmentID == "" {
		return authz.ErrScopedAuthorizationUnavailable
	}
	invoked := false
	err := g.db.WithTenantCommand(ctx, scope.TenantID, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT workforce_lock_supervision(?::uuid,?::uuid)`, scope.TenantID, scope.HospitalID).Error; err != nil {
			return err
		}
		return g.actors.WithCurrentStaffActor(ctx, authz.StaffActorScope{TenantID: scope.TenantID, Subject: scope.Subject, HospitalID: scope.HospitalID}, func(actorCtx context.Context, actor authz.StaffActorReference) error {
			return g.db.WithTenantCommand(actorCtx, scope.TenantID, func(tx *gorm.DB) (err error) {
				restore, err := savePostingSourceFrame(tx, []string{"app.workforce_subject", "app.workforce_hospital", "app.workforce_posting_staff", "app.workforce_posting", "app.workforce_posting_review_mode", "app.workforce_posting_permission", "app.workforce_posting_scoped", "app.workforce_posting_actor", "app.workforce_posting_link", "app.workforce_posting_generation", "app.workforce_posting_assignment", "app.workforce_posting_assignment_version", "app.workforce_posting_scope_epoch", "app.workforce_posting_membership_epoch"})
				if err != nil {
					return err
				}
				restored := false
				defer func() {
					if !restored {
						err = errors.Join(err, restore())
					}
				}()
				if err = tx.Exec(`SELECT set_config('app.workforce_subject',?,true),set_config('app.workforce_hospital',?,true),set_config('app.workforce_posting_staff',?,true),set_config('app.workforce_posting','read',true),set_config('app.workforce_posting_review_mode','source_use',true),set_config('app.workforce_posting_permission',?,true),set_config('app.workforce_posting_scoped','read',true),set_config('app.workforce_posting_actor',?,true),set_config('app.workforce_posting_link',?,true),set_config('app.workforce_posting_generation',?,true),set_config('app.workforce_posting_assignment',?,true),set_config('app.workforce_posting_assignment_version',?,true),set_config('app.workforce_posting_scope_epoch',?,true),set_config('app.workforce_posting_membership_epoch',?,true)`, scope.Subject, scope.HospitalID.String(), scope.StaffID.String(), string(authz.StaffPostingSourceUse), actor.StaffID.String(), actor.LinkID.String(), actor.GenerationID.String(), grant.AssignmentID, strconv.FormatInt(grant.Version, 10), strconv.FormatInt(grant.ScopeEpoch, 10), strconv.FormatInt(grant.MembershipEpoch, 10)).Error; err != nil {
					return err
				}
				lifecycle, err := newPostingLifecycleReview(tx)
				if err != nil {
					return err
				}
				p := authn.Principal{TenantID: scope.TenantID, Subject: scope.Subject}
				s := postingDraftScope{hospital: scope.HospitalID, staff: scope.StaffID, draft: scope.PostingID}
				observations := make([]postingUseObservation, 0, postingChainLimit)
				read := func(selected postingDraftScope, revision uuid.UUID) (postingChainSource, error) {
					node, until, e := readCurrentPostingNode(tx, p, selected, revision, lifecycle)
					if e == nil {
						r := node.Posting
						observations = append(observations, postingUseObservation{StaffID: r.StaffID, PostingID: r.ID, RevisionID: r.RevisionID, GenerationID: r.GenerationID, StaffVersion: r.StaffVersion, EmploymentEpoch: r.EmploymentEpoch, DepartmentID: r.DepartmentID, DepartmentRevisionID: r.DepartmentRevisionID, NominalUntil: postingApprovalDeadline(r), CurrentUntil: until, Cutoff: lifecycle.Cutoffs[r.ID]})
					}
					return node, e
				}
				selected, err := read(s, scope.RevisionID)
				if err != nil {
					return err
				}
				row := selected.Posting
				if row.Version != scope.Version || row.DepartmentID != scope.DepartmentID || row.DepartmentRevisionID != scope.DepartmentRevisionID {
					return authz.ErrScopedCommandDenied
				}
				plan, err := collectPostingSourcePlan(p.TenantID, s.hospital, row, selected.Intent, read)
				if err != nil {
					return err
				}
				if len(observations) != len(plan.Sources)+1 {
					return authz.ErrScopedAuthorizationUnavailable
				}
				if err = validatePostingSourceChainProof(tx, p, s, row, plan); err != nil {
					return err
				}
				if err = g.db.ConstrainGuardedCommand(actorCtx, scope.TenantID, lifecycle.EffectiveUntil); err != nil {
					return err
				}
				var hold func(context.Context, int) error
				hold = func(current context.Context, index int) error {
					if index < len(plan.References) {
						return g.departments.WithCurrentDepartment(current, plan.References[index], func(held context.Context) error { return hold(held, index+1) })
					}
					return g.db.WithTenantCommand(current, scope.TenantID, func(held *gorm.DB) error {
						useID, e := uuid.NewV7()
						if e != nil {
							return e
						}
						state, e := json.Marshal(observations)
						if e != nil {
							return e
						}
						digest := sha256.Sum256(state)
						ref := authz.StaffPostingSourceReference{UseID: useID, StaffID: row.StaffID, PostingID: row.ID, RevisionID: row.RevisionID, GenerationID: row.GenerationID, DepartmentID: row.DepartmentID, DepartmentRevisionID: row.DepartmentRevisionID, Version: row.Version, StaffVersion: row.StaffVersion, EmploymentEpoch: row.EmploymentEpoch, Actor: actor, SourceCount: len(observations), CheckedAt: lifecycle.CheckedAt, SourceUntil: lifecycle.EffectiveUntil, SourceHash: hex.EncodeToString(digest[:])}
						data, e := json.Marshal(map[string]any{"permission": string(authz.StaffPostingSourceUse), "assignment_id": grant.AssignmentID, "assignment_version": grant.Version, "scope_epoch": grant.ScopeEpoch, "membership_epoch": grant.MembershipEpoch, "actor_staff_id": actor.StaffID, "actor_link_id": actor.LinkID, "actor_generation_id": actor.GenerationID, "source_hash": ref.SourceHash, "checked_at": ref.CheckedAt, "source_until": ref.SourceUntil, "sources": observations})
						if e != nil {
							return e
						}
						if e = held.Exec(`INSERT INTO workforce_audit(id,tenant_id,hospital_id,actor_subject,action,resource_id,changes)VALUES(?,?,?,?,'workforce.posting_source.use',?,?::jsonb)`, useID, scope.TenantID, scope.HospitalID, scope.Subject, row.ID, string(data)).Error; e != nil {
							return e
						}
						if e = restore(); e != nil {
							return e
						}
						restored = true
						invoked = true
						return action(current, ref)
					})
				}
				return hold(actorCtx, 0)
			})
		})
	})
	if invoked || err == nil {
		return err
	}
	if errors.Is(err, authz.ErrScopedCommandDenied) || errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, errPostingProposalConflict) || errors.Is(err, authz.ErrDepartmentReferenceChanged) || errors.Is(err, authz.ErrDepartmentReferenceHidden) || errors.Is(err, tenantdb.ErrGuardedCommandExpired) {
		return authz.ErrScopedCommandDenied
	}
	return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
}
