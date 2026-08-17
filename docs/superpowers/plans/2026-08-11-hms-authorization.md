# Helivanta Authorization (OpenFGA) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make OpenFGA the single authorization decision point for Helivanta — every route declares a permission at compile time, permissions resolve in one FGA call per request, and an `iam` module owns tenant membership and roles as a rebuildable system of record.

**Architecture:** Permissions are FGA *objects* (`perm:<tenantID>/<permission>`) granted to role objects (`role:<tenantID>/<key>`), so roles and permissions are data — the FGA model never changes when a zone, permission or role is added. `authz.Middleware` runs after `authn.Middleware` and resolves the caller's whole permission set for the token's tenant with one `ListObjects` call; `authz.Require(perm)` is then an in-memory lookup. `Module.Routes` takes a `*platform.Router` whose verb methods require an `authz.Permission` argument, making an undeclared route inexpressible. The `iam` module writes membership rows plus an outbox event in one transaction; the `iam-fga-sync` consumer applies tuples idempotently, so FGA is fully rebuildable from Postgres.

**Tech Stack:** Go 1.26, Gin, GORM + PostgreSQL 16 (forced RLS), NATS JetStream, `github.com/openfga/go-sdk` v0.8.2, `github.com/testcontainers/testcontainers-go/modules/openfga` v0.43.0, Next.js 16, React 19, TanStack Query, Tailwind v4, pnpm + Turborepo, Playwright.

**Spec:** `docs/superpowers/specs/2026-08-11-hms-authorization-design.md`

## Global Constraints

- Go module path is exactly `github.com/tesserix/helivanta`, rooted at `backend/`.
- Modules must never import another module's packages (depguard + `internal/archtest`); cross-module data flows only via events.
- Every table with a `tenant_id` column MUST have RLS enabled **and forced**, with a policy carrying both `USING` and `WITH CHECK`. `db.LintRLS` runs at boot and in every harness test.
- Runtime DB access only via `tenantdb.WithTenant` / `WithSystem` — no exported raw `*gorm.DB`.
- Migration IDs are globally unique and append-only. This phase adds exactly one: `0001_iam`.
- Event subjects are `helivanta.<dir>.<module>.<event>.vN`; consumer names are `<module>-<purpose>`. This phase adds `helivanta.in.iam.member_granted.v1`, `helivanta.in.iam.member_revoked.v1`, and consumer `iam-fga-sync`.
- Handlers use `authn.TenantPrincipal(c)` for identity and `respond.*` for every response. 404 (never 403) for cross-tenant; 403 only for "member of the tenant, lacking the permission".
- **Fail closed:** any FGA error, timeout or unreachable store denies the request with `503 authz_unavailable`. Never fail open.
- slog only (logrus banned); wrap errors with `%w`.
- Auth is GIP only. The string `MedCora` must never appear in the repo.
- Commit messages: conventional commits, single line, no signatures.
- Frontend ports: shell 4301, medicore 4302, pharmacy 4303, lab 4304; backend API 8080. Dev infra: Postgres 5432, NATS 4222, Redis 6379, OpenFGA 8090, Firebase Auth emulator 9099.
- JS package manager is pnpm via corepack, Node 22. Run `pnpm install` from the repo root after adding any workspace dependency.
- Backend tests require Docker (testcontainers). Run them as `cd backend && go test ./<pkg>/`.
- Before the phase is done: `make lint-go` clean, `cd backend && ./scripts/coverage-gate.sh` green (70% floor), `go test -race ./...` green, and `pnpm turbo lint type-check test build` green.

---

### Task 1: `pkg/authz` — permission vocabulary and set

Pure data types with no I/O, so this task has no container dependency and runs in milliseconds.

**Files:**

- Create: `backend/pkg/authz/authz.go`
- Test: `backend/pkg/authz/authz_test.go`

**Interfaces:**

- Consumes: nothing.
- Produces:
  - `type Permission string`, `type Role string`
  - `const Public Permission = "public"` — the explicit unguarded-route sentinel
  - `const RoleTenantAdmin/RoleDoctor/RoleNurse/RolePharmacist/RoleLabTech Role`
  - `type Grant struct { Permission Permission; Roles []Role }`
  - `type PermissionSet map[Permission]struct{}`
  - `func NewPermissionSet(perms ...Permission) PermissionSet`
  - `func (s PermissionSet) Has(p Permission) bool` — always true for `Public`
  - `func (s PermissionSet) Sorted() []string`

- [ ] **Step 1: Write the failing test**

Create `backend/pkg/authz/authz_test.go`:

```go
package authz_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/pkg/authz"
)

func TestPermissionSetHas(t *testing.T) {
	s := authz.NewPermissionSet("medicore.visit.create", "medicore.visit.read")

	require.True(t, s.Has("medicore.visit.create"))
	require.False(t, s.Has("pharmacy.dispense.fulfil"))
}

func TestPublicIsAlwaysAllowedEvenOnEmptySet(t *testing.T) {
	require.True(t, authz.NewPermissionSet().Has(authz.Public))
}

func TestSortedIsDeterministic(t *testing.T) {
	s := authz.NewPermissionSet("b.x.y", "a.x.y", "c.x.y")
	require.Equal(t, []string{"a.x.y", "b.x.y", "c.x.y"}, s.Sorted())
}

func TestSortedOnEmptySetIsEmptyNotNil(t *testing.T) {
	require.Equal(t, []string{}, authz.NewPermissionSet().Sorted())
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./pkg/authz/`
Expected: FAIL — `no required module provides package github.com/tesserix/helivanta/pkg/authz`

- [ ] **Step 3: Write the implementation**

Create `backend/pkg/authz/authz.go`:

```go
// Package authz is the single authorization decision point. Permissions
// are FGA objects granted to role objects, so adding a zone, permission
// or role is a tuple write — the authorization model never changes.
package authz

import "sort"

// Permission names an action, formatted "<module>.<resource>.<action>".
type Permission string

// Public marks a route as deliberately unguarded. It is a real value
// rather than an empty string so that an unguarded route is greppable
// and can never be created by forgetting an argument.
const Public Permission = "public"

// Role is a role key. The five below ship as seeded system roles;
// because roles are data, tenants may define others without a model
// change.
type Role string

const (
	RoleTenantAdmin Role = "tenant_admin"
	RoleDoctor      Role = "doctor"
	RoleNurse       Role = "nurse"
	RolePharmacist  Role = "pharmacist"
	RoleLabTech     Role = "lab_tech"
)

// Grant declares that a permission is held by the listed system roles.
// Modules return these from Permissions(); the reconciler turns them
// into tuples. RoleTenantAdmin is implicit — the reconciler grants it
// every declared permission, so modules never list it.
type Grant struct {
	Permission Permission
	Roles      []Role
}

// PermissionSet is a caller's resolved permissions for one tenant.
type PermissionSet map[Permission]struct{}

func NewPermissionSet(perms ...Permission) PermissionSet {
	s := make(PermissionSet, len(perms))
	for _, p := range perms {
		s[p] = struct{}{}
	}
	return s
}

// Has reports whether the set carries p. Public is always allowed, so
// unguarded routes need no special-casing at the call site.
func (s PermissionSet) Has(p Permission) bool {
	if p == Public {
		return true
	}
	_, ok := s[p]
	return ok
}

// Sorted returns the permissions as a stable, non-nil string slice so
// the /iam/me/permissions response is deterministic and marshals to []
// rather than null when empty.
func (s PermissionSet) Sorted() []string {
	out := make([]string, 0, len(s))
	for p := range s {
		out = append(out, string(p))
	}
	sort.Strings(out)
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd backend && go test ./pkg/authz/`
Expected: PASS (4 tests)

- [ ] **Step 5: Commit**

```bash
git add backend/pkg/authz/
git commit -m "feat: authz permission vocabulary and resolved permission set"
```

---

### Task 2: `pkg/authz` — OpenFGA client, model bootstrap and Resolve

**Files:**

- Create: `backend/pkg/authz/client.go`
- Create: `backend/pkg/authz/model.go`
- Modify: `backend/internal/testinfra/containers.go` (append `StartOpenFGA`)
- Modify: `backend/go.mod`, `backend/go.sum`
- Test: `backend/pkg/authz/client_test.go`

**Interfaces:**

- Consumes: `authz.Permission`, `authz.Role` (Task 1).
- Produces:
  - `func NewClient(ctx context.Context, apiURL, storeName string) (*Client, error)` — creates or reuses the store, writes the model if absent, returns a ready client.
  - `func (c *Client) Resolve(ctx context.Context, subject, tenantID string) (PermissionSet, error)`
  - `func (c *Client) GrantRole(ctx context.Context, tenantID, subject string, role Role) error`
  - `func (c *Client) RevokeRole(ctx context.Context, tenantID, subject string, role Role) error`
  - `func (c *Client) GrantPermission(ctx context.Context, tenantID string, perm Permission, role Role) error`
  - `func (c *Client) Ping(ctx context.Context) error`
  - `func RoleObject(tenantID string, r Role) string` → `"role:<tenantID>/<role>"`
  - `func PermObject(tenantID string, p Permission) string` → `"perm:<tenantID>/<permission>"`
  - `testinfra.StartOpenFGA(t) string` — returns the HTTP API URL.

All write helpers are **idempotent**: writing a tuple that already exists is not an error, and deleting one that does not exist is not an error. The consumer in Task 6 relies on this.

- [ ] **Step 1: Add dependencies**

```bash
cd backend
go get github.com/openfga/go-sdk@v0.8.2
go get github.com/testcontainers/testcontainers-go/modules/openfga@v0.43.0
go mod tidy
```

- [ ] **Step 2: Add the OpenFGA test container helper**

Append to `backend/internal/testinfra/containers.go` (and add `tcopenfga "github.com/testcontainers/testcontainers-go/modules/openfga"` to its import block):

```go
// StartOpenFGA boots an in-memory OpenFGA and returns its HTTP API URL.
func StartOpenFGA(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	fga, err := tcopenfga.Run(ctx, "openfga/openfga:v1.8.4")
	if err != nil {
		t.Fatalf("start openfga: %v", err)
	}
	t.Cleanup(func() { _ = fga.Terminate(context.Background()) })

	url, err := fga.HttpEndpoint(ctx)
	if err != nil {
		t.Fatalf("openfga endpoint: %v", err)
	}
	return url
}
```

- [ ] **Step 3: Write the failing test**

Create `backend/pkg/authz/client_test.go`:

```go
package authz_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/pkg/authz"
)

const (
	tenantA = "11111111-1111-1111-1111-111111111111"
	tenantB = "22222222-2222-2222-2222-222222222222"
)

func newClient(t *testing.T) *authz.Client {
	t.Helper()
	c, err := authz.NewClient(context.Background(), testinfra.StartOpenFGA(t), "hms-test")
	require.NoError(t, err)
	return c
}

func TestResolveReturnsPermissionsGrantedViaRole(t *testing.T) {
	ctx := context.Background()
	c := newClient(t)

	require.NoError(t, c.GrantPermission(ctx, tenantA, "pharmacy.dispense.fulfil", authz.RolePharmacist))
	require.NoError(t, c.GrantRole(ctx, tenantA, "alice", authz.RolePharmacist))

	set, err := c.Resolve(ctx, "alice", tenantA)
	require.NoError(t, err)
	require.True(t, set.Has("pharmacy.dispense.fulfil"))
}

func TestResolveIsTenantScoped(t *testing.T) {
	ctx := context.Background()
	c := newClient(t)

	require.NoError(t, c.GrantPermission(ctx, tenantA, "pharmacy.dispense.fulfil", authz.RolePharmacist))
	require.NoError(t, c.GrantRole(ctx, tenantA, "alice", authz.RolePharmacist))

	// Same user, different tenant: no membership, so no permissions.
	set, err := c.Resolve(ctx, "alice", tenantB)
	require.NoError(t, err)
	require.Empty(t, set.Sorted(), "permissions must not leak across tenants")
}

func TestResolveForNonMemberIsEmptyNotError(t *testing.T) {
	set, err := newClient(t).Resolve(context.Background(), "nobody", tenantA)
	require.NoError(t, err)
	require.Empty(t, set.Sorted())
}

func TestRevokeRoleRemovesPermissions(t *testing.T) {
	ctx := context.Background()
	c := newClient(t)

	require.NoError(t, c.GrantPermission(ctx, tenantA, "lab.order.fulfil", authz.RoleLabTech))
	require.NoError(t, c.GrantRole(ctx, tenantA, "bob", authz.RoleLabTech))
	require.NoError(t, c.RevokeRole(ctx, tenantA, "bob", authz.RoleLabTech))

	set, err := c.Resolve(ctx, "bob", tenantA)
	require.NoError(t, err)
	require.False(t, set.Has("lab.order.fulfil"))
}

func TestWritesAreIdempotent(t *testing.T) {
	ctx := context.Background()
	c := newClient(t)

	require.NoError(t, c.GrantPermission(ctx, tenantA, "medicore.visit.read", authz.RoleDoctor))
	require.NoError(t, c.GrantPermission(ctx, tenantA, "medicore.visit.read", authz.RoleDoctor))
	require.NoError(t, c.GrantRole(ctx, tenantA, "carol", authz.RoleDoctor))
	require.NoError(t, c.GrantRole(ctx, tenantA, "carol", authz.RoleDoctor))

	// Revoking twice must also be a no-op, since consumers retry.
	require.NoError(t, c.RevokeRole(ctx, tenantA, "carol", authz.RoleDoctor))
	require.NoError(t, c.RevokeRole(ctx, tenantA, "carol", authz.RoleDoctor))
}

func TestResolveFailsWhenStoreUnreachable(t *testing.T) {
	c, err := authz.NewClient(context.Background(), "http://127.0.0.1:1", "hms-test")
	if err == nil {
		_, err = c.Resolve(context.Background(), "alice", tenantA)
	}
	require.Error(t, err, "an unreachable store must surface an error so the middleware can fail closed")
}
```

- [ ] **Step 4: Run test to verify it fails**

Run: `cd backend && go test ./pkg/authz/ -run TestResolve`
Expected: FAIL — `undefined: authz.NewClient`

- [ ] **Step 5: Write the authorization model**

Create `backend/pkg/authz/model.go`:

