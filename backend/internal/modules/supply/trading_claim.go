package supply

import (
	"context"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/suppliertrade"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"io"
	"net/http"
	"time"
	"unicode/utf8"
)

type tradeClaimCaller interface {
	Verify(context.Context, string) error
	Subject() string
}

// NewTradingSourceCheckHandler returns local owner facts, never trading or user authority.
func NewTradingSourceCheckHandler(deps platform.Deps, caller tradeClaimCaller) gin.HandlerFunc {
	return func(c *gin.Context) {
		if deps.DB == nil || deps.TradingEndpoints == nil || caller == nil {
			respond.Error(c, 503, "source_check_unavailable", "Current source verification is not configured.")
			return
		}
		ctx, cancel, ok := tradeCallerContext(c, caller)
		if !ok {
			return
		}
		defer cancel()
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
		raw, err := io.ReadAll(c.Request.Body)
		if err != nil || !utf8.Valid(raw) {
			respond.BadRequest(c, errors.New("invalid source-check body"))
			return
		}
		var input suppliertrade.SourceCheckInput
		if suppliertrade.DecodeObject(raw, &input) != nil || input.RequestID.Version() != 7 || input.RequestID.Variant() != uuid.RFC4122 || !suppliertrade.ValidEndpointUse(input.Endpoint, input.Category, input.Operation) {
			respond.BadRequest(c, errors.New("provide one exact source scope"))
			return
		}
		result := suppliertrade.SourceCheckResult{RequestID: input.RequestID, VerificationKind: "local_source_eligibility", ObservedAt: time.Now().UTC()}
		actor := "system:supplier-trade-current"
		err = deps.DB.WithGuardedTenantCommand(tenantdb.WithActor(ctx, actor), input.Endpoint.TenantID.String(), time.Now().Add(3*time.Second), func(*gorm.DB) error { return nil }, func(ctx context.Context) error {
			return deps.TradingEndpoints.WithCurrentTradingEndpoint(ctx, input.Endpoint, input.Category, input.Operation, func(ctx context.Context, ref suppliertrade.EndpointReference) error {
				result.Eligible = true
				result.Reference = &ref
				return deps.DB.WithTenantCommand(ctx, input.Endpoint.TenantID.String(), func(tx *gorm.DB) error {
					return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,'current_trading_source_read','trading_source',?)`, input.Endpoint.TenantID, actor, input.Endpoint.LocationID).Error
				})
			})
		})
		if errors.Is(err, suppliertrade.ErrIneligible) {
			result.Eligible = false
			result.Reference = nil
		} else if err != nil {
			respond.Error(c, 503, "source_check_unavailable", "The current source could not be verified.")
			return
		}
		result.ObservedAt = time.Now().UTC()
		c.Header("Cache-Control", "no-store")
		respond.OK(c, result)
	}
}
