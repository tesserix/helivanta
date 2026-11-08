package iam

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func operatingCommandID(c *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param("command"))
	if err != nil || id.Version() != 7 || id.Variant() != uuid.RFC4122 || id.String() != c.Param("command") {
		respond.NotFound(c, "operating duty command")
		return uuid.Nil, false
	}
	return id, true
}
func loadOperatingCommand(tx *gorm.DB, p authn.Principal, duty, id uuid.UUID) (OperatingDutyDecision, error) {
	var row struct {
		Receipt      json.RawMessage
		ActorSubject string
	}
	err := tx.Table("iam_operating_commands").Select("receipt,actor_subject").Where("tenant_id=? AND duty_id=? AND id=?", p.TenantID, duty, id).Take(&row).Error
	if err != nil {
		return OperatingDutyDecision{}, err
	}
	if row.ActorSubject != p.Subject {
		return OperatingDutyDecision{}, gorm.ErrRecordNotFound
	}
	var result OperatingDutyDecision
	err = json.Unmarshal(row.Receipt, &result)
	return result, err
}
func (h *operatingHandlers) withCurrentBasis(ctx context.Context, p authn.Principal, row operatingDutyRow, in OperatingDutyInput, review bool, action func(authz.OperatingAssignment) error) error {
	epochErr := h.db.WithTenantCommand(ctx, p.TenantID, func(tx *gorm.DB) error {
		epoch, err := proposalMembershipEpoch(tx, p.TenantID, in.Subject)
		if err != nil {
			return err
		}
		if epoch != row.MembershipEpoch || epoch < 1 {
			return errProposalConflict
		}
		return nil
	})
	if epochErr != nil {
		return epochErr
	}
	if h.staff == nil || h.locations == nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	expectedScope := authz.OperatingLocationScope{TenantID: p.TenantID, EmployerKind: in.Scope.EmployerKind, EmployerID: in.Scope.EmployerID, LocationID: in.Scope.LocationID}
	source, err := h.locations.CurrentOperatingLocation(ctx, expectedScope)
	if err != nil {
		return err
	}
	if source.Scope != expectedScope || source.Country != "IN" {
		return authz.ErrScopedAuthorizationUnavailable
	}
	if source.EmployerVersion != in.Scope.ExpectedEmployerVersion || source.LocationVersion != in.Scope.ExpectedLocationVersion {
		return errProposalConflict
	}
	template, ok := operatingTemplate(in.TemplateKey, in.Scope.EmployerKind)
	if !ok {
		return errProposalConflict
	}
	consume := func(_ context.Context, identity authz.IndependentStaffReference) error {
		if identity != (authz.IndependentStaffReference{StaffID: in.Identity.StaffID, StaffVersion: in.Identity.StaffVersion, LinkID: in.Identity.LinkID, LinkVersion: in.Identity.LinkVersion}) {
			return errProposalConflict
		}
		permissions := make([]authz.Permission, len(template.Permissions))
		for i, permission := range template.Permissions {
			permissions[i] = authz.Permission(permission)
		}
		return action(authz.OperatingAssignment{TenantID: p.TenantID, EmployerKind: in.Scope.EmployerKind, EmployerID: in.Scope.EmployerID.String(), LocationID: in.Scope.LocationID.String(), ID: row.ID.String(), Subject: in.Subject, Version: row.Version + 1, MembershipEpoch: row.MembershipEpoch, ScopeEpoch: row.AuthorityEpoch, SourceVersion: source.EmployerVersion, LocationVersion: source.LocationVersion, Identity: identity, ValidFrom: in.ValidFrom, ExpiresAt: in.ExpiresAt, Permissions: permissions})
	}
	scope := authz.IndependentStaffScope{TenantID: p.TenantID, EmployerKind: in.Scope.EmployerKind, EmployerID: in.Scope.EmployerID, Subject: in.Subject}
	if review {
		return h.staff.WithCurrentIndependentStaffForReview(ctx, scope, row.PreparedBy, consume)
	}
	return h.staff.WithCurrentIndependentStaff(ctx, scope, consume)
}
func (h *operatingHandlers) decide(c *gin.Context) {
	if _, _, ok := authn.TenantPrincipal(c); !ok {
		return
	}
	var in OperatingDutyDecisionInput
	if !operatingDecode(c, &in) {
		return
	}
	in.Reason = strings.TrimSpace(in.Reason)
	permission, phase := PermOperatingReview, "review"
	switch in.Decision {
	case "approve", "reject":
	case "withdraw":
		permission, phase = PermOperatingPrepare, "withdraw"
	case "revoke":
		permission, phase = PermOperatingRevoke, "revoke"
	default:
		respond.Error(c, 400, "invalid_request", "Choose approve, reject, withdraw or revoke.")
		return
	}
	p, id, ok := operatingScope(c, permission)
	if !ok {
		return
	}
	commandID, ok := operatingCommandID(c)
	if !ok {
		return
	}
	if in.ExpectedVersion < 1 || in.ExpectedVersion > 9007199254740990 || !utf8.ValidString(in.Reason) || utf8.RuneCountInString(in.Reason) < 1 || utf8.RuneCountInString(in.Reason) > 500 || strings.ContainsRune(in.Reason, 0) {
		respond.Error(c, 400, "invalid_request", "Enter the current duty version and a reason.")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	proof, err := h.actorProof(ctx, p, permission)
	if err != nil {
		operatingError(c, err)
		return
	}
	var result OperatingDutyDecision
	created := false
	err = h.transaction(ctx, p, proof, phase, func(commandCtx context.Context, tx *gorm.DB) error {
		existing, err := loadOperatingCommand(tx, p, id, commandID)
		if err == nil {
			return h.employerAccess(commandCtx, p.TenantID, existing.Result.Input.Scope, "reader", func(context.Context) error {
				if existing.Input != in {
					return errProposalConflict
				}
				result = existing
				return operatingAudit(tx, p, id, "iam.operating.command_recovered")
			})
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var row operatingDutyRow
		if err := tx.Table("iam_operating_duties").Select("tenant_id,id,input,prepared_by,status,membership_epoch,version,authority_epoch,created_at,updated_at").Where("tenant_id=? AND id=?", p.TenantID, id).Clauses(clause.Locking{Strength: "UPDATE"}).Take(&row).Error; err != nil {
			return err
		}
		// A concurrent identical command may have completed while this request waited for the owner row.
		existing, err = loadOperatingCommand(tx, p, id, commandID)
		if err == nil {
			return h.employerAccess(commandCtx, p.TenantID, existing.Result.Input.Scope, "reader", func(context.Context) error {
				if existing.Input != in {
					return errProposalConflict
				}
				result = existing
				return operatingAudit(tx, p, id, "iam.operating.command_recovered")
			})
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		view, err := operatingDutyView(row, false)
		if err != nil {
			return err
		}
		return h.employerAccess(commandCtx, p.TenantID, view.Input.Scope, "manager", func(commandCtx context.Context) error {
			next := map[string]string{"approve": "approved_pending_activation", "reject": "rejected", "withdraw": "withdrawn", "revoke": "revoked"}[in.Decision]
			if row.Version != in.ExpectedVersion || row.Status != "pending_review" && (in.Decision != "revoke" || row.Status != "approved_pending_activation" && row.Status != "active") {
				return errProposalConflict
			}
			if in.Decision == "withdraw" && p.Subject != row.PreparedBy {
				return errProposalConflict
			}
			if phase == "review" && (p.Subject == row.PreparedBy || p.Subject == view.Input.Subject) {
				return errProposalConflict
			}
			transition := func(snapshot *authz.OperatingAssignment) error {
				epoch := row.AuthorityEpoch
				if in.Decision == "revoke" {
					epoch++
				}
				if err := tx.Exec(`UPDATE iam_operating_duties SET status=?,version=version+1,authority_epoch=?,actor_subject=?,reason=? WHERE tenant_id=? AND id=? AND version=?`, next, epoch, p.Subject, in.Reason, p.TenantID, id, row.Version).Error; err != nil {
					return err
				}
				if snapshot != nil {
					document, err := json.Marshal(snapshot)
					if err != nil {
						return err
					}
					if err := tx.Exec(`INSERT INTO iam_operating_snapshots(tenant_id,duty_id,version,authority_epoch,document)VALUES(?,?,?,?,?::jsonb)`, p.TenantID, id, row.Version+1, row.AuthorityEpoch, string(document)).Error; err != nil {
						return err
					}
				}
				changed, err := loadOperatingDuty(tx, p.TenantID, id)
				if err != nil {
					return err
				}
				current, err := operatingDutyView(changed, false)
				if err != nil {
					return err
				}
				result = OperatingDutyDecision{ID: commandID, DutyID: id, Input: in, Result: current, ActorSubject: p.Subject, CreatedAt: changed.UpdatedAt.UTC()}
				receipt, err := json.Marshal(result)
				if err != nil {
					return err
				}
				input, err := json.Marshal(in)
				if err != nil {
					return err
				}
				if err := tx.Exec(`INSERT INTO iam_operating_commands(tenant_id,id,duty_id,actor_subject,expected_version,input,receipt,created_at)VALUES(?,?,?,?,?,?::jsonb,?::jsonb,?)`, p.TenantID, commandID, id, p.Subject, in.ExpectedVersion, string(input), string(receipt), result.CreatedAt).Error; err != nil {
					return err
				}
				created = true
				return h.publish(tx, p.TenantID, id, changed.Version)
			}
			if phase == "review" {
				if in.Decision == "approve" && !view.Input.ExpiresAt.After(time.Now()) {
					return errOperatingValidity
				}
				return h.withCurrentBasis(commandCtx, p, row, view.Input, true, func(snapshot authz.OperatingAssignment) error {
					if in.Decision == "approve" {
						return transition(&snapshot)
					}
					return transition(nil)
				})
			}
			return transition(nil)
		})
	})
	if err != nil {
		operatingError(c, err)
		return
	}
	if created {
		respond.Created(c, result)
	} else {
		respond.OK(c, result)
	}
}
func (h *operatingHandlers) getCommand(c *gin.Context) {
	p, id, ok := operatingScope(c, PermOperatingRead)
	if !ok {
		return
	}
	command, ok := operatingCommandID(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()
	proof, err := h.actorProof(ctx, p, PermOperatingRead)
	if err != nil {
		operatingError(c, err)
		return
	}
	var result OperatingDutyDecision
	err = h.transaction(ctx, p, proof, "read", func(commandCtx context.Context, tx *gorm.DB) error {
		var err error
		result, err = loadOperatingCommand(tx, p, id, command)
		if err != nil {
			return err
		}
		return h.employerAccess(commandCtx, p.TenantID, result.Result.Input.Scope, "reader", func(context.Context) error { return operatingAudit(tx, p, id, "iam.operating.command_read") })
	})
	if err != nil {
		operatingError(c, err)
		return
	}
	respond.OK(c, result)
}
