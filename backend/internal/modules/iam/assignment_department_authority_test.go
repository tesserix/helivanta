package iam_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tesserix/helivanta/internal/globalapi"
	"github.com/tesserix/helivanta/internal/modules/iam" //nolint:depguard // External test uses its own module; architecture checks enforce ownership.
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
	"gorm.io/gorm"
)

const departmentGrantSubject = "user-department0x"

type departmentGrantHarness struct {
	db        *tenantdb.DB
	bus       *events.Bus
	fga       *authz.Client
	authority interface {
		authz.ScopedAuthorizer
		authz.ScopedCommandGuard
		authz.DepartmentScopeLister
	}
	gateway  http.Handler
	hospital string
}

func newDepartmentGrantHarness(t *testing.T) *departmentGrantHarness {
	t.Helper()
	return newGrantHarness(t, iam.New(nil))
}

// newGrantHarness lets a backfill test stop IAM before a migration while Workforce stays current.
func newGrantHarness(t *testing.T, module platform.Module) *departmentGrantHarness {
	t.Helper()
	fga, err := authz.NewClient(t.Context(), testinfra.StartOpenFGA(t), testinfra.IsolationKey(t))
	require.NoError(t, err)
	r, db, bus, _ := testutil.NewHarness(t, testutil.HarnessOptions{
		Writer: fga, Roles: fga,
		Tokens: map[string]string{"admin": testutil.TenantA, "reviewer": testutil.TenantA},
		Perms: map[string][]authz.Permission{
			"admin":    {iam.PermMemberManage, assignmentHospitalManage, "iam.assignment.manage"},
			"reviewer": {"iam.assignment.review"},
		},
		Modules: []platform.Module{module, assignmentWorkforceModule()},
	})
	upstream := httptest.NewServer(r)
	t.Cleanup(upstream.Close)
	gateway, err := globalapi.New(globalapi.Config{Development: true, Timeout: 5 * time.Second, Upstreams: map[string]string{"platform": upstream.URL, "workforce": upstream.URL}})
	require.NoError(t, err)
	authority, ok := iam.NewScopedAuthorizer(db, fga).(interface {
		authz.ScopedAuthorizer
		authz.ScopedCommandGuard
		authz.DepartmentScopeLister
	})
	require.True(t, ok, "IAM lists department grants beside command authority")
	h := &departmentGrantHarness{db: db, bus: bus, fga: fga, authority: authority, gateway: gateway, hospital: uuid.NewString()}
	for _, command := range []struct{ path, body string }{
		{"/v1/workforce/hospitals/" + h.hospital, `{"name":"Synthetic department authority"}`},
		{"/v1/workforce/hospitals/" + h.hospital + "/access/user-admin", `{"level":"manager"}`},
		{"/v1/workforce/hospitals/" + h.hospital + "/access/user-reviewer", `{"level":"manager"}`},
	} {
		w := h.call("PUT", command.path, "admin", command.body)
		require.Equal(t, 200, w.Code, w.Body.String())
	}
	for subject, role := range map[string]string{"user-admin": "tenant_admin", "user-reviewer": "tenant_admin", departmentGrantSubject: "staff_member"} {
		w := h.call("POST", "/v1/iam/members", "admin", fmt.Sprintf(`{"subject":%q,"role_key":%q}`, subject, role))
		require.Equal(t, 202, w.Code, w.Body.String())
	}
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		member, err := fga.IsMember(t.Context(), departmentGrantSubject, testutil.TenantA)
		require.NoError(c, err)
		require.True(c, member)
	}, 20*time.Second, 100*time.Millisecond)
	return h
}

func (h *departmentGrantHarness) call(method, path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.gateway.ServeHTTP(w, req)
	return w
}

