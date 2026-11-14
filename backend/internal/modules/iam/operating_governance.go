package iam

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/pagination"
	"gorm.io/gorm"
)

//go:embed operating_governance.sql
var operatingGovernanceSchema string

type operatingAdministrationProof struct {
	membership *authz.Membership
	permission authz.Permission
}
type operatingNativeRequest struct {
	scope  authz.IndependentEmployerScope
	duty   string
	active *operatingNativeAuthority
}
type operatingNativeAuthority struct {
	p          authn.Principal
	source     authz.GovernanceSourceEvidence
	capability authz.GovernanceCapability
}
type operatingGovernanceAuthority struct {
	TenantID           string                     `json:"tenant_id"`
	EmployerKind       string                     `json:"employer_kind"`
	EmployerID         uuid.UUID                  `json:"employer_id"`
	ActorSubject       string                     `json:"actor_subject"`
	Capability         authz.GovernanceCapability `json:"capability"`
	Kind               authz.GovernanceSourceKind `json:"authority_kind"`
	MembershipEpoch    int64                      `json:"membership_epoch"`
	OwnerMembershipID  *uuid.UUID                 `json:"owner_membership_id,omitempty"`
	SourceGrantID      *uuid.UUID                 `json:"source_grant_id,omitempty"`
	SourceGrantVersion *int64                     `json:"source_grant_version,omitempty"`
	AuthenticatedAt    time.Time                  `json:"authenticated_at"`
	IdleDeadline       time.Time                  `json:"idle_deadline"`
	ExpiresAt          time.Time                  `json:"expires_at"`
}

