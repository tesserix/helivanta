package authz

import (
	"context"
	"fmt"

	fgaclient "github.com/openfga/go-sdk/client"
)

// tenantType is the object type whose `member` relation answers "does
// this subject belong to this tenant". Named as a constant because
// TenantOfObject branches on it.
const tenantType = "tenant"

// TenantObject is the object id for one tenant. Unlike RoleObject and
// PermObject it has no /name segment: the tenant object IS the tenant,
// so there is nothing to name within it.
func TenantObject(tenantID string) string {
	return tenantType + ":" + tenantID
}

// GrantTenantRole wires a role into its tenant so that assignees of the
// role are members of the tenant. Idempotent, like every other write:
// the reconciler re-applies it on every boot.
//
// This edge is per (tenant, role), not per member — it is written once
// per role a tenant uses, and membership for any number of subjects
// derives through it.
func (c *Client) GrantTenantRole(ctx context.Context, tenantID string, role Role) error {
	return c.write(ctx, RoleObject(tenantID, role), "granted_role", TenantObject(tenantID))
}

// RevokeTenantRole removes the tenant->role edge. Only the reconciler's
// prune pass calls this: removing it while a role still has assignees
// would strip membership from people who still hold the role.
func (c *Client) RevokeTenantRole(ctx context.Context, tenantID string, role Role) error {
	return c.delete(ctx, RoleObject(tenantID, role), "granted_role", TenantObject(tenantID))
}

// IsMember reports whether subject holds any role in tenantID.
//
// One Check, not a ListObjects: the question is a yes/no about a single
// object, and Check is the operation OpenFGA optimises for that. It
// returns an error rather than false on failure so callers fail closed
// deliberately rather than by accident — a false here and a false from
// a genuine non-member are indistinguishable, and only one of them
// should produce a 403.
func (c *Client) IsMember(ctx context.Context, subject, tenantID string) (bool, error) {
	res, err := c.api.Check(ctx).Body(fgaclient.ClientCheckRequest{
		User:     userObject(subject),
		Relation: "member",
		Object:   TenantObject(tenantID),
	}).Execute()
	if err != nil {
		return false, fmt.Errorf("check membership of %s in %s: %w", subject, tenantID, err)
	}
	return res.GetAllowed(), nil
}