```go
package authz

// modelJSON is the complete Helivanta authorization model. It deliberately
// contains no role names and no permission names: roles and permissions
// are objects, and granting is a tuple write. This file changes only if
// the *shape* of authorization changes (for example when per-record or
// department scoping lands), never when a zone, permission or role is
// added.
//
//	role:<tenantID>/<roleKey>          assignee     user:<subject>
//	perm:<tenantID>/<permission>       granted_role role:<tenantID>/<roleKey>
//
// Tenant isolation lives in the object-id namespace, so a ListObjects
// for one tenant can never return another tenant's perm objects.
const modelJSON = `{
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
```

- [ ] **Step 6: Write the client**

Create `backend/pkg/authz/client.go`:

```go
package authz

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	openfga "github.com/openfga/go-sdk"
	fgaclient "github.com/openfga/go-sdk/client"
)

// Client is the OpenFGA decision point. Every write helper is
// idempotent, because the iam-fga-sync consumer retries on failure and
// may redeliver.
type Client struct {
	api *fgaclient.OpenFgaClient
}

// RoleObject and PermObject namespace every object by tenant, which is
// what keeps ListObjects results tenant-scoped.
func RoleObject(tenantID string, r Role) string {
	return fmt.Sprintf("role:%s/%s", tenantID, r)
}

func PermObject(tenantID string, p Permission) string {
	return fmt.Sprintf("perm:%s/%s", tenantID, p)
}

func userObject(subject string) string { return "user:" + subject }

// NewClient connects to OpenFGA, reusing the named store if it exists
// and creating it otherwise, then ensures the authorization model is
// written. Safe to call from every replica at boot.
func NewClient(ctx context.Context, apiURL, storeName string) (*Client, error) {
	api, err := fgaclient.NewSdkClient(&fgaclient.ClientConfiguration{ApiUrl: apiURL})
	if err != nil {
		return nil, fmt.Errorf("openfga client: %w", err)
	}
	c := &Client{api: api}

	storeID, err := c.ensureStore(ctx, storeName)
	if err != nil {
		return nil, err
	}
	if err := api.SetStoreId(storeID); err != nil {
		return nil, fmt.Errorf("set store id: %w", err)
	}
	if err := c.ensureModel(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Client) ensureStore(ctx context.Context, name string) (string, error) {
	stores, err := c.api.ListStores(ctx).Execute()
	if err != nil {
		return "", fmt.Errorf("list stores: %w", err)
	}
	for _, s := range stores.GetStores() {
		if s.GetName() == name {
			return s.GetId(), nil
		}
	}
	created, err := c.api.CreateStore(ctx).
		Body(fgaclient.ClientCreateStoreRequest{Name: name}).Execute()
	if err != nil {
		return "", fmt.Errorf("create store: %w", err)
	}
	return created.GetId(), nil
}

// ensureModel writes modelJSON unless a model already exists. The model
// is immutable in practice, so a store that has one is already correct.
func (c *Client) ensureModel(ctx context.Context) error {
	existing, err := c.api.ReadAuthorizationModels(ctx).Execute()
	if err != nil {
		return fmt.Errorf("read models: %w", err)
	}
	if len(existing.GetAuthorizationModels()) > 0 {
		return nil
	}
	var body fgaclient.ClientWriteAuthorizationModelRequest
	if err := json.Unmarshal([]byte(modelJSON), &body); err != nil {
		return fmt.Errorf("parse model: %w", err)
	}
	if _, err := c.api.WriteAuthorizationModel(ctx).Body(body).Execute(); err != nil {
		return fmt.Errorf("write model: %w", err)
	}
	return nil
}

func (c *Client) Ping(ctx context.Context) error {
	if _, err := c.api.ReadAuthorizationModels(ctx).Execute(); err != nil {
		return fmt.Errorf("openfga ping: %w", err)
	}
	return nil
}

// Resolve returns every permission the subject holds in the tenant, in
// exactly one FGA call. Any error returns an error and never a partial
// set, so callers can fail closed unambiguously.
func (c *Client) Resolve(ctx context.Context, subject, tenantID string) (PermissionSet, error) {
	res, err := c.api.ListObjects(ctx).Body(fgaclient.ClientListObjectsRequest{
		User:     userObject(subject),
		Relation: "can_do",
		Type:     "perm",
	}).Execute()
	if err != nil {
		return nil, fmt.Errorf("list objects: %w", err)
	}
	prefix := "perm:" + tenantID + "/"
	set := PermissionSet{}
	for _, obj := range res.GetObjects() {
		if rest, ok := strings.CutPrefix(obj, prefix); ok {
			set[Permission(rest)] = struct{}{}
		}
	}
	return set, nil
}

func (c *Client) GrantRole(ctx context.Context, tenantID, subject string, role Role) error {
	return c.write(ctx, userObject(subject), "assignee", RoleObject(tenantID, role))
}

func (c *Client) RevokeRole(ctx context.Context, tenantID, subject string, role Role) error {
	return c.delete(ctx, userObject(subject), "assignee", RoleObject(tenantID, role))
}

func (c *Client) GrantPermission(ctx context.Context, tenantID string, perm Permission, role Role) error {
	return c.write(ctx, RoleObject(tenantID, role), "granted_role", PermObject(tenantID, perm))
}

// write is idempotent: OpenFGA rejects a duplicate tuple with a
// write_failed_due_to_invalid_input / already-exists error, which we
// swallow so retried consumer deliveries succeed.
func (c *Client) write(ctx context.Context, user, relation, object string) error {
	_, err := c.api.Write(ctx).Body(fgaclient.ClientWriteRequest{
		Writes: []fgaclient.ClientTupleKey{{User: user, Relation: relation, Object: object}},
	}).Execute()
	if err != nil && !isAlreadyExists(err) {
		return fmt.Errorf("write tuple %s#%s@%s: %w", object, relation, user, err)
	}
	return nil
}

// delete is idempotent for the same reason as write.
func (c *Client) delete(ctx context.Context, user, relation, object string) error {
	_, err := c.api.Write(ctx).Body(fgaclient.ClientWriteRequest{
		Deletes: []fgaclient.ClientTupleKeyWithoutCondition{{User: user, Relation: relation, Object: object}},
	}).Execute()
	if err != nil && !isNotFound(err) {
		return fmt.Errorf("delete tuple %s#%s@%s: %w", object, relation, user, err)
	}
	return nil
}

func isAlreadyExists(err error) bool {
	return containsAny(err.Error(), "already exists", "write_failed_due_to_invalid_input")
}

func isNotFound(err error) bool {
	return containsAny(err.Error(), "not found", "cannot delete a tuple which does not exist")
}

func containsAny(s string, subs ...string) bool {
	lower := strings.ToLower(s)
	for _, sub := range subs {
		if strings.Contains(lower, sub) {
			return true
		}
	}
	return false
}

var _ = openfga.APIError{} // keep the base SDK import meaningful for error typing
```

If `go build` reports the `openfga.APIError{}` blank identifier as unused or wrong, delete that final line and the `openfga` import — it exists only to pin the base package and carries no behaviour.

- [ ] **Step 7: Run tests to verify they pass**

Run: `cd backend && go test ./pkg/authz/`
Expected: PASS (all tests from Steps 1 and 3)

- [ ] **Step 8: Commit**

```bash
git add backend/pkg/authz/ backend/internal/testinfra/containers.go backend/go.mod backend/go.sum
git commit -m "feat: openfga client with tenant-scoped permission resolution"
```

---

### Task 3: `pkg/authz` — middleware, Require, and `respond.Forbidden`

**Files:**

- Create: `backend/pkg/authz/middleware.go`
- Modify: `backend/internal/platform/respond/respond.go`
- Test: `backend/pkg/authz/middleware_test.go`
- Test: `backend/internal/platform/respond/respond_test.go` (append one case)

**Interfaces:**

- Consumes: `PermissionSet`, `Permission`, `Public` (Task 1); `authn.PrincipalFrom` (existing).
- Produces:
  - `type Resolver interface { Resolve(ctx context.Context, subject, tenantID string) (PermissionSet, error) }` — `*Client` satisfies it; tests use fakes.
  - `func Middleware(r Resolver) gin.HandlerFunc`
  - `func Require(p Permission) gin.HandlerFunc`
  - `func PermissionsFrom(c *gin.Context) (PermissionSet, bool)`
  - `respond.Forbidden(c *gin.Context, message string)` → `403 {"error":"forbidden","message":...}`

- [ ] **Step 1: Write the failing test**

Create `backend/pkg/authz/middleware_test.go`:

```go
package authz_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
)

type fakeResolver struct {
	set authz.PermissionSet
	err error
}

func (f fakeResolver) Resolve(context.Context, string, string) (authz.PermissionSet, error) {
	return f.set, f.err
}

// principalStub stands in for authn.Middleware, which is already tested.
func principalStub(tenant string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("authn.principal", authn.Principal{Subject: "alice", TenantID: tenant})
		c.Next()
	}
}

func guarded(t *testing.T, r authz.Resolver, perm authz.Permission) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	e := gin.New()
	g := e.Group("/v1", principalStub(tenantA), authz.Middleware(r))
	g.GET("/thing", authz.Require(perm), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	return e
}

func get(e *gin.Engine) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest("GET", "/v1/thing", nil))
	return w
}

func TestRequireAllowsHeldPermission(t *testing.T) {
	r := fakeResolver{set: authz.NewPermissionSet("medicore.visit.read")}
	require.Equal(t, http.StatusOK, get(guarded(t, r, "medicore.visit.read")).Code)
}

func TestRequireDeniesMissingPermissionWith403(t *testing.T) {
	r := fakeResolver{set: authz.NewPermissionSet("medicore.visit.read")}
	w := get(guarded(t, r, "pharmacy.dispense.fulfil"))

	require.Equal(t, http.StatusForbidden, w.Code)
	require.Contains(t, w.Body.String(), `"forbidden"`)
}

func TestNonMemberResolvesEmptyAndIsDenied(t *testing.T) {
	r := fakeResolver{set: authz.NewPermissionSet()}
	require.Equal(t, http.StatusForbidden, get(guarded(t, r, "medicore.visit.read")).Code)
}

func TestPublicRouteAllowedWithEmptySet(t *testing.T) {
	r := fakeResolver{set: authz.NewPermissionSet()}
	require.Equal(t, http.StatusOK, get(guarded(t, r, authz.Public)).Code)
}

// Fail-closed is the single most important property of this package.
func TestResolverErrorFailsClosedWith503(t *testing.T) {
	r := fakeResolver{err: errors.New("openfga unreachable")}
	w := get(guarded(t, r, "medicore.visit.read"))

	require.Equal(t, http.StatusServiceUnavailable, w.Code)
	require.Contains(t, w.Body.String(), `"authz_unavailable"`)
}

func TestResolverErrorFailsClosedEvenOnPublicRoutes(t *testing.T) {
	r := fakeResolver{err: errors.New("openfga unreachable")}
	require.Equal(t, http.StatusServiceUnavailable, get(guarded(t, r, authz.Public)).Code)
}

func TestHandlerNeverRunsWithoutResolvedSet(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	ran := false
	// No principal set: the middleware must abort before the handler.
	g := e.Group("/v1", authz.Middleware(fakeResolver{set: authz.NewPermissionSet()}))
	g.GET("/thing", authz.Require(authz.Public), func(c *gin.Context) { ran = true })

	require.Equal(t, http.StatusUnauthorized, get(e).Code)
	require.False(t, ran, "handler must not run without a resolved permission set")
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./pkg/authz/ -run TestRequire`
Expected: FAIL — `undefined: authz.Middleware`

- [ ] **Step 3: Add `respond.Forbidden`**

Append to `backend/internal/platform/respond/respond.go`:

```go
// Forbidden means the caller is a member of the tenant but lacks the
// permission. Cross-tenant access returns NotFound instead — the record
// does not exist for that caller.
func Forbidden(c *gin.Context, message string) {
	Error(c, http.StatusForbidden, "forbidden", message)
}
```

Append to `backend/internal/platform/respond/respond_test.go` inside `TestErrorHelpers` (or as a new test if the existing one is table-driven, matching its style):

```go
func TestForbiddenEnvelope(t *testing.T) {
	w := run(func(c *gin.Context) { respond.Forbidden(c, "missing permission") })

	require.Equal(t, http.StatusForbidden, w.Code)
	require.JSONEq(t, `{"error":"forbidden","message":"missing permission"}`, w.Body.String())
}
```

- [ ] **Step 4: Write the middleware**

Create `backend/pkg/authz/middleware.go`:

```go
package authz

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
)

const permissionsKey = "authz.permissions"

// Resolver returns a subject's permissions within one tenant. *Client
// implements it; tests substitute fakes.
type Resolver interface {
	Resolve(ctx context.Context, subject, tenantID string) (PermissionSet, error)
}

// Middleware resolves the caller's whole permission set for the token's
// tenant in one call and parks it on the context, so Require is a pure
// in-memory lookup no matter how many permissions a route declares.
//
// It fails closed without exception: any resolver error denies with 503,
// including on Public routes. There is no code path on which a handler
// runs without a resolved set.
func Middleware(r Resolver) gin.HandlerFunc {
	return func(c *gin.Context) {
		p, ok := authn.PrincipalFrom(c)
		if !ok {
			respond.Unauthenticated(c, "missing principal")
			return
		}
		set, err := r.Resolve(c.Request.Context(), p.Subject, p.TenantID)
		if err != nil {
			slog.ErrorContext(c.Request.Context(), "authz resolve failed",
				"err", err, "subject", p.Subject, "tenant_id", p.TenantID)
			respond.Error(c, http.StatusServiceUnavailable,
				"authz_unavailable", "authorization is temporarily unavailable")
			return
		}
		c.Set(permissionsKey, set)
		c.Next()
	}
}

// Require denies with 403 unless the resolved set carries p. A caller
// who is not a member of the tenant resolves to an empty set, so
// membership needs no separate check.
func Require(p Permission) gin.HandlerFunc {
	return func(c *gin.Context) {
		set, ok := PermissionsFrom(c)
		if !ok {
			// Router construction guarantees Middleware runs first; this
			// is a programming error, not a client error.
			respond.Internal(c, "authorization not initialized")
			return
		}
		if !set.Has(p) {
			respond.Forbidden(c, "missing permission "+string(p))
			return
		}
		c.Next()
	}
}

