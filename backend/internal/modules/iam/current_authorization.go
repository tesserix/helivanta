package iam

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type currentGraph interface {
	ResolveMembership(context.Context, authz.Membership) (authz.PermissionSet, error)
	IsMembershipReady(context.Context, authz.Membership) (bool, error)
	ListRoleCandidates(context.Context, string) ([]authz.RoleBinding, error)
}

// ListRoles treats graph bindings as candidate tenants, never current authority.
func (a *CurrentAuthorization) ListRoles(ctx context.Context, subject string) ([]authz.RoleBinding, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	candidates, err := a.graph.ListRoleCandidates(ctx, subject)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
	var result []authz.RoleBinding
	for _, candidate := range candidates {
		if seen[candidate.TenantID] {
			continue
		}
		seen[candidate.TenantID] = true
		before, err := currentMemberships(ctx, a.db, candidate.TenantID, subject)
		if err != nil {
			return nil, err
		}
		proofs := make(map[authz.Membership]bool, len(before))
		for _, member := range before {
			ready, err := a.graph.IsMembershipReady(ctx, member)
			if err != nil {
				return nil, err
			}
			proofs[member] = ready
		}
		after, err := currentMemberships(ctx, a.db, candidate.TenantID, subject)
		if err != nil {
			return nil, err
		}
		for _, member := range after {
			if proofs[member] {
				result = append(result, authz.RoleBinding{TenantID: member.TenantID, Role: member.Role})
			}
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].TenantID != result[j].TenantID {
			return result[i].TenantID < result[j].TenantID
		}
		return result[i].Role < result[j].Role
	})
	return result, nil
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

// Resolve returns graph proof for source incarnations that remain current afterward.
func (a *CurrentAuthorization) Resolve(ctx context.Context, subject, tenant string) (authz.PermissionSet, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	before, err := currentMemberships(ctx, a.db, tenant, subject)
	if err != nil {
		return nil, err
	}
	proofs := make(map[authz.Membership]authz.PermissionSet, len(before))
	for _, member := range before {
		proof, err := a.graph.ResolveMembership(ctx, member)
		if err != nil {
			return nil, fmt.Errorf("resolve membership permissions: %w", err)
		}
		proofs[member] = proof
	}
	after, err := currentMemberships(ctx, a.db, tenant, subject)
	if err != nil {
		return nil, err
	}
	allowed := authz.NewPermissionSet()
	for _, member := range after {
		for permission := range a.permissions[member.Role] {
			if proofs[member].Has(permission) {
				allowed[permission] = struct{}{}
			}
		}
	}
	return allowed, nil
}

// IsMember requires a graph-ready incarnation that still exists in current IAM.
func (a *CurrentAuthorization) IsMember(ctx context.Context, subject, tenant string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	before, err := currentMemberships(ctx, a.db, tenant, subject)
	if err != nil {
		return false, err
	}
	proofs := make(map[authz.Membership]bool, len(before))
	for _, member := range before {
		ready, err := a.graph.IsMembershipReady(ctx, member)
		if err != nil {
			return false, fmt.Errorf("check membership projection: %w", err)
		}
		proofs[member] = ready
	}
	after, err := currentMemberships(ctx, a.db, tenant, subject)
	if err != nil {
		return false, err
	}
	for _, member := range after {
		if proofs[member] {
			return true, nil
		}
	}
	return false, nil
}
