package authz

import (
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testinfra"
)

func syntheticOperatingAssignment() OperatingAssignment {
	return OperatingAssignment{TenantID: "11111111-1111-4111-8111-111111111111", EmployerKind: "supplier", EmployerID: "019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa", LocationID: "019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaab", ID: "019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaac", Version: 1, Subject: "TEST-operator", Identity: IndependentStaffReference{StaffID: uuid.MustParse("33333333-3333-4333-8333-333333333333"), StaffVersion: 1, LinkID: uuid.MustParse("019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaad"), LinkVersion: 2}, MembershipEpoch: 1, SourceVersion: 1, LocationVersion: 1, ScopeEpoch: 1, ValidFrom: time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC), ExpiresAt: time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC), Permissions: []Permission{"supply.operations.read", "supply.operations.write"}}
}
func TestOperatingAssignmentGraphRequiresMembershipAndCannotCrossSourceOrIdentity(t *testing.T) {
	ctx := t.Context()
	client, err := NewClient(ctx, testinfra.StartOpenFGA(t), testinfra.IsolationKey(t))
	require.NoError(t, err)
	assignment := syntheticOperatingAssignment()
	permission := assignment.Permissions[0]
	require.NoError(t, client.ProjectOperatingAssignment(ctx, assignment))
	allowed, err := client.CheckOperatingAssignment(ctx, assignment, assignment.Subject, permission)
	require.NoError(t, err)
	require.False(t, allowed)
	require.NoError(t, client.GrantTenantRole(ctx, assignment.TenantID, RoleStaffMember))
	require.NoError(t, client.GrantRole(ctx, assignment.TenantID, assignment.Subject, RoleStaffMember))
	allowed, err = client.CheckOperatingAssignment(ctx, assignment, assignment.Subject, permission)
	require.NoError(t, err)
	require.True(t, allowed)
	for name, change := range map[string]func(*OperatingAssignment){
		"employer kind":    func(v *OperatingAssignment) { v.EmployerKind = "pharmacy" },
		"employer":         func(v *OperatingAssignment) { v.EmployerID = "019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaae" },
		"location":         func(v *OperatingAssignment) { v.LocationID = "019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaae" },
		"identity":         func(v *OperatingAssignment) { v.Identity.StaffVersion++ },
		"link":             func(v *OperatingAssignment) { v.Identity.LinkVersion++ },
		"membership":       func(v *OperatingAssignment) { v.MembershipEpoch++ },
		"source version":   func(v *OperatingAssignment) { v.SourceVersion++ },
		"location version": func(v *OperatingAssignment) { v.LocationVersion++ },
		"scope epoch":      func(v *OperatingAssignment) { v.ScopeEpoch++ },
		"validity":         func(v *OperatingAssignment) { v.ExpiresAt = v.ExpiresAt.Add(time.Hour) },
	} {
		t.Run(name, func(t *testing.T) {
			changed := assignment
			change(&changed)
			allowed, err := client.CheckOperatingAssignment(ctx, changed, assignment.Subject, permission)
			require.NoError(t, err)
			require.False(t, allowed)
		})
	}
	allowed, err = client.CheckOperatingAssignment(ctx, assignment, "TEST-other", permission)
	require.NoError(t, err)
	require.False(t, allowed)
	resolved, err := client.Resolve(ctx, assignment.Subject, assignment.TenantID)
	require.NoError(t, err)
	require.False(t, resolved.Has(permission))
	require.NoError(t, client.RevokeRole(ctx, assignment.TenantID, assignment.Subject, RoleStaffMember))
	allowed, err = client.CheckOperatingAssignment(ctx, assignment, assignment.Subject, permission)
	require.NoError(t, err)
	require.False(t, allowed)
}
func TestOperatingAssignmentCanonicalContentAndInvalidScopes(t *testing.T) {
	original := syntheticOperatingAssignment()
	key, err := original.objectKey()
	require.NoError(t, err)
	reordered := original
	reordered.Permissions = slices.Clone(original.Permissions)
	slices.Reverse(reordered.Permissions)
	same, err := reordered.objectKey()
	require.NoError(t, err)
	require.Equal(t, key, same)
	reordered = original
	reordered.ValidFrom = original.ValidFrom.In(time.FixedZone("TEST", 19800))
	same, err = reordered.objectKey()
	require.NoError(t, err)
	require.Equal(t, key, same)
	for name, change := range map[string]func(*OperatingAssignment){
		"hospital root":        func(v *OperatingAssignment) { v.EmployerKind = "hospital" },
		"missing identity":     func(v *OperatingAssignment) { v.Identity = IndependentStaffReference{} },
		"missing source":       func(v *OperatingAssignment) { v.LocationID = "" },
		"ambiguous permission": func(v *OperatingAssignment) { v.Permissions = append(slices.Clone(v.Permissions), v.Permissions[0]) },
		"unsafe version":       func(v *OperatingAssignment) { v.Version = 9007199254740992 },
		"unreviewed link":      func(v *OperatingAssignment) { v.Identity.LinkVersion = 1 },
		"no validity":          func(v *OperatingAssignment) { v.ExpiresAt = v.ValidFrom },
	} {
		t.Run(name, func(t *testing.T) {
			changed := original
			change(&changed)
			_, err := changed.objectKey()
			require.ErrorIs(t, err, ErrInvalidScopedAssignment)
		})
	}
}
