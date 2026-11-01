package iam

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	iamcontract "github.com/tesserix/helivanta/internal/modules/iam/contract"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type membershipProjector interface {
	ProjectMembership(context.Context, authz.Membership) error
	ReadMembershipBindings(context.Context, string, string) ([]authz.Tuple, error)
	RetireMembershipBinding(context.Context, authz.Tuple) error
}

type fgaSyncHandlers struct {
	db        *tenantdb.DB
	authz     platform.TupleWriter
	reconcile func(context.Context, string) error
}

func membershipSyncData(evt events.Event) (iamcontract.MemberChangedData, error) {
	var data iamcontract.MemberChangedData
	tenant, err := uuid.Parse(evt.TenantID)
	if err != nil || tenant == uuid.Nil || tenant.String() != evt.TenantID || evt.Version != 1 || evt.DestinationTenantID != "" || json.Unmarshal(evt.Data, &data) != nil || !proposalSubject(data.Subject) || !knownRole(data.RoleKey) {
		return data, errors.New("invalid IAM membership reconciliation hint")
	}
	return data, nil
}

func (*fgaSyncHandlers) handle(_ context.Context, _ *gorm.DB, evt events.Event) error {
	_, err := membershipSyncData(evt)
	return err
}

func (h *fgaSyncHandlers) project(ctx context.Context, evt events.Event) error {
	data, err := membershipSyncData(evt)
	if err != nil {
		return err
	}
	writer, ok := h.authz.(membershipProjector)
	if !ok || h.db == nil {
		return authz.ErrMembershipUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	observed, err := writer.ReadMembershipBindings(ctx, data.Subject, evt.TenantID)
	if err != nil {
		return err
	}
	current, err := currentMemberships(ctx, h.db, evt.TenantID, data.Subject)
	if err != nil {
		return err
	}
	if h.reconcile != nil && len(current) > 0 {
		if err := h.reconcile(ctx, evt.TenantID); err != nil {
			return fmt.Errorf("reconcile tenant permissions: %w", err)
		}
	}
	desired := make(map[string]struct{}, len(current))
	for _, member := range current {
		if err := writer.ProjectMembership(ctx, member); err != nil {
			return err
		}
		object, err := member.Object()
		if err != nil {
			return err
		}
		desired[object] = struct{}{}
	}
	// Legacy real-user edges are repair residue, never an incarnation proof.
	if err := h.authz.RevokeRole(ctx, evt.TenantID, data.Subject, authz.Role(data.RoleKey)); err != nil {
		return err
	}
	for _, binding := range observed {
		if _, keep := desired[binding.Object]; !keep {
			if err := writer.RetireMembershipBinding(ctx, binding); err != nil {
				return err
			}
		}
	}
	latest, err := currentMemberships(ctx, h.db, evt.TenantID, data.Subject)
	if err != nil {
		return err
	}
	if len(latest) != len(current) {
		return authz.ErrMembershipUnavailable
	}
	for _, member := range latest {
		object, err := member.Object()
		if err != nil {
			return err
		}
		if _, ok := desired[object]; !ok {
			return authz.ErrMembershipUnavailable
		}
	}
	return nil
}

func (*Module) Consumers(deps platform.Deps) []events.Consumer {
	sync := &fgaSyncHandlers{db: deps.DB, authz: deps.Authz, reconcile: deps.Reconcile}
	return []events.Consumer{
		proposalScopeConsumer(deps),
		assignmentProjectionConsumer(deps),
		{Name: "iam-fga-sync", Subject: iamcontract.SubjectMemberGranted, Handle: sync.handle, AfterCommit: sync.project},
		{Name: "iam-fga-sync-revoke", Subject: iamcontract.SubjectMemberRevoked, Handle: sync.handle, AfterCommit: sync.project},
	}
}
