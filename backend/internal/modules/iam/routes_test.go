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
	r, _, _, _ := testutil.ModuleHarnessWithAuthz(t,
		map[string]string{"admin": testutil.TenantA},
		map[string][]authz.Permission{"admin": {iam.PermMemberManage}},
		w, iam.New())

	res := testutil.Do(r, "POST", "/v1/iam/members", "admin",
		`{"subject":"dr-jane","role_key":"doctor"}`)
	require.Equal(t, http.StatusAccepted, res.Code)

	eventually(t, func() bool {
		return len(w.grants()) == 1 &&
			w.grants()[0] == testutil.TenantA+"|dr-jane|doctor"
	})
}

func TestGrantRequiresMemberManage(t *testing.T) {
	r, _, _, _ := testutil.ModuleHarnessWithAuthz(t,
		map[string]string{"nurse": testutil.TenantA},
		map[string][]authz.Permission{"nurse": {}},
		&recordingWriter{}, iam.New())

	res := testutil.Do(r, "POST", "/v1/iam/members", "nurse",
		`{"subject":"dr-jane","role_key":"doctor"}`)
	require.Equal(t, http.StatusForbidden, res.Code)
}

func TestGrantRejectsUnknownRole(t *testing.T) {
	r, _, _, _ := testutil.ModuleHarnessWithAuthz(t,
		map[string]string{"admin": testutil.TenantA},
		map[string][]authz.Permission{"admin": {iam.PermMemberManage}},
		&recordingWriter{}, iam.New())

	res := testutil.Do(r, "POST", "/v1/iam/members", "admin",
		`{"subject":"dr-jane","role_key":"wizard"}`)
	require.Equal(t, http.StatusBadRequest, res.Code)
}

func TestRevokeRemovesRowAndTuple(t *testing.T) {
	w := &recordingWriter{}
	r, _, _, _ := testutil.ModuleHarnessWithAuthz(t,
		map[string]string{"admin": testutil.TenantA},
		map[string][]authz.Permission{"admin": {iam.PermMemberManage}},
		w, iam.New())

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
	r, _, _, _ := testutil.ModuleHarnessWithAuthz(t,
		map[string]string{"admin-a": testutil.TenantA, "admin-b": testutil.TenantB},
		map[string][]authz.Permission{
			"admin-a": {iam.PermMemberManage},
			"admin-b": {iam.PermMemberManage},
		},
		&recordingWriter{}, iam.New())

	testutil.Do(r, "POST", "/v1/iam/members", "admin-a", `{"subject":"dr-jane","role_key":"doctor"}`)

	list := testutil.Do(r, "GET", "/v1/iam/members", "admin-b", "")
	var body struct {
		Data []json.RawMessage `json:"data"`
	}
	require.NoError(t, json.Unmarshal(list.Body.Bytes(), &body))
	require.Empty(t, body.Data, "tenant B must never see tenant A's members")
}
