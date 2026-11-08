package iam

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
	"time"
)

type sourceAdministrationGuard struct {
	db    *tenantdb.DB
	graph currentGraph
}

func NewSourceAdministrationGuard(db *tenantdb.DB, graph currentGraph) authz.SourceAdministrationGuard {
	return &sourceAdministrationGuard{db: db, graph: graph}
}
func (g *sourceAdministrationGuard) WithSourceAdministration(ctx context.Context, p authn.Principal, permission authz.Permission, action func(context.Context) error) error {
	if g.db == nil || g.graph == nil || action == nil {
		return authz.ErrScopedAuthorizationUnavailable
	}
	tenant, err := uuid.Parse(p.TenantID)
	if err != nil || tenant == uuid.Nil || !proposalSubject(p.Subject) || permission == "" || p.AuthTime.IsZero() || !p.IdleDeadline.After(time.Now()) || g.db.GuardedCommandActive(ctx) || (tenantdb.ActorFrom(ctx) != "" && tenantdb.ActorFrom(ctx) != p.Subject) {
		return authz.ErrInvalidScopedAssignment
	}
	h := operatingHandlers{db: g.db, graph: g.graph}
	proof, err := h.actorProofFor(ctx, p, permission, nil)
	if errors.Is(err, errAssignmentDenied) {
		return authz.ErrScopedCommandDenied
	}
	if err != nil {
		return err
	}
	err = h.transaction(ctx, p, proof, "read", func(commandCtx context.Context, tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.iam_operating_access','',true),set_config('app.iam_operating_command','',true)`).Error; err != nil {
			return err
		}
		return action(commandCtx)
	})
	if errors.Is(err, errAssignmentDenied) {
		return authz.ErrScopedCommandDenied
	}
	return err
}
