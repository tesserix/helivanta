package iam

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (h *assignmentAuthority) WithScopeGrant(ctx context.Context, r authz.ScopeRequest, grant authz.ScopeGrant, authTime time.Time, action func(context.Context) error) error {
	if h.db == nil || h.graph == nil || action == nil || authTime.IsZero() || r.Subject == "" || !staffAccessPermissionSupported(r.Permission) || grant.Version <= 0 || grant.ScopeEpoch <= 0 || grant.MembershipEpoch <= 0 {
		return authz.ErrScopedAuthorizationUnavailable
	}
	for _, value := range []string{r.TenantID, r.HospitalID, grant.AssignmentID} {
		if id, err := uuid.Parse(value); err != nil || id.String() != value {
			return authz.ErrScopedAuthorizationUnavailable
		}
	}
	ctx, cancelBudget := context.WithTimeout(ctx, 5*time.Second)
	defer cancelBudget()
	ctx, cancelExpiry := context.WithCancelCause(ctx)
	defer cancelExpiry(nil)
	var expiryTimer *time.Timer
	defer func() {
		if expiryTimer != nil {
			expiryTimer.Stop()
		}
	}()
	until, _ := ctx.Deadline()
	err := h.db.WithGuardedTenantCommand(ctx, r.TenantID, until, func(tx *gorm.DB) error {
		// The subject lock also covers the absence of a revocation watermark row.
		if err := tx.Exec(`SELECT pg_advisory_xact_lock_shared(hashtextextended(?,0))`, "iam.credential:"+r.Subject).Error; err != nil {
			return err
		}
		var revoked bool
		if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM iam_credential_revocations WHERE subject=? AND revoked_at>=?)`, r.Subject, authTime).Scan(&revoked).Error; err != nil {
			return err
		}
		if revoked {
			return authz.ErrScopedCredentialRevoked
		}
		if err := assignmentAccess(tx); err != nil {
			return err
		}
		var current []struct {
			ExpiresAt time.Time
		}
		query := authorityCandidates(tx, r).Select("s.expires_at").Where("s.id=? AND s.version=? AND s.membership_epoch=? AND q.epoch=?", grant.AssignmentID, grant.Version, grant.MembershipEpoch, grant.ScopeEpoch)
		if err := query.Clauses(clause.Locking{Strength: "SHARE", Table: clause.Table{Name: "p,m,q", Raw: true}}).Find(&current).Error; err != nil {
			return err
		}
		if len(current) != 1 {
			return authz.ErrScopedCommandDenied
		}
		expiryTimer = time.AfterFunc(time.Until(current[0].ExpiresAt), func() { cancelExpiry(authz.ErrScopedCommandDenied) })
		return nil
	}, func(commandCtx context.Context) error {
		if err := action(commandCtx); err != nil {
			return err
		}
		return h.db.WithTenantCommand(commandCtx, r.TenantID, func(tx *gorm.DB) error {
			var valid bool
			if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM iam_assignment_snapshots WHERE tenant_id=? AND id=? AND version=? AND expires_at>clock_timestamp())`, r.TenantID, grant.AssignmentID, grant.Version).Scan(&valid).Error; err != nil {
				return err
			}
			if !valid {
				return authz.ErrScopedCommandDenied
			}
			return nil
		})
	})
	if err != nil && errors.Is(context.Cause(ctx), authz.ErrScopedCommandDenied) {
		return authz.ErrScopedCommandDenied
	}
	return err
}
