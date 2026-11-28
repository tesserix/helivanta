package iam

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"github.com/tesserix/onboarding-service/api/customercontract"
	"github.com/tesserix/onboarding-service/api/staffclient"
	"gorm.io/gorm"
)

type staffMFAResetService struct {
	db          *tenantdb.DB
	owner       *staffclient.Client
	identity    UserStateChecker
	revocations *revocationHandlers
	membership  MembershipIdentityReader
}

// StaffMFARecovery exposes current reset authority through the published owner boundary.
func (m *Module) StaffMFARecovery(deps platform.Deps) platform.StaffMFARecoveryService {
	return &staffMFAResetService{db: deps.DB, owner: deps.StaffAccounts, identity: m.userState, membership: m.membershipIdentity, revocations: &revocationHandlers{db: deps.DB, bus: deps.Bus, checker: m.checker}}
}

func (h *staffMFAResetService) validate(p authn.Principal) error {
	tenant, err := uuid.Parse(p.TenantID)
	if err != nil || tenant == uuid.Nil || tenant.String() != p.TenantID || !identitySubjectValid(p.Subject) {
		return staffclient.ErrInvalid
	}
	now := time.Now()
	if p.AuthTime.IsZero() || !p.AuthTime.After(now.Add(-5*time.Minute)) || p.AuthTime.After(now.Add(30*time.Second)) || !now.Before(p.IdleDeadline) {
		return authz.ErrScopedCredentialRevoked
	}
	if h == nil || h.db == nil || h.owner == nil || h.identity == nil || h.membership == nil || h.revocations == nil || h.revocations.bus == nil || h.revocations.checker == nil {
		return staffclient.ErrUnavailable
	}
	return nil
}

func identitySubjectValid(subject string) bool {
	if len(subject) < 1 || len(subject) > 200 {
		return false
	}
	for _, r := range subject {
		if r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func mfaAuthorityError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, authz.ErrScopedCredentialRevoked):
		return authz.ErrScopedCredentialRevoked
	case errors.Is(err, errRevocationActorNotAdmin), errors.Is(err, authz.ErrScopedCommandDenied):
		return authz.ErrScopedCommandDenied
	case errors.Is(err, gorm.ErrRecordNotFound), errors.Is(err, staffclient.ErrNotFound):
		return staffclient.ErrNotFound
	case errors.Is(err, errStaffMFACancelRequested):
		return errStaffMFACancelRequested
	case errors.Is(err, staffclient.ErrConflict):
		return staffclient.ErrConflict
	default:
		return staffclient.ErrUnavailable
	}
}

