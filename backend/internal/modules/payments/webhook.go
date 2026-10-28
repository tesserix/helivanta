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
		err = deps.DB.WithTenant(ctx, tenant.String(), func(tx *gorm.DB) error {
			var a account
			if err := tx.Raw(`SELECT * FROM payment_accounts WHERE id=?`, accountID).Scan(&a).Error; err != nil {
				return err
			}
			if a.ID == uuid.Nil {
				return errWebhookRefused
			}
			secret, err := svc.unseal(ctx, a, a.WebhookSecretSealed)
			if err != nil {
				return err
			}
			if !validWebhookSignature(body, c.GetHeader("X-Razorpay-Signature"), secret) || eventID == "" || len(eventID) > 100 {
				return errWebhookRefused
			}
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
			if err := json.Unmarshal(body, &delivery); err != nil || delivery.Event == "" || len(delivery.Event) > 100 {
				return errWebhookRefused
			}
			res := tx.Exec(`INSERT INTO payment_webhook_deliveries (tenant_id,account_id,event_id,event) VALUES (?,?,?,?) ON CONFLICT DO NOTHING`,
				tenant, a.ID, eventID, delivery.Event)
			if res.Error != nil || res.RowsAffected == 0 {
				return res.Error
			}
			if delivery.Event != "qr_code.credited" && delivery.Event != "qr_code.closed" {
				return nil
			}
			in, found, err := lockIntent(tx, false, "provider_ref=? AND account_id=? AND status IN ('awaiting_payment','expired','cancelled')",
				delivery.Payload.QRCode.Entity.ID, a.ID)
			if err != nil || !found {
				return err
			}
			return svc.check(ctx, tx, in, "webhook", "")
		})
		switch {
		case errors.Is(err, errWebhookRefused):
			refuse()
		case err != nil:
			// The delivery row rolled back with the failed check, so
			// Razorpay's redelivery is processed in full.
			slog.WarnContext(ctx, "payments webhook not processed", "tenant_id", tenant, "account_id", accountID, "error", err)
			respond.Error(c, http.StatusServiceUnavailable, "service_unavailable", "webhook not processed; retry")
		default:
			c.Status(http.StatusOK)
		}
	}
}
