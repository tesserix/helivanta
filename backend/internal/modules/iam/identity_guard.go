package iam

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/modules/iam/loginclient"
	"github.com/tesserix/helivanta/internal/platform/requestid"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/audit"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// NewIdentityGuard requires current identity state before protected traffic (ADR-0047).
func NewIdentityGuard(db *tenantdb.DB, bus *events.Bus, checker *RevocationChecker, provider UserStateChecker) gin.HandlerFunc {
	return newIdentityGuard(db, bus, checker, provider, time.Now)
}

func newIdentityGuard(db *tenantdb.DB, bus *events.Bus, checker *RevocationChecker, provider UserStateChecker, now func() time.Time) gin.HandlerFunc {
	revocations := &revocationHandlers{db: db, bus: bus, checker: checker}
	return func(c *gin.Context) {
		p, ok := authn.PrincipalFrom(c)
		if !ok || p.AuthTime.IsZero() {
			respond.Unauthenticated(c, "missing authenticated identity")
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()
		unavailable := func(err error) {
			requestid.Logger(c).ErrorContext(ctx, "current identity check unavailable", "err", err)
			respond.Error(c, http.StatusServiceUnavailable, "identity_unavailable", "could not verify account status")
		}
		if db == nil || bus == nil || checker == nil {
			unavailable(errors.New("identity guard dependency missing"))
			return
		}
		admit := func() {
			var revoked bool
			if err := db.WithSystem(ctx, func(tx *gorm.DB) error {
				return tx.Raw(`SELECT EXISTS(SELECT 1 FROM iam_credential_revocations WHERE subject=? AND revoked_at>=?)`, p.Subject, p.AuthTime).Scan(&revoked).Error
			}); err != nil {
				unavailable(err)
				return
			}
			if revoked {
				respond.Unauthenticated(c, "credential revoked")
				return
			}
			c.Next()
		}
		if c.Request.Method == http.MethodPost && c.FullPath() == "/v1/iam/me/sign-out" {
			admit()
			return
		}
		if provider == nil {
			unavailable(errors.New("identity provider missing"))
			return
		}
		observedAt := now().UTC()
		state, err := provider.UserState(ctx, p.Subject)
		if err == nil {
			err = ctx.Err()
		}
		if err != nil {
			unavailable(err)
			return
		}
		switch state {
		case loginclient.UserStateActive:
			admit()
		case loginclient.UserStateInactive:
			if err := revocations.revoke(ctx, p.Subject, observedAt, "identity_inactive", "system:identity-state"); err != nil {
				unavailable(err)
				return
			}
			audit.Log(ctx, requestid.Logger(c), slog.LevelWarn, "auth.identity.inactive", "inactive identity credential revoked",
				"actor_subject", "system:identity-state", "target_subject", p.Subject, "tenant_id", p.TenantID, "revoked_at", observedAt)
			respond.Unauthenticated(c, "account is no longer active")
		default:
			unavailable(errors.New("identity state unrecognized"))
		}
	}
}
