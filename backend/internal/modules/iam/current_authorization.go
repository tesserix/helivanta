package iam

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type currentGraph interface {
	authz.Resolver
	authz.MembershipChecker
}

// CurrentAuthorization bounds graph authority by current IAM memberships.
type CurrentAuthorization struct {
	db          *tenantdb.DB
	graph       currentGraph
	permissions map[authz.Role]authz.PermissionSet
}

// NewCurrentAuthorization constructs current tenant permission and membership checks.
func NewCurrentAuthorization(db *tenantdb.DB, registry *platform.Registry, graph currentGraph) (*CurrentAuthorization, error) {
	if db == nil || registry == nil || graph == nil {
		return nil, errors.New("current authorization dependencies missing")
	}
	grants, err := platform.GrantsFor(registry)
	if err != nil {
		return nil, err
	}
	a := &CurrentAuthorization{db: db, graph: graph, permissions: make(map[authz.Role]authz.PermissionSet)}
	for _, grant := range grants {
		for _, role := range grant.Roles {
			if a.permissions[role] == nil {
				a.permissions[role] = authz.NewPermissionSet()
			}
			a.permissions[role][grant.Permission] = struct{}{}
		}
	}
	return a, nil
}

// Resolve intersects graph permissions with the current IAM role catalogue grants.
func (a *CurrentAuthorization) Resolve(ctx context.Context, subject, tenant string) (authz.PermissionSet, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	graph, err := a.graph.Resolve(ctx, subject, tenant)
	if err != nil {
		return nil, fmt.Errorf("resolve graph permissions: %w", err)
	}
	roles, err := a.currentRoles(ctx, subject, tenant)
	if err != nil {
		return nil, err
	}
	allowed := authz.NewPermissionSet()
	for _, role := range roles {
		for permission := range a.permissions[role] {
			if graph.Has(permission) {
				allowed[permission] = struct{}{}
			}
		}
	}
	return allowed, ctx.Err()
}

// IsMember requires both projected membership and a current known IAM role.
func (a *CurrentAuthorization) IsMember(ctx context.Context, subject, tenant string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	member, err := a.graph.IsMember(ctx, subject, tenant)
	if err != nil {
		return false, fmt.Errorf("resolve graph membership: %w", err)
	}
	if !member {
		return false, ctx.Err()
	}
	roles, err := a.currentRoles(ctx, subject, tenant)
	if err != nil {
		return false, err
	}
	return len(roles) > 0, ctx.Err()
}

func (a *CurrentAuthorization) currentRoles(ctx context.Context, subject, tenant string) ([]authz.Role, error) {
	tenantID, err := uuid.Parse(tenant)
	if err != nil || tenantID == uuid.Nil || tenantID.String() != tenant || subject == "" {
		return nil, errors.New("current membership scope invalid")
	}
	var roles []authz.Role
	bound := len(authz.SystemRoles())
	err = a.db.WithTenant(ctx, tenant, func(tx *gorm.DB) error {
		return tx.Table("iam_members").Where("tenant_id=? AND subject=?", tenant, subject).
			Order("role_key").Limit(bound+1).Pluck("role_key", &roles).Error
	})
	if err != nil {
		return nil, fmt.Errorf("read current IAM memberships: %w", err)
	}
	// Unknown or truncated source authority denies instead of trusting stale tuples.
	if len(roles) > bound {
		return nil, errors.New("current membership role bound exceeded")
	}
	for _, role := range roles {
		if !authz.KnownRole(role) {
			return nil, errors.New("current membership role unknown")
		}
	}
	return roles, nil
}
