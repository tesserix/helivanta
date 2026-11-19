package iam

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/onboarding-service/api/customercontract"
	"github.com/tesserix/onboarding-service/api/staffclient"
	"gorm.io/gorm"
)

var errStaffMFACancelRequested = errors.New("staff MFA cancellation requested")

type staffMFAIntent struct {
	TenantID, ActorSubject, EmployerKind string
	EmployerID, StaffID                  uuid.UUID
	Fingerprint                          string
	Request                              []byte
}

func validStaffMFAScope(scope platform.StaffMFARecoveryScope) bool {
	return scope.EmployerID != uuid.Nil && scope.StaffID != uuid.Nil && (scope.EmployerKind == "hospital" || scope.EmployerKind == "pharmacy" || scope.EmployerKind == "supplier")
}
func staffMFAScope(in customercontract.StaffMFARecoveryRequest) platform.StaffMFARecoveryScope {
	return platform.StaffMFARecoveryScope{EmployerKind: in.Target.EmployerKind, EmployerID: uuid.MustParse(in.Target.EmployerID), StaffID: uuid.MustParse(in.Target.StaffID)}
}
func (held staffMFAIntent) matches(p authn.Principal, scope platform.StaffMFARecoveryScope) bool {
	return held.ActorSubject == p.Subject && held.EmployerKind == scope.EmployerKind && held.EmployerID == scope.EmployerID && held.StaffID == scope.StaffID
}
func staffMFACommandLock(tx *gorm.DB, tenant, command string) error {
	return tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, "iam.staff-mfa-fence:"+tenant+":"+command).Error
}
func readStaffMFAIntent(tx *gorm.DB, p authn.Principal, command string) (staffMFAIntent, error) {
	var held staffMFAIntent
	err := tx.Table("iam_staff_mfa_commands").Select("tenant_id,actor_subject,employer_kind,employer_id,staff_id,fingerprint,request").Where("tenant_id=? AND command_id=?", p.TenantID, command).Take(&held).Error
	return held, err
}
func (held staffMFAIntent) decode() (customercontract.StaffMFARecoveryRequest, error) {
	var in customercontract.StaffMFARecoveryRequest
	if len(held.Request) == 0 || len(held.Request) > 8192 || json.Unmarshal(held.Request, &in) != nil || in.Validate() != nil {
		return in, staffclient.ErrUnavailable
	}
	if in.ActorSubject != held.ActorSubject || in.Target.TenantID != held.TenantID || in.Target.EmployerKind != held.EmployerKind || in.Target.EmployerID != held.EmployerID.String() || in.Target.StaffID != held.StaffID.String() {
		return in, staffclient.ErrUnavailable
	}
	_, fingerprint, err := staffMFAFingerprint(in)
	if err != nil || fingerprint != held.Fingerprint {
		return in, staffclient.ErrUnavailable
	}
	return in, nil
}
func staffMFACancellation(tx *gorm.DB, p authn.Principal, command string) (bool, error) {
	var found bool
	err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM iam_staff_mfa_cancellations WHERE tenant_id=? AND command_id=? AND actor_subject=?)`, p.TenantID, command, p.Subject).Scan(&found).Error
	return found, err
}

// remember commits the original canonical intent before any remote preparation.
func (h *staffMFAResetService) remember(ctx context.Context, p authn.Principal, in customercontract.StaffMFARecoveryRequest) (bool, error) {
	raw, fingerprint, err := staffMFAFingerprint(in)
	if err != nil {
		return false, err
	}
	cancelled := false
	err = h.db.WithTenant(ctx, p.TenantID, func(tx *gorm.DB) error {
		if err := membershipActor(tx, p, in.Target.Subject); err != nil {
			return err
		}
		if err := ordinaryMFATarget(tx, p, in.Target.Subject); err != nil {
			return err
		}
		if err := staffMFACommandLock(tx, p.TenantID, in.Intent.CommandID); err != nil {
			return err
		}
		held, err := readStaffMFAIntent(tx, p, in.Intent.CommandID)
		if err == nil {
			if !held.matches(p, staffMFAScope(in)) {
				return staffclient.ErrNotFound
			}
			if len(held.Request) != 0 && held.Fingerprint != fingerprint {
				return staffclient.ErrConflict
			}
			cancelled, err = staffMFACancellation(tx, p, in.Intent.CommandID)
			if err != nil {
				return err
			}
			if len(held.Request) == 0 && !cancelled {
				return staffclient.ErrUnavailable
			}
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.Exec(`SELECT set_config('app.actor_subject',?,true)`, p.Subject).Error; err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO iam_staff_mfa_commands(tenant_id,command_id,actor_subject,employer_kind,employer_id,staff_id,fingerprint,request) VALUES(?,?,?,?,?,?,?,?::jsonb)`, p.TenantID, in.Intent.CommandID, p.Subject, in.Target.EmployerKind, in.Target.EmployerID, in.Target.StaffID, fingerprint, string(raw)).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,?,'staff_mfa_intent_retained','iam_staff_mfa_commands',?)`, p.TenantID, p.Subject, in.Intent.CommandID).Error
	})
	return cancelled, mfaAuthorityError(err)
}

func (h *staffMFAResetService) loadIntent(ctx context.Context, p authn.Principal, command string, scope platform.StaffMFARecoveryScope) (staffMFAIntent, error) {
	var held staffMFAIntent
	err := h.db.WithTenant(ctx, p.TenantID, func(tx *gorm.DB) error {
		if err := membershipActor(tx, p, ""); err != nil {
			return err
		}
		var err error
		held, err = readStaffMFAIntent(tx, p, command)
		if err != nil {
			return err
		}
		if !held.matches(p, scope) {
			return staffclient.ErrNotFound
		}
		if len(held.Request) == 0 {
			cancelled, err := staffMFACancellation(tx, p, command)
			if err != nil {
				return err
			}
			if !cancelled {
				return staffclient.ErrUnavailable
			}
		}
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,?,'staff_mfa_intent_read','iam_staff_mfa_commands',?)`, p.TenantID, p.Subject, command).Error
	})
	return held, mfaAuthorityError(err)
}