func operatingGovernanceCapability(permission authz.Permission) (authz.GovernanceCapability, bool) {
	switch permission {
	case PermOperatingRead, PermOperatingPrepare, PermOperatingRevoke:
		return authz.GovernanceOperatingManage, true
	case PermOperatingReview:
		return authz.GovernanceOperatingReview, true
	default:
		return "", false
	}
}
func (h *operatingHandlers) scope(c *gin.Context, permission authz.Permission) (authn.Principal, uuid.UUID, bool) {
	if h.native != nil {
		capability, ok := operatingGovernanceCapability(permission)
		if !ok {
			respond.Forbidden(c, "Current operating duty administration authority is required.")
			return authn.Principal{}, uuid.Nil, false
		}
		permission = authz.Permission(capability)
	}
	return operatingScope(c, permission)
}
func (h *operatingHandlers) nativeHandler(c *gin.Context) (*operatingHandlers, bool) {
	kind := c.Param("kind")
	employer, err := uuid.Parse(c.Param("employer"))
	if err != nil || employer.Version() != 7 || employer.Variant() != uuid.RFC4122 || employer.String() != c.Param("employer") || (kind != "pharmacy" && kind != "supplier") {
		respond.NotFound(c, "operating employer")
		return nil, false
	}
	copy := *h
	copy.native = &operatingNativeRequest{scope: authz.IndependentEmployerScope{EmployerKind: kind, EmployerID: employer}, duty: c.Param("id")}
	return &copy, true
}
func registerOperatingGovernanceRoutes(r *platform.Router, h *operatingHandlers) {
	registerOperatingLocationChoices(r, h)
	wrap := func(action func(*operatingHandlers, *gin.Context)) gin.HandlerFunc {
		return func(c *gin.Context) {
			copy, ok := h.nativeHandler(c)
			if !ok {
				return
			}
			if c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery {
				respond.Error(c, http.StatusBadRequest, "invalid_request", "Do not add filters to this operating duty command.")
				return
			}
			action(copy, c)
		}
	}
	prefix := "/operating-governance-employers/:kind/:employer"
	manage := authz.Permission(authz.GovernanceOperatingManage)
	r.PUT(prefix+"/duties/:id", manage, wrap((*operatingHandlers).prepare))
	r.GET(prefix+"/duties/:id", manage, wrap((*operatingHandlers).get))
	r.GET(prefix+"/duties/:id/proposal-receipt", manage, wrap((*operatingHandlers).proposalReceipt))
	r.PUT(prefix+"/duties/:id/commands/:command", authz.Public, wrap((*operatingHandlers).decide))
	r.GET(prefix+"/duties/:id/commands/:command", manage, wrap((*operatingHandlers).getCommand))
	platform.ListRoute(r, prefix+"/staff/:staff/duties", manage, func(c *gin.Context, page pagination.Params) ([]OperatingDuty, error) {
		copy, ok := h.nativeHandler(c)
		if !ok {
			return nil, nil
		}
		return copy.list(c, page)
	})
}
func (h *operatingHandlers) actorProof(ctx context.Context, p authn.Principal, permission authz.Permission) (operatingAdministrationProof, error) {
	result := operatingAdministrationProof{permission: permission}
	if h.native != nil {
		if _, ok := operatingGovernanceCapability(permission); !ok {
			return result, authz.ErrScopedCommandDenied
		}
		return result, nil
	}
	member, err := h.legacyActorProof(ctx, p, permission)
	if err == nil {
		result.membership = &member
	}
	return result, err
}
func validOperatingGovernanceSource(p authn.Principal, scope authz.IndependentEmployerScope, source authz.GovernanceSourceEvidence) bool {
	if scope.EmployerKind != "pharmacy" && scope.EmployerKind != "supplier" {
		return false
	}
	if source.Scope.TenantID != p.TenantID || source.Scope.Kind != scope.EmployerKind || source.Scope.ID == nil || *source.Scope.ID != scope.EmployerID || source.MembershipEpoch < 1 || source.MembershipEpoch > 9007199254740991 || source.OwnerCount < 1 || source.OwnerCount > 1000 || !source.ExpiresAt.After(time.Now()) {
		return false
	}
	switch source.Kind {
	case authz.GovernanceOwnerSource:
		return source.OwnerMembershipID != uuid.Nil && source.OwnerMembershipID.Variant() == uuid.RFC4122 && source.GrantID == uuid.Nil && source.Version == 0 && source.TemplateKey == ""
	case authz.GovernanceGrantSource:
		return source.OwnerMembershipID == uuid.Nil && source.GrantID.Version() == 7 && source.GrantID.Variant() == uuid.RFC4122 && source.Version >= 1 && source.Version <= 9007199254740991 && source.TemplateKey == "iam.operating_access_reviewer.v1"
	default:
		return false
	}
}
func operatingGovernanceProof(p authn.Principal, scope authz.IndependentEmployerScope, capability authz.GovernanceCapability, source authz.GovernanceSourceEvidence) operatingGovernanceAuthority {
	proof := operatingGovernanceAuthority{TenantID: p.TenantID, EmployerKind: scope.EmployerKind, EmployerID: scope.EmployerID, ActorSubject: p.Subject, Capability: capability, Kind: source.Kind, MembershipEpoch: source.MembershipEpoch, AuthenticatedAt: p.AuthTime, IdleDeadline: p.IdleDeadline, ExpiresAt: source.ExpiresAt}
	if source.Kind == authz.GovernanceOwnerSource {
		id := source.OwnerMembershipID
		proof.OwnerMembershipID = &id
	} else {
		id, version := source.GrantID, source.Version
		proof.SourceGrantID = &id
		proof.SourceGrantVersion = &version
	}
	return proof
}
func (h *operatingHandlers) withAdministration(ctx context.Context, p authn.Principal, proof operatingAdministrationProof, phase string, action func(context.Context, *gorm.DB) error) error {
	if h.native == nil {
		if proof.membership == nil {
			return authz.ErrScopedAuthorizationUnavailable
		}
		return h.transaction(ctx, p, *proof.membership, phase, action)
	}
	capability, ok := operatingGovernanceCapability(proof.permission)
	if !ok || proof.membership != nil {
		return authz.ErrScopedCommandDenied
	}
	if h.governanceSources == nil || h.db == nil || action == nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	scope := h.native.scope
	scope.TenantID = p.TenantID
	req := authz.GovernanceSourceRequest{Principal: p, Capability: capability, Scope: authz.GovernanceEstablishmentScope{TenantID: p.TenantID, Kind: scope.EmployerKind, ID: &scope.EmployerID}}
	return h.governanceSources.WithGovernanceSource(ctx, req, func(commandCtx context.Context, source authz.GovernanceSourceEvidence) error {
		if !validOperatingGovernanceSource(p, scope, source) || !h.db.GuardedCommandActive(commandCtx) {
			return authz.ErrScopedAuthorizationUnavailable
		}
		if err := h.db.ConstrainGuardedCommand(commandCtx, p.TenantID, source.ExpiresAt); err != nil {
			return err
		}
		raw, err := json.Marshal(operatingGovernanceProof(p, scope, capability, source))
		if err != nil {
			return err
		}
		h.native.active = &operatingNativeAuthority{p: p, source: source, capability: capability}
		defer func() { h.native.active = nil }()
		return h.db.WithTenantCommand(commandCtx, p.TenantID, func(tx *gorm.DB) (err error) {
			if err = operatingNativeSupport(tx); err != nil {
				return err
			}
			var previous string
			if err = tx.Raw(`SELECT jsonb_object_agg(key,COALESCE(current_setting(key,true),''))::text FROM unnest(ARRAY['app.iam_operating_access','app.iam_operating_command','app.iam_operating_native_authority','app.iam_operating_native_kind','app.iam_operating_native_employer','app.iam_operating_native_duty','app.iam_operating_native_target'])AS key`).Scan(&previous).Error; err != nil {
				return err
			}
			defer func() {
				err = errors.Join(err, tx.Exec(`SELECT set_config(key,value,true)FROM jsonb_each_text(?::jsonb)`, previous).Error)
			}()
			if err = tx.Exec(`SELECT set_config('app.iam_operating_access','authorized',true),set_config('app.iam_operating_command',?,true),set_config('app.iam_operating_native_authority',?,true),set_config('app.iam_operating_native_kind',?,true),set_config('app.iam_operating_native_employer',?,true),set_config('app.iam_operating_native_duty',?,true)`, phase, string(raw), scope.EmployerKind, scope.EmployerID.String(), h.native.duty).Error; err != nil {
				return err
			}
			if err = action(commandCtx, tx); err != nil {
				return err
			}
			if err = tx.Exec(`SET CONSTRAINTS iam_operating_native_completion IMMEDIATE`).Error; err != nil {
				return err
			}
			return tx.Exec(`SET CONSTRAINTS iam_operating_native_completion DEFERRED`).Error
		})
	})
}
func (h *operatingHandlers) governedEmployerAccess(ctx context.Context, tenant string, scope OperatingDutyScope, action func(context.Context) error) error {
	if h.native == nil || h.native.active == nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	native := h.native.active
	if native.p.TenantID != tenant || scope.EmployerKind != h.native.scope.EmployerKind || scope.EmployerID != h.native.scope.EmployerID {
		return gorm.ErrRecordNotFound
	}
	guard, ok := h.staff.(authz.GovernedIndependentEmployerGuard)
	if !ok {
		return authz.ErrScopedAuthorizationUnavailable
	}
	return guard.WithGovernedIndependentEmployerAccess(ctx, authz.GovernedIndependentEmployerRequest{Principal: native.p, Scope: authz.IndependentEmployerScope{TenantID: tenant, EmployerKind: scope.EmployerKind, EmployerID: scope.EmployerID}, Capability: native.capability, Source: native.source}, action)
}
func sameOperatingGovernanceSource(previous, current operatingGovernanceAuthority) bool {
	return previous.Kind == current.Kind && reflect.DeepEqual(previous.OwnerMembershipID, current.OwnerMembershipID) && reflect.DeepEqual(previous.SourceGrantID, current.SourceGrantID) && reflect.DeepEqual(previous.SourceGrantVersion, current.SourceGrantVersion) && previous.MembershipEpoch == current.MembershipEpoch && previous.TenantID == current.TenantID && previous.EmployerKind == current.EmployerKind && previous.EmployerID == current.EmployerID && previous.ActorSubject == current.ActorSubject
}

