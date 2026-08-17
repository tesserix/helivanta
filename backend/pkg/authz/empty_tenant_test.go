package authz_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/pkg/authz"
)

// TestEmptyTenantDeniesAgainstRealOpenFGA is plan Task 4's Step Zero,
// pinned permanently rather than run once and discarded: the interim
// state between Task 3 and Task 4/5 (docs/superpowers/plans/2026-08-15-zitadel-auth.md)
// left authn.Principal.TenantID == "" for every request, on the REASONED
// but never OBSERVED belief that Resolve and IsMember both deny an empty
// tenant. Engineering-principles.md §5 forbids relying on a claim that
// was never tested against the real thing this ran against — a real
// OpenFGA, not a fake — so this proves it and keeps proving it on every
// CI run, since a future change to Resolve/IsMember's object-namespacing
// could silently reopen the hole this interim state depended on staying
// shut.
//
// A fully-privileged tenant_admin is granted real roles and permissions
// in a real tenant, precisely so that an empty tenant admitting anything
// would be caught here rather than reasoned away: if the emptiness of
// tenantID broke the "perm:<tenantID>/" or "tenant:<tenantID>" object
// namespacing in some way that leaked ACROSS tenants, this subject is the
// one that would show it.
func TestEmptyTenantDeniesAgainstRealOpenFGA(t *testing.T) {
	ctx := context.Background()
	c := newClient(t)
	const (
		realTenant = "11111111-1111-1111-1111-111111111111"
		subject    = "uid-fully-privileged"
	)

	require.NoError(t, c.GrantTenantRole(ctx, realTenant, authz.RoleTenantAdmin))
	require.NoError(t, c.GrantPermission(ctx, realTenant, "iam.member.manage", authz.RoleTenantAdmin))
	require.NoError(t, c.GrantRole(ctx, realTenant, subject, authz.RoleTenantAdmin))

	// Sanity: the subject really does hold real permissions in its real
	// tenant, so the assertions below are not vacuously true because
	// nothing was ever granted anywhere.
	inReal, err := c.Resolve(ctx, subject, realTenant)
	require.NoError(t, err)
	require.NotEmpty(t, inReal.Sorted(), "sanity: the subject must hold permissions in its real tenant")

	realMember, err := c.IsMember(ctx, subject, realTenant)
	require.NoError(t, err)
	require.True(t, realMember, "sanity: the subject must be a member of its real tenant")

	// The claim under test: an empty tenant admits NOTHING, for the very
	// same subject that just proved it holds real access elsewhere.
	//
	// Resolve(subject, "") is OBSERVED to return no error and an EMPTY
	// permission set: "perm:/" matches no real object, because every
	// real permission object is namespaced "perm:<realTenantID>/...".
	inEmpty, err := c.Resolve(ctx, subject, "")
	require.NoError(t, err)
	require.Empty(t, inEmpty.Sorted(),
		"an empty tenant_id must resolve to an empty permission set — if this fails, TenantID==\"\" is a cross-tenant hole")

	// IsMember(subject, "") is OBSERVED to behave DIFFERENTLY from
	// Resolve: TenantObject("") is the malformed object id "tenant:",
	// which OpenFGA's Check API refuses outright with a validation
	// error, not a clean "false". That is still a denial in the
	// direction that matters — authz.RequireMembership treats any
	// IsMember error as 503 "authorization is temporarily unavailable"
	// and never as an implicit pass (pkg/authz/membership.go) — but it
	// is a materially different failure mode from what the interim-state
	// note reasoned ("an empty tenant resolves to ... no membership"),
	// so it is asserted explicitly here rather than folded into a
	// generic NoError/False pair that would silently paper over which
	// one actually happened.
	emptyMember, err := c.IsMember(ctx, subject, "")
	if err != nil {
		require.False(t, emptyMember, "an error from IsMember must never be paired with member=true")
	} else {
		require.False(t, emptyMember,
			"an empty tenant_id must never report membership — if this fails, TenantID==\"\" is a cross-tenant hole")
	}
}
