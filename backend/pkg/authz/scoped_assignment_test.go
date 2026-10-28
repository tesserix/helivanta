package authz

import (
	"fmt"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testinfra"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

func TestScopedAssignmentIsolation(t *testing.T) {
	ctx := t.Context()
	client, err := NewClient(ctx, testinfra.StartOpenFGA(t), testinfra.IsolationKey(t))
	require.NoError(t, err)
	a := syntheticScopedAssignment()
	require.NoError(t, client.ProjectScopedAssignment(ctx, a))
	allowed, err := client.CheckScopedAssignment(ctx, a, a.Subject, a.Permissions[0])
	require.NoError(t, err)
	require.False(t, allowed, "a scoped assignment must not establish tenant membership")
	require.NoError(t, client.GrantTenantRole(ctx, a.TenantID, RoleStaffMember))
	require.NoError(t, client.GrantRole(ctx, a.TenantID, a.Subject, RoleStaffMember))
	allowed, err = client.CheckScopedAssignment(ctx, a, a.Subject, a.Permissions[0])
	require.NoError(t, err)
	require.True(t, allowed)
	for name, changed := range map[string]ScopedAssignment{
		"hospital": func() ScopedAssignment { v := a; v.HospitalID = "44444444-4444-4444-8444-444444444444"; return v }(),
		"staff":    func() ScopedAssignment { v := a; v.ScopeID = "44444444-4444-4444-8444-444444444444"; return v }(),
		"tenant":   func() ScopedAssignment { v := a; v.TenantID = "44444444-4444-4444-8444-444444444444"; return v }(),
		"revision": func() ScopedAssignment { v := a; v.Version = 2; return v }(),
	} {
		t.Run(name, func(t *testing.T) {
			allowed, err := client.CheckScopedAssignment(ctx, changed, a.Subject, a.Permissions[0])
			require.NoError(t, err)
			require.False(t, allowed)
		})
	}
	allowed, err = client.CheckScopedAssignment(ctx, a, "other-staff", a.Permissions[0])
	require.NoError(t, err)
	require.False(t, allowed)
	allowed, err = client.CheckScopedAssignment(ctx, a, a.Subject, "workforce.leave.approve")
	require.NoError(t, err)
	require.False(t, allowed)
	resolved, err := client.Resolve(ctx, a.Subject, a.TenantID)
	require.NoError(t, err)
	require.False(t, resolved.Has(a.Permissions[0]), "scoped permission must not enter tenant-wide resolution")
	require.NoError(t, client.ProjectScopedAssignment(ctx, a))
	require.NoError(t, client.RevokeRole(ctx, a.TenantID, a.Subject, RoleStaffMember))
	allowed, err = client.CheckScopedAssignment(ctx, a, a.Subject, a.Permissions[0])
	require.NoError(t, err)
	require.False(t, allowed)
}

func syntheticScopedAssignment() ScopedAssignment {
	return ScopedAssignment{
		TenantID:   "11111111-1111-4111-8111-111111111111",
		HospitalID: "22222222-2222-4222-8222-222222222222",
		ScopeKind:  "staff", ScopeID: "33333333-3333-4333-8333-333333333333",
		ID: "019aaaaa-aaaa-7aaa-8aaa-aaaaaaaaaaaa", Version: 1,
		Subject: "synthetic-staff", Permissions: []Permission{"workforce.leave.self_request"},
	}
}

func TestScopedAssignmentRejectsMalformedInput(t *testing.T) {
	var client Client // Invalid input must return before attempting any network operation.
	mutations := map[string]func(*ScopedAssignment){
		"missing tenant":       func(a *ScopedAssignment) { a.TenantID = "" },
		"nil hospital":         func(a *ScopedAssignment) { a.HospitalID = "00000000-0000-0000-0000-000000000000" },
		"unknown scope":        func(a *ScopedAssignment) { a.ScopeKind = "everything" },
		"mismatched hospital":  func(a *ScopedAssignment) { a.ScopeKind = "hospital" },
		"invalid staff":        func(a *ScopedAssignment) { a.ScopeID = "staff:other" },
		"old command ID":       func(a *ScopedAssignment) { a.ID = a.TenantID },
		"no revision":          func(a *ScopedAssignment) { a.Version = 0 },
		"wildcard subject":     func(a *ScopedAssignment) { a.Subject = "*" },
		"userset subject":      func(a *ScopedAssignment) { a.Subject = "role#assignee" },
		"spaced subject":       func(a *ScopedAssignment) { a.Subject = " user" },
		"unicode space":        func(a *ScopedAssignment) { a.Subject = "user\u0085name" },
		"no permission":        func(a *ScopedAssignment) { a.Permissions = nil },
		"public permission":    func(a *ScopedAssignment) { a.Permissions = []Permission{Public} },
		"wildcard permission":  func(a *ScopedAssignment) { a.Permissions = []Permission{"workforce.*.read"} },
		"duplicate permission": func(a *ScopedAssignment) { a.Permissions = append(a.Permissions, a.Permissions[0]) },
		"too many permissions": func(a *ScopedAssignment) { a.Permissions = make([]Permission, 33) },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			a := syntheticScopedAssignment()
			mutate(&a)
			require.ErrorIs(t, client.ProjectScopedAssignment(t.Context(), a), ErrInvalidScopedAssignment)
			allowed, err := client.CheckScopedAssignment(t.Context(), a, "synthetic-staff", "workforce.leave.self_request")
			require.ErrorIs(t, err, ErrInvalidScopedAssignment)
			require.False(t, allowed)
		})
	}
}

