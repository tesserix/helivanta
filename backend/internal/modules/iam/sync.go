package iam

import (
	"context"
	"encoding/json"
	"fmt"

	"gorm.io/gorm"

	"github.com/tesserix/hms/internal/platform"
	"github.com/tesserix/hms/pkg/authz"
	"github.com/tesserix/hms/pkg/events"
)

// Consumers keeps OpenFGA in step with the membership tables. Postgres
// is the system of record, so a failed tuple write is retried by the bus
// and the tuple helpers are idempotent — redelivery is harmless.
//
// Handle runs inside the bus's transaction; it must not open its own.
func (m *Module) Consumers(deps platform.Deps) []events.Consumer {
	apply := func(grant bool) func(context.Context, *gorm.DB, events.Event) error {
		return func(ctx context.Context, _ *gorm.DB, evt events.Event) error {
			var data MemberChangedData
			if err := json.Unmarshal(evt.Data, &data); err != nil {
				return fmt.Errorf("iam sync payload: %w", err)
			}
			role := authz.Role(data.RoleKey)
			if grant {
				if deps.Reconcile != nil {
					if err := deps.Reconcile(ctx, evt.TenantID); err != nil {
						return fmt.Errorf("reconcile tenant: %w", err)
					}
				}
				return deps.Authz.GrantRole(ctx, evt.TenantID, data.Subject, role)
			}
			return deps.Authz.RevokeRole(ctx, evt.TenantID, data.Subject, role)
		}
	}
	return []events.Consumer{
		{Name: "iam-fga-sync", Subject: SubjectMemberGranted, Handle: apply(true)},
		{Name: "iam-fga-sync-revoke", Subject: SubjectMemberRevoked, Handle: apply(false)},
	}
}