func PermissionsFrom(c *gin.Context) (PermissionSet, bool) {
	v, ok := c.Get(permissionsKey)
	if !ok {
		return nil, false
	}
	set, ok := v.(PermissionSet)
	return set, ok
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd backend && go test ./pkg/authz/ ./internal/platform/respond/`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add backend/pkg/authz/ backend/internal/platform/respond/
git commit -m "feat: fail-closed authz middleware and permission requirement"
```

---

### Task 4: `platform.Router` — compile-time route declaration

Makes an undeclared route inexpressible, and migrates the four existing modules onto it.

**Files:**

- Create: `backend/internal/platform/router.go`
- Modify: `backend/internal/platform/platform.go` (the `Module` interface and `Deps`)
- Modify: `backend/internal/modules/{reference,medicore,pharmacy,lab}/module.go`
- Modify: `backend/internal/testutil/harness.go`
- Modify: `backend/cmd/api/main.go`
- Test: `backend/internal/platform/router_test.go`
- Test: `backend/internal/archtest/arch_test.go` (append two tests)

**Interfaces:**

- Consumes: `authz.Permission`, `authz.Grant`, `authz.Require` (Tasks 1, 3).
- Produces:
  - `type Router struct { ... }` with `Group(prefix string) *Router`, and `GET/POST/PUT/PATCH/DELETE(path string, perm authz.Permission, h ...gin.HandlerFunc)`
  - `func NewRouter(g *gin.RouterGroup) *Router`
  - `func (r *Router) Declared() []authz.Permission` — every permission used, for the arch test and matrix suite
  - `Module` interface gains `Permissions() []authz.Grant`; `Routes` signature becomes `Routes(r *Router, deps Deps)`

- [ ] **Step 1: Write the failing test**

Create `backend/internal/platform/router_test.go`:

```go
package platform_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestRouterAppliesRequireForDeclaredPermission(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	// Stand in for authz.Middleware with a set that lacks the permission.
	e.Use(func(c *gin.Context) {
		c.Set("authz.permissions", authz.NewPermissionSet("other.thing.read"))
		c.Next()
	})
	r := platform.NewRouter(e.Group("/v1"))
	r.GET("/thing", "medicore.visit.read", func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest("GET", "/v1/thing", nil))
	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestRouterPublicRouteSkipsTheCheck(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	e.Use(func(c *gin.Context) {
		c.Set("authz.permissions", authz.NewPermissionSet())
		c.Next()
	})
	r := platform.NewRouter(e.Group("/v1"))
	r.GET("/open", authz.Public, func(c *gin.Context) { c.Status(http.StatusOK) })

	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest("GET", "/v1/open", nil))
	require.Equal(t, http.StatusOK, w.Code)
}

func TestDeclaredCollectsPermissionsAcrossGroups(t *testing.T) {
	gin.SetMode(gin.TestMode)
	e := gin.New()
	r := platform.NewRouter(e.Group("/v1"))
	g := r.Group("/medicore")
	g.POST("/visits", "medicore.visit.create", func(c *gin.Context) {})
	g.GET("/visits", "medicore.visit.read", func(c *gin.Context) {})
	r.GET("/open", authz.Public, func(c *gin.Context) {})

	require.ElementsMatch(t,
		[]authz.Permission{"medicore.visit.create", "medicore.visit.read", authz.Public},
		r.Declared())
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/platform/ -run TestRouter`
Expected: FAIL — `undefined: platform.NewRouter`

- [ ] **Step 3: Write the router**

Create `backend/internal/platform/router.go`:

```go
package platform

import (
	"github.com/gin-gonic/gin"

	"github.com/tesserix/helivanta/pkg/authz"
)

// Router wraps a gin route group so that every route must declare the
// permission it requires. This replaces the boot-time check the
// repo-setup spec proposed: an undeclared route is not expressible, so
// the guarantee holds at compile time rather than at startup.
//
// Use authz.Public for deliberately unguarded routes — it is an explicit,
// greppable opt-out rather than an omission.
type Router struct {
	group    *gin.RouterGroup
	declared *[]authz.Permission
}

func NewRouter(g *gin.RouterGroup) *Router {
	return &Router{group: g, declared: &[]authz.Permission{}}
}

// Group returns a nested router that shares the parent's declaration
// list, so Declared() sees every route regardless of nesting.
func (r *Router) Group(prefix string) *Router {
	return &Router{group: r.group.Group(prefix), declared: r.declared}
}

func (r *Router) GET(path string, perm authz.Permission, h ...gin.HandlerFunc) {
	r.handle(http.MethodGet, path, perm, h)
}

func (r *Router) POST(path string, perm authz.Permission, h ...gin.HandlerFunc) {
	r.handle(http.MethodPost, path, perm, h)
}

func (r *Router) PUT(path string, perm authz.Permission, h ...gin.HandlerFunc) {
	r.handle(http.MethodPut, path, perm, h)
}

func (r *Router) PATCH(path string, perm authz.Permission, h ...gin.HandlerFunc) {
	r.handle(http.MethodPatch, path, perm, h)
}

func (r *Router) DELETE(path string, perm authz.Permission, h ...gin.HandlerFunc) {
	r.handle(http.MethodDelete, path, perm, h)
}

func (r *Router) handle(method, path string, perm authz.Permission, h []gin.HandlerFunc) {
	*r.declared = append(*r.declared, perm)
	chain := append([]gin.HandlerFunc{authz.Require(perm)}, h...)
	r.group.Handle(method, path, chain...)
}

// Declared lists every permission declared through this router and its
// nested groups, including authz.Public. The architecture test and the
// adversarial matrix suite both build on it.
func (r *Router) Declared() []authz.Permission {
	return append([]authz.Permission(nil), *r.declared...)
}
```

Add `"net/http"` to the import block.

- [ ] **Step 4: Update the Module interface**

In `backend/internal/platform/platform.go`, change the `Module` interface and add the import:

```go
// Module is the registration contract from issue #2.
type Module interface {
	Name() string
	Migrations() []tenantdb.Migration
	// Permissions declares every permission this module's routes use and
	// which system roles hold it. authz.RoleTenantAdmin is implicit — the
	// reconciler grants it everything, so never list it here.
	Permissions() []authz.Grant
	Routes(r *Router, deps Deps)
	Consumers(deps Deps) []events.Consumer
}
```

- [ ] **Step 5: Migrate the four existing modules**

For each of `reference`, `medicore`, `pharmacy`, `lab`: change the signature to `func (m *Module) Routes(r *platform.Router, deps platform.Deps)`, replace `g := r.Group("/<name>")` (which now returns `*platform.Router`, so the line is unchanged) and add the permission argument to each route. Also add a `Permissions()` method and a permission const block per module.

`medicore/module.go`:

```go
const (
	PermVisitCreate authz.Permission = "medicore.visit.create"
	PermVisitRead   authz.Permission = "medicore.visit.read"
	PermVisitUpdate authz.Permission = "medicore.visit.update"
)

func (m *Module) Permissions() []authz.Grant {
	return []authz.Grant{
		{Permission: PermVisitCreate, Roles: []authz.Role{authz.RoleDoctor}},
		{Permission: PermVisitRead, Roles: []authz.Role{authz.RoleDoctor, authz.RoleNurse}},
		{Permission: PermVisitUpdate, Roles: []authz.Role{authz.RoleNurse}},
	}
}
```

and its routes become `g.POST("/visits", PermVisitCreate, func(c *gin.Context) {...})` and `g.GET("/visits", PermVisitRead, ...)`.

`pharmacy/module.go`:

```go
const (
	PermDispenseRead   authz.Permission = "pharmacy.dispense.read"
	PermDispenseFulfil authz.Permission = "pharmacy.dispense.fulfil"
	PermMedicationRead authz.Permission = "pharmacy.medication.read"
	PermMedicationWrite authz.Permission = "pharmacy.medication.write"
)

func (m *Module) Permissions() []authz.Grant {
	return []authz.Grant{
		{Permission: PermDispenseRead, Roles: []authz.Role{authz.RolePharmacist, authz.RoleDoctor}},
		{Permission: PermDispenseFulfil, Roles: []authz.Role{authz.RolePharmacist}},
		{Permission: PermMedicationRead, Roles: []authz.Role{authz.RolePharmacist}},
		{Permission: PermMedicationWrite, Roles: []authz.Role{authz.RolePharmacist}},
	}
}
```

with `POST /medications` → `PermMedicationWrite`, `GET /medications` → `PermMedicationRead`, `GET /dispenses` → `PermDispenseRead`, `POST /dispenses/:id/dispense` → `PermDispenseFulfil`.

`lab/module.go`:

```go
const (
	PermOrderRead   authz.Permission = "lab.order.read"
	PermOrderFulfil authz.Permission = "lab.order.fulfil"
)

func (m *Module) Permissions() []authz.Grant {
	return []authz.Grant{
		{Permission: PermOrderRead, Roles: []authz.Role{authz.RoleLabTech, authz.RoleDoctor}},
		{Permission: PermOrderFulfil, Roles: []authz.Role{authz.RoleLabTech}},
	}
}
```

with the list route → `PermOrderRead` and the fulfil route → `PermOrderFulfil`.

`reference/module.go` is the ping/reference module: give every route `authz.Public` and return `nil` from `Permissions()`, since it exposes no tenant data.

- [ ] **Step 6: Update the test harness and main.go to compile**

In `backend/internal/testutil/harness.go`, replace the route-registration block. Add a `perms map[string][]authz.Permission` parameter documented as "token → permissions", and a fake resolver, so existing module tests keep working:

```go
// harnessResolver resolves from a static token→permissions map, so module
// tests need no OpenFGA container. Tests that must exercise real tuple
// resolution use the matrix suite instead.
type harnessResolver struct {
	tokens map[string]string
	perms  map[string][]authz.Permission
}

func (h harnessResolver) Resolve(_ context.Context, subject, _ string) (authz.PermissionSet, error) {
	return authz.NewPermissionSet(h.perms[strings.TrimPrefix(subject, "user-")]...), nil
}
```

and in `ModuleHarness`, after the authn middleware:

```go
	resolver := harnessResolver{tokens: tokens, perms: perms}
	api := platform.NewRouter(r.Group("/v1",
		authn.Middleware(StaticVerifier(tokens)),
		authz.Middleware(resolver)))
	for _, m := range mods {
		m.Routes(api, deps)
		require.NoError(t, bus.StartConsumers(ctx, db, m.Consumers(deps)))
	}
```

Change the signature to `ModuleHarness(t *testing.T, tokens map[string]string, perms map[string][]authz.Permission, mods ...platform.Module)`. Update every existing module test call site to pass the permissions each test's token needs — for example `medicore/module_test.go` passes `map[string][]authz.Permission{"tok-a": {medicore.PermVisitCreate, medicore.PermVisitRead}}`.

In `backend/cmd/api/main.go`, replace the group construction (the authz client itself is wired in Task 9; for now compile against the router only):

```go
	api := platform.NewRouter(srv.Engine.Group("/v1", authn.Middleware(verifier)))
```

- [ ] **Step 7: Add the architecture tests**

Append to `backend/internal/archtest/arch_test.go`:

```go
// TestModulesDoNotUseRawGinGroups keeps the compile-time guarantee that
// every route declares a permission: a module that reached for
// *gin.RouterGroup directly could register an unguarded route.
func TestModulesDoNotUseRawGinGroups(t *testing.T) {
	root := "../modules"
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(src), "gin.RouterGroup") {
			t.Errorf("%s references gin.RouterGroup; modules must use *platform.Router", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk modules: %v", err)
	}
}

// TestEveryDeclaredPermissionIsGranted catches a module that guards a
// route with a permission it never declared in Permissions() — the route
// would be permanently unreachable for every non-admin role.
func TestEveryDeclaredPermissionIsGranted(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, m := range allModules() {
		declaredByGrants := map[authz.Permission]bool{authz.Public: true}
		for _, g := range m.Permissions() {
			declaredByGrants[g.Permission] = true
		}
		e := gin.New()
		r := platform.NewRouter(e.Group("/v1"))
		m.Routes(r, platform.Deps{})
		for _, p := range r.Declared() {
			if !declaredByGrants[p] {
				t.Errorf("module %q guards a route with %q but does not declare it in Permissions()", m.Name(), p)
			}
		}
	}
}
```

Add `io/fs`, `path/filepath`, `github.com/gin-gonic/gin` and `github.com/tesserix/helivanta/pkg/authz` to the imports.

> `m.Routes(r, platform.Deps{})` passes zero-value deps. That is safe because `Routes` only closes over `deps` inside handler bodies, which this test never invokes.

- [ ] **Step 8: Run the full backend suite**

Run: `cd backend && go build ./... && go test ./internal/platform/ ./internal/archtest/ ./internal/modules/...`
Expected: PASS

- [ ] **Step 9: Commit**

```bash
git add backend/
git commit -m "feat: compile-time route permission declaration via platform.Router"
```

---

### Task 5: `iam` module — schema and system roles

**Files:**

- Create: `backend/internal/modules/iam/module.go`
- Create: `backend/internal/modules/iam/roles.go`
- Test: `backend/internal/modules/iam/module_test.go`

**Interfaces:**

- Consumes: `platform.Module`, `tenantdb.Migration`, `authz.Role`, `authz.Grant`.
- Produces:
  - `iam.New() *Module` with migration ID `0001_iam`
  - Tables `iam_roles(id, tenant_id, key, label, is_system, created_at)` and `iam_members(id, tenant_id, subject, role_key, created_at)`, both forced-RLS
  - `const PermMemberManage authz.Permission = "iam.member.manage"`
  - `func SystemRoles() []SystemRole` where `type SystemRole struct { Key authz.Role; Label string }`

- [ ] **Step 1: Write the failing test**

Create `backend/internal/modules/iam/module_test.go`:

```go
package iam_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/modules/iam"
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestMigrationIDIsPhaseScoped(t *testing.T) {
	migs := iam.New().Migrations()
	require.Len(t, migs, 1)
	require.Equal(t, "0001_iam", migs[0].ID)
}

func TestSystemRolesAreTheFiveShippedRoles(t *testing.T) {
	keys := make([]authz.Role, 0, len(iam.SystemRoles()))
	for _, r := range iam.SystemRoles() {
		keys = append(keys, r.Key)
	}
	require.ElementsMatch(t, []authz.Role{
		authz.RoleTenantAdmin, authz.RoleDoctor, authz.RoleNurse,
		authz.RolePharmacist, authz.RoleLabTech,
	}, keys)
}

func TestModuleDeclaresMemberManage(t *testing.T) {
	grants := iam.New().Permissions()
	require.Len(t, grants, 1)
	require.Equal(t, iam.PermMemberManage, grants[0].Permission)
	// tenant_admin is implicit, so the grant lists no roles.
	require.Empty(t, grants[0].Roles)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/modules/iam/`
Expected: FAIL — package does not exist

- [ ] **Step 3: Write the system roles**

Create `backend/internal/modules/iam/roles.go`:

```go
package iam

import "github.com/tesserix/helivanta/pkg/authz"

// SystemRole is a role seeded into every tenant. Because roles are data
// rather than model relations, a tenant may define additional roles
// without a model change or redeploy; this phase ships no UI for that.
type SystemRole struct {
	Key   authz.Role
	Label string
}

func SystemRoles() []SystemRole {
	return []SystemRole{
		{Key: authz.RoleTenantAdmin, Label: "Tenant Admin"},
		{Key: authz.RoleDoctor, Label: "Doctor"},
		{Key: authz.RoleNurse, Label: "Nurse"},
		{Key: authz.RolePharmacist, Label: "Pharmacist"},
		{Key: authz.RoleLabTech, Label: "Lab Technician"},
	}
}
```

- [ ] **Step 4: Write the module skeleton and migration**

Create `backend/internal/modules/iam/module.go`:

```go
// Package iam owns tenant membership and roles. Postgres is the system
// of record; OpenFGA is the decision point, and is fully rebuildable
// from these tables by the reconciler.
package iam

import (
	"time"

	"github.com/google/uuid"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

const PermMemberManage authz.Permission = "iam.member.manage"

const (
	SubjectMemberGranted = "helivanta.in.iam.member_granted.v1"
	SubjectMemberRevoked = "helivanta.in.iam.member_revoked.v1"
)

type Module struct{}

func New() *Module { return &Module{} }

func (m *Module) Name() string { return "iam" }

// Permissions declares only iam.member.manage. It lists no roles because
// tenant_admin is implicit — the reconciler grants it every declared
// permission — and no other system role may manage membership.
func (m *Module) Permissions() []authz.Grant {
	return []authz.Grant{{Permission: PermMemberManage}}
}

func (m *Module) Migrations() []tenantdb.Migration {
	return []tenantdb.Migration{{
		ID: "0001_iam",
		SQL: `
			CREATE TABLE iam_roles (
			  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			  tenant_id uuid NOT NULL,
			  key text NOT NULL,
			  label text NOT NULL,
			  is_system boolean NOT NULL DEFAULT false,
			  created_at timestamptz NOT NULL DEFAULT now(),
			  UNIQUE (tenant_id, key)
			);
			ALTER TABLE iam_roles ENABLE ROW LEVEL SECURITY;
			ALTER TABLE iam_roles FORCE ROW LEVEL SECURITY;
			CREATE POLICY tenant_isolation ON iam_roles
			  USING (tenant_id = current_setting('app.tenant_id', true)::uuid)
			  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);

			CREATE TABLE iam_members (
			  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
			  tenant_id uuid NOT NULL,
			  subject text NOT NULL,
			  role_key text NOT NULL,
			  created_at timestamptz NOT NULL DEFAULT now(),
			  UNIQUE (tenant_id, subject, role_key)
			);
			ALTER TABLE iam_members ENABLE ROW LEVEL SECURITY;
			ALTER TABLE iam_members FORCE ROW LEVEL SECURITY;
			CREATE POLICY tenant_isolation ON iam_members
			  USING (tenant_id = current_setting('app.tenant_id', true)::uuid)
			  WITH CHECK (tenant_id = current_setting('app.tenant_id', true)::uuid);
			CREATE INDEX ON iam_members (tenant_id, subject);
			CREATE INDEX ON iam_members (subject);`,
	}}
}

type role struct {
	ID        uuid.UUID `gorm:"type:uuid;default:gen_random_uuid()" json:"id"`
	TenantID  uuid.UUID `json:"-"`
	Key       string    `json:"key"`
	Label     string    `json:"label"`
	IsSystem  bool      `json:"is_system"`
	CreatedAt time.Time `json:"created_at"`
}

func (role) TableName() string { return "iam_roles" }

type member struct {
	ID        uuid.UUID `gorm:"type:uuid;default:gen_random_uuid()" json:"id"`
	TenantID  uuid.UUID `json:"-"`
	Subject   string    `json:"subject"`
	RoleKey   string    `json:"role_key"`
	CreatedAt time.Time `json:"created_at"`
}

func (member) TableName() string { return "iam_members" }

// MemberChangedData is the v1 payload of member_granted and
// member_revoked.
type MemberChangedData struct {
	Subject string `json:"subject"`
	RoleKey string `json:"role_key"`
}

// Routes and Consumers are filled in by the next tasks.
func (m *Module) Routes(r *platform.Router, deps platform.Deps) {}

func (m *Module) Consumers(deps platform.Deps) []events.Consumer { return nil }
```

