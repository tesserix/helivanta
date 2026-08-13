package iam_test

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/hms/internal/modules/iam" //nolint:depguard // external test package importing the module under test (self-import), not cross-module coupling
	"github.com/tesserix/hms/internal/platform"
	"github.com/tesserix/hms/internal/testutil"
	"github.com/tesserix/hms/pkg/authz"
)

// recordingWriter captures tuple writes so tests can assert the consumer
// applied them, without needing an OpenFGA container.
type recordingWriter struct {
	mu      sync.Mutex
	granted []string
	revoked []string
}

func (w *recordingWriter) GrantRole(_ context.Context, tenantID, subject string, role authz.Role) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.granted = append(w.granted, tenantID+"|"+subject+"|"+string(role))
	return nil
}

func (w *recordingWriter) RevokeRole(_ context.Context, tenantID, subject string, role authz.Role) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.revoked = append(w.revoked, tenantID+"|"+subject+"|"+string(role))
	return nil
}

func (w *recordingWriter) GrantPermission(context.Context, string, authz.Permission, authz.Role) error {
	return nil
}

func (w *recordingWriter) GrantTenantRole(context.Context, string, authz.Role) error {
	return nil
}

func (w *recordingWriter) grants() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.granted...)
}

func (w *recordingWriter) revokes() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.revoked...)
}

func eventually(t *testing.T, fn func() bool) {
	t.Helper()
	require.Eventually(t, fn, 20*time.Second, 100*time.Millisecond)
}

func TestGrantReturns202AndAppliesTuple(t *testing.T) {
	w := &recordingWriter{}
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"admin": testutil.TenantA},
		Perms:  map[string][]authz.Permission{"admin": {iam.PermMemberManage}},
		Writer: w, Modules: []platform.Module{iam.New(nil)},
	})

	res := testutil.Do(r, "POST", "/v1/iam/members", "admin",
		`{"subject":"dr-jane","role_key":"doctor"}`)
	require.Equal(t, http.StatusAccepted, res.Code)

	eventually(t, func() bool {
		return len(w.grants()) == 1 &&
			w.grants()[0] == testutil.TenantA+"|dr-jane|doctor"
	})
}

// TestRepeatGrantRepublishesTuple asserts that re-issuing an identical
// grant is not a true no-op at the outbox level: it must republish and
// re-apply the tuple. This is the operator's only repair mechanism for
// FGA drift (the reconciler does not re-apply member->role tuples), so a
// suppressed republish on the "already granted" path would silently
// break self-healing.
func TestRepeatGrantRepublishesTuple(t *testing.T) {
	w := &recordingWriter{}
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"admin": testutil.TenantA},
		Perms:  map[string][]authz.Permission{"admin": {iam.PermMemberManage}},
		Writer: w, Modules: []platform.Module{iam.New(nil)},
	})

	body := `{"subject":"dr-jane","role_key":"doctor"}`
	res1 := testutil.Do(r, "POST", "/v1/iam/members", "admin", body)
	require.Equal(t, http.StatusAccepted, res1.Code)
	eventually(t, func() bool { return len(w.grants()) == 1 })

	res2 := testutil.Do(r, "POST", "/v1/iam/members", "admin", body)
	require.Equal(t, http.StatusAccepted, res2.Code)

	eventually(t, func() bool { return len(w.grants()) == 2 })
	require.Equal(t, testutil.TenantA+"|dr-jane|doctor", w.grants()[1])
}

func TestGrantRequiresMemberManage(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"nurse": testutil.TenantA},
		Perms:  map[string][]authz.Permission{"nurse": {}},
		Writer: &recordingWriter{}, Modules: []platform.Module{iam.New(nil)},
	})

	res := testutil.Do(r, "POST", "/v1/iam/members", "nurse",
		`{"subject":"dr-jane","role_key":"doctor"}`)
	require.Equal(t, http.StatusForbidden, res.Code)
}

// denyMembership is a fixed authz.MembershipChecker that refuses every
// caller.
type denyMembership struct{}

