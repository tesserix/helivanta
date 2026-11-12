package iam

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/internal/customeronboarding"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/onboarding-service/api/customercontract"
	"gorm.io/gorm"
	"io"
	"net/http"
	"time"
)

type OwnerSecurityClient interface {
	OwnerSecurity(context.Context, customercontract.SecurityRequest, bool) (customercontract.SecurityView, error)
}

func (m *Module) SetOwnerSecurityClient(client OwnerSecurityClient) *Module {
	m.ownerSecurity = client
	return m
}

type ownerSecurityHandlers struct {
	deps   platform.Deps
	client OwnerSecurityClient
}

func (h *ownerSecurityHandlers) read(c *gin.Context)      { h.serve(c, false) }
func (h *ownerSecurityHandlers) reconcile(c *gin.Context) { h.serve(c, true) }
func (h *ownerSecurityHandlers) serve(c *gin.Context, apply bool) {
	c.Header("Cache-Control", "no-store")
	p, tenant, ok := membershipCommandPrincipal(c)
	if !ok {
		return
	}
	if apply && (len(c.Request.Header.Values("Origin")) != 1 || c.GetHeader("Origin") != "https://helivanta.app" || c.GetHeader("Sec-Fetch-Site") == "cross-site") {
		respond.Forbidden(c, "Security changes require a same-origin request.")
		return
	}
	now := time.Now()
	if p.AuthTime.IsZero() || p.IdleDeadline.IsZero() || !now.Before(p.IdleDeadline) {
		respond.Unauthenticated(c, "Sign in again to manage security.")
		return
	}
	if apply && (p.AuthTime.After(now.Add(time.Minute)) || now.Sub(p.AuthTime) > 5*time.Minute) {
		respond.Error(c, 401, "reauthentication_required", "Sign out and sign in again before applying security requirements.")
		return
	}
	in := customercontract.SecurityRequest{TenantID: tenant.String(), Subject: p.Subject, RequestID: uuid.NewString()}
	if apply {
		var input customercontract.SecurityInput
		decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 8192))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil || decoder.Decode(new(any)) != io.EOF || input.Validate() != nil {
			respond.Error(c, 400, "invalid_request", "Invalid security request.")
			return
		}
		in.RequestID = input.RequestID
		in.ExpectedRevision = input.ExpectedRevision
	}
	if h.client == nil || h.deps.DB == nil {
		respond.Error(c, 503, "security_unavailable", "Workspace security is unavailable.")
		return
	}
	var view customercontract.SecurityView
	until := now.Add(12 * time.Second)
	if p.IdleDeadline.Before(until) {
		until = p.IdleDeadline
	}
	err := h.deps.DB.WithGuardedTenantCommand(c.Request.Context(), tenant.String(), until, func(tx *gorm.DB) error {
		if err := membershipActor(tx, p, p.Subject); err != nil {
			return err
		}
		allowed, err := assignmentAdministrator(tx, tenant.String(), p.Subject)
		if err != nil {
			return err
		}
		if !allowed {
			return authz.ErrScopedCommandDenied
		}
		return nil
	}, func(ctx context.Context) error {
		var err error
		view, err = h.client.OwnerSecurity(ctx, in, apply)
		return err
	})
	if err != nil {
		switch {
		case errors.Is(err, authz.ErrScopedCredentialRevoked):
			respond.Unauthenticated(c, "Sign in again to manage security.")
		case errors.Is(err, customeronboarding.ErrSecurityDenied), errors.Is(err, authz.ErrScopedCommandDenied):
			respond.Forbidden(c, "Current workspace owner access is required.")
		case errors.Is(err, customeronboarding.ErrSecurityConflict):
			respond.Error(c, 409, "security_changed", "Security changed. Refresh before trying again.")
		default:
			respond.Error(c, 503, "security_unavailable", "Could not verify workspace security. Refresh to check its current state.")
		}
		return
	}
	if apply {
		respond.Accepted(c, view)
	} else {
		respond.OK(c, view)
	}
}