> `iam_members` carries an index on `subject` alone (not just `(tenant_id, subject)`). `GET /iam/me/tenants` in Task 8 queries across tenants under `WithSystem`, and needs it.

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd backend && go test ./internal/modules/iam/`
Expected: PASS (3 tests)

- [ ] **Step 6: Commit**

```bash
git add backend/internal/modules/iam/
git commit -m "feat: iam module schema with forced-rls membership and roles"
```

---

### Task 6: `iam` — grant/revoke routes, events, and the FGA sync consumer

**Files:**

- Modify: `backend/internal/modules/iam/module.go` (`Routes`, `Consumers`)
- Create: `backend/internal/modules/iam/sync.go`
- Test: `backend/internal/modules/iam/routes_test.go`

**Interfaces:**

- Consumes: `authz.Client` write helpers (Task 2), `platform.Router` (Task 4), `events.Bus.Publish`, `Deps`.
- Produces:
  - `Deps` gains an `Authz TupleWriter` field (interface below), so the module never imports the concrete client in tests.
  - `type TupleWriter interface { GrantRole(ctx, tenantID, subject string, role authz.Role) error; RevokeRole(...) error; GrantPermission(ctx, tenantID string, perm authz.Permission, role authz.Role) error }` in `internal/platform`
  - Routes: `POST /iam/members`, `DELETE /iam/members/:subject/:role`, `GET /iam/members`, `GET /iam/roles`
  - Consumer `iam-fga-sync` on both member subjects

- [ ] **Step 1: Add TupleWriter to platform.Deps**

In `backend/internal/platform/platform.go`:

```go
// TupleWriter is the subset of the authz client that modules may use to
// mutate authorization state. Narrow by design: modules grant and revoke,
// they never resolve — resolution belongs to the middleware.
type TupleWriter interface {
	GrantRole(ctx context.Context, tenantID, subject string, role authz.Role) error
	RevokeRole(ctx context.Context, tenantID, subject string, role authz.Role) error
	GrantPermission(ctx context.Context, tenantID string, perm authz.Permission, role authz.Role) error
}

type Deps struct {
	DB    *tenantdb.DB
	Bus   *events.Bus
	Authz TupleWriter
}
```

- [ ] **Step 2: Write the failing test**

Create `backend/internal/modules/iam/routes_test.go`:

```go
package iam_test

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/modules/iam"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
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
```

- [ ] **Step 3: Extend the test harness for authz-aware modules**

Append to `backend/internal/testutil/harness.go`:

```go
// ModuleHarnessWithAuthz is ModuleHarness plus a TupleWriter, for modules
// that mutate authorization state. Existing callers keep using
// ModuleHarness, which passes a no-op writer.
func ModuleHarnessWithAuthz(
	t *testing.T,
	tokens map[string]string,
	perms map[string][]authz.Permission,
	writer platform.TupleWriter,
	mods ...platform.Module,
) (*gin.Engine, *tenantdb.DB, *events.Bus, context.Context) {
	t.Helper()
	return moduleHarness(t, tokens, perms, writer, mods...)
}
```

Refactor the existing `ModuleHarness` body into an unexported `moduleHarness(t, tokens, perms, writer, mods...)` that sets `deps := platform.Deps{DB: db, Bus: bus, Authz: writer}`, and have `ModuleHarness` call it with `noopWriter{}`:

```go
type noopWriter struct{}

func (noopWriter) GrantRole(context.Context, string, string, authz.Role) error  { return nil }
func (noopWriter) RevokeRole(context.Context, string, string, authz.Role) error { return nil }
func (noopWriter) GrantPermission(context.Context, string, authz.Permission, authz.Role) error {
	return nil
}
```

- [ ] **Step 4: Run test to verify it fails**

Run: `cd backend && go test ./internal/modules/iam/ -run TestGrant`
Expected: FAIL — 404 on `/v1/iam/members`, since `Routes` is still empty

- [ ] **Step 5: Write the routes**

Replace the stub `Routes` in `backend/internal/modules/iam/module.go`:

```go
type grantRequest struct {
	Subject string `json:"subject" binding:"required,max=200"`
	RoleKey string `json:"role_key" binding:"required,max=100"`
}

// knownRole reports whether key is a system role. Custom roles are
// supported by the model but not yet creatable, so anything else is a
// client error rather than a silently-dead grant.
func knownRole(key string) bool {
	for _, r := range SystemRoles() {
		if string(r.Key) == key {
			return true
		}
	}
	return false
}

func (m *Module) Routes(r *platform.Router, deps platform.Deps) {
	g := r.Group("/iam")

	g.POST("/members", PermMemberManage, func(c *gin.Context) {
		p, tenantUUID, ok := authn.TenantPrincipal(c)
		if !ok {
			return
		}
		var req grantRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respond.BadRequest(c, err)
			return
		}
		if !knownRole(req.RoleKey) {
			respond.BadRequest(c, fmt.Errorf("unknown role %q", req.RoleKey))
			return
		}
		row := member{TenantID: tenantUUID, Subject: req.Subject, RoleKey: req.RoleKey}
		err := deps.DB.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
			// Re-granting an existing role is a no-op, not a conflict:
			// the caller's intent is already satisfied.
			if err := tx.Where("subject = ? AND role_key = ?", req.Subject, req.RoleKey).
				FirstOrCreate(&row, member{
					TenantID: tenantUUID, Subject: req.Subject, RoleKey: req.RoleKey,
				}).Error; err != nil {
				return err
			}
			data, err := json.Marshal(MemberChangedData{Subject: req.Subject, RoleKey: req.RoleKey})
			if err != nil {
				return err
			}
			return deps.Bus.Publish(tx, SubjectMemberGranted, events.Event{
				Type: "MemberGranted", Version: 1, TenantID: p.TenantID, Data: data,
			})
		})
		if err != nil {
			respond.Internal(c, "could not grant role")
			return
		}
		respond.Accepted(c, gin.H{"id": row.ID.String()})
	})

	g.DELETE("/members/:subject/:role", PermMemberManage, func(c *gin.Context) {
		p, _, ok := authn.TenantPrincipal(c)
		if !ok {
			return
		}
		subject, roleKey := c.Param("subject"), c.Param("role")
		err := deps.DB.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
			if err := tx.Where("subject = ? AND role_key = ?", subject, roleKey).
				Delete(&member{}).Error; err != nil {
				return err
			}
			data, err := json.Marshal(MemberChangedData{Subject: subject, RoleKey: roleKey})
			if err != nil {
				return err
			}
			return deps.Bus.Publish(tx, SubjectMemberRevoked, events.Event{
				Type: "MemberRevoked", Version: 1, TenantID: p.TenantID, Data: data,
			})
		})
		if err != nil {
			respond.Internal(c, "could not revoke role")
			return
		}
		respond.Accepted(c, gin.H{"subject": subject, "role_key": roleKey})
	})

	g.GET("/members", PermMemberManage, func(c *gin.Context) {
		p, _, ok := authn.TenantPrincipal(c)
		if !ok {
			return
		}
		var rows []member
		err := deps.DB.WithTenant(c.Request.Context(), p.TenantID, func(tx *gorm.DB) error {
			return tx.Order("created_at DESC").Limit(500).Find(&rows).Error
		})
		if err != nil {
			respond.Internal(c, "could not list members")
			return
		}
		respond.OK(c, gin.H{"data": rows})
	})

	g.GET("/roles", PermMemberManage, func(c *gin.Context) {
		respond.OK(c, gin.H{"data": SystemRoles()})
	})
}
```

Add `encoding/json`, `fmt`, `github.com/gin-gonic/gin`, `gorm.io/gorm`, `github.com/tesserix/helivanta/internal/platform/respond` and `github.com/tesserix/helivanta/pkg/authn` to the imports.

- [ ] **Step 6: Write the sync consumer**

Create `backend/internal/modules/iam/sync.go`:

```go
package iam

import (
	"context"
	"encoding/json"
	"fmt"

	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
)

