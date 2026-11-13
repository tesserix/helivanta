package iam

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"github.com/tesserix/onboarding-service/api/staffclient"
	"github.com/tesserix/onboarding-service/api/staffcontract"
)

type staffAccountHandlers struct {
	db    *tenantdb.DB
	owner *staffclient.Client
}

func registerStaffAccounts(g *platform.Router, deps platform.Deps) {
	h := &staffAccountHandlers{db: deps.DB, owner: deps.StaffAccounts}
	g.PUT("/staff-account-commands/:id", PermMemberManage, h.create)
	g.GET("/staff-account-commands/:id", PermMemberManage, h.read)
	g.POST("/staff-account-commands/:id/observations", PermMemberManage, h.observe)
	g.PUT("/staff-account-commands/:id/invitations/:delivery", PermMemberManage, h.deliver)
}

func (h *staffAccountHandlers) create(c *gin.Context)  { h.serve(c, "create") }
func (h *staffAccountHandlers) read(c *gin.Context)    { h.serve(c, "read") }
func (h *staffAccountHandlers) observe(c *gin.Context) { h.serve(c, "observe") }
func (h *staffAccountHandlers) deliver(c *gin.Context) { h.serve(c, "deliver") }

func staffAccountID(raw string) (uuid.UUID, bool) {
	id, err := uuid.Parse(raw)
	return id, err == nil && id.Version() == 7 && id.Variant() == uuid.RFC4122 && id.String() == raw
}

func (h *staffAccountHandlers) authorize(ctx context.Context, p authn.Principal, id uuid.UUID, action string) error {
	if h.db == nil {
		return errMembershipAuthority
	}
	return h.db.WithTenant(ctx, p.TenantID, func(tx *gorm.DB) error {
		if err := membershipActor(tx, p, p.Subject); err != nil {
			return err
		}
		if err := tx.Exec("INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(?,?,?,'iam_staff_account_commands',?)", p.TenantID, p.Subject, action, id).Error; err != nil {
			return errors.Join(errMembershipAuthority, err)
		}
		return nil
	})
}

func (h *staffAccountHandlers) serve(c *gin.Context, operation string) {
	p, tenant, ok := membershipCommandPrincipal(c)
	if !ok {
		return
	}
	id, ok := staffAccountID(c.Param("id"))
	if !ok {
		respond.NotFound(c, "account command")
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 4096))
	if err != nil || c.Request.URL.RawQuery != "" || c.Request.URL.ForceQuery {
		respond.Error(c, 400, "invalid_request", "Use one bounded account command without query parameters.")
		return
	}
	var input staffcontract.StaffIdentityCreateInput
	var delivery uuid.UUID
	if operation == "create" {
		input, err = staffcontract.DecodeStaffContact(raw)
		if err != nil {
			respond.Error(c, 400, "invalid_request", "Provide only email, given name, family name and reason.")
			return
		}
		input.ActorSubject = p.Subject
	} else {
		var body map[string]json.RawMessage
		if operation == "read" {
			if len(raw) != 0 {
				respond.Error(c, 400, "invalid_request", "An account read has no request body.")
				return
			}
		} else if json.Unmarshal(raw, &body) != nil || body == nil || len(body) != 0 {
			respond.Error(c, 400, "invalid_request", "This account command requires an empty object.")
			return
		}
	}
	if operation == "deliver" {
		delivery, ok = staffAccountID(c.Param("delivery"))
		if !ok {
			respond.NotFound(c, "invitation command")
			return
		}
	}
	until := time.Now().Add(30 * time.Second)
	if p.IdleDeadline.Before(until) {
		until = p.IdleDeadline
	}
	ctx, cancel := context.WithDeadline(c.Request.Context(), until)
	defer cancel()
	if err = h.authorize(ctx, p, id, "staff_account_"+operation+"_requested"); err != nil {
		membershipCommandError(c, err)
		return
	}
	if h.owner == nil {
		staffAccountError(c, staffclient.ErrUnavailable)
		return
	}
	scope := staffclient.Scope{TenantID: tenant, CommandID: id, ActorSubject: p.Subject}
	var result any
	status := http.StatusOK
	switch operation {
	case "create":
		var receipt staffcontract.StaffIdentityReceipt
		receipt, err = h.owner.Create(ctx, scope, input)
		result = receipt
		if receipt.CommandState != "created" {
			status = http.StatusAccepted
		}
	case "read":
		result, err = h.owner.Get(ctx, scope)
	case "observe":
		result, err = h.owner.Observe(ctx, scope)
	case "deliver":
		result, err = h.owner.Deliver(ctx, scope, delivery)
	default:
		respond.NotFound(c, "account action")
		return
	}
	if err != nil {
		staffAccountError(c, err)
		return
	}
	if err = h.authorize(ctx, p, id, "staff_account_"+operation+"_disclosed"); err != nil {
		membershipCommandError(c, err)
		return
	}
	c.JSON(status, result)
}

func staffAccountError(c *gin.Context, err error) {
	var limited staffclient.RateLimitError
	switch {
	case errors.Is(err, staffclient.ErrInvalid):
		respond.Error(c, 400, "invalid_request", "Check the account contact and required reason.")
	case errors.Is(err, staffclient.ErrNotFound):
		respond.NotFound(c, "account command")
	case errors.Is(err, staffclient.ErrConflict):
		respond.Error(c, 409, "identity_command_conflict", "This account command or contact is already retained or needs review. Reconcile the same command before continuing.")
	case errors.As(err, &limited):
		c.Header("Retry-After", strconv.Itoa(limited.RetryAfterSeconds))
		respond.Error(c, 429, "rate_limited", "Wait before requesting another setup link.")
	default:
		respond.Error(c, 503, "staff_identity_unavailable", "Account setup is unavailable. Retry or check the same command.")
	}
}
