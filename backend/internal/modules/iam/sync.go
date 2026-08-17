package iam

import (
	"context"
	"encoding/json"
	"fmt"

	"gorm.io/gorm"

	iamcontract "github.com/tesserix/helivanta/internal/modules/iam/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
)

type fgaSyncHandlers struct {
	authz     platform.TupleWriter
	reconcile func(ctx context.Context, tenantID string) error
}

// grant applies a member_granted event to OpenFGA. It reconciles the
// tenant's full permission set first (self-healing any prior drift)
// before writing the new role tuple.
func (h *fgaSyncHandlers) grant(ctx context.Context, _ *gorm.DB, evt events.Event) error {
	var data iamcontract.MemberChangedData
	if err := json.Unmarshal(evt.Data, &data); err != nil {
		return fmt.Errorf("iam sync payload: %w", err)
	}
	if h.reconcile != nil {
		if err := h.reconcile(ctx, evt.TenantID); err != nil {
			return fmt.Errorf("reconcile tenant: %w", err)
		}
	}
	return h.authz.GrantRole(ctx, evt.TenantID, data.Subject, authz.Role(data.RoleKey))
}

// revoke applies a member_revoked event to OpenFGA.
func (h *fgaSyncHandlers) revoke(ctx context.Context, _ *gorm.DB, evt events.Event) error {
	var data iamcontract.MemberChangedData
	if err := json.Unmarshal(evt.Data, &data); err != nil {
		return fmt.Errorf("iam sync payload: %w", err)
	}
	return h.authz.RevokeRole(ctx, evt.TenantID, data.Subject, authz.Role(data.RoleKey))
}

// Consumers keeps OpenFGA in step with the membership tables. Postgres
// is the system of record, so a failed tuple write is retried by the bus
// and the tuple helpers are idempotent — redelivery is harmless.
//
// Handle runs inside the bus's transaction; it must not open its own.
func (m *Module) Consumers(deps platform.Deps) []events.Consumer {
	sync := &fgaSyncHandlers{authz: deps.Authz, reconcile: deps.Reconcile}
	return []events.Consumer{
		{Name: "iam-fga-sync", Subject: iamcontract.SubjectMemberGranted, Handle: sync.grant},
		{Name: "iam-fga-sync-revoke", Subject: iamcontract.SubjectMemberRevoked, Handle: sync.revoke},
	}
}