func (h *staffMFAResetService) authorize(ctx context.Context, p authn.Principal, target string, id uuid.UUID) error {
	if err := h.validate(p); err != nil {
		return err
	}
	status, err := h.identity.UserCredentialState(ctx, p.TenantID, p.Subject)
	if err != nil {
		return staffclient.ErrUnavailable
	}
	current, err := credentialIsCurrent(status, p.AuthTime, p.RecoveryGeneration)
	if err != nil {
		return staffclient.ErrUnavailable
	}
	if !current {
		return authz.ErrScopedCredentialRevoked
	}
	eligible, err := h.membership.MembershipEligible(ctx, p.TenantID, p.Subject)
	if err != nil {
		return staffclient.ErrUnavailable
	}
	if !eligible {
		return authz.ErrScopedCredentialRevoked
	}
	return mfaAuthorityError(h.db.WithTenant(ctx, p.TenantID, func(tx *gorm.DB) error {
		if err := membershipActor(tx, p, target); err != nil {
			return err
		}
		if target != "" {
			if err := ordinaryMFATarget(tx, p, target); err != nil {
				return err
			}
		}
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,?,'staff_mfa_reset_authorized','iam_staff_mfa_fences',?)`, p.TenantID, p.Subject, id).Error
	}))
}

func ordinaryMFATarget(tx *gorm.DB, p authn.Principal, target string) error {
	if target == p.Subject {
		return authz.ErrScopedCommandDenied
	}
	// Lock the subject epoch that every membership transition must update.
	var count int64
	if err := tx.Raw(`SELECT member_count FROM iam_subject_membership_epochs WHERE tenant_id=? AND subject=? FOR UPDATE`, p.TenantID, target).Scan(&count).Error; err != nil {
		return err
	}
	if count < 1 {
		return gorm.ErrRecordNotFound
	}
	var roles []string
	if err := tx.Table("iam_members").Select("role_key").Where("tenant_id=? AND subject=?", p.TenantID, target).Order("role_key").Limit(len(authz.SystemRoles()) + 1).Scan(&roles).Error; err != nil {
		return err
	}
	if len(roles) < 1 || len(roles) > len(authz.SystemRoles()) {
		return authz.ErrScopedCommandDenied
	}
	for _, role := range roles {
		if !authz.KnownRole(authz.Role(role)) {
			return authz.ErrScopedCommandDenied
		}
		switch authz.Role(role) {
		case authz.RoleTenantAdmin, authz.RoleGovernanceDelegate, authz.RolePharmacyOwner, authz.RoleSupplyOwner, authz.RoleSupplyAdmin:
			return authz.ErrScopedCommandDenied
		}
	}
	return nil
}

func (h *staffMFAResetService) recheck(ctx context.Context, p authn.Principal, in customercontract.StaffMFARecoveryRequest, source platform.StaffMFATargetReader) error {
	if source == nil {
		return staffclient.ErrUnavailable
	}
	target, err := source(ctx, in.Intent)
	if err != nil {
		return mfaAuthorityError(err)
	}
	if target != in.Target {
		return staffclient.ErrConflict
	}
	return h.authorize(ctx, p, target.Subject, uuid.MustParse(in.Intent.CommandID))
}

func (h *staffMFAResetService) Reset(ctx context.Context, p authn.Principal, intent customercontract.StaffMFAResetInput, source platform.StaffMFATargetReader) (result customercontract.StaffMFARecoveryReceipt, resultErr error) {
	stage := "validate"
	defer func() {
		if errors.Is(resultErr, staffclient.ErrUnavailable) {
			slog.WarnContext(ctx, "Staff authenticator recovery dependency unavailable", "stage", stage)
		}
	}()
	var empty customercontract.StaffMFARecoveryReceipt
	if err := h.validate(p); err != nil {
		return empty, err
	}
	if intent.Validate() != nil {
		return empty, staffclient.ErrInvalid
	}
	ctx, cancel := staffMFAResetContext(ctx, p)
	defer cancel()
	id := uuid.MustParse(intent.CommandID)
	stage = "authorize_actor"
	if err := h.authorize(ctx, p, "", id); err != nil {
		return empty, err
	}
	if source == nil {
		return empty, staffclient.ErrUnavailable
	}
	stage = "read_staff"
	target, err := source(ctx, intent)
	if err != nil {
		return empty, mfaAuthorityError(err)
	}
	in := customercontract.StaffMFARecoveryRequest{ActorSubject: p.Subject, Target: target, Intent: intent}
	if target.Subject == p.Subject {
		return empty, authz.ErrScopedCommandDenied
	}
	if target.TenantID != p.TenantID {
		return empty, staffclient.ErrNotFound
	}
	if in.Validate() != nil {
		return empty, staffclient.ErrUnavailable
	}
	stage = "authorize_target"
	if err = h.authorize(ctx, p, target.Subject, id); err != nil {
		return empty, err
	}
	stage = "retain_intent"
	cancelled, err := h.remember(ctx, p, in)
	if err != nil {
		return empty, err
	}
	if cancelled {
		return h.Cancel(ctx, p, intent.CommandID, staffMFAScope(in))
	}
	scope := staffclient.Scope{TenantID: uuid.MustParse(p.TenantID), CommandID: id, ActorSubject: p.Subject}
	stage = "prepare_owner"
	receipt, err := h.owner.PrepareMFA(ctx, scope, in)
	if err != nil {
		return empty, err
	}
	stage = "recheck_before_fence"
	if err = h.recheck(ctx, p, in, source); err != nil {
		return empty, err
	}
	if receipt.State == "prepared" {
		stage = "commit_fence"
		if err = h.fence(ctx, p, in); err != nil {
			if errors.Is(err, errStaffMFACancelRequested) {
				return h.Cancel(ctx, p, intent.CommandID, staffMFAScope(in))
			}
			return empty, err
		}
		stage = "recheck_before_dispatch"
		if err = h.recheck(ctx, p, in, source); err != nil {
			return empty, err
		}
		stage = "dispatch_owner"
		receipt, err = h.owner.DispatchMFA(ctx, scope, customercontract.StaffMFADispatchRequest{Request: in, ApplicationFenceID: intent.CommandID})
		if err != nil {
			return empty, err
		}
	}
	stage = "recheck_after_dispatch"
	if err = h.recheck(ctx, p, in, source); err != nil {
		return empty, err
	}
	stage = "forget_removed_keys"
	if err = h.forgetRemovedKeys(ctx, p, intent.CommandID, receipt.State); err != nil {
		return empty, err
	}
	return receipt, nil
}

// forgetRemovedKeys deletes the target's iam_security_keys rows once the owner
// has confirmed the native removal of every key (#1905). Only rows registered
// before the fence cutoff go: a later row is the replacement the person
// enrolled, so this is safe on every confirmed reset or status read. A
// prepared, cancelled or unconfirmed reset keeps them, because those keys may
// still be READY in Zitadel and deleting them would lock the person out. A
// row this misses can never count for another key: Contains matches only the
// ids Zitadel reports READY, and Zitadel never reuses one.
func (h *staffMFAResetService) forgetRemovedKeys(ctx context.Context, p authn.Principal, command, state string) error {
	if state != "awaiting_enrollment" && state != "completed" {
		return nil
	}
	err := h.db.WithTenant(ctx, p.TenantID, func(tx *gorm.DB) error {
		return tx.Exec(`DELETE FROM iam_security_keys k USING iam_staff_mfa_fences f
WHERE f.tenant_id=? AND f.command_id=? AND k.subject=f.target_subject AND k.registered_at<=f.revoked_at`, p.TenantID, command).Error
	})
	if err != nil {
		return fmt.Errorf("forget removed security keys: %w: %w", staffclient.ErrUnavailable, err)
	}
	return nil
}

func (h *staffMFAResetService) Read(ctx context.Context, p authn.Principal, command string, scope platform.StaffMFARecoveryScope, source platform.StaffMFATargetReader) (customercontract.StaffMFARecoveryReceipt, error) {
	var empty customercontract.StaffMFARecoveryReceipt
	if err := h.validate(p); err != nil {
		return empty, err
	}
	id, ok := staffAccountID(command)
	if !ok || !validStaffMFAScope(scope) {
		return empty, staffclient.ErrInvalid
	}
	ctx, cancel := staffMFAResetContext(ctx, p)
	defer cancel()
	if err := h.authorize(ctx, p, "", id); err != nil {
		return empty, err
	}
	held, err := h.loadIntent(ctx, p, command, scope)
	if err != nil {
		return empty, err
	}
	if len(held.Request) == 0 {
		if err = h.authorize(ctx, p, "", id); err != nil {
			return empty, err
		}
		return customercontract.StaffMFARecoveryReceipt{CommandID: command, State: "cancelled"}, nil
	}
	original, err := held.decode()
	if err != nil {
		return empty, err
	}
	snapshot, err := h.owner.GetMFA(ctx, staffclient.Scope{TenantID: uuid.MustParse(p.TenantID), CommandID: id, ActorSubject: p.Subject})
	if errors.Is(err, staffclient.ErrNotFound) {
		return empty, staffclient.ErrUnavailable
	}
	if err != nil {
		return empty, err
	}
	if snapshot.Request != original {
		return empty, staffclient.ErrUnavailable
	}
	if snapshot.Receipt.State == "cancelled" {
		err = h.authorize(ctx, p, "", id)
	} else {
		err = h.recheck(ctx, p, original, source)
	}
	if err != nil {
		return empty, err
	}
	if err = h.forgetRemovedKeys(ctx, p, command, snapshot.Receipt.State); err != nil {
		return empty, err
	}
	return snapshot.Receipt, nil
}

func (h *staffMFAResetService) fence(ctx context.Context, p authn.Principal, in customercontract.StaffMFARecoveryRequest) error {
	_, fingerprint, err := staffMFAFingerprint(in)
	if err != nil {
		return err
	}
	err = h.db.WithTenant(ctx, p.TenantID, func(tx *gorm.DB) error {
		if err := membershipActor(tx, p, in.Target.Subject); err != nil {
			return err
		}
		if err := ordinaryMFATarget(tx, p, in.Target.Subject); err != nil {
			return err
		}
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, "iam.staff-mfa-fence:"+p.TenantID+":"+in.Intent.CommandID).Error; err != nil {
			return err
		}
		cancelled, err := staffMFACancellation(tx, p, in.Intent.CommandID)
		if err != nil {
			return err
		}
		if cancelled {
			return errStaffMFACancelRequested
		}
		var held struct{ Fingerprint string }
		found := tx.Table("iam_staff_mfa_fences").Select("fingerprint").Where("tenant_id=? AND command_id=?", p.TenantID, in.Intent.CommandID).Take(&held).Error
		if found == nil {
			if held.Fingerprint != fingerprint {
				return staffclient.ErrConflict
			}
			return nil
		}
		if !errors.Is(found, gorm.ErrRecordNotFound) {
			return found
		}
		var cutoff time.Time
		if err := tx.Raw(`SELECT clock_timestamp()`).Scan(&cutoff).Error; err != nil {
			return err
		}
		if err := h.revocations.writeRevocation(tx, in.Target.Subject, cutoff, "admin_revoke", p.Subject); err != nil {
			return err
		}
		if err := tx.Exec(`SELECT set_config('app.actor_subject',?,true)`, p.Subject).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO iam_staff_mfa_fences(tenant_id,command_id,target_subject,actor_subject,fingerprint,revoked_at) VALUES(?,?,?,?,?,?)`, p.TenantID, in.Intent.CommandID, in.Target.Subject, p.Subject, fingerprint, cutoff).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,?,'staff_mfa_reset_fenced','iam_staff_mfa_fences',?)`, p.TenantID, p.Subject, in.Intent.CommandID).Error
	})
	if err == nil {
		h.revocations.checker.Invalidate(in.Target.Subject)
	}
	return mfaAuthorityError(err)
}

func staffMFAResetContext(ctx context.Context, p authn.Principal) (context.Context, context.CancelFunc) {
	until := time.Now().Add(30 * time.Second)
	if p.IdleDeadline.Before(until) {
		until = p.IdleDeadline
	}
	return context.WithDeadline(ctx, until)
}

// Cancel never removes a native factor or restores a revoked application credential.
func (h *staffMFAResetService) Cancel(ctx context.Context, p authn.Principal, command string, scope platform.StaffMFARecoveryScope) (customercontract.StaffMFARecoveryReceipt, error) {
	var empty customercontract.StaffMFARecoveryReceipt
	if err := h.validate(p); err != nil {
		return empty, err
	}
	id, ok := staffAccountID(command)
	if !ok || !validStaffMFAScope(scope) {
		return empty, staffclient.ErrInvalid
	}
	ctx, cancel := staffMFAResetContext(ctx, p)
	defer cancel()
	if err := h.authorize(ctx, p, "", id); err != nil {
		return empty, err
	}
	held, err := h.requestCancellation(ctx, p, command, scope)
	if err != nil {
		return empty, err
	}
	if len(held.Request) == 0 {
		if err = h.authorize(ctx, p, "", id); err != nil {
			return empty, err
		}
		return customercontract.StaffMFARecoveryReceipt{CommandID: command, State: "cancelled"}, nil
	}
	original, err := held.decode()
	if err != nil {
		return empty, err
	}
	if err = h.authorize(ctx, p, "", id); err != nil {
		return empty, err
	}
	receipt, err := h.owner.CancelMFA(ctx, staffclient.Scope{TenantID: uuid.MustParse(p.TenantID), CommandID: id, ActorSubject: p.Subject}, original)
	if err != nil {
		return empty, err
	}
	if err = h.authorize(ctx, p, "", id); err != nil {
		return empty, err
	}
	return receipt, nil
}

func staffMFAFingerprint(in customercontract.StaffMFARecoveryRequest) ([]byte, string, error) {
	if in.Validate() != nil {
		return nil, "", staffclient.ErrInvalid
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return nil, "", staffclient.ErrInvalid
	}
	hash := sha256.Sum256(raw)
	return raw, hex.EncodeToString(hash[:]), nil
}