// Consumers keeps OpenFGA in step with the membership tables. Postgres
// is the system of record, so a failed tuple write is retried by the bus
// and the tuple helpers are idempotent — redelivery is harmless.
//
// Handle runs inside the bus's transaction; it must not open its own.
func (m *Module) Consumers(deps platform.Deps) []events.Consumer {
	apply := func(grant bool) func(context.Context, *gorm.DB, events.Event) error {
		return func(ctx context.Context, _ *gorm.DB, evt events.Event) error {
			var data MemberChangedData
			if err := json.Unmarshal(evt.Data, &data); err != nil {
				return fmt.Errorf("iam sync payload: %w", err)
			}
			role := authz.Role(data.RoleKey)
			if grant {
				return deps.Authz.GrantRole(ctx, evt.TenantID, data.Subject, role)
			}
			return deps.Authz.RevokeRole(ctx, evt.TenantID, data.Subject, role)
		}
	}
	return []events.Consumer{
		{Name: "iam-fga-sync", Subject: SubjectMemberGranted, Handle: apply(true)},
		{Name: "iam-fga-sync-revoke", Subject: SubjectMemberRevoked, Handle: apply(false)},
	}
}
```

> Two consumer names are required: the bus keys idempotency on `(consumer, event_id)` and creates one durable subscription per consumer, so a single name cannot serve two subjects.

- [ ] **Step 7: Run tests to verify they pass**

Run: `cd backend && go test ./internal/modules/iam/`
Expected: PASS (all tests from Steps 1 of Task 5 and Step 2 here)

- [ ] **Step 8: Commit**

```bash
git add backend/internal/modules/iam/ backend/internal/platform/ backend/internal/testutil/
git commit -m "feat: iam membership routes with outbox-driven fga tuple sync"
```

---

### Task 7: Permission reconciler

Makes "deploy a new zone and every tenant's admin gets its permissions" true.

**Files:**

- Create: `backend/internal/platform/reconcile.go`
- Test: `backend/internal/platform/reconcile_test.go`

**Interfaces:**

- Consumes: `Registry.All()`, `Module.Permissions()`, `TupleWriter.GrantPermission`, `tenantdb.DB.WithSystem`.
- Produces:
  - `func Reconcile(ctx context.Context, reg *Registry, db *tenantdb.DB, w TupleWriter) error` — reconciles every tenant known to `iam_members`
  - `func ReconcileTenant(ctx context.Context, reg *Registry, w TupleWriter, tenantID string) error`
  - `func GrantsFor(reg *Registry) []authz.Grant` — every module's grants with `RoleTenantAdmin` appended to each

- [ ] **Step 1: Write the failing test**

Create `backend/internal/platform/reconcile_test.go`:

```go
package platform_test

import (
	"context"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

type grantingModule struct{ name string }

func (g grantingModule) Name() string                     { return g.name }
func (g grantingModule) Migrations() []tenantdb.Migration { return nil }
func (g grantingModule) Permissions() []authz.Grant {
	return []authz.Grant{
		{Permission: "x.thing.read", Roles: []authz.Role{authz.RoleNurse}},
		{Permission: "x.thing.write", Roles: []authz.Role{authz.RoleDoctor}},
	}
}
func (g grantingModule) Routes(*platform.Router, platform.Deps)          {}
func (g grantingModule) Consumers(platform.Deps) []events.Consumer       { return nil }

type capturingWriter struct{ pairs []string }

func (c *capturingWriter) GrantRole(context.Context, string, string, authz.Role) error  { return nil }
func (c *capturingWriter) RevokeRole(context.Context, string, string, authz.Role) error { return nil }
func (c *capturingWriter) GrantPermission(_ context.Context, tenantID string, p authz.Permission, r authz.Role) error {
	c.pairs = append(c.pairs, string(p)+"@"+string(r))
	return nil
}

func TestTenantAdminReceivesEveryDeclaredPermission(t *testing.T) {
	reg := platform.NewRegistry()
	require.NoError(t, reg.Register(grantingModule{"x"}))
	w := &capturingWriter{}

	require.NoError(t, platform.ReconcileTenant(context.Background(), reg, w, "tenant-1"))

	sort.Strings(w.pairs)
	require.Equal(t, []string{
		"x.thing.read@nurse",
		"x.thing.read@tenant_admin",
		"x.thing.write@doctor",
		"x.thing.write@tenant_admin",
	}, w.pairs)
}

func TestReconcileIsIdempotent(t *testing.T) {
	reg := platform.NewRegistry()
	require.NoError(t, reg.Register(grantingModule{"x"}))
	w := &capturingWriter{}

	require.NoError(t, platform.ReconcileTenant(context.Background(), reg, w, "tenant-1"))
	first := len(w.pairs)
	require.NoError(t, platform.ReconcileTenant(context.Background(), reg, w, "tenant-1"))

	// Writes repeat, but GrantPermission is idempotent at the client, so
	// the operation stays safe to run on every boot.
	require.Equal(t, first*2, len(w.pairs))
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/platform/ -run TestTenantAdmin`
Expected: FAIL — `undefined: platform.ReconcileTenant`

- [ ] **Step 3: Write the reconciler**

Create `backend/internal/platform/reconcile.go`:

```go
package platform

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"github.com/tesserix/helivanta/pkg/authz"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

// GrantsFor returns every module's declared grants with RoleTenantAdmin
// appended to each, which is why modules never list tenant_admin
// themselves.
func GrantsFor(reg *Registry) []authz.Grant {
	var out []authz.Grant
	for _, m := range reg.All() {
		for _, g := range m.Permissions() {
			roles := append([]authz.Role(nil), g.Roles...)
			roles = append(roles, authz.RoleTenantAdmin)
			out = append(out, authz.Grant{Permission: g.Permission, Roles: roles})
		}
	}
	return out
}

// ReconcileTenant makes the tenant's perm objects and system-role grants
// match the module registry. Idempotent, so deploying a new zone grants
// its permissions to every existing tenant on the next boot with no
// migration and no manual step.
func ReconcileTenant(ctx context.Context, reg *Registry, w TupleWriter, tenantID string) error {
	for _, g := range GrantsFor(reg) {
		for _, role := range g.Roles {
			if err := w.GrantPermission(ctx, tenantID, g.Permission, role); err != nil {
				return fmt.Errorf("grant %s to %s in %s: %w", g.Permission, role, tenantID, err)
			}
		}
	}
	return nil
}

// Reconcile reconciles every tenant that has at least one membership row.
// iam_members is the only place tenants are known, so a tenant with no
// members has nothing to authorize and needs no tuples; its first grant
// creates the row and the next boot reconciles it.
func Reconcile(ctx context.Context, reg *Registry, db *tenantdb.DB, w TupleWriter) error {
	var tenantIDs []string
	err := db.WithSystem(ctx, func(tx *gorm.DB) error {
		return tx.Raw(`SELECT DISTINCT tenant_id::text FROM iam_members`).Scan(&tenantIDs).Error
	})
	if err != nil {
		return fmt.Errorf("list tenants: %w", err)
	}
	for _, id := range tenantIDs {
		if err := ReconcileTenant(ctx, reg, w, id); err != nil {
			return err
		}
	}
	return nil
}
```

- [ ] **Step 4: Reconcile on grant as well as at boot**

In `backend/internal/modules/iam/sync.go`, the grant branch must also ensure the tenant's permission tuples exist — otherwise the very first member of a brand-new tenant has a role with no permissions attached until the next restart. Add a `Reconcile func(ctx context.Context, tenantID string) error` field to `platform.Deps`, set it in `main.go` (Task 9), and call it in the grant path before `GrantRole`:

```go
			if grant {
				if deps.Reconcile != nil {
					if err := deps.Reconcile(ctx, evt.TenantID); err != nil {
						return fmt.Errorf("reconcile tenant: %w", err)
					}
				}
				return deps.Authz.GrantRole(ctx, evt.TenantID, data.Subject, role)
			}
```

Add to `platform.Deps`:

```go
	// Reconcile ensures a tenant's permission tuples match the registry.
	// Set by main.go; nil in tests that do not exercise it.
	Reconcile func(ctx context.Context, tenantID string) error
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd backend && go test ./internal/platform/ ./internal/modules/iam/`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add backend/internal/platform/ backend/internal/modules/iam/
git commit -m "feat: permission reconciler grants new zone permissions to existing tenants"
```

---

### Task 8: `/iam/me/permissions`, `/iam/me/tenants`, and tenant switch

**Files:**

- Modify: `backend/internal/modules/iam/module.go` (three routes)
- Create: `backend/internal/modules/iam/me.go`
- Test: `backend/internal/modules/iam/me_test.go`

**Interfaces:**

- Consumes: `authz.PermissionsFrom` (Task 3), `authn.TenantPrincipal`.
- Produces:
  - `GET /v1/iam/me/permissions` → `{"data":["medicore.visit.read", ...]}` (sorted, never null)
  - `GET /v1/iam/me/tenants` → `{"data":[{"tenant_id":"...","roles":["doctor"]}]}`
  - `POST /v1/iam/me/tenant` body `{"tenant_id":"..."}` → `200 {"tenant_id":"..."}` when a membership row exists, `403` otherwise

All three are `authz.Public` — any authenticated caller may ask what they can do and where they belong. The switch endpoint is `Public` at the route level but performs its own **membership check**, which is the actual gate.

- [ ] **Step 1: Write the failing test**

Create `backend/internal/modules/iam/me_test.go`:

```go
package iam_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/modules/iam"
	"github.com/tesserix/helivanta/internal/testutil"
	"github.com/tesserix/helivanta/pkg/authz"
)

func TestMePermissionsReturnsResolvedSetSorted(t *testing.T) {
	r, _, _, _ := testutil.ModuleHarnessWithAuthz(t,
		map[string]string{"doc": testutil.TenantA},
		map[string][]authz.Permission{"doc": {"medicore.visit.read", "lab.order.read"}},
		&recordingWriter{}, iam.New())

	res := testutil.Do(r, "GET", "/v1/iam/me/permissions", "doc", "")
	require.Equal(t, http.StatusOK, res.Code)

	var body struct {
		Data []string `json:"data"`
	}
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &body))
	require.Equal(t, []string{"lab.order.read", "medicore.visit.read"}, body.Data)
}

func TestMePermissionsIsEmptyArrayNotNullForNonMember(t *testing.T) {
	r, _, _, _ := testutil.ModuleHarnessWithAuthz(t,
		map[string]string{"nobody": testutil.TenantA},
		map[string][]authz.Permission{"nobody": {}},
		&recordingWriter{}, iam.New())

	res := testutil.Do(r, "GET", "/v1/iam/me/permissions", "nobody", "")
	require.Equal(t, http.StatusOK, res.Code)
	require.JSONEq(t, `{"data":[]}`, res.Body.String())
}

func TestMeTenantsListsEveryMembership(t *testing.T) {
	r, _, _, _ := testutil.ModuleHarnessWithAuthz(t,
		map[string]string{
			"admin-a": testutil.TenantA,
			"admin-b": testutil.TenantB,
			"jane":    testutil.TenantA,
		},
		map[string][]authz.Permission{
			"admin-a": {iam.PermMemberManage},
			"admin-b": {iam.PermMemberManage},
			"jane":    {},
		},
		&recordingWriter{}, iam.New())

	// user-jane is the subject StaticVerifier derives from token "jane".
	testutil.Do(r, "POST", "/v1/iam/members", "admin-a", `{"subject":"user-jane","role_key":"doctor"}`)
	testutil.Do(r, "POST", "/v1/iam/members", "admin-b", `{"subject":"user-jane","role_key":"nurse"}`)

	res := testutil.Do(r, "GET", "/v1/iam/me/tenants", "jane", "")
	require.Equal(t, http.StatusOK, res.Code)

	var body struct {
		Data []struct {
			TenantID string   `json:"tenant_id"`
			Roles    []string `json:"roles"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(res.Body.Bytes(), &body))
	require.Len(t, body.Data, 2, "a clinician working at two hospitals must see both")
}

func TestSwitchTenantRequiresMembership(t *testing.T) {
	r, _, _, _ := testutil.ModuleHarnessWithAuthz(t,
		map[string]string{"jane": testutil.TenantA},
		map[string][]authz.Permission{"jane": {}},
		&recordingWriter{}, iam.New())

	res := testutil.Do(r, "POST", "/v1/iam/me/tenant", "jane",
		`{"tenant_id":"`+testutil.TenantB+`"}`)
	require.Equal(t, http.StatusForbidden, res.Code,
		"switching into a tenant you are not a member of must be denied")
}

func TestSwitchTenantSucceedsWithMembership(t *testing.T) {
	r, _, _, _ := testutil.ModuleHarnessWithAuthz(t,
		map[string]string{"admin-b": testutil.TenantB, "jane": testutil.TenantA},
		map[string][]authz.Permission{"admin-b": {iam.PermMemberManage}, "jane": {}},
		&recordingWriter{}, iam.New())

	testutil.Do(r, "POST", "/v1/iam/members", "admin-b", `{"subject":"user-jane","role_key":"nurse"}`)

	res := testutil.Do(r, "POST", "/v1/iam/me/tenant", "jane",
		`{"tenant_id":"`+testutil.TenantB+`"}`)
	require.Equal(t, http.StatusOK, res.Code)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd backend && go test ./internal/modules/iam/ -run TestMe`
Expected: FAIL — 404 on `/v1/iam/me/permissions`

- [ ] **Step 3: Write the routes**

Create `backend/internal/modules/iam/me.go`:

```go
package iam

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/platform/respond"
	"github.com/tesserix/helivanta/pkg/authn"
	"github.com/tesserix/helivanta/pkg/authz"
)

type tenantMembership struct {
	TenantID string   `json:"tenant_id"`
	Roles    []string `json:"roles"`
}

type switchRequest struct {
	TenantID string `json:"tenant_id" binding:"required,uuid"`
}

// registerMe adds the self-service routes. They are authz.Public because
// any authenticated caller may ask what they can do and where they
// belong; the switch endpoint gates on membership itself.
func (m *Module) registerMe(g *platform.Router, deps platform.Deps) {
	g.GET("/me/permissions", authz.Public, func(c *gin.Context) {
		set, ok := authz.PermissionsFrom(c)
		if !ok {
			respond.Internal(c, "authorization not initialized")
			return
		}
		respond.OK(c, gin.H{"data": set.Sorted()})
	})

	// Memberships span tenants, so this query runs under WithSystem and
	// filters by subject explicitly. It returns tenant ids and role keys
	// only — never any tenant's clinical data.
	g.GET("/me/tenants", authz.Public, func(c *gin.Context) {
		p, ok := authn.PrincipalFrom(c)
		if !ok {
			respond.Unauthenticated(c, "missing principal")
			return
		}
		var rows []struct {
			TenantID string
			RoleKey  string
		}
		err := deps.DB.WithSystem(c.Request.Context(), func(tx *gorm.DB) error {
			return tx.Raw(
				`SELECT tenant_id::text AS tenant_id, role_key FROM iam_members WHERE subject = ? ORDER BY tenant_id`,
				p.Subject).Scan(&rows).Error
		})
		if err != nil {
			respond.Internal(c, "could not list tenants")
			return
		}
		byTenant := map[string][]string{}
		var order []string
		for _, r := range rows {
			if _, seen := byTenant[r.TenantID]; !seen {
				order = append(order, r.TenantID)
			}
			byTenant[r.TenantID] = append(byTenant[r.TenantID], r.RoleKey)
		}
		out := make([]tenantMembership, 0, len(order))
		for _, id := range order {
			out = append(out, tenantMembership{TenantID: id, Roles: byTenant[id]})
		}
		respond.OK(c, gin.H{"data": out})
	})

	g.POST("/me/tenant", authz.Public, func(c *gin.Context) {
		p, ok := authn.PrincipalFrom(c)
		if !ok {
			respond.Unauthenticated(c, "missing principal")
			return
		}
		var req switchRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respond.BadRequest(c, err)
			return
		}
		target, err := uuid.Parse(req.TenantID)
		if err != nil {
			respond.BadRequest(c, err)
			return
		}
		var count int64
		err = deps.DB.WithSystem(c.Request.Context(), func(tx *gorm.DB) error {
			return tx.Raw(
				`SELECT count(*) FROM iam_members WHERE subject = ? AND tenant_id = ?`,
				p.Subject, target).Scan(&count).Error
		})
		if err != nil {
			respond.Internal(c, "could not verify membership")
			return
		}
		if count == 0 {
			respond.Forbidden(c, "not a member of that tenant")
			return
		}
		// The session is re-minted by the shell, which exchanges this
		// confirmation for a token carrying the new tenant_id claim.
		respond.OK(c, gin.H{"tenant_id": req.TenantID})
	})
}
```

Then call `m.registerMe(g, deps)` at the end of `Routes` in `module.go`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd backend && go test ./internal/modules/iam/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add backend/internal/modules/iam/
git commit -m "feat: self-service permissions, tenant list and membership-gated switch"
```

