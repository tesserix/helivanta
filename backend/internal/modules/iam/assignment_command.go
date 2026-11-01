package iam

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authz"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (h *assignmentAuthority) WithScopeGrant(ctx context.Context, r authz.ScopeRequest, grant authz.ScopeGrant, authTime time.Time, action func(context.Context) error) error {
	return h.WithScopeGrants(ctx, []authz.ScopeCheck{{Request: r, Grant: grant}}, authTime, action)
}

func (h *assignmentAuthority) WithScopeGrants(ctx context.Context, checks []authz.ScopeCheck, authTime time.Time, action func(context.Context) error) error {
	if h.db == nil || h.graph == nil || action == nil || authTime.IsZero() || !validScopeChecks(checks) {
		return authz.ErrScopedAuthorizationUnavailable
	}
	checks = slices.Clone(checks)
	slices.SortFunc(checks, func(a, b authz.ScopeCheck) int { return strings.Compare(a.Grant.AssignmentID, b.Grant.AssignmentID) })
	r := checks[0].Request
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
		if err := lockScopeChecks(tx, checks); err != nil {
			return err
		}
		expires, err := currentScopeChecks(tx, checks)
		if err != nil {
			return err
		}
		expiryTimer = time.AfterFunc(time.Until(expires), func() { cancelExpiry(authz.ErrScopedCommandDenied) })
		return nil
	}, func(commandCtx context.Context) error {
		if err := action(commandCtx); err != nil {
			return err
		}
		return h.db.WithTenantCommand(commandCtx, r.TenantID, func(tx *gorm.DB) error {
			_, err := currentScopeChecks(tx, checks)
			return err
		})
	})
	if err != nil && errors.Is(context.Cause(ctx), authz.ErrScopedCommandDenied) {
		return authz.ErrScopedCommandDenied
	}
	return err
}

func validScopeChecks(checks []authz.ScopeCheck) bool {
	if len(checks) < 1 || len(checks) > 2 {
		return false
	}
	first := checks[0]
	for i, check := range checks {
		r, grant := check.Request, check.Grant
		if !proposalSubject(r.Subject) || !staffAccessPermissionSupported(r.Permission) || grant.Version <= 0 || grant.ScopeEpoch <= 0 || grant.MembershipEpoch <= 0 {
			return false
		}
		for _, value := range []string{r.TenantID, r.HospitalID, grant.AssignmentID} {
			if id, err := uuid.Parse(value); err != nil || id == uuid.Nil || id.String() != value {
				return false
			}
		}
		if r.TenantID != first.Request.TenantID || r.Subject != first.Request.Subject || r.HospitalID != first.Request.HospitalID || grant.ScopeEpoch != first.Grant.ScopeEpoch || grant.MembershipEpoch != first.Grant.MembershipEpoch {
			return false
		}
		if i > 0 && (r.Permission == first.Request.Permission || (grant.AssignmentID == first.Grant.AssignmentID && grant != first.Grant)) {
			return false
		}
	}
	return true
}

func lockScopeChecks(tx *gorm.DB, checks []authz.ScopeCheck) error {
	r := checks[0].Request
	ids := make([]string, 0, len(checks))
	for _, check := range checks {
		if len(ids) == 0 || ids[len(ids)-1] != check.Grant.AssignmentID {
			ids = append(ids, check.Grant.AssignmentID)
		}
	}
	var proposals []struct{ ID uuid.UUID }
	// Match withdrawal/activation order: all active proposals, membership, scope.
	if err := tx.Table("iam_assignment_proposals").Select("id").Where("tenant_id=? AND hospital_id=? AND subject=? AND status='active' AND id IN ?", r.TenantID, r.HospitalID, r.Subject, ids).Order("id").Clauses(clause.Locking{Strength: "SHARE"}).Find(&proposals).Error; err != nil {
		return err
	}
	if len(proposals) != len(ids) {
		return authz.ErrScopedCommandDenied
	}
	epoch, err := proposalMembershipEpoch(tx, r.TenantID, r.Subject)
	if err != nil {
		return err
	}
	if epoch != checks[0].Grant.MembershipEpoch {
		return authz.ErrScopedCommandDenied
	}
	var scope struct{ Epoch, AcknowledgedEpoch int64 }
	err = tx.Table("iam_assignment_scope_epochs").Select("epoch,acknowledged_epoch").Where("tenant_id=? AND hospital_id=? AND subject=?", r.TenantID, r.HospitalID, r.Subject).Clauses(clause.Locking{Strength: "SHARE"}).Take(&scope).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return authz.ErrScopedCommandDenied
	}
	if err != nil {
		return err
	}
	if scope.Epoch != checks[0].Grant.ScopeEpoch || scope.AcknowledgedEpoch != scope.Epoch {
		return authz.ErrScopedCommandDenied
	}
	return nil
}

func currentScopeChecks(tx *gorm.DB, checks []authz.ScopeCheck) (time.Time, error) {
	var earliest time.Time
	for _, check := range checks {
		var current []struct{ ExpiresAt time.Time }
		grant := check.Grant
		if err := authorityCandidates(tx, check.Request).Select("s.expires_at").Where("s.id=? AND s.version=? AND s.membership_epoch=? AND q.epoch=?", grant.AssignmentID, grant.Version, grant.MembershipEpoch, grant.ScopeEpoch).Find(&current).Error; err != nil {
			return time.Time{}, err
		}
		if len(current) != 1 {
			return time.Time{}, authz.ErrScopedCommandDenied
		}
		if earliest.IsZero() || current[0].ExpiresAt.Before(earliest) {
			earliest = current[0].ExpiresAt
		}
	}
	return earliest, nil
}
