package payments

import (
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type accountHandlers struct {
	db          *tenantdb.DB
	svc         *service
	webhookBase string
}

func accountRoutes(g *platform.Router, h *accountHandlers) {
	g.GET("/razorpay-account", PermAccountManage, h.get)
	g.PUT("/razorpay-account", PermAccountManage, h.put)
}

// accountView never carries a secret, sealed or not: only whether each
// one is set.
type accountView struct {
	Configured       bool       `json:"configured"`
	Mode             string     `json:"mode,omitempty"`
	KeyID            string     `json:"key_id,omitempty"`
	KeySecretSet     bool       `json:"key_secret_set"`
	WebhookSecretSet bool       `json:"webhook_secret_set"`
	UpdatedBy        string     `json:"updated_by,omitempty"`
	UpdatedAt        *time.Time `json:"updated_at,omitempty"`
	WebhookPath      string     `json:"webhook_path,omitempty"`
	// WebhookURL is what to paste into the Razorpay dashboard; absent when
	// this deployment exposes no public webhook origin, and payments are
	// then confirmed by polling Razorpay alone.
	WebhookURL string `json:"webhook_url,omitempty"`
}

func (h *accountHandlers) view(a account) accountView {
	if a.ID == uuid.Nil {
		return accountView{}
	}
	at := a.UpdatedAt
	v := accountView{Configured: true, Mode: a.Mode, KeyID: a.KeyID, KeySecretSet: a.KeySecretSealed != "", WebhookSecretSet: a.WebhookSecretSealed != "",
		UpdatedBy: a.UpdatedBy, UpdatedAt: &at, WebhookPath: "/v1/payments/webhooks/razorpay/" + a.TenantID.String() + "/" + a.ID.String()}
	if h.webhookBase != "" {
		v.WebhookURL = strings.TrimRight(h.webhookBase, "/") + v.WebhookPath
	}
	return v
}

func (h *accountHandlers) load(c *gin.Context, tenant string) (account, error) {
	var a account
	err := h.db.WithTenant(c.Request.Context(), tenant, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT * FROM payment_accounts`).Scan(&a).Error
	})
	return a, err
}

func (h *accountHandlers) get(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, _, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}
	a, err := h.load(c, p.TenantID)
	if err != nil {
		respond.InternalErr(c, err, "could not read the payment account")
		return
	}
	respond.OK(c, h.view(a))
}

var keyIDPattern = regexp.MustCompile(`^rzp_(test|live)_[A-Za-z0-9]{1,40}$`)

type accountInput struct {
	Mode          string  `json:"mode" binding:"required,oneof=test live"`
	KeyID         string  `json:"key_id" binding:"required,max=60"`
	KeySecret     *string `json:"key_secret" binding:"omitempty,min=8,max=200"`
	WebhookSecret *string `json:"webhook_secret" binding:"omitempty,min=8,max=200"`
}

// put replaces the account. Keys are checked with Razorpay before anything
// is stored, so a saved account is one that worked when it was saved.
func (h *accountHandlers) put(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	p, tenant, ok := authn.TenantPrincipal(c)
	if !ok {
		return
	}
	// Without the vault there is nowhere safe to keep a secret; refuse
	// rather than store one in the clear.
	if h.svc.vault == nil {
		respond.Error(c, http.StatusServiceUnavailable, "service_unavailable", "encrypted credential storage is not configured in this deployment")
		return
	}
	var in accountInput
	if err := c.ShouldBindJSON(&in); err != nil {
		respond.BadRequest(c, err)
		return
	}
	m := keyIDPattern.FindStringSubmatch(in.KeyID)
	if m == nil || m[1] != in.Mode {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "the key id must be a Razorpay "+in.Mode+" key (rzp_"+in.Mode+"_…)")
		return
	}
	current, err := h.load(c, p.TenantID)
	if err != nil {
		respond.InternalErr(c, err, "could not read the payment account")
		return
	}
	// A new key id is a new key pair; its secret cannot be the old one's.
	if (current.ID == uuid.Nil || current.KeyID != in.KeyID) && in.KeySecret == nil {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "a key secret is required for a new key id")
		return
	}
	if current.ID == uuid.Nil && in.WebhookSecret == nil {
		respond.Error(c, http.StatusBadRequest, "invalid_request", "a webhook secret is required")
		return
	}
	ctx := c.Request.Context()
	next := current
	if next.ID == uuid.Nil {
		next = account{ID: uuid.New(), TenantID: tenant}
	}
	keySecret := ""
	if in.KeySecret != nil {
		keySecret = *in.KeySecret
	} else if keySecret, err = h.svc.unseal(ctx, current, current.KeySecretSealed); err != nil {
		respond.Error(c, http.StatusServiceUnavailable, "service_unavailable", "the stored key secret could not be read")
		return
	}
	switch err := h.svc.provider.Verify(ctx, credentials{KeyID: in.KeyID, KeySecret: keySecret}); {
	case errors.Is(err, errProviderRejected):
		respond.Error(c, http.StatusUnprocessableEntity, "credentials_rejected", "Razorpay did not accept this key id and secret")
		return
	case err != nil:
		respond.Error(c, http.StatusServiceUnavailable, "provider_unavailable", "Razorpay could not be reached to check the keys")
		return
	}
	seal := func(plain *string, sealed string) (string, error) {
		if plain == nil {
			return sealed, nil
		}
		return h.svc.vault.Encrypt(ctx, credentialKey, derivation(tenant, next.ID), []byte(*plain))
	}
	if next.KeySecretSealed, err = seal(in.KeySecret, current.KeySecretSealed); err == nil {
		next.WebhookSecretSealed, err = seal(in.WebhookSecret, current.WebhookSecretSealed)
	}
	if err != nil {
		respond.Error(c, http.StatusServiceUnavailable, "service_unavailable", "encrypted credential storage is unavailable")
		return
	}
	conflict := false
	err = h.db.WithTenant(tenantdb.WithActor(ctx, p.Subject), p.TenantID, func(tx *gorm.DB) error {
		var res *gorm.DB
		if current.ID == uuid.Nil {
			// The ciphertexts are bound to next.ID; a concurrent first save
			// with another id must not win the row and orphan them.
			res = tx.Exec(`INSERT INTO payment_accounts (id,tenant_id,provider,mode,key_id,key_secret_sealed,webhook_secret_sealed,updated_by)
				VALUES (?,?,'razorpay',?,?,?,?,?) ON CONFLICT (tenant_id) DO NOTHING`, next.ID, tenant, in.Mode, in.KeyID, next.KeySecretSealed, next.WebhookSecretSealed, p.Subject)
		} else {
			res = tx.Exec(`UPDATE payment_accounts SET mode=?,key_id=?,key_secret_sealed=?,webhook_secret_sealed=?,updated_by=?,updated_at=now()
				WHERE id=? AND updated_at=?`, in.Mode, in.KeyID, next.KeySecretSealed, next.WebhookSecretSealed, p.Subject, current.ID, current.UpdatedAt)
		}
		if res.Error != nil {
			return res.Error
		}
		conflict = res.RowsAffected == 0
		return nil
	})
	if err != nil {
		respond.InternalErr(c, err, "could not save the payment account")
		return
	}
	if conflict {
		respond.Conflict(c, "the payment account changed while saving; reload and try again")
		return
	}
	saved, err := h.load(c, p.TenantID)
	if err != nil {
		respond.InternalErr(c, err, "could not read the payment account")
		return
	}
	respond.OK(c, h.view(saved))
}