func TestScopedAssignmentProjectionRecovery(t *testing.T) {
	ctx := t.Context()
	endpoint, err := url.Parse(testinfra.StartOpenFGA(t))
	require.NoError(t, err)
	reverse := httputil.NewSingleHostReverseProxy(endpoint)
	var failing atomic.Bool
	var writes atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failing.Load() && strings.HasSuffix(r.URL.Path, "/write") && writes.Add(1) >= 3 {
			http.Error(w, `{"code":"validation_error","message":"synthetic interrupted projection"}`, 400)
			return
		}
		reverse.ServeHTTP(w, r)
	}))
	t.Cleanup(proxy.Close)
	client, err := NewClient(ctx, proxy.URL, testinfra.IsolationKey(t))
	require.NoError(t, err)
	a := syntheticScopedAssignment()
	require.NoError(t, client.GrantTenantRole(ctx, a.TenantID, RoleStaffMember))
	require.NoError(t, client.GrantRole(ctx, a.TenantID, a.Subject, RoleStaffMember))
	failing.Store(true)
	err = client.ProjectScopedAssignment(ctx, a)
	require.ErrorIs(t, err, ErrScopedAuthorizationUnavailable)
	require.NotContains(t, err.Error(), a.Subject)
	allowed, err := client.CheckScopedAssignment(ctx, a, a.Subject, a.Permissions[0])
	require.NoError(t, err)
	require.False(t, allowed)
	failing.Store(false)
	require.NoError(t, client.ProjectScopedAssignment(ctx, a))
	allowed, err = client.CheckScopedAssignment(ctx, a, a.Subject, a.Permissions[0])
	require.NoError(t, err)
	require.True(t, allowed)
	current := a
	current.Version = 2
	current.Permissions = []Permission{"workforce.leave.self_balance"}
	require.NoError(t, client.ProjectScopedAssignment(ctx, current))
	require.NoError(t, client.ProjectScopedAssignment(ctx, a))
	allowed, err = client.CheckScopedAssignment(ctx, current, current.Subject, a.Permissions[0])
	require.NoError(t, err)
	require.False(t, allowed)
	// Changing content without a new version cannot inherit the prior projection.
	changed := current
	changed.Permissions = []Permission{a.Permissions[0]}
	allowed, err = client.CheckScopedAssignment(ctx, changed, changed.Subject, a.Permissions[0])
	require.NoError(t, err)
	require.False(t, allowed)
	tuples, err := client.ReadTuplesByTenant(ctx)
	require.NoError(t, err)
	for _, rows := range tuples {
		for _, row := range rows {
			require.NotContains(t, row.Object, "assignment:")
			require.NotContains(t, row.Object, "scoped_permission:")
		}
	}
	proxy.Close()
	allowed, err = client.CheckScopedAssignment(ctx, current, current.Subject, current.Permissions[0])
	require.ErrorIs(t, err, ErrScopedAuthorizationUnavailable)
	require.False(t, allowed)
	require.ErrorIs(t, client.ProjectScopedAssignment(ctx, current), ErrScopedAuthorizationUnavailable)
}

func TestScopedAssignmentBoundedProjection(t *testing.T) {
	ctx := t.Context()
	client, err := NewClient(ctx, testinfra.StartOpenFGA(t), testinfra.IsolationKey(t))
	require.NoError(t, err)
	a := syntheticScopedAssignment()
	a.ScopeKind = "hospital"
	a.ScopeID = a.HospitalID
	a.Subject = strings.Repeat("s", 200)
	a.Permissions = make([]Permission, 32)
	for i := range a.Permissions {
		a.Permissions[i] = Permission(fmt.Sprintf("workforce.%s_%02d.read", strings.Repeat("p", 82), i))
	}
	require.Len(t, string(a.Permissions[0]), 100)
	require.NoError(t, client.GrantTenantRole(ctx, a.TenantID, RoleStaffMember))
	require.NoError(t, client.GrantRole(ctx, a.TenantID, a.Subject, RoleStaffMember))
	require.NoError(t, client.ProjectScopedAssignment(ctx, a))
	reversed := a
	reversed.Permissions = slices.Clone(a.Permissions)
	slices.Reverse(reversed.Permissions)
	for _, permission := range a.Permissions {
		allowed, err := client.CheckScopedAssignment(ctx, reversed, a.Subject, permission)
		require.NoError(t, err)
		require.True(t, allowed, "permission order must not change projection identity")
	}
	require.NoError(t, client.ProjectScopedAssignment(ctx, reversed))
}