func (h *operatingHandlers) checkOperatingMode(tx *gorm.DB, p authn.Principal, id uuid.UUID, replay bool, command *uuid.UUID) error {
	var marker struct{ NativeAdministration bool }
	if err := tx.Table("iam_operating_duties").Select("native_administration").Where("tenant_id=? AND id=?", p.TenantID, id).Take(&marker).Error; err != nil {
		return err
	}
	if marker.NativeAdministration != (h.native != nil) {
		return gorm.ErrRecordNotFound
	}
	if !marker.NativeAdministration {
		return nil
	}
	var rows []struct{ Authority json.RawMessage }
	if err := tx.Table("iam_operating_governance_origins").Select("authority").Where("tenant_id=? AND duty_id=?", p.TenantID, id).Limit(1).Find(&rows).Error; err != nil {
		return err
	}
	if h.native == nil {
		if len(rows) != 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	}
	if len(rows) != 1 || h.native.active == nil {
		return gorm.ErrRecordNotFound
	}
	if !replay {
		return operatingAudit(tx, p, id, "iam.operating.governance.source.use")
	}
	raw := rows[0].Authority
	if command != nil {
		var stored struct{ Authority json.RawMessage }
		if err := tx.Table("iam_operating_governance_commands").Select("authority").Where("tenant_id=? AND duty_id=? AND command_id=?", p.TenantID, id, *command).Take(&stored).Error; err != nil {
			return err
		}
		raw = stored.Authority
	}
	var previous operatingGovernanceAuthority
	if err := json.Unmarshal(raw, &previous); err != nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	current := operatingGovernanceProof(p, h.native.scope, h.native.active.capability, h.native.active.source)
	if !sameOperatingGovernanceSource(previous, current) {
		return gorm.ErrRecordNotFound
	}
	return operatingAudit(tx, p, id, "iam.operating.governance.source.use")
}

func (h *operatingHandlers) selectOperatingTarget(ctx context.Context, p authn.Principal, subject string) error {
	if h.native == nil {
		return nil
	}
	if h.native.active == nil || !proposalSubject(subject) {
		return authz.ErrScopedAuthorizationUnavailable
	}
	return h.db.WithTenantCommand(ctx, p.TenantID, func(tx *gorm.DB) error {
		return tx.Exec(`SELECT set_config('app.iam_operating_native_target',?,true)`, subject).Error
	})
}

func operatingNativeSupport(tx *gorm.DB) error {
	var installed bool
	if err := tx.Raw(`SELECT iam_operating_governance_support()`).Scan(&installed).Error; err != nil {
		return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
	}
	if !installed {
		return authz.ErrScopedAuthorizationUnavailable
	}
	return nil
}

func (h *operatingHandlers) operatingError(c *gin.Context, err error) {
	if h.native != nil && errors.Is(err, authz.ErrScopedCommandDenied) {
		respond.Forbidden(c, "Your access or the selected staff member could not be verified.")
		return
	}
	operatingError(c, err)
}