// requestCancellation also tombstones a command that has not reached preparation.
func (h *staffMFAResetService) requestCancellation(ctx context.Context, p authn.Principal, command string, scope platform.StaffMFARecoveryScope) (staffMFAIntent, error) {
	var held staffMFAIntent
	err := h.db.WithTenant(ctx, p.TenantID, func(tx *gorm.DB) error {
		if err := membershipActor(tx, p, ""); err != nil {
			return err
		}
		if err := staffMFACommandLock(tx, p.TenantID, command); err != nil {
			return err
		}
		if err := tx.Exec(`SELECT set_config('app.actor_subject',?,true)`, p.Subject).Error; err != nil {
			return err
		}
		var err error
		held, err = readStaffMFAIntent(tx, p, command)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			held = staffMFAIntent{TenantID: p.TenantID, ActorSubject: p.Subject, EmployerKind: scope.EmployerKind, EmployerID: scope.EmployerID, StaffID: scope.StaffID}
			if err = tx.Exec(`INSERT INTO iam_staff_mfa_commands(tenant_id,command_id,actor_subject,employer_kind,employer_id,staff_id,fingerprint,request) VALUES(?,?,?,?,?,?,'',NULL)`, p.TenantID, command, p.Subject, scope.EmployerKind, scope.EmployerID, scope.StaffID).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if !held.matches(p, scope) {
			return staffclient.ErrNotFound
		}
		if err = tx.Exec(`INSERT INTO iam_staff_mfa_cancellations(tenant_id,command_id,actor_subject) VALUES(?,?,?) ON CONFLICT(tenant_id,command_id) DO NOTHING`, p.TenantID, command, p.Subject).Error; err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,?,'staff_mfa_cancel_requested','iam_staff_mfa_commands',?)`, p.TenantID, p.Subject, command).Error
	})
	return held, mfaAuthorityError(err)
}