---

### Task 9: Wire it all into `main.go`, config and dev tooling

**Files:**

- Modify: `backend/cmd/api/main.go`
- Modify: `backend/internal/config/config.go`
- Modify: `backend/internal/archtest/arch_test.go` (`allModules`)
- Modify: `scripts/seed-dev.mjs`
- Modify: `Makefile`
- Modify: `README.md`

**Interfaces:**

- Consumes: everything from Tasks 1–8.
- Produces: a booting API with authz enforced end to end, and `make seed` granting the dev user `tenant_admin`.

- [ ] **Step 1: Add config**

In `backend/internal/config/config.go`, add to `Config` and `Load`:

```go
	OpenFGAURL   string
	OpenFGAStore string
```

```go
		OpenFGAURL:   getenv("OPENFGA_URL", "http://localhost:8090"),
		OpenFGAStore: getenv("OPENFGA_STORE", "helivanta"),
```

- [ ] **Step 2: Wire main.go**

In `backend/cmd/api/main.go`, register `iam.New()` first in the module list, build the authz client, add it to the ready checks, reconcile at boot, and mount the middleware:

```go
	registry := platform.NewRegistry()
	for _, mod := range []platform.Module{
		iam.New(), reference.New(), medicore.New(), pharmacy.New(), lab.New(),
	} {
		if err := registry.Register(mod); err != nil {
			return err
		}
	}
```

after the bus is created:

```go
	fga, err := authz.NewClient(ctx, cfg.OpenFGAURL, cfg.OpenFGAStore)
	if err != nil {
		return err
	}
	if err := platform.Reconcile(ctx, registry, db, fga); err != nil {
		return err
	}
```

then the server and routes:

```go
	srv := httpserver.New(
		[]httpserver.ReadyCheck{
			{Name: "postgres", Check: db.PingContext},
			{Name: "nats", Check: bus.Ping},
			{Name: "openfga", Check: fga.Ping},
		},
		requestid.Middleware(),
	)
	deps := platform.Deps{
		DB:    db,
		Bus:   bus,
		Authz: fga,
		Reconcile: func(ctx context.Context, tenantID string) error {
			return platform.ReconcileTenant(ctx, registry, fga, tenantID)
		},
	}
	api := platform.NewRouter(srv.Engine.Group("/v1",
		authn.Middleware(verifier),
		authz.Middleware(fga),
	))
	for _, m := range registry.All() {
		m.Routes(api, deps)
		if err := bus.StartConsumers(ctx, db, m.Consumers(deps)); err != nil {
			return err
		}
	}
```

Add `github.com/tesserix/helivanta/internal/modules/iam` and `github.com/tesserix/helivanta/pkg/authz` to the imports.

- [ ] **Step 3: Register iam in the arch test**

In `backend/internal/archtest/arch_test.go`, add `iam.New()` as the first entry of `allModules()` and import the package. `cmd/api/main.go` and this list must stay in sync.

- [ ] **Step 4: Seed the first tenant_admin**

The first `tenant_admin` cannot be granted through a route guarded by `iam.member.manage`, so the seed path writes it directly. Append to `scripts/seed-dev.mjs`, after the claims are set:

```js
// Bootstrap: the first tenant_admin cannot be granted through a route
// that requires iam.member.manage, so the seed writes the membership row
// directly. The API's reconciler turns it into tuples on next boot; the
// iam-fga-sync consumer does it immediately for later grants.
const { Client } = await import("pg");
const pg = new Client({
  connectionString:
    process.env.ADMIN_DATABASE_URL ??
    "postgres://hms:hms@localhost:5432/hms?sslmode=disable",
});
await pg.connect();
await pg.query(
  `INSERT INTO iam_members (tenant_id, subject, role_key)
   VALUES ($1, $2, 'tenant_admin')
   ON CONFLICT (tenant_id, subject, role_key) DO NOTHING`,
  [TENANT_ID, localId],
);
await pg.end();
console.log(`Granted tenant_admin to ${EMAIL} in tenant ${TENANT_ID}`);
```

Add `pg` to the root `package.json` devDependencies and run `pnpm install` from the repo root.

> `make seed` must now run **after** `make dev` has started the API at least once, because the `iam_members` table is created by the API's migrations. Note this in the README.

- [ ] **Step 5: Update the README quick start**

In `README.md`, change the quick start to make the ordering explicit:

```
    make dev        # infra (Postgres, NATS, Redis, OpenFGA, GIP emulator) + API + web
    make seed       # dev tenant + test user (test@hms.dev / password123), granted tenant_admin
    open http://localhost:4301
```

and add below it:

> `make seed` must run after `make dev` has started the API once — it writes the
> bootstrap `tenant_admin` membership into tables the API's migrations create.
> The dev OpenFGA runs with an in-memory datastore, so its tuples are lost on
> restart; the reconciler rebuilds them from Postgres at every boot.

- [ ] **Step 6: Verify the stack boots end to end**

```bash
make dev-infra
cd backend && go build ./... && go run ./cmd/api &
sleep 5 && curl -s localhost:8080/ready
```

Expected: `ready` reports `postgres`, `nats` and `openfga` healthy. Stop the API afterwards.

- [ ] **Step 7: Run the full backend suite**

Run: `cd backend && go test -race ./... && ./scripts/coverage-gate.sh && golangci-lint run ./...`
Expected: PASS, coverage ≥ 70% per package, lint clean

- [ ] **Step 8: Commit**

```bash
git add backend/ scripts/seed-dev.mjs package.json pnpm-lock.yaml Makefile README.md
git commit -m "feat: wire openfga authorization into the api boot path"
```

---

### Task 10: Adversarial matrix suite

The phase gate. Generated from the registry so a new module's routes are covered automatically.

**Files:**

- Create: `backend/internal/archtest/matrix_test.go`

**Interfaces:**

- Consumes: `allModules()`, `platform.Router.Declared()`, `authz.Client`, `testinfra.StartOpenFGA`, `platform.ReconcileTenant`.
- Produces: no exported symbols — this is the enforcement gate.

- [ ] **Step 1: Write the test**

Create `backend/internal/archtest/matrix_test.go`:

```go
package archtest

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tesserix/helivanta/internal/modules/iam"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/internal/testinfra"
	"github.com/tesserix/helivanta/pkg/authz"
)

const (
	tenantA = "11111111-1111-1111-1111-111111111111"
	tenantB = "22222222-2222-2222-2222-222222222222"
)

func allRoles() []authz.Role {
	return []authz.Role{
		authz.RoleTenantAdmin, authz.RoleDoctor, authz.RoleNurse,
		authz.RolePharmacist, authz.RoleLabTech,
	}
}

// expectedPermissions is the ground truth derived from module
// declarations: which permissions a role should hold after reconcile.
func expectedPermissions(reg *platform.Registry, role authz.Role) authz.PermissionSet {
	set := authz.PermissionSet{}
	for _, g := range platform.GrantsFor(reg) {
		for _, r := range g.Roles {
			if r == role {
				set[g.Permission] = struct{}{}
			}
		}
	}
	return set
}

func registry(t *testing.T) *platform.Registry {
	t.Helper()
	reg := platform.NewRegistry()
	for _, m := range allModules() {
		require.NoError(t, reg.Register(m))
	}
	return reg
}

// TestPermissionMatrix is the phase gate: every role × every declared
// permission, asserted allow/deny against a real OpenFGA, in both
// tenants. The matrix is generated from the registry, so adding a module
// extends the suite automatically — a route cannot ship untested.
func TestPermissionMatrix(t *testing.T) {
	ctx := context.Background()
	reg := registry(t)
	client, err := authz.NewClient(ctx, testinfra.StartOpenFGA(t), "hms-matrix")
	require.NoError(t, err)

	require.NoError(t, platform.ReconcileTenant(ctx, reg, client, tenantA))
	require.NoError(t, platform.ReconcileTenant(ctx, reg, client, tenantB))

	// One user per role, a member of tenant A only.
	for _, role := range allRoles() {
		require.NoError(t, client.GrantRole(ctx, tenantA, "user-"+string(role), role))
	}

	allPerms := map[authz.Permission]struct{}{}
	for _, g := range platform.GrantsFor(reg) {
		allPerms[g.Permission] = struct{}{}
	}

	for _, role := range allRoles() {
		role := role
		t.Run(string(role), func(t *testing.T) {
			got, err := client.Resolve(ctx, "user-"+string(role), tenantA)
			require.NoError(t, err)
			want := expectedPermissions(reg, role)

			for perm := range allPerms {
				if want.Has(perm) {
					require.Truef(t, got.Has(perm), "%s must hold %s", role, perm)
				} else {
					require.Falsef(t, got.Has(perm), "%s must NOT hold %s", role, perm)
				}
			}
		})
	}
}

// TestCrossTenantDenial is the adversarial half: a fully-privileged
// tenant_admin in tenant A must hold nothing at all in tenant B.
func TestCrossTenantDenial(t *testing.T) {
	ctx := context.Background()
	reg := registry(t)
	client, err := authz.NewClient(ctx, testinfra.StartOpenFGA(t), "hms-matrix-cross")
	require.NoError(t, err)

	require.NoError(t, platform.ReconcileTenant(ctx, reg, client, tenantA))
	require.NoError(t, platform.ReconcileTenant(ctx, reg, client, tenantB))
	require.NoError(t, client.GrantRole(ctx, tenantA, "admin-a", authz.RoleTenantAdmin))

	inA, err := client.Resolve(ctx, "admin-a", tenantA)
	require.NoError(t, err)
	require.NotEmpty(t, inA.Sorted(), "sanity: the admin must hold permissions in its own tenant")

	inB, err := client.Resolve(ctx, "admin-a", tenantB)
	require.NoError(t, err)
	require.Empty(t, inB.Sorted(), "tenant A admin must hold nothing in tenant B")
}

// TestEveryGuardedRouteIsCoveredByTheMatrix fails if a module guards a
// route with a permission that no system role holds — such a route would
// be unreachable for everyone except tenant_admin, which is nearly always
// a declaration bug.
func TestEveryGuardedRouteIsCoveredByTheMatrix(t *testing.T) {
	reg := registry(t)
	holders := map[authz.Permission]int{}
	for _, g := range platform.GrantsFor(reg) {
		holders[g.Permission] = len(g.Roles)
	}
	for perm, n := range holders {
		// Every grant gets tenant_admin appended, so a permission held by
		// exactly one role is admin-only.
		if n <= 1 && perm != iam.PermMemberManage {
			t.Errorf("permission %q is held by tenant_admin only; declare the role that needs it", perm)
		}
	}
}
```

- [ ] **Step 2: Run the suite**

Run: `cd backend && go test ./internal/archtest/ -run 'TestPermissionMatrix|TestCrossTenant|TestEveryGuarded' -v`
Expected: PASS, with a subtest per role

- [ ] **Step 3: Commit**

```bash
git add backend/internal/archtest/matrix_test.go
git commit -m "test: adversarial role and cross-tenant permission matrix"
```

---

### Task 11: `packages/api` — `usePermissions` and `<Can>`

**Files:**

- Create: `packages/api/src/permissions.tsx`
- Modify: `packages/api/src/index.ts`
- Test: `packages/api/src/permissions.test.tsx`

**Interfaces:**

- Consumes: `useApiQuery` from `./hooks`.
- Produces:
  - `usePermissions(): { permissions: Set<string>; isLoading: boolean; can: (p: string) => boolean }`
  - `<Can permission="..." fallback={...}>{children}</Can>`

- [ ] **Step 1: Write the failing test**

Create `packages/api/src/permissions.test.tsx`:

```tsx
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import { renderWithProviders } from "./testing";
import { Can } from "./permissions";

function mockPermissions(data: string[]) {
  vi.stubGlobal(
    "fetch",
    vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ data }),
    }),
  );
}

describe("Can", () => {
  beforeEach(() => vi.restoreAllMocks());
  afterEach(() => vi.unstubAllGlobals());

  it("renders children when the permission is held", async () => {
    mockPermissions(["pharmacy.dispense.fulfil"]);
    renderWithProviders(<Can permission="pharmacy.dispense.fulfil">Dispense</Can>);

    await waitFor(() => expect(screen.getByText("Dispense")).toBeInTheDocument());
  });

  it("renders nothing when the permission is missing", async () => {
    mockPermissions(["medicore.visit.read"]);
    renderWithProviders(<Can permission="pharmacy.dispense.fulfil">Dispense</Can>);

    await waitFor(() => expect(screen.queryByText("Dispense")).not.toBeInTheDocument());
  });

  it("renders the fallback when the permission is missing", async () => {
    mockPermissions([]);
    renderWithProviders(
      <Can permission="pharmacy.dispense.fulfil" fallback={<span>Not allowed</span>}>
        Dispense
      </Can>,
    );

    await waitFor(() => expect(screen.getByText("Not allowed")).toBeInTheDocument());
  });

  it("renders nothing while permissions are loading", () => {
    vi.stubGlobal("fetch", vi.fn(() => new Promise(() => {})));
    renderWithProviders(<Can permission="pharmacy.dispense.fulfil">Dispense</Can>);

    expect(screen.queryByText("Dispense")).not.toBeInTheDocument();
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm --filter @helivanta/api test`
Expected: FAIL — cannot resolve `./permissions`

