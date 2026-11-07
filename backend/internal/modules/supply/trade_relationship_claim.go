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
	"github.com/tesserix/helivanta/pkg/workloadidentity"
	"gorm.io/gorm"
	"io"
	"net/http"
	"time"
)

func tradeCallerContext(c *gin.Context, caller tradeClaimCaller) (context.Context, context.CancelFunc, bool) {
	c.Header("Cache-Control", "no-store")
	if caller == nil {
		respond.Error(c, 503, "peer_unavailable", "Current peer verification is not configured.")
		return nil, nil, false
	}
	values := c.Request.Header.Values(suppliertrade.SourceCallerHeader)
	if len(values) != 1 || len(c.Request.Header.Values("Authorization")) != 0 || len(c.Request.Header.Values("Cookie")) != 0 || c.Request.URL.RawQuery != "" || c.ContentType() != "application/json" {
		respond.Error(c, 401, "caller_required", "A dedicated authenticated peer caller is required.")
		return nil, nil, false
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	if err := caller.Verify(ctx, values[0]); err != nil {
		cancel()
		if errors.Is(err, workloadidentity.ErrRejected) {
			respond.Error(c, 401, "caller_rejected", "The peer caller was rejected.")
		} else {
			respond.Error(c, 503, "caller_unavailable", "The peer caller could not be verified.")
		}
		return nil, nil, false
	}
	return ctx, cancel, true
}

// NewRelationshipCheckHandler exposes current consent only; source, actor and object gates remain separate.
func NewRelationshipCheckHandler(deps platform.Deps, caller tradeClaimCaller) gin.HandlerFunc {
	return func(c *gin.Context) {
		if deps.DB == nil {
			respond.Error(c, 503, "relationship_unavailable", "The current relationship owner is unavailable.")
			return
		}
		ctx, cancel, ok := tradeCallerContext(c, caller)
		if !ok {
			return
		}
		defer cancel()
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
		raw, err := io.ReadAll(c.Request.Body)
		var input suppliertrade.RelationshipCheckInput
		if err != nil || suppliertrade.DecodeObject(raw, &input) != nil || !suppliertrade.ValidRelationshipCheck(input) || input.RequestID.Version() != 7 || input.RequestID.Variant() != uuid.RFC4122 {
			respond.BadRequest(c, suppliertrade.ErrInvalid)
			return
		}
		result := suppliertrade.RelationshipCheckResult{RequestID: input.RequestID, OwnerTenantID: input.OwnerTenantID, PeerTenantID: input.PeerTenantID, RelationshipID: input.RelationshipID, TermsSha256: input.TermsSha256, Purpose: input.Purpose, ObservedAt: time.Now().UTC()}
		err = deps.DB.WithGuardedTenantCommand(tenantdb.WithActor(ctx, "system:supplier-relationship-current"), input.OwnerTenantID.String(), time.Now().Add(3*time.Second), func(*gorm.DB) error { return nil }, func(ctx context.Context) error {
			return deps.DB.WithTenantCommand(ctx, input.OwnerTenantID.String(), func(tx *gorm.DB) error {
				if err := relationshipLock(tx, input.OwnerTenantID, input.RelationshipID); err != nil {
					return err
				}
				r, err := loadTradeRelationship(tx, input.OwnerTenantID, input.RelationshipID)
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return nil
				}
				if err != nil {
					return err
				}
				remote := r.Terms.Buyer.TenantID
				if r.Side == "buyer" {
					remote = r.Terms.Seller.TenantID
				}
				if remote != input.PeerTenantID || r.TermsSha256 != input.TermsSha256 {
					return nil
				}
				result.AuthorityEpoch = r.AuthorityEpoch
				result.RemoteEpoch = r.RemoteEpoch
				if input.Purpose == "new_business" {
					result.Eligible = suppliertrade.RelationshipCurrent(r, time.Now()) && r.AuthorityEpoch == input.ExpectedAuthorityEpoch
				} else {
					var active bool
					if err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM supply_trade_relationship_history WHERE tenant_id=? AND relationship_id=? AND body->>'status'='active')`, input.OwnerTenantID, input.RelationshipID).Scan(&active).Error; err != nil {
						return err
					}
					result.Eligible = active && r.SignedBy != nil && r.RemoteSignedBy != nil && r.Status != "rejected"
				}
				return tx.Exec(`INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(?,?,'current_relationship_read','supply_trade_relationships',?)`, input.OwnerTenantID, "system:supplier-relationship-current", input.RelationshipID).Error
			})
		})
		if err != nil {
			respond.Error(c, 503, "relationship_unavailable", "Current relationship consent could not be verified.")
			return
		}
		result.ObservedAt = time.Now().UTC()
		c.Header("Cache-Control", "no-store")
		respond.OK(c, result)
	}
}
