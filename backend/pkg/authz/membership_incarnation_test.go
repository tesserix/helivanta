package authz

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/testinfra"
)

func TestMembershipProjectionBindsCurrentIncarnationPermissions(t *testing.T) {
	ctx := t.Context()
	graph, err := NewClient(ctx, testinfra.StartOpenFGA(t), testinfra.IsolationKey(t))
	require.NoError(t, err)
	a := Membership{TenantID: "11111111-1111-4111-8111-111111111111", ID: "22222222-2222-4222-8222-222222222222", Subject: "synthetic-membership-holder", Role: RoleDoctor, Epoch: 1}
	permission := Permission("medicore.visit.read")
	require.NoError(t, graph.GrantPermission(ctx, a.TenantID, permission, a.Role))
	require.NoError(t, graph.ProjectMembership(ctx, a))
	set, err := graph.ResolveMembership(ctx, a)
	require.NoError(t, err)
	require.True(t, set.Has(permission))
	ready, err := graph.IsMembershipReady(ctx, a)
	require.NoError(t, err)
	require.True(t, ready)
	b := a
	b.ID = "33333333-3333-4333-8333-333333333333"
	b.Epoch = 2
	set, err = graph.ResolveMembership(ctx, b)
	require.NoError(t, err)
	require.Empty(t, set, "an unprojected replacement cannot borrow the old principal's permissions")
	require.NoError(t, graph.ProjectMembership(ctx, b))
	bindings, err := graph.ReadMembershipBindings(ctx, a.Subject, a.TenantID)
	require.NoError(t, err)
	require.Len(t, bindings, 2)
	aObject, err := a.Object()
	require.NoError(t, err)
	retired := false
	for _, binding := range bindings {
		if binding.Object == aObject {
			require.NoError(t, graph.RetireMembershipBinding(ctx, binding))
			retired = true
		}
	}
	require.True(t, retired)
	set, err = graph.ResolveMembership(ctx, b)
	require.NoError(t, err)
	require.True(t, set.Has(permission), "delayed revocation of A must preserve B")
	set, err = graph.ResolveMembership(ctx, a)
	require.NoError(t, err)
	require.Empty(t, set)
	for _, changed := range []Membership{
		{TenantID: b.TenantID, ID: b.ID, Subject: "another-synthetic-holder", Role: b.Role, Epoch: b.Epoch},
		{TenantID: b.TenantID, ID: b.ID, Subject: b.Subject, Role: RolePharmacist, Epoch: b.Epoch},
		{TenantID: b.TenantID, ID: b.ID, Subject: b.Subject, Role: b.Role, Epoch: 3},
		{TenantID: "44444444-4444-4444-8444-444444444444", ID: b.ID, Subject: b.Subject, Role: b.Role, Epoch: b.Epoch},
	} {
		ready, err = graph.IsMembershipReady(ctx, changed)
		require.NoError(t, err)
		require.False(t, ready)
	}
}

func TestMembershipGraphRejectsIncompleteAndConflictingProof(t *testing.T) {
	upstream, err := url.Parse(testinfra.StartOpenFGA(t))
	require.NoError(t, err)
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	var fault atomic.Int32
	proxy.ModifyResponse = func(response *http.Response) error {
		mode := fault.Load()
		isList := strings.HasSuffix(response.Request.URL.Path, "/list-objects")
		if mode == 0 || (mode == 6 && !isList) || (mode != 6 && !strings.HasSuffix(response.Request.URL.Path, "/batch-check")) {
			return nil
		}
		results := map[string]any{"subject": map[string]any{"allowed": true}, "tenant": map[string]any{"allowed": true}}
		switch mode {
		case 1:
			delete(results, "tenant")
		case 2:
			results["tenant"] = map[string]any{"error": map[string]any{"message": "synthetic provider failure"}}
		case 3:
			results["tenant"] = map[string]any{}
		case 4:
			results["extra"] = map[string]any{"allowed": true}
		case 5:
			results["tenant"] = map[string]any{"allowed": false}
		}
		wire := map[string]any{"result": results}
		if mode == 6 {
			wire = map[string]any{"unexpected": "missing required objects"}
		}
		body, err := json.Marshal(wire)
		if err != nil {
			return err
		}
		if err := response.Body.Close(); err != nil {
			return err
		}
		response.Body = io.NopCloser(bytes.NewReader(body))
		response.ContentLength = int64(len(body))
		response.Header.Set("Content-Length", strconv.Itoa(len(body)))
		return nil
	}
	server := httptest.NewServer(proxy)
	t.Cleanup(server.Close)
	graph, err := NewClient(t.Context(), server.URL, testinfra.IsolationKey(t))
	require.NoError(t, err)
	member := Membership{TenantID: "11111111-1111-4111-8111-111111111111", ID: "22222222-2222-4222-8222-222222222222", Subject: "synthetic-membership-proof", Role: RoleDoctor, Epoch: 1}
	require.NoError(t, graph.GrantPermission(t.Context(), member.TenantID, "medicore.visit.read", member.Role))
	require.NoError(t, graph.ProjectMembership(t.Context(), member))
	for mode, name := range map[int32]string{1: "missing_result", 2: "item_error", 3: "missing_allowed", 4: "extra_result", 5: "explicit_denial", 6: "missing_objects"} {
		t.Run(name, func(t *testing.T) {
			fault.Store(mode)
			ready, err := graph.IsMembershipReady(t.Context(), member)
			require.False(t, ready)
			if mode == 5 {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, ErrMembershipUnavailable)
			}
			permissions, err := graph.ResolveMembership(t.Context(), member)
			require.Empty(t, permissions)
			if mode == 5 {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, ErrMembershipUnavailable)
			}
		})
	}
	fault.Store(0)
	object, err := member.Object()
	require.NoError(t, err)
	require.NoError(t, graph.write(t.Context(), object, "assignee", RoleObject(member.TenantID, RoleNurse)))
	ready, err := graph.IsMembershipReady(t.Context(), member)
	require.False(t, ready)
	require.ErrorIs(t, err, ErrMembershipUnavailable, "an extra graph role cannot enlarge or substitute the source role")
}