func (h *departmentGrantHarness) session(t require.TestingT, fn func(*gorm.DB) error) {
	require.NoError(t, h.db.WithTenant(context.Background(), testutil.TenantA, func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT set_config('app.assignment_access','authorized',true),set_config('app.assignment_department_scope','enabled',true)`).Error; err != nil {
			return err
		}
		return fn(tx)
	}))
}

func (h *departmentGrantHarness) row(t require.TestingT, id string) departmentProposalRow {
	var out departmentProposalRow
	h.session(t, func(tx *gorm.DB) error {
		return tx.Table("iam_assignment_proposals").Select("request_id,status,version").Where("id=?", id).Take(&out).Error
	})
	return out
}

// settle waits for Workforce, then answers for Organization when the proposal names a department.
func (h *departmentGrantHarness) settle(t *testing.T, id, department, want string) {
	t.Helper()
	var current departmentProposalRow
	require.EventuallyWithT(t, func(c *assert.CollectT) {
		current = h.row(c, id)
		var owners int64
		h.session(c, func(tx *gorm.DB) error {
			return tx.Table("iam_assignment_department_verifications").Where("proposal_id=? AND request_id=? AND owner='workforce'", id, current.RequestID).Count(&owners).Error
		})
		require.True(c, current.Status == want || owners == 1, "Workforce answers the current request")
	}, 20*time.Second, 100*time.Millisecond)
	if department != "" {
		data, err := json.Marshal(map[string]any{"proposal_id": id, "request_id": current.RequestID, "hospital_id": h.hospital, "department_id": department, "eligible": true})
		require.NoError(t, err)
		h.session(t, func(tx *gorm.DB) error {
			return h.bus.Publish(tx, departmentVerified, events.Event{ID: uuid.Must(uuid.NewV7()).String(), Type: departmentVerified, Version: 1, TenantID: testutil.TenantA, Data: data})
		})
	}
	require.EventuallyWithT(t, func(c *assert.CollectT) { require.Equal(c, want, h.row(c, id).Status) }, 20*time.Second, 100*time.Millisecond)
}

// activate drives one proposal through both owners, an independent approval and projection.
func (h *departmentGrantHarness) activate(t *testing.T, template, department string) string {
	t.Helper()
	field := ""
	if department != "" {
		field = fmt.Sprintf(`"department_id":%q,`, department)
	}
	id := uuid.Must(uuid.NewV7()).String()
	body := fmt.Sprintf(`{"hospital_id":%q,%s"subject":%q,"template_key":%q,"reason":"Synthetic department authority","valid_from":%q,"expires_at":%q}`,
		h.hospital, field, departmentGrantSubject, template, time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Add(time.Hour).Format(time.RFC3339))
	w := h.call("PUT", "/v1/iam/assignment-proposals/"+id, "admin", body)
	require.Equal(t, 200, w.Code, w.Body.String())
	h.settle(t, id, department, "pending_review")
	w = h.call("POST", "/v1/iam/assignment-proposals/"+id+"/decision", "reviewer", fmt.Sprintf(`{"expected_version":%d,"decision":"approve","reason":"Independent synthetic department review"}`, h.row(t, id).Version))
	require.Equal(t, 200, w.Code, w.Body.String())
	h.settle(t, id, department, "active")
	return id
}

func (h *departmentGrantHarness) request(path ...string) authz.ScopeRequest {
	return authz.ScopeRequest{TenantID: testutil.TenantA, Subject: departmentGrantSubject, HospitalID: h.hospital, Permission: "workforce.posting.review", DepartmentPath: path}
}

func v7() string { return uuid.Must(uuid.NewV7()).String() }

func TestDepartmentGrantReachesOnlyItsSubtree(t *testing.T) {
	h := newDepartmentGrantHarness(t)
	department, child, sibling, parent := v7(), v7(), v7(), v7()
	id := h.activate(t, departmentReviewTemplate, department)
	ctx := t.Context()

	t.Run("projection writes the department scope", func(t *testing.T) {
		var snapshot struct {
			Version     int64
			Permissions []byte
		}
		h.session(t, func(tx *gorm.DB) error {
			return tx.Table("iam_assignment_snapshots").Select("version,permissions").Where("id=?", id).Order("version DESC").Take(&snapshot).Error
		})
		var permissions []authz.Permission
		require.NoError(t, json.Unmarshal(snapshot.Permissions, &permissions))
		graph := authz.ScopedAssignment{TenantID: testutil.TenantA, HospitalID: h.hospital, ScopeKind: "department", ScopeID: department, ID: id, Version: snapshot.Version, Subject: departmentGrantSubject, Permissions: permissions}
		allowed, err := h.fga.CheckScopedAssignment(ctx, graph, departmentGrantSubject, "workforce.posting.review")
		require.NoError(t, err)
		require.True(t, allowed)
		graph.ScopeKind, graph.ScopeID = "hospital", h.hospital
		allowed, err = h.fga.CheckScopedAssignment(ctx, graph, departmentGrantSubject, "workforce.posting.review")
		require.NoError(t, err)
		require.False(t, allowed, "a department grant is never projected as hospital authority")
	})
	t.Run("a hospital-only request never matches a department grant", func(t *testing.T) {
		grant, err := h.authority.AuthorizeScope(ctx, h.request())
		require.NoError(t, err)
		require.Empty(t, grant.AssignmentID)
	})
	t.Run("a target in the department or a sub-unit is granted", func(t *testing.T) {
		for _, path := range [][]string{{department, parent}, {child, department, parent}, {department}} {
			grant, err := h.authority.AuthorizeScope(ctx, h.request(path...))
			require.NoError(t, err)
			require.Equal(t, id, grant.AssignmentID)
			require.Equal(t, "department", grant.ScopeKind)
			require.Equal(t, department, grant.DepartmentID)
		}
	})
	t.Run("sibling and parent targets are denied", func(t *testing.T) {
		for _, path := range [][]string{{sibling, parent}, {parent}} {
			grant, err := h.authority.AuthorizeScope(ctx, h.request(path...))
			require.NoError(t, err)
			require.Empty(t, grant.AssignmentID)
		}
	})
	t.Run("malformed paths are denied", func(t *testing.T) {
		long := []string{department}
		for range authz.MaxDepartmentPath {
			long = append(long, v7())
		}
		for name, path := range map[string][]string{
			"too deep": long, "repeated": {department, department}, "hospital": {department, h.hospital},
			"non-v7": {department, uuid.NewString()}, "non-canonical": {strings.ToUpper(department)}, "empty": {""},
		} {
			t.Run(name, func(t *testing.T) {
				grant, err := h.authority.AuthorizeScope(ctx, h.request(path...))
				require.NoError(t, err)
				require.Empty(t, grant.AssignmentID)
			})
		}
	})
	t.Run("a foreign tenant or hospital sees nothing", func(t *testing.T) {
		foreign := h.request(department)
		foreign.TenantID = testutil.TenantB
		other := h.request(department)
		other.HospitalID = uuid.NewString()
		for _, r := range []authz.ScopeRequest{foreign, other} {
			grant, err := h.authority.AuthorizeScope(ctx, r)
			require.NoError(t, err)
			require.Empty(t, grant.AssignmentID)
			r.DepartmentPath = nil
			scopes, err := h.authority.DepartmentScopes(ctx, r)
			require.NoError(t, err)
			require.Empty(t, scopes)
		}
	})

	checked := h.request(child, department, parent)
	grant, err := h.authority.AuthorizeScope(ctx, checked)
	require.NoError(t, err)
	t.Run("commands hold the grant only for its checked path", func(t *testing.T) {
		ran := false
		require.NoError(t, h.authority.WithScopeGrant(ctx, checked, grant, time.Now().UTC(), func(ctx context.Context) error {
			ran = true
			held, ok := authz.ScopedCommandGrant(ctx, checked)
			require.True(t, ok)
			require.Equal(t, grant, held)
			return nil
		}))
		require.True(t, ran)
		for name, r := range map[string]authz.ScopeRequest{"hospital-only": h.request(), "sibling": h.request(sibling, parent), "parent": h.request(parent)} {
			t.Run(name, func(t *testing.T) {
				err := h.authority.WithScopeGrant(ctx, r, grant, time.Now().UTC(), func(context.Context) error {
					t.Fatal("a department grant cannot be held for a target outside it")
					return nil
				})
				require.Error(t, err)
			})
		}
		relabelled := grant
		relabelled.DepartmentID = parent
		err := h.authority.WithScopeGrant(ctx, h.request(child, department, parent), relabelled, time.Now().UTC(), func(context.Context) error {
			t.Fatal("the grant's department is bound to the stored snapshot")
			return nil
		})
		require.Error(t, err)
	})
	t.Run("discovery lists current department grants", func(t *testing.T) {
		scopes, err := h.authority.DepartmentScopes(ctx, h.request())
		require.NoError(t, err)
		require.Equal(t, []authz.ScopeGrant{grant}, scopes)
		scopes, err = h.authority.DepartmentScopes(ctx, h.request(department))
		require.NoError(t, err)
		require.Empty(t, scopes, "discovery is hospital-wide and takes no target path")
	})
	var hospitalGrant string
	t.Run("hospital grants are tried first and share the epoch", func(t *testing.T) {
		hospitalGrant = h.activate(t, "workforce.posting_reviewer.v1", "")
		err := h.authority.WithScopeGrant(ctx, checked, grant, time.Now().UTC(), func(context.Context) error {
			t.Fatal("a later status change in the hospital invalidates the earlier department receipt")
			return nil
		})
		require.ErrorIs(t, err, authz.ErrScopedCommandDenied)
		current, err := h.authority.AuthorizeScope(ctx, checked)
		require.NoError(t, err)
		require.Equal(t, hospitalGrant, current.AssignmentID)
		require.Equal(t, "hospital", current.ScopeKind)
		require.Empty(t, current.DepartmentID)
		scopes, err := h.authority.DepartmentScopes(ctx, h.request())
		require.NoError(t, err)
		require.Len(t, scopes, 1)
		require.Equal(t, id, scopes[0].AssignmentID)
		require.Greater(t, scopes[0].ScopeEpoch, grant.ScopeEpoch)
	})
	// racing withdraws one proposal after discovery's first graph check and waits for the new epoch to be acknowledged.
	racing := func(t *testing.T, withdrawn string) authz.DepartmentScopeLister {
		var once sync.Once
		lister, ok := iam.NewScopedAuthorizer(h.db, &interceptedScopedChecker{Client: h.fga, after: func() {
			once.Do(func() {
				w := h.call("POST", "/v1/iam/assignment-proposals/"+withdrawn+"/decision", "admin", fmt.Sprintf(`{"expected_version":%d,"decision":"withdraw","reason":"Synthetic withdrawal during discovery"}`, h.row(t, withdrawn).Version))
				require.Equal(t, 200, w.Code, w.Body.String())
				require.EventuallyWithT(t, func(c *assert.CollectT) {
					var scope struct{ Epoch, AcknowledgedEpoch int64 }
					h.session(c, func(tx *gorm.DB) error {
						return tx.Table("iam_assignment_scope_epochs").Select("epoch,acknowledged_epoch").Where("hospital_id=? AND subject=?", h.hospital, departmentGrantSubject).Take(&scope).Error
					})
					require.Equal(c, scope.Epoch, scope.AcknowledgedEpoch)
					require.Equal(c, "withdrawn", h.row(c, withdrawn).Status)
				}, 20*time.Second, 100*time.Millisecond)
			})
		}}).(authz.DepartmentScopeLister)
		require.True(t, ok)
		return lister
	}
	t.Run("a grant whose shared epoch moved during discovery is dropped", func(t *testing.T) {
		scopes, err := racing(t, hospitalGrant).DepartmentScopes(ctx, h.request())
		require.NoError(t, err)
		require.Empty(t, scopes, "the still-active grant was checked at an older epoch")
		scopes, err = h.authority.DepartmentScopes(ctx, h.request())
		require.NoError(t, err)
		require.Len(t, scopes, 1, "a fresh discovery lists it at the current epoch")
	})
	t.Run("a grant withdrawn during discovery is dropped", func(t *testing.T) {
		scopes, err := racing(t, id).DepartmentScopes(ctx, h.request())
		require.NoError(t, err)
		require.Empty(t, scopes)
		scopes, err = h.authority.DepartmentScopes(ctx, h.request())
		require.NoError(t, err)
		require.Empty(t, scopes)
		grant, err := h.authority.AuthorizeScope(ctx, checked)
		require.NoError(t, err)
		require.Empty(t, grant.AssignmentID, "with both grants withdrawn the department target is denied")
	})
}

func TestDepartmentGrantSelectionIsBounded(t *testing.T) {
	h := newDepartmentGrantHarness(t)
	department := v7()
	id := h.activate(t, departmentReviewTemplate, department)
	ctx := t.Context()
	scopes, err := h.authority.DepartmentScopes(ctx, h.request())
	require.NoError(t, err)
	require.Len(t, scopes, 1)

	// Clones of the active grant stand in for 50 more approvals; the owner disables guards only inside this fixture.
	var values []string
	for range authz.MaxDepartmentScopes {
		values = append(values, fmt.Sprintf("('%s'::uuid,'%s'::uuid)", v7(), v7()))
	}
	tables := []string{"iam_assignment_proposals", "iam_assignment_proposal_history", "iam_assignment_snapshots"}
	var relax, restore []string
	for _, table := range tables {
		relax = append(relax, fmt.Sprintf("ALTER TABLE %[1]s DISABLE TRIGGER USER; ALTER TABLE %[1]s NO FORCE ROW LEVEL SECURITY;", table))
		restore = append(restore, fmt.Sprintf("ALTER TABLE %[1]s FORCE ROW LEVEL SECURITY; ALTER TABLE %[1]s ENABLE TRIGGER USER;", table))
	}
	clone := func(table string, fields string) string {
		return fmt.Sprintf(`CREATE TEMP TABLE clone_%[1]s ON COMMIT DROP AS SELECT * FROM %[1]s WHERE id='%[2]s';
			INSERT INTO %[1]s SELECT (jsonb_populate_record(o, to_jsonb(o) || jsonb_build_object(%[3]s))).* FROM clone_%[1]s o CROSS JOIN clone_ids c;`, table, id, fields)
	}
	require.NoError(t, h.db.Migrate(ctx, []tenantdb.Migration{{ID: "test_clone_department_grants", SQL: strings.Join(relax, "\n") + fmt.Sprintf(`
		CREATE TEMP TABLE clone_ids(id uuid, request_id uuid) ON COMMIT DROP;
		INSERT INTO clone_ids VALUES %s;`, strings.Join(values, ",")) +
		clone("iam_assignment_proposals", "'id',c.id,'request_id',c.request_id") +
		clone("iam_assignment_proposal_history", "'id',c.id") +
		clone("iam_assignment_snapshots", "'id',c.id") + strings.Join(restore, "\n")}}))
	var current int64
	h.session(t, func(tx *gorm.DB) error {
		return tx.Table("iam_assignment_proposals").Where("status='active' AND department_id=?", department).Count(&current).Error
	})
	require.Equal(t, int64(authz.MaxDepartmentScopes+1), current, "the fixture holds one grant over the bound")

	_, err = h.authority.DepartmentScopes(ctx, h.request())
	require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable, "discovery fails closed rather than truncating")
	_, err = h.authority.AuthorizeScope(ctx, h.request(department))
	require.ErrorIs(t, err, authz.ErrScopedAuthorizationUnavailable, "selection fails closed rather than choosing among too many grants")
}
