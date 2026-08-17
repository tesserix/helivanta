package authz_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/pkg/authz"
)

// TestMembershipIsDerivedFromRoleAssignment is the model-shape test for
// D6: membership must not be a separately-writable relation. It can only
// ever be produced by wiring a role into its tenant AND assigning that
// role to a subject — either alone must leave the subject a non-member.
func TestMembershipIsDerivedFromRoleAssignment(t *testing.T) {
	c := newClient(t) // same helper the existing authz integration tests use
	ctx := context.Background()
	const (
		tenantA = "11111111-1111-1111-1111-111111111111"
		tenantB = "22222222-2222-2222-2222-222222222222"
		subject = "uid-nurse"
	)

	// A tenant with no role tuples has no members.
	member, err := c.IsMember(ctx, subject, tenantA)
	require.NoError(t, err)
	require.False(t, member, "a tenant with no roles wired has no members")

	// Wiring the tenant->role edge alone does not make anyone a member:
	// membership derives through an assignee, so an unassigned role
	// grants membership to nobody.
	require.NoError(t, c.GrantTenantRole(ctx, tenantA, authz.RoleNurse))
	member, err = c.IsMember(ctx, subject, tenantA)
	require.NoError(t, err)
	require.False(t, member, "a tenant->role edge with no assignee makes nobody a member")

	// Assigning the role is what makes the subject a member.
	require.NoError(t, c.GrantRole(ctx, tenantA, subject, authz.RoleNurse))
	member, err = c.IsMember(ctx, subject, tenantA)
	require.NoError(t, err)
	require.True(t, member)

	// Membership does not leak across tenants.
	member, err = c.IsMember(ctx, subject, tenantB)
	require.NoError(t, err)
	require.False(t, member, "membership in A must not imply membership in B")

	// Revoking the role revokes membership, with no separate write.
	require.NoError(t, c.RevokeRole(ctx, tenantA, subject, authz.RoleNurse))
	member, err = c.IsMember(ctx, subject, tenantA)
	require.NoError(t, err)
	require.False(t, member, "revoking the role revokes membership")
}

// TestEnsureModelUpgradesAnExistingStore is the migration-safety test:
// ensureModel used to return early whenever any model existed, which
// would leave a store created before the tenant type forever without a
// `member` relation. See D6 in the design spec — every IsMember call
// against such a store would return false, locking out every user in
// every tenant.
func TestEnsureModelUpgradesAnExistingStore(t *testing.T) {
	url := testinfra.StartOpenFGA(t)
	ctx := context.Background()
	store := "upgrade-" + t.Name()

	// Boot once against a deliberately older model to simulate a store
	// created before the tenant type existed.
	old, err := authz.NewClient(ctx, url, store)
	require.NoError(t, err)
	require.NoError(t, old.WriteModelForTest(ctx, legacyModelJSON))

	// Booting again must notice the store's model differs from the
	// desired one and write a new version.
	upgraded, err := authz.NewClient(ctx, url, store)
	require.NoError(t, err)

	const (
		tenantID = "33333333-3333-3333-3333-333333333333"
		subject  = "uid-upgrade"
	)
	require.NoError(t, upgraded.GrantTenantRole(ctx, tenantID, authz.RoleNurse))
	require.NoError(t, upgraded.GrantRole(ctx, tenantID, subject, authz.RoleNurse))

	member, err := upgraded.IsMember(ctx, subject, tenantID)
	require.NoError(t, err)
	require.True(t, member, "a store created before the tenant type must be upgraded on boot")
}

// legacyModelJSON is a verbatim copy of modelJSON before the tenant type
// existed (user/role/perm only), used to seed a store the way an
// existing production store would look going into this change.
const legacyModelJSON = `{
  "schema_version": "1.1",
  "type_definitions": [
    { "type": "user" },
    {
      "type": "role",
      "relations": { "assignee": { "this": {} } },
      "metadata": {
        "relations": {
          "assignee": { "directly_related_user_types": [{ "type": "user" }] }
        }
      }
    },
    {
      "type": "perm",
      "relations": {
        "granted_role": { "this": {} },
        "can_do": {
          "tupleToUserset": {
            "tupleset": { "relation": "granted_role" },
            "computedUserset": { "relation": "assignee" }
          }
        }
      },
      "metadata": {
        "relations": {
          "granted_role": { "directly_related_user_types": [{ "type": "role" }] }
        }
      }
    }
  ]
}`
