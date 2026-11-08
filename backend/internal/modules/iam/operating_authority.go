package iam

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type operatingChecker interface {
	CheckOperatingAssignment(context.Context, authz.OperatingAssignment, string, authz.Permission) (bool, error)
}

// A failed protected action is not a failed duty candidate. Keep it opaque until
// the authority boundary returns so another duty cannot retry the business call.
type operatingActionError struct{ cause error }

func (*operatingActionError) Error() string { return "operating protected action failed" }

type operatingAuthority struct {
	db        *tenantdb.DB
	graph     operatingChecker
	staff     authz.CurrentIndependentStaffGuard
	locations authz.CurrentOperatingLocationGuard
}

func NewOperatingAuthorizer(db *tenantdb.DB, graph operatingChecker, staff authz.CurrentIndependentStaffGuard, locations authz.CurrentOperatingLocationGuard) authz.OperatingScopeAuthorizer {
	return &operatingAuthority{db: db, graph: graph, staff: staff, locations: locations}
}
func (a *operatingAuthority) WithOperatingScope(ctx context.Context, request authz.OperatingScopeRequest, action func(context.Context, authz.OperatingScopeGrant) error) error {
	p := request.Principal
	scope := authz.OperatingLocationScope{TenantID: p.TenantID, EmployerKind: request.EmployerKind, EmployerID: request.EmployerID, LocationID: request.LocationID}
	if a.db == nil || a.graph == nil || a.staff == nil || a.locations == nil || action == nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	if !authz.ValidOperatingLocationScope(scope) || !proposalSubject(p.Subject) || p.AuthTime.IsZero() || !p.IdleDeadline.After(time.Now()) || (tenantdb.ActorFrom(ctx) != "" && tenantdb.ActorFrom(ctx) != p.Subject) {
		return authz.ErrInvalidScopedAssignment
	}
	known := false
	for _, template := range operatingDutyTemplates {
		if template.EmployerKind == scope.EmployerKind {
			for _, permission := range template.Permissions {
				if authz.Permission(permission) == request.Permission {
					known = true
				}
			}
		}
	}
	if !known {
		return authz.ErrScopedCommandDenied
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	ctx = tenantdb.WithActor(ctx, p.Subject)
	type candidate struct {
		Document       json.RawMessage
		DutyVersion    int64
		AuthorityEpoch int64
	}
	var candidates []candidate
	err := a.db.WithTenant(ctx, p.TenantID, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.iam_operating_access','authorized',true)`).Error; err != nil {
			return err
		}
		return tx.Raw(`SELECT s.document,d.version AS duty_version,d.authority_epoch FROM iam_operating_duties d JOIN iam_operating_snapshots s ON(s.tenant_id,s.duty_id,s.version,s.authority_epoch)=(d.tenant_id,d.id,d.version-1,d.authority_epoch)JOIN iam_operating_projection_acks k ON(k.tenant_id,k.duty_id,k.version,k.authority_epoch)=(s.tenant_id,s.duty_id,s.version,s.authority_epoch)WHERE d.tenant_id=? AND d.subject=? AND d.employer_kind=? AND d.employer_id=? AND d.location_id=? AND d.status='active' AND jsonb_exists(s.document->'Permissions',?) ORDER BY d.id LIMIT 33`, p.TenantID, p.Subject, scope.EmployerKind, scope.EmployerID, scope.LocationID, string(request.Permission)).Scan(&candidates).Error
	})
	if err != nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	if len(candidates) > 32 {
		return authz.ErrScopedAuthorizationUnavailable
	}
	for _, candidate := range candidates {
		var snapshot authz.OperatingAssignment
		if err := json.Unmarshal(candidate.Document, &snapshot); err != nil {
			return authz.ErrScopedAuthorizationUnavailable
		}
		now := time.Now()
		if now.Before(snapshot.ValidFrom) || !now.Before(snapshot.ExpiresAt) {
			continue
		}
		if snapshot.TenantID != p.TenantID || snapshot.Subject != p.Subject || snapshot.EmployerKind != scope.EmployerKind || snapshot.EmployerID != scope.EmployerID.String() || snapshot.LocationID != scope.LocationID.String() || snapshot.Version+1 != candidate.DutyVersion || snapshot.ScopeEpoch != candidate.AuthorityEpoch {
			return authz.ErrScopedAuthorizationUnavailable
		}
		allowed, err := a.graph.CheckOperatingAssignment(ctx, snapshot, p.Subject, request.Permission)
		if err != nil {
			return authz.ErrScopedAuthorizationUnavailable
		}
		if !allowed {
			continue
		}
		err = a.consume(ctx, p, scope, request.Permission, snapshot, candidate.DutyVersion, action)
		var failedAction *operatingActionError
		if errors.As(err, &failedAction) {
			return failedAction.cause
		}
		if errors.Is(err, authz.ErrScopedCommandDenied) {
			continue
		}
		return err
	}
	return authz.ErrScopedCommandDenied
}
func (a *operatingAuthority) consume(ctx context.Context, p authn.Principal, scope authz.OperatingLocationScope, permission authz.Permission, snapshot authz.OperatingAssignment, version int64, action func(context.Context, authz.OperatingScopeGrant) error) error {
	id, err := uuid.Parse(snapshot.ID)
	if err != nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	deadline := time.Now().Add(5 * time.Second)
	if p.IdleDeadline.Before(deadline) {
		deadline = p.IdleDeadline
	}
	var previous string
	var row operatingDutyRow
	return a.db.WithGuardedTenantCommand(ctx, p.TenantID, deadline, func(tx *gorm.DB) error {
		if err := tx.Raw(`SELECT jsonb_object_agg(key,COALESCE(current_setting(key,true),''))::text FROM unnest(ARRAY['app.iam_operating_access','app.iam_operating_command'])AS key`).Scan(&previous).Error; err != nil {
			return err
		}
		if err := tx.Exec(`SELECT set_config('app.iam_operating_access','authorized',true)`).Error; err != nil {
			return err
		}
		if err := tx.Table("iam_operating_duties").Select("tenant_id,id,input,prepared_by,status,membership_epoch,version,authority_epoch,created_at,updated_at").Where("tenant_id=? AND id=? AND subject=?", p.TenantID, id, p.Subject).Clauses(clause.Locking{Strength: "SHARE"}).Take(&row).Error; errors.Is(err, gorm.ErrRecordNotFound) {
			return authz.ErrScopedCommandDenied
		} else if err != nil {
			return err
		}
		now := time.Now()
		if row.Status != "active" || row.Version != version || row.AuthorityEpoch != snapshot.ScopeEpoch || row.MembershipEpoch != snapshot.MembershipEpoch || now.Before(snapshot.ValidFrom) || !now.Before(snapshot.ExpiresAt) {
			return authz.ErrScopedCommandDenied
		}
		if err := tx.Exec(`SELECT pg_advisory_xact_lock_shared(hashtextextended(?,0))`, "iam.credential:"+p.Subject).Error; err != nil {
			return err
		}
		var revoked bool
		if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM iam_credential_revocations WHERE subject=? AND revoked_at>=?)`, p.Subject, p.AuthTime).Scan(&revoked).Error; err != nil {
			return err
		}
		if revoked {
			return authz.ErrScopedCredentialRevoked
		}
		epoch, err := proposalMembershipEpoch(tx, p.TenantID, p.Subject)
		if err != nil {
			return err
		}
		if epoch != snapshot.MembershipEpoch || epoch < 1 {
			return authz.ErrScopedCommandDenied
		}
		return nil
	}, func(commandCtx context.Context) error {
		h := operatingHandlers{db: a.db, staff: a.staff, locations: a.locations}
		view, err := operatingDutyView(row, false)
		if err != nil {
			return err
		}
		var actionError error
		err = h.withCurrentBasis(commandCtx, p, row, view.Input, false, func(current authz.OperatingAssignment) error {
			if current.Identity != snapshot.Identity || current.SourceVersion != snapshot.SourceVersion || current.LocationVersion != snapshot.LocationVersion {
				return authz.ErrScopedCommandDenied
			}
			return a.db.WithTenantCommand(commandCtx, p.TenantID, func(tx *gorm.DB) error {
				if err := tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,'iam.operating.used','iam_operating_duties',?)`, p.TenantID, p.Subject, id).Error; err != nil {
					return err
				}
				if err := tx.Exec(`SELECT set_config(key,value,true)FROM jsonb_each_text(?::jsonb)`, previous).Error; err != nil {
					return err
				}
				actionError = action(commandCtx, authz.OperatingScopeGrant{Scope: scope, Permission: permission, DutyID: id, SnapshotVersion: snapshot.Version, AuthorityEpoch: snapshot.ScopeEpoch, Identity: snapshot.Identity})
				return actionError
			})
		})
		if actionError != nil {
			return &operatingActionError{cause: actionError}
		}
		if errors.Is(err, errProposalConflict) || errors.Is(err, gorm.ErrRecordNotFound) {
			return authz.ErrScopedCommandDenied
		}
		return err
	})
}
