package iam

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
)

var errRevocationScopeUnavailable = errors.New("current revocation scope unavailable")
var errRevocationActorNotAdmin = errors.New("current revocation administrator missing")

func (h *revocationHandlers) revokeCurrentMember(ctx context.Context, actor authn.Principal, target string) error {
	if actor.AuthTime.IsZero() || actor.IdleDeadline.IsZero() || !time.Now().Before(actor.IdleDeadline) {
		return authz.ErrScopedCredentialRevoked
	}
	deadline := time.Now().Add(5 * time.Second)
	if actor.IdleDeadline.Before(deadline) {
		deadline = actor.IdleDeadline
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	checked := false
	err := h.db.WithTenant(ctx, actor.TenantID, func(tx *gorm.DB) error {
		if err := lockRevocationCredentials(tx, actor.Subject, target); err != nil {
			return fmt.Errorf("%w: %w", errRevocationScopeUnavailable, err)
		}
		var revoked int64
		if err := tx.Table("iam_credential_revocations").Where("subject=? AND revoked_at>=?", actor.Subject, actor.AuthTime).Count(&revoked).Error; err != nil {
			return fmt.Errorf("%w: %w", errRevocationScopeUnavailable, err)
		}
		if revoked > 0 || !time.Now().Before(actor.IdleDeadline) {
			return authz.ErrScopedCredentialRevoked
		}
		var roles []struct {
			Subject string
			RoleKey authz.Role
		}
		bound := len(authz.SystemRoles()) + 1
		err := tx.Table("iam_members").Select("subject,role_key").Where("tenant_id=? AND ((subject=? AND role_key=?) OR (subject=? AND role_key IN ?))", actor.TenantID, actor.Subject, authz.RoleTenantAdmin, target, authz.SystemRoles()).
			Order("subject,role_key").Limit(bound + 1).Clauses(clause.Locking{Strength: "SHARE"}).Find(&roles).Error
		if err != nil {
			return fmt.Errorf("%w: %w", errRevocationScopeUnavailable, err)
		}
		if len(roles) > bound {
			return errRevocationScopeUnavailable
		}
		var administrator, member bool
		for _, role := range roles {
			administrator = administrator || role.Subject == actor.Subject && role.RoleKey == authz.RoleTenantAdmin
			member = member || role.Subject == target
		}
		if !administrator {
			return errRevocationActorNotAdmin
		}
		if !member {
			return gorm.ErrRecordNotFound
		}
		if !time.Now().Before(actor.IdleDeadline) {
			return authz.ErrScopedCredentialRevoked
		}
		checked = true
		return h.writeRevocation(tx, target, time.Now().UTC(), "admin_revoke", actor.Subject)
	})
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) && !time.Now().Before(actor.IdleDeadline) {
			return authz.ErrScopedCredentialRevoked
		}
		if !checked && !errors.Is(err, gorm.ErrRecordNotFound) && !errors.Is(err, errRevocationActorNotAdmin) && !errors.Is(err, authz.ErrScopedCredentialRevoked) {
			return fmt.Errorf("%w: %w", errRevocationScopeUnavailable, err)
		}
		return err
	}
	h.checker.Invalidate(target)
	return nil
}

func lockRevocationCredentials(tx *gorm.DB, actor, target string) error {
	subjects := []string{actor}
	if target != actor {
		subjects = append(subjects, target)
	}
	slices.Sort(subjects)
	for _, subject := range subjects {
		statement := `SELECT pg_advisory_xact_lock_shared(hashtextextended('iam.credential:' || ?,0))`
		if subject == target {
			statement = `SELECT pg_advisory_xact_lock(hashtextextended('iam.credential:' || ?,0))`
		}
		if err := tx.Exec(statement, subject).Error; err != nil {
			return err
		}
	}
	return nil
}