func (denyMembership) IsMember(context.Context, string, string) (bool, error) { return false, nil }

// TestPermissionGuardedRouteChecksMembershipBeforePermission closes a
// coverage gap found while implementing #781: with a caller who is
// BOTH not a member of the tenant AND missing the route's declared
// permission, both a correct and an (incorrectly) swapped middleware
// order produce the same 403 status code — status alone cannot tell
// membership actually ran first. What only the message body reveals is
// which check produced the denial. platform.Router.handle
// (internal/platform/router.go) runs RequireMembership before Require
// deliberately (see its comment: "a non-member gets the same answer on
// every route regardless of what permission it declares"), so the
// response here must be the membership denial, not "missing permission
// iam.member.manage" — a caller must never learn which permissions a
// route requires before learning they are not even a member.
func TestPermissionGuardedRouteChecksMembershipBeforePermission(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens:     map[string]string{"outsider": testutil.TenantA},
		Perms:      map[string][]authz.Permission{"outsider": {}}, // holds nothing, including iam.member.manage
		Writer:     &recordingWriter{},
		Membership: denyMembership{},
		Modules:    []platform.Module{iam.New(nil)},
	})

	res := testutil.Do(r, "POST", "/v1/iam/members", "outsider",
		`{"subject":"dr-jane","role_key":"doctor"}`)
	require.Equal(t, http.StatusForbidden, res.Code)
	require.Contains(t, res.Body.String(), "not a member of this tenant",
		"a non-member's denial must come from RequireMembership, not leak which permission the route requires")
	require.NotContains(t, res.Body.String(), "missing permission",
		"Require must never run first for a non-member")
}

func TestGrantRejectsUnknownRole(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"admin": testutil.TenantA},
		Perms:  map[string][]authz.Permission{"admin": {iam.PermMemberManage}},
		Writer: &recordingWriter{}, Modules: []platform.Module{iam.New(nil)},
	})

	res := testutil.Do(r, "POST", "/v1/iam/members", "admin",
		`{"subject":"dr-jane","role_key":"wizard"}`)
	require.Equal(t, http.StatusBadRequest, res.Code)
}

func TestRevokeRemovesRowAndTuple(t *testing.T) {
	w := &recordingWriter{}
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"admin": testutil.TenantA},
		Perms:  map[string][]authz.Permission{"admin": {iam.PermMemberManage}},
		Writer: w, Modules: []platform.Module{iam.New(nil)},
	})

	testutil.Do(r, "POST", "/v1/iam/members", "admin", `{"subject":"dr-jane","role_key":"doctor"}`)
	eventually(t, func() bool { return len(w.grants()) == 1 })

	res := testutil.Do(r, "DELETE", "/v1/iam/members/dr-jane/doctor", "admin", "")
	require.Equal(t, http.StatusAccepted, res.Code)

	eventually(t, func() bool {
		return len(w.revokes()) == 1 && w.revokes()[0] == testutil.TenantA+"|dr-jane|doctor"
	})

	list := testutil.Do(r, "GET", "/v1/iam/members", "admin", "")
	var body struct {
		Data []struct {
			Subject string `json:"subject"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(list.Body.Bytes(), &body))
	require.Empty(t, body.Data)
}

func TestMembersAreTenantIsolated(t *testing.T) {
	r, _, _, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Tokens: map[string]string{"admin-a": testutil.TenantA, "admin-b": testutil.TenantB},
		Perms: map[string][]authz.Permission{
			"admin-a": {iam.PermMemberManage},
			"admin-b": {iam.PermMemberManage},
		},
		Writer: &recordingWriter{}, Modules: []platform.Module{iam.New(nil)},
	})

	testutil.Do(r, "POST", "/v1/iam/members", "admin-a", `{"subject":"dr-jane","role_key":"doctor"}`)

	list := testutil.Do(r, "GET", "/v1/iam/members", "admin-b", "")
	var body struct {
		Data []json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(list.Body.Bytes(), &body))
	require.Empty(t, body.Data, "tenant B must never see tenant A's members")
}