- [ ] **Step 3: Write the implementation**

Create `packages/api/src/permissions.tsx`:

```tsx
"use client";

import type { ReactNode } from "react";
import { useApiQuery } from "./hooks";

const PERMISSIONS_KEY = ["iam", "me", "permissions"];

/**
 * The caller's resolved permissions for the active tenant.
 *
 * This is a convenience layer for hiding what a user cannot do — the API
 * is the enforcement point. Never treat `can()` as a security boundary.
 */
export function usePermissions(): {
  permissions: Set<string>;
  isLoading: boolean;
  can: (permission: string) => boolean;
} {
  const { data, isLoading } = useApiQuery<{ data: string[] }>(
    PERMISSIONS_KEY,
    "/iam/me/permissions",
  );
  const permissions = new Set(data?.data ?? []);
  return {
    permissions,
    isLoading,
    // Deny while loading, so a slow response never flashes an action the
    // user cannot perform.
    can: (permission: string) => !isLoading && permissions.has(permission),
  };
}

export function Can({
  permission,
  children,
  fallback = null,
}: {
  permission: string;
  children: ReactNode;
  fallback?: ReactNode;
}) {
  const { can, isLoading } = usePermissions();
  if (isLoading) return null;
  return can(permission) ? <>{children}</> : <>{fallback}</>;
}
```

- [ ] **Step 4: Export from the package index**

In `packages/api/src/index.ts`:

```ts
export { Can, usePermissions } from "./permissions";
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `pnpm --filter @helivanta/api test && pnpm --filter @helivanta/api type-check`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add packages/api/
git commit -m "feat: usePermissions hook and Can guard in the shared api package"
```

---

### Task 12: Zone gating, action gating and the tenant picker

**Files:**

- Modify: `packages/ui/src/zones.ts`
- Modify: `packages/ui/src/hms-shell.tsx`
- Create: `packages/ui/src/tenant-picker.tsx`
- Modify: `apps/pharmacy/components/dispense-list.tsx`
- Modify: `apps/lab/components/order-list.tsx`
- Modify: `apps/medicore/components/visit-panel.tsx`
- Test: `packages/ui/src/zones.test.ts`
- Test: `packages/ui/src/tenant-picker.test.tsx`
- Modify: `e2e/tests/smoke.spec.ts`

**Interfaces:**

- Consumes: `usePermissions`, `Can` (Task 11); `GET /iam/me/tenants`, `POST /iam/me/tenant` (Task 8).
- Produces:
  - `Zone` and `ZonePage` gain `permission: string`
  - `visibleZones(can: (p: string) => boolean): Zone[]`
  - `<TenantPicker />`

- [ ] **Step 1: Write the failing test**

Create `packages/ui/src/zones.test.ts`:

```ts
import { describe, expect, it } from "vitest";
import { ZONES, visibleZones } from "./zones";

describe("visibleZones", () => {
  it("hides zones whose permission the user lacks", () => {
    const keys = visibleZones((p) => p === "pharmacy.dispense.read").map((z) => z.key);

    expect(keys).toContain("pharmacy");
    expect(keys).not.toContain("lab");
  });

  it("always keeps the dashboard", () => {
    expect(visibleZones(() => false).map((z) => z.key)).toEqual(["dashboard"]);
  });

  it("filters pages within a visible zone", () => {
    const pharmacy = visibleZones((p) => p === "pharmacy.dispense.read").find(
      (z) => z.key === "pharmacy",
    );

    expect(pharmacy?.pages.map((p) => p.label)).toEqual(["Dispenses"]);
  });

  it("declares a permission on every zone and page", () => {
    for (const zone of ZONES) {
      expect(zone.permission).toBeTruthy();
      for (const page of zone.pages) expect(page.permission).toBeTruthy();
    }
  });
});
```

- [ ] **Step 2: Run test to verify it fails**

Run: `pnpm --filter @helivanta/ui test`
Expected: FAIL — `visibleZones` is not exported

- [ ] **Step 3: Add permissions to the zone registry**

In `packages/ui/src/zones.ts`, extend the types and every entry, then add the filter:

```ts
export type ZonePage = { label: string; href: string; icon: LucideIcon; permission: string };

export type Zone = {
  key: string;
  label: string;
  icon: LucideIcon;
  href: string;
  permission: string;
  pages: ZonePage[];
};
```

Set `permission: "public"` on the dashboard zone and its single page; `"medicore.visit.read"` on the medicore zone, OPD and IPD pages; `"pharmacy.dispense.read"` on the pharmacy zone and its Dispenses page with `"pharmacy.medication.read"` on Medications; `"lab.order.read"` on the lab zone and its Orders page.

Append:

```ts
// Navigation is a convenience layer: the API enforces permissions, this
// only avoids showing doors that will not open. The dashboard always
// stays so a user with no permissions still lands somewhere coherent.
export function visibleZones(can: (permission: string) => boolean): Zone[] {
  return ZONES.filter((zone) => zone.key === "dashboard" || can(zone.permission)).map((zone) => ({
    ...zone,
    pages: zone.pages.filter((page) => page.permission === "public" || can(page.permission)),
  }));
}
```

- [ ] **Step 4: Use it in the shell**

In `packages/ui/src/hms-shell.tsx`, replace the `ZONES` import with `visibleZones` plus `activeZone`, call `const { can } = usePermissions()` (import from `@helivanta/api`), and render `visibleZones(can)` in place of `ZONES` in both rails. Add `@helivanta/api` to `packages/ui/package.json` dependencies if it is not already there, then run `pnpm install` from the repo root.

- [ ] **Step 5: Write the tenant picker test**

Create `packages/ui/src/tenant-picker.test.tsx`:

```tsx
import { describe, expect, it, vi, afterEach } from "vitest";
import { screen, waitFor } from "@testing-library/react";
import { renderWithProviders } from "@helivanta/api/testing";
import { TenantPicker } from "./tenant-picker";

afterEach(() => vi.unstubAllGlobals());

describe("TenantPicker", () => {
  it("renders nothing for a single-tenant user", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => ({ data: [{ tenant_id: "t1", roles: ["doctor"] }] }),
      }),
    );
    const { container } = renderWithProviders(<TenantPicker />);

    await waitFor(() => expect(container).toBeEmptyDOMElement());
  });

  it("lists every tenant for a multi-hospital user", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue({
        ok: true,
        status: 200,
        json: async () => ({
          data: [
            { tenant_id: "t1", roles: ["doctor"] },
            { tenant_id: "t2", roles: ["nurse"] },
          ],
        }),
      }),
    );
    renderWithProviders(<TenantPicker />);

    await waitFor(() => expect(screen.getByRole("combobox")).toBeInTheDocument());
    expect(screen.getAllByRole("option")).toHaveLength(2);
  });
});
```

- [ ] **Step 6: Write the tenant picker**

Create `packages/ui/src/tenant-picker.tsx`:

```tsx
"use client";

import { useApiMutation, useApiQuery, apiFetch } from "@helivanta/api";

type Membership = { tenant_id: string; roles: string[] };

/**
 * Lets a clinician working at more than one hospital switch tenants.
 * Renders nothing for single-tenant users, which is almost everyone.
 * The backend re-checks membership — this only offers the choices.
 */
export function TenantPicker() {
  const { data } = useApiQuery<{ data: Membership[] }>(["iam", "me", "tenants"], "/iam/me/tenants");
  const memberships = data?.data ?? [];

  const switchTenant = useApiMutation(
    (tenantId: string) =>
      apiFetch<{ tenant_id: string }>("/iam/me/tenant", {
        method: "POST",
        body: JSON.stringify({ tenant_id: tenantId }),
      }),
    {
      successToast: "Switched hospital",
      // A tenant switch changes every cached query, so reload rather than
      // trying to invalidate selectively.
      onSuccess: () => window.location.reload(),
    },
  );

  if (memberships.length < 2) return null;

  return (
    <label className="flex flex-col gap-1 px-3 py-2 text-xs">
      <span className="text-muted-foreground">Hospital</span>
      <select
        className="rounded-md border bg-transparent px-2 py-1 text-sm"
        onChange={(e) => switchTenant.mutate(e.target.value)}
        defaultValue={memberships[0].tenant_id}
      >
        {memberships.map((m) => (
          <option key={m.tenant_id} value={m.tenant_id}>
            {m.tenant_id.slice(0, 8)} — {m.roles.join(", ")}
          </option>
        ))}
      </select>
    </label>
  );
}
```

Export it from `packages/ui/src/index.ts` and render `<TenantPicker />` in the shell's zone rail above the logout control.

> `apiFetch` must accept an options argument for this to compile. If `packages/api/src/client.ts` does not already take one, extend its signature to `apiFetch<T>(path: string, init?: RequestInit)` and pass `init` through to `fetch`, preserving the existing headers and error handling.

- [ ] **Step 7: Gate the panel actions**

In `apps/pharmacy/components/dispense-list.tsx`, wrap the dispense button in `<Can permission="pharmacy.dispense.fulfil">`; in `apps/lab/components/order-list.tsx` wrap the fulfil button in `<Can permission="lab.order.fulfil">`; in `apps/medicore/components/visit-panel.tsx` wrap the create-visit form in `<Can permission="medicore.visit.create">`. Import `Can` from `@helivanta/api`.

Update each panel's existing test to stub `/iam/me/permissions` with the permission the assertion needs, since `Can` renders nothing while loading and the buttons would otherwise be absent.

- [ ] **Step 8: Keep the e2e smoke test green**

In `e2e/tests/smoke.spec.ts`, the seeded dev user is `tenant_admin` and therefore sees every zone, so existing selectors keep working. Add one assertion after login:

```ts
test("seeded admin sees every zone", async ({ page }) => {
  await page.goto("/");
  for (const zone of ["MediCore", "Pharmacy", "Lab"]) {
    await expect(page.getByRole("link", { name: zone })).toBeVisible();
  }
});
```

- [ ] **Step 9: Run the full frontend suite**

Run: `pnpm install && pnpm turbo lint format:check type-check test build`
Expected: PASS

- [ ] **Step 10: Commit**

```bash
git add packages/ apps/ e2e/ pnpm-lock.yaml
git commit -m "feat: permission-gated zone navigation, actions and tenant picker"
```

---

### Task 13: Documentation and agent enforcement

**Files:**

- Modify: `docs/standards/backend.md`
- Modify: `docs/standards/frontend.md`
- Modify: `CLAUDE.md`
- Modify: `.claude/skills/hms-backend/SKILL.md`
- Modify: `.claude/skills/hms-frontend/SKILL.md`
- Modify: `backend/scripts/new-module.sh`

- [ ] **Step 1: Update the backend standards**

Add an "Authorization" section to `docs/standards/backend.md` covering: every route declares an `authz.Permission` through `*platform.Router` (`authz.Public` is the explicit opt-out); modules declare `Permissions() []authz.Grant` and never list `RoleTenantAdmin`; permissions are named `<module>.<resource>.<action>`; `403` means member-without-permission while `404` remains the cross-tenant answer; FGA failures are `503` and never fail open; membership changes flow through the outbox, so grants are `202` and eventually consistent; new modules must be added to `allModules()` in the arch test.

- [ ] **Step 2: Update the frontend standards**

Add to `docs/standards/frontend.md`: permission gating uses `Can` / `usePermissions` from `@helivanta/api` and is a **convenience layer only**; every zone and page in `packages/ui/src/zones.ts` must declare a permission; never treat `can()` as a security boundary.

- [ ] **Step 3: Update CLAUDE.md**

Add to the backend rules section:

```
- Every route declares a permission via `*platform.Router` (`authz.Public` to opt out); modules declare `Permissions() []authz.Grant` and never list `RoleTenantAdmin`.
- Authorization fails closed: FGA errors are 503, never fail open. 403 = member lacking permission; 404 stays the cross-tenant answer.
```

and to the frontend rules section:

```
- Permission gating uses `Can`/`usePermissions` from `@helivanta/api` and is convenience only — the API enforces. Every zone/page in `packages/ui/src/zones.ts` declares a permission.
```

- [ ] **Step 4: Update the skills**

Mirror the same rules into `.claude/skills/hms-backend/SKILL.md` and `.claude/skills/hms-frontend/SKILL.md` rule tables, pointing at `backend/pkg/authz/` and `packages/api/src/permissions.tsx` as the reference implementations.

- [ ] **Step 5: Update the module generator**

In `backend/scripts/new-module.sh`, add a `Permissions()` stub to the generated `module.go`:

```go
func (m *Module) Permissions() []authz.Grant {
	return []authz.Grant{
		{Permission: PermExampleRead, Roles: []authz.Role{authz.RoleDoctor}},
	}
}
```

with a matching permission const block, switch the generated `Routes` signature to `*platform.Router` with permissions on each route, and extend the printed follow-up reminders to include "register in `internal/archtest/arch_test.go` `allModules()`" and "declare permissions and the roles that hold them".

- [ ] **Step 6: Verify the generator still produces a working module**

```bash
cd backend && ./scripts/new-module.sh scratchtest && go build ./... && go test ./internal/modules/scratchtest/
rm -rf internal/modules/scratchtest
```

Expected: builds and tests pass, then the scratch module is removed.

- [ ] **Step 7: Full green gate**

```bash
make lint-go
cd backend && go test -race ./... && ./scripts/coverage-gate.sh
cd .. && pnpm turbo lint format:check type-check test build
```

Expected: all PASS

- [ ] **Step 8: Commit**

```bash
git add docs/ CLAUDE.md .claude/ backend/scripts/new-module.sh
git commit -m "docs: authorization standards and generator support"
```

---

### Task 14: The whole stack runs locally, verified

Phase 3 is only done when a developer can clone, run one command, log in, and walk the cross-zone journey with permissions enforced. This task removes the seed-ordering trap introduced in Task 9, adds a scripted health check, and proves the journey end to end with two differently-privileged users.

**Files:**

