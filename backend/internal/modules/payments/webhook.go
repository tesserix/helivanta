package payments

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/ratelimit"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// WebhookPath is mounted outside the authenticated chain: Razorpay holds
// no Helivanta session, so the account's webhook secret is the only
// credential (bootstrap.UnauthenticatedRoutes).
const WebhookPath = "/v1/payments/webhooks/razorpay/:tenant/:account"

const maxWebhookBytes = 64 << 10

var errWebhookRefused = errors.New("payments: webhook refused")

// Webhook takes a Razorpay delivery as a nudge. Nothing in the body
// settles anything: a verified event naming one of our QR codes makes the
// same provider API check the poller makes, so a forged, malformed or
// replayed body can at most cause an extra read.
func Webhook(deps platform.Deps, limiter ratelimit.Limiter) gin.HandlerFunc {
	if limiter == nil {
		panic("payments webhook requires a rate limiter")
	}
	svc := serviceFor(deps)
	return func(c *gin.Context) {
		// One answer for unknown tenant, unknown account and bad signature,
		// so the endpoint does not confirm which tenants or accounts exist.
		refuse := func() { respond.Error(c, http.StatusUnauthorized, "unauthenticated", "webhook not accepted") }
		tenant, err1 := uuid.Parse(c.Param("tenant"))
		accountID, err2 := uuid.Parse(c.Param("account"))
		if err1 != nil || err2 != nil {
			refuse()
			return
		}
		// Budgeted per account, before the vault is asked for its secret: an
		// unauthenticated caller must not be able to spend Transit calls
		// without limit. Razorpay sends a handful per payment.
		if !limiter.Allow("payments-webhook:"+accountID.String(), ratelimit.Rule{Rate: 120, Burst: 60, Per: time.Minute}, time.Now()).Allowed {
			respond.Error(c, http.StatusTooManyRequests, "rate_limited", "too many webhook deliveries")
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, maxWebhookBytes))
		if err != nil {
			respond.Error(c, http.StatusRequestEntityTooLarge, "invalid_request", "webhook body too large")
			return
		}
		eventID := c.GetHeader("X-Razorpay-Event-Id")
		ctx := tenantdb.WithActor(c.Request.Context(), "system:payments-webhook")
		var a account
		var intentID uuid.UUID
		var event string
		// Each step is its own short transaction: the vault and the provider
		// are called with none open.
		err = deps.DB.WithTenant(ctx, tenant.String(), func(tx *gorm.DB) error {
			return tx.Raw(`SELECT * FROM payment_accounts WHERE id=?`, accountID).Scan(&a).Error
		})
		if err == nil && a.ID == uuid.Nil {
			err = errWebhookRefused
		}
		var secret string
		if err == nil {
			secret, err = svc.unseal(ctx, a, a.WebhookSecretSealed)
		}
		if err == nil && (!validWebhookSignature(body, c.GetHeader("X-Razorpay-Signature"), secret) || eventID == "" || len(eventID) > 100) {
			err = errWebhookRefused
		}
		if err == nil {
			var delivery struct {
				Event   string `json:"event"`
				Payload struct {
					QRCode struct {
						Entity struct {
							ID string `json:"id"`
						} `json:"entity"`
					} `json:"qr_code"`
				} `json:"payload"`
			}
			if json.Unmarshal(body, &delivery) != nil || delivery.Event == "" || len(delivery.Event) > 100 {
				err = errWebhookRefused
			}
			event = delivery.Event
			duplicate := false
			if err == nil {
				err = deps.DB.WithTenant(ctx, tenant.String(), func(tx *gorm.DB) error {
					var seen int64
					if err := tx.Raw(`SELECT count(*) FROM payment_webhook_deliveries WHERE account_id=? AND event_id=?`, a.ID, eventID).Scan(&seen).Error; err != nil {
						return err
					}
					if duplicate = seen > 0; duplicate || (event != "qr_code.credited" && event != "qr_code.closed") {
						return nil
					}
					var row struct{ ID uuid.UUID }
					err := tx.Raw(`SELECT id FROM payment_intents WHERE provider_ref=? AND account_id=? AND status IN ('awaiting_payment','expired','cancelled')`,
						delivery.Payload.QRCode.Entity.ID, a.ID).Scan(&row).Error
					intentID = row.ID
					return err
				})
			}
			if err == nil && !duplicate {
				if intentID != uuid.Nil {
					err = svc.check(ctx, tenant.String(), intentID, "webhook", false)
				}
				// Recorded only once its check succeeded: a failed check
				// answers 503, and Razorpay's redelivery is processed in full.
				if err == nil {
					err = deps.DB.WithTenant(ctx, tenant.String(), func(tx *gorm.DB) error {
						return tx.Exec(`INSERT INTO payment_webhook_deliveries (tenant_id,account_id,event_id,event) VALUES (?,?,?,?) ON CONFLICT DO NOTHING`,
							tenant, a.ID, eventID, event).Error
					})
				}
			}
		}
		switch {
		case errors.Is(err, errWebhookRefused):
			refuse()
		case err != nil:
			slog.WarnContext(ctx, "payments webhook not processed", "tenant_id", tenant, "account_id", accountID, "error", err)
			respond.Error(c, http.StatusServiceUnavailable, "service_unavailable", "webhook not processed; retry")
		default:
			c.Status(http.StatusOK)
		}
	}
}
