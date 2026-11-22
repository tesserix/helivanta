package iam

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
)

func (h *operatingHandlers) templateAvailability(ctx context.Context, p authn.Principal, template string) error {
	tenant, err := uuid.Parse(p.TenantID)
	if err != nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	available, err := h.releasePolicy.OperatingTemplateEnabled(ctx, tenant, p.Subject, template)
	if err != nil {
		return errors.Join(authz.ErrScopedAuthorizationUnavailable, err)
	}
	if !available {
		return authz.ErrScopedCommandDenied
	}
	return nil
}