- Create: `backend/cmd/migrate/main.go`
- Create: `scripts/verify-local.sh`
- Modify: `Makefile`
- Modify: `scripts/seed-dev.mjs`
- Modify: `e2e/tests/smoke.spec.ts`
- Create: `e2e/tests/journey.spec.ts`
- Modify: `README.md`

**Interfaces:**

- Consumes: everything from Tasks 1–13.
- Produces:
  - `make migrate` — applies migrations and exits, so `make seed` no longer depends on having started the API
  - `make verify-local` — asserts every process and dependency is healthy
  - A second seeded user, `pharmacist@hms.dev`, holding only `pharmacist`

- [ ] **Step 1: Add a migrate-only entrypoint**

Task 9 Step 4 left a trap: `make seed` writes into `iam_members`, which only exists after the API has booted once. Fix it properly rather than documenting around it.

Create `backend/cmd/migrate/main.go`:

```go
// Command migrate applies all module migrations and exits. It exists so
// that seeding, CI and a first-time clone do not need to boot the API
// (and therefore NATS and OpenFGA) just to create tables.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/tesserix/helivanta/internal/config"
	"github.com/tesserix/helivanta/internal/modules/iam"
	"github.com/tesserix/helivanta/internal/modules/lab"
	"github.com/tesserix/helivanta/internal/modules/medicore"
	"github.com/tesserix/helivanta/internal/modules/pharmacy"
	"github.com/tesserix/helivanta/internal/modules/reference"
	"github.com/tesserix/helivanta/internal/platform"
	"github.com/tesserix/helivanta/pkg/events"
	"github.com/tesserix/helivanta/pkg/tenantdb"
)

func main() {
	if err := run(); err != nil {
		slog.Error("migrate failed", "err", err)
		os.Exit(1)
	}
	slog.Info("migrations applied")
}

func run() error {
	cfg := config.Load()
	ctx := context.Background()

	db, err := tenantdb.Open(cfg.AppDatabaseURL, cfg.AdminDatabaseURL)
	if err != nil {
		return err
	}

	registry := platform.NewRegistry()
	for _, mod := range []platform.Module{
		iam.New(), reference.New(), medicore.New(), pharmacy.New(), lab.New(),
	} {
		if err := registry.Register(mod); err != nil {
			return err
		}
	}

	migs := events.Migrations()
	for _, m := range registry.All() {
		migs = append(migs, m.Migrations()...)
	}
	if err := db.Migrate(ctx, migs); err != nil {
		return err
	}
	bad, err := db.LintRLS(ctx)
	if err != nil {
		return err
	}
	if len(bad) > 0 {
		return fmt.Errorf("tables missing forced RLS: %v", bad)
	}
	return nil
}
```

> The module list appears in three places now (`cmd/api`, `cmd/migrate`, `archtest.allModules`). Task 13 Step 5 already adds the generator reminder; extend it to name `cmd/migrate/main.go` too.

- [ ] **Step 2: Seed a second, differently-privileged user**

In `scripts/seed-dev.mjs`, extract the user creation into a reusable function and seed two users: the existing `test@hms.dev` as `tenant_admin`, and a new `pharmacist@hms.dev` (same password) as `pharmacist` only. The second user is what makes permission gating observable by hand and testable in e2e.

```js
const USERS = [
  { email: "test@hms.dev", role: "tenant_admin" },
  { email: "pharmacist@hms.dev", role: "pharmacist" },
];
```

Loop over `USERS`, performing the existing signUp / lookup / custom-claims steps for each, then the `iam_members` insert with that user's role. Print both credentials at the end:

```js
console.log(`Seeded:
  test@hms.dev       / password123  (tenant_admin — sees every zone)
  pharmacist@hms.dev / password123  (pharmacist — sees Pharmacy only)`);
```

- [ ] **Step 3: Add the Makefile targets**

In `Makefile`, add `migrate` and `verify-local` to `.PHONY` and define:

```makefile
migrate:
	cd backend && go run ./cmd/migrate

seed: migrate
	node scripts/seed-dev.mjs

verify-local:
	./scripts/verify-local.sh
```

Making `seed` depend on `migrate` removes the ordering trap entirely: `make dev-infra && make seed` now works on a fresh clone with no API running.

- [ ] **Step 4: Write the local verification script**

Create `scripts/verify-local.sh` (and `chmod +x` it):

```bash
#!/usr/bin/env bash
# Verifies that the whole Helivanta stack is up and healthy locally.
# Usage: make verify-local   (after `make dev` in another terminal)
set -uo pipefail

fail=0

check() {
  local name="$1" url="$2"
  if curl -fsS --max-time 5 "$url" >/dev/null 2>&1; then
    printf '  ok    %-24s %s\n' "$name" "$url"
  else
    printf '  FAIL  %-24s %s\n' "$name" "$url"
    fail=1
  fi
}

echo "Infrastructure:"
docker compose -f docker-compose.dev.yml ps --status running --format '  ok    {{.Service}}' || fail=1
check "openfga"          "http://localhost:8090/healthz"
check "gip emulator"     "http://localhost:9099/"

echo "Backend:"
check "api health"       "http://localhost:8080/health"
check "api ready"        "http://localhost:8080/ready"

echo "Frontend zones:"
check "shell    (4301)"  "http://localhost:4301/login"
check "medicore (4302)"  "http://localhost:4302"
check "pharmacy (4303)"  "http://localhost:4303"
check "lab      (4304)"  "http://localhost:4304"

echo
if [ "$fail" -eq 0 ]; then
  echo "All checks passed. Log in at http://localhost:4301/login"
  echo "  test@hms.dev       / password123  (tenant_admin)"
  echo "  pharmacist@hms.dev / password123  (pharmacist)"
else
  echo "Some checks failed. Common causes:"
  echo "  - 'make dev' not running, or still starting (Next.js takes ~20s)"
  echo "  - NODE_AUTH_TOKEN unset: export NODE_AUTH_TOKEN=\$(gh auth token)"
  echo "  - Docker not running (infra and backend tests both need it)"
  echo "  - /ready failing on openfga: check 'docker compose -f docker-compose.dev.yml logs openfga'"
fi
exit "$fail"
```

- [ ] **Step 5: Run it against a live stack**

In one terminal: `make dev`. In another, once Next.js has finished starting:

```bash
make seed
make verify-local
```

Expected: every line reports `ok`, and the two credential lines print.

- [ ] **Step 6: Write the end-to-end journey test**

This is the cross-zone journey with authorization actually enforced. Create `e2e/tests/journey.spec.ts`:

```ts
import { expect, test } from "@playwright/test";

const ADMIN = { email: "test@hms.dev", password: "password123" };
const PHARMACIST = { email: "pharmacist@hms.dev", password: "password123" };

async function login(page, user: { email: string; password: string }) {
  await page.goto("/login");
  await page.getByLabel(/email/i).fill(user.email);
  await page.getByLabel(/password/i).fill(user.password);
  await page.getByRole("button", { name: /sign in/i }).click();
  await expect(page).not.toHaveURL(/\/login/);
}

test("admin creates a visit and it lands in pharmacy and lab", async ({ page }) => {
  await login(page, ADMIN);

  const patient = `E2E Patient ${Date.now()}`;
  await page.goto("/medicore/opd");
  await page.getByLabel(/patient name/i).fill(patient);
  await page.getByRole("button", { name: /create visit/i }).click();
  await expect(page.getByText(patient)).toBeVisible();

  // visit_created flows through JetStream into both consumers.
  await page.goto("/pharmacy");
  await expect(page.getByText(patient)).toBeVisible({ timeout: 15_000 });

  await page.goto("/lab");
  await expect(page.getByText(patient)).toBeVisible({ timeout: 15_000 });
});

test("pharmacist sees only the pharmacy zone", async ({ page }) => {
  await login(page, PHARMACIST);

  await expect(page.getByRole("link", { name: "Pharmacy" })).toBeVisible();
  await expect(page.getByRole("link", { name: "MediCore" })).toHaveCount(0);
  await expect(page.getByRole("link", { name: "Lab" })).toHaveCount(0);
});

test("pharmacist cannot create a visit even by navigating directly", async ({ page }) => {
  await login(page, PHARMACIST);
  await page.goto("/medicore/opd");

  // The UI hides the action; the API is the enforcement point, so the
  // form must be absent rather than merely disabled.
  await expect(page.getByRole("button", { name: /create visit/i })).toHaveCount(0);
});

test("pharmacist can dispense", async ({ page }) => {
  await login(page, PHARMACIST);
  await page.goto("/pharmacy");

  await expect(page.getByRole("button", { name: /dispense/i }).first()).toBeVisible();
});
```

Adjust the selectors to match the actual labels in `apps/medicore/components/visit-panel.tsx` and `apps/pharmacy/components/dispense-list.tsx` — read those files rather than assuming.

- [ ] **Step 7: Run the e2e suite against the local stack**

With `make dev` running and `make seed` applied:

```bash
make e2e
```

Expected: `smoke.spec.ts` and `journey.spec.ts` both pass.

> If the pharmacist tests fail because the seeded tuples are missing, the OpenFGA container was restarted after seeding — its dev datastore is in-memory. Restart the API (`make dev-api`) to let the reconciler rebuild tuples from Postgres, which is exactly the property D6 buys.

- [ ] **Step 8: Document the local run**

Rewrite the `README.md` quick start:

```markdown
## Quick start

Requires Docker, Go 1.26, Node 22 (`corepack enable`).

Set `NODE_AUTH_TOKEN` to a GitHub token with `read:packages`
(`export NODE_AUTH_TOKEN=$(gh auth token)`) — required for `@tesserix/web`
from GitHub Packages.

    pnpm install
    make dev-infra      # Postgres, NATS, Redis, OpenFGA, GIP emulator
    make seed           # migrations + dev tenant + two test users
    make dev            # API (8080) + all four zone apps
    make verify-local   # asserts every process is healthy

Log in at http://localhost:4301/login:

| User                 | Password      | Sees                        |
| -------------------- | ------------- | --------------------------- |
| `test@hms.dev`       | `password123` | every zone (`tenant_admin`) |
| `pharmacist@hms.dev` | `password123` | Pharmacy only               |

Ports: shell 4301, medicore 4302, pharmacy 4303, lab 4304, API 8080,
Postgres 5432, NATS 4222, Redis 6379, OpenFGA 8090, GIP emulator 9099.

### Notes

- The dev OpenFGA uses an in-memory datastore, so its tuples vanish on
  restart. Postgres is the system of record — restarting the API rebuilds
  every tuple via the reconciler.
- `make seed` runs migrations first, so it works on a fresh clone with no
  API running.
- Backend tests need Docker (testcontainers): `cd backend && go test ./...`
```

- [ ] **Step 9: Full green gate**

```bash
make lint-go
cd backend && go test -race ./... && ./scripts/coverage-gate.sh
cd .. && pnpm turbo lint format:check type-check test build
make verify-local && make e2e
```

Expected: all PASS

- [ ] **Step 10: Commit**

```bash
git add backend/cmd/migrate/ scripts/ Makefile e2e/ README.md
git commit -m "feat: migrate-only entrypoint, local verification and cross-zone e2e journey"
```

---

## Self-Review

**Spec coverage:** D1 role→action (Tasks 1, 4 permission declarations); D2 resolve-once (Tasks 2, 3); D3 fail-closed (Task 3, tests assert 503 on every path including Public); D4 roles-as-data (Task 2 `model.go` — no role or permission names in the model); D5 compile-time declaration (Task 4 `platform.Router` + two arch tests); D6 Postgres-as-truth (Tasks 5–7, outbox + reconciler); D7 membership-by-resolution (Task 3 empty-set denial test, Task 8 `/me/tenants`); D8 tenant switching (Task 8 endpoints, Task 12 picker); D9 departments designed-for (no scope parameter shipped — see the note below). FGA model, permission vocabulary, five system roles, reconciler, `iam` module, `respond.Forbidden`, frontend gating, bootstrapping, error-handling table, and the full testing section all map to tasks.

**Known deviation from the spec:** D9 says `pkg/authz` should "take scope as a parameter that is always tenant-wide in this phase". This plan does **not** add that parameter — `Resolve(ctx, subject, tenantID)` has no scope argument, and `Require(perm)` has no scope argument. Adding an always-constant parameter to every call site is speculative generality that YAGNI rejects, and the object-namespace model (`perm:<tenantID>/<permission>`) already leaves room to extend the object id to `perm:<tenantID>/<departmentID>/<permission>` later without reshaping the model's type definitions. The reservation the spec actually needs — a model shape that admits scoping — is preserved. Flag this to the reviewer at Task 1; if they want the parameter, it is a one-line change to `Resolver` before Task 3 lands.

**Placeholder scan:** no TBD/TODO; every code step carries real code; the two judgement-call steps (Task 2 Step 6's `openfga.APIError` line, Task 12 Step 6's `apiFetch` signature) state the exact fallback rather than leaving it open.

**Type consistency:** `PermissionSet`/`Permission`/`Role`/`Grant` are used identically in Tasks 1, 2, 3, 4, 7, 10. `TupleWriter` (Task 6) is the interface `*authz.Client` (Task 2) satisfies — method names `GrantRole`/`RevokeRole`/`GrantPermission` match exactly. `ModuleHarnessWithAuthz` (Task 6 Step 3) is used with the same argument order in Tasks 6 and 8. `visibleZones` (Task 12) matches its test. `usePermissions().can` (Task 11) is what `visibleZones` consumes in Task 12 Step 4.

**Ordering risk:** Task 4 changes the `Module` interface and every module's `Routes` signature at once, so the tree does not compile mid-task. Its steps are ordered so that Step 8 is the first full build; a reviewer should treat Task 4 as atomic.

**Scope note on Task 14:** Task 14 is beyond the spec, added because "the whole stack runs locally" is this phase's real definition of done and because Task 9 Step 4 introduces a seed-ordering trap that should not survive the phase. It is deliberately limited to *verifying and unblocking* the existing `make dev` flow — a migrate-only entrypoint, a health-check script, a second seeded user, and the cross-zone e2e journey. It does **not** attempt broader local-dev hardening (container-per-service parity with production, hot-reload for the Go API, seeded clinical fixtures, offline `@tesserix/web` mirroring). If the local developer experience needs that, it deserves its own spec rather than being smuggled into an authorization phase.
