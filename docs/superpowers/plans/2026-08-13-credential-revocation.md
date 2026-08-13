# Credential Revocation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make revocation of an account, a session or a tenant membership take effect on the next request instead of at token expiry.

**Architecture:** A per-subject revocation watermark in Postgres, owned by the `iam` module, checked on the authentication path against the token's `auth_time` and cached per replica with NATS broadcast invalidation. Membership becomes a first-class derived OpenFGA relation checked per request, so `authz.Public` stops meaning "membership unexamined". Sign-out writes the watermark, revokes GIP refresh tokens, and clears the Firebase SDK session so a shared workstation cannot resurrect it.

**Tech Stack:** Go 1.26, Gin, GORM, Postgres 16, OpenFGA, NATS JetStream, Firebase Admin SDK (GIP), Next.js 16 / React 19, Playwright, testcontainers.

**Spec:** `docs/superpowers/specs/2026-08-13-credential-revocation-design.md`
**Issue:** #781. Branch: `feat/781-credential-revocation`.

## Global Constraints

- `docs/standards/engineering-principles.md` is binding. No minimal/MVP/temporary solutions; scope down, never quality down; fail closed; enforce structurally; verify the claim, not a proxy.
- **Every new assertion must be observed failing before it passes.** Break the implementation, watch it go red, restore. A step that says "prove it can fail" is not optional.
- `make lint-go` clean; `cd backend && go test -race ./...` green; `cd backend && ./scripts/coverage-gate.sh` green (70% floor); `pnpm turbo lint type-check test build` green.
- Migration IDs are `NNNN_<module>` and **append-only**. The next `iam` migration is `0003_iam`.
- Modules never import other modules. Cross-module data flows via events only.
- Tenant data only via `WithTenant`. `WithAdmin` is boot/ops-only and arch-test restricted.
- `respond.*` helpers for every response. `slog` only. Wrap errors with `%w`.
- Fail-closed HTTP contract, unchanged: authorization infrastructure failure is `503` with code `authz_unavailable`; a member lacking permission is `403`; cross-tenant is `404`.
- Cache constants are **code constants, never configuration**: `maxRevocationCacheEntries = 10_000`, `revocationCacheTTL = 5 * time.Minute`.
- The watermark compares **`auth_time`**, never `iat`. This is spec D2 and is the single detail that decides whether the mechanism works.

---

## File Structure

**New:**

| File | Responsibility |
|---|---|
| `backend/pkg/authz/tenant.go` | `TenantObject`, `GrantTenantRole`, `RevokeTenantRole`, `IsMember` — the tenant type's client surface |
| `backend/pkg/authz/tenant_test.go` | Model shape, membership derivation, tenant-object parsing |
| `backend/pkg/authz/membership.go` | `MembershipChecker` interface, `MembershipMiddleware`, `RequireMembership` |
| `backend/pkg/authn/revocation.go` | `RevocationChecker` interface and its integration into `Middleware` |
| `backend/internal/modules/iam/revocation.go` | The watermark table's model, the checker implementation, the LRU cache |
| `backend/internal/modules/iam/revocation_test.go` | Watermark semantics, cache behaviour, fail-closed |
| `backend/internal/modules/iam/signout.go` | `POST /me/sign-out` and `POST /subjects/:subject/revoke` handlers |
| `backend/internal/modules/iam/signout_test.go` | Handler behaviour, cross-tenant bounding, GIP revoke call |
| `backend/pkg/events/broadcast.go` | Ephemeral fanout subscriptions — every replica receives every message |
| `backend/pkg/events/broadcast_test.go` | Fanout proof: N subscribers, N deliveries |
| `apps/shell/app/logout/route.ts` (rewrite) | POST-only, same-origin checked, calls API + Firebase `signOut()` |
| `apps/shell/app/logout/logout.test.ts` | Same-origin rejection, ordering of the three teardown steps |
| `e2e/tests/signout.spec.ts` | The shared-workstation guarantee, end to end |

**Modified:**

| File | Change |
|---|---|
| `backend/pkg/authz/model.go` | Add the `tenant` type; `member` derived from `granted_role → assignee` |
| `backend/pkg/authz/client.go` | `ensureModel` compares and versions instead of returning early |
| `backend/pkg/authz/read.go` | `TenantOfObject` becomes type-aware so `tenant:<uuid>` parses |
| `backend/pkg/authz/authz.go` | Add `NoTenantMembership`; remove `Has`'s `Public` short-circuit |
| `backend/pkg/authz/middleware.go` | `Require` skips the set for `Public`/`NoTenantMembership` rather than lying about it |
| `backend/pkg/authn/authn.go` | `Middleware` gains the revocation check |
| `backend/internal/platform/router.go` | `handle` inserts `RequireMembership` unless the marker opts out |
| `backend/internal/platform/module.go` | `Deps` gains nothing; `TupleWriter`/`TupleReconciler` gain the tenant-tuple methods |
| `backend/internal/platform/reconcile.go` | Write and prune `tenant:<id> granted_role role:<id>/<key>` |
| `backend/internal/modules/iam/module.go` | Migration `0003_iam`; new permission; new routes |
| `backend/internal/modules/iam/me.go` | Three routes move to `NoTenantMembership` |
| `backend/internal/archtest/arch_test.go` | `NoTenantMembership` allowlist test |
| `backend/internal/testutil/harness.go` | Four `ModuleHarness*` variants collapse into one options struct |
| `backend/pkg/tenantdb/db.go` | `LintRLS` allowlist gains `iam_credential_revocations` **with its reason** |
| `backend/cmd/api/main.go` | Wire the checker, the membership checker, the broadcast consumer |
| `packages/ui/src/hms-shell.tsx:115,197` | Logout anchor becomes a form submission |
| `docs/standards/backend.md` | Consumer-naming deviation for broadcast consumers |
| `docs/standards/frontend.md` | Sign-out is the documented exception to "links are plain `<a>`" |

---

## Task 1: The `tenant` type and a versionable model

**Files:**
- Create: `backend/pkg/authz/tenant.go`, `backend/pkg/authz/tenant_test.go`
- Modify: `backend/pkg/authz/model.go`, `backend/pkg/authz/client.go:156-174`, `backend/pkg/authz/read.go:74-84`

**Interfaces:**
- Produces: `authz.TenantObject(tenantID string) string`; `(*Client).GrantTenantRole(ctx, tenantID string, role Role) error`; `(*Client).RevokeTenantRole(ctx, tenantID string, role Role) error`; `(*Client).IsMember(ctx, subject, tenantID string) (bool, error)`.
- Consumes: nothing from other tasks.

**Why this is first.** Everything else depends on the model having a `member` relation, and on `ensureModel` being willing to write a second model version. Until both exist, nothing downstream can be tested against a real store.

- [ ] **Step 1: Write the failing test for tenant-object parsing**

`TenantOfObject` currently requires `<type>:<tenantID>/<name>`. A `tenant:<uuid>` object has no `/`, so it returns `false` — and `prune` would then treat every tenant tuple as unparseable. Making it lenient (any object without `/` parses as a tenant) would be dangerous: a malformed `perm:garbage` would parse as tenant `garbage` and become a delete candidate in the wrong bucket. So it becomes **type-aware**.

Add to `backend/pkg/authz/read_test.go`:

```go
func TestTenantOfObjectIsTypeAware(t *testing.T) {
	const tid = "7c9e6679-7425-40de-944b-e07fc1f90ae7"

	got, ok := TenantOfObject("tenant:" + tid)
	require.True(t, ok, "a tenant object carries its tenant id directly, with no /name segment")
	require.Equal(t, tid, got)

	got, ok = TenantOfObject("perm:" + tid + "/medicore.visit.read")
	require.True(t, ok)
	require.Equal(t, tid, got)

	// The dangerous case: a non-tenant type with no /name segment must
	// NOT parse. If it did, prune would bucket it by a tenant id that
	// was never validated and could delete a tuple it does not
	// understand.
	_, ok = TenantOfObject("perm:garbage")
	require.False(t, ok, "a non-tenant object without a /name segment must not parse")

	_, ok = TenantOfObject("tenant:")
	require.False(t, ok, "an empty tenant id must not parse")
}
```

- [ ] **Step 2: Run it and confirm it fails**

```bash
cd backend && go test ./pkg/authz/ -run TestTenantOfObjectIsTypeAware -v
```

Expected: FAIL on the first assertion — `TenantOfObject("tenant:<uuid>")` returns `ok == false` because there is no `/`.

- [ ] **Step 3: Make `TenantOfObject` type-aware**

Replace `backend/pkg/authz/read.go:74-84`:

```go
// TenantOfObject returns the tenant an object is namespaced to.
//
// Two shapes exist, and the distinction is load-bearing for prune:
//
//	role:<tenantID>/<roleKey>      perm:<tenantID>/<permission>
//	tenant:<tenantID>
//
// A tenant object IS its tenant id, so it has no /name segment. Every
// other type must have one. Accepting a missing /name segment for any
// type would let a malformed object such as "perm:garbage" parse as
// tenant "garbage" and become a delete candidate bucketed under a
// tenant id nothing validated — for an operation whose whole purpose is
// removing authorization, an unexplained object must fail to parse
// rather than parse into a guess.
func TenantOfObject(object string) (string, bool) {
	typ, rest, ok := strings.Cut(object, ":")
	if !ok || rest == "" {
		return "", false
	}
	if typ == tenantType {
		// A tenant object carries no /name segment; one appearing here
		// means the object is not the shape this function believes.
		if strings.Contains(rest, "/") {
			return "", false
		}
		return rest, true
	}
	tenantID, name, ok := strings.Cut(rest, "/")
	if !ok || tenantID == "" || name == "" {
		return "", false
	}
	return tenantID, true
}
```

- [ ] **Step 4: Run it and confirm it passes**

```bash
cd backend && go test ./pkg/authz/ -run TestTenantOfObjectIsTypeAware -v
```

Expected: PASS.

- [ ] **Step 5: Prove the dangerous assertion can fail**

Temporarily delete the `if strings.Contains(rest, "/")` guard, re-run, and confirm no test fails — then temporarily change the `typ == tenantType` branch to `true` (accept any type without a slash) and confirm `TestTenantOfObjectIsTypeAware` fails on the `perm:garbage` case. Restore both.

This is the assertion that stops a future "simplification" from re-opening the prune hazard, so it must be seen failing.

- [ ] **Step 6: Add the `tenant` type to the model**

Modify `backend/pkg/authz/model.go`. Update the doc comment and add the type:

```go
// modelJSON is the complete HMS authorization model. It deliberately
// contains no role names and no permission names: roles and permissions
// are objects, and granting is a tuple write. This file changes only if
// the *shape* of authorization changes (for example when per-record or
// department scoping lands), never when a zone, permission or role is
// added.
//
//	role:<tenantID>/<roleKey>          assignee     user:<subject>
//	perm:<tenantID>/<permission>       granted_role role:<tenantID>/<roleKey>
//	tenant:<tenantID>                  granted_role role:<tenantID>/<roleKey>
//
// Tenant isolation lives in the object-id namespace, so a ListObjects
// for one tenant can never return another tenant's perm objects.
//
// `member` on tenant is DERIVED, never granted directly: it resolves
// through the same role assignment that grants permissions, so
// "member of this tenant" and "holds a role in this tenant" cannot
// drift apart. A directly-writable member relation would be a second,
// independent definition of membership and therefore a second thing to
// keep in sync.
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
    },
    {
      "type": "tenant",
      "relations": {
        "granted_role": { "this": {} },
        "member": {
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

- [ ] **Step 7: Write the failing test for the client surface**

Create `backend/pkg/authz/tenant_test.go`. This needs a real OpenFGA container — follow the pattern in the existing `pkg/authz` integration tests (`testinfra.StartOpenFGA(t)`; check the exact helper name in `backend/internal/testinfra/containers.go` and match it).

```go
func TestMembershipIsDerivedFromRoleAssignment(t *testing.T) {
	c := newTestClient(t) // same helper the existing authz integration tests use
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
	require.NoError(t, c.GrantTenantRole(ctx, tenantA, RoleNurse))
	member, err = c.IsMember(ctx, subject, tenantA)
	require.NoError(t, err)
	require.False(t, member, "a tenant->role edge with no assignee makes nobody a member")

	// Assigning the role is what makes the subject a member.
	require.NoError(t, c.GrantRole(ctx, tenantA, subject, RoleNurse))
	member, err = c.IsMember(ctx, subject, tenantA)
	require.NoError(t, err)
	require.True(t, member)

	// Membership does not leak across tenants.
	member, err = c.IsMember(ctx, subject, tenantB)
	require.NoError(t, err)
	require.False(t, member, "membership in A must not imply membership in B")

	// Revoking the role revokes membership, with no separate write.
	require.NoError(t, c.RevokeRole(ctx, tenantA, subject, RoleNurse))
	member, err = c.IsMember(ctx, subject, tenantA)
	require.NoError(t, err)
	require.False(t, member, "revoking the role revokes membership")
}
```

- [ ] **Step 8: Run it and confirm it fails**

```bash
cd backend && go test ./pkg/authz/ -run TestMembershipIsDerivedFromRoleAssignment -v
```

Expected: FAIL to compile — `IsMember`, `GrantTenantRole` undefined.

- [ ] **Step 9: Implement `backend/pkg/authz/tenant.go`**

```go
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
```

- [ ] **Step 10: Run and confirm the membership test passes**

```bash
cd backend && go test ./pkg/authz/ -run TestMembershipIsDerivedFromRoleAssignment -v
```

Expected: PASS. If `IsMember` returns false after `GrantRole`, the model did not take — see the next step, which is the cause.

- [ ] **Step 11: Write the failing test for model versioning**

`ensureModel` returns early when any model exists, so a store created before this change keeps the two-type model forever and `IsMember` fails against it. That is the exact upgrade path every existing environment is on.

```go
func TestEnsureModelUpgradesAnExistingStore(t *testing.T) {
	url := testinfra.StartOpenFGA(t)
	ctx := context.Background()
	store := "upgrade-" + t.Name()

	// Boot once against a deliberately older model to simulate a store
	// created before the tenant type existed.
	old, err := NewClient(ctx, url, store)
	require.NoError(t, err)
	require.NoError(t, old.writeModelForTest(ctx, legacyModelJSON))

	// Booting again must notice the store's model differs from the
	// desired one and write a new version.
	upgraded, err := NewClient(ctx, url, store)
	require.NoError(t, err)

	const (
		tenantID = "33333333-3333-3333-3333-333333333333"
		subject  = "uid-upgrade"
	)
	require.NoError(t, upgraded.GrantTenantRole(ctx, tenantID, RoleNurse))
	require.NoError(t, upgraded.GrantRole(ctx, tenantID, subject, RoleNurse))

	member, err := upgraded.IsMember(ctx, subject, tenantID)
	require.NoError(t, err)
	require.True(t, member, "a store created before the tenant type must be upgraded on boot")
}
```

Add `legacyModelJSON` to the test file as a verbatim copy of the pre-change `modelJSON` (user/role/perm only), and `writeModelForTest` as an exported-for-test helper in `export_test.go` that calls `WriteAuthorizationModel` directly.

- [ ] **Step 12: Run it and confirm it fails**

```bash
cd backend && go test ./pkg/authz/ -run TestEnsureModelUpgradesAnExistingStore -v
```

Expected: FAIL — `IsMember` errors or returns false, because `ensureModel` saw an existing model and returned without writing the new one.

- [ ] **Step 13: Make `ensureModel` version**

Replace `backend/pkg/authz/client.go:156-174`:

```go
// ensureModel writes modelJSON unless the store's latest model already
// has the same type definitions.
//
// The previous implementation returned early whenever ANY model
// existed, on the reasoning that the model is immutable in practice.
// That stopped being true when the tenant type was added: every store
// created before it would otherwise keep a model with no `member`
// relation forever, and every membership check against such a store
// fails — which locks out every user in every tenant. So this compares
// rather than assumes.
//
// OpenFGA models are immutable and append-only: writing produces a new
// model id rather than mutating the old one, so an upgrade never
// invalidates tuples. Racing replicas may both write; both then re-read
// and converge on the same latest id, exactly as ensureStore does for
// the store id and for the same reason.
func (c *Client) ensureModel(ctx context.Context) error {
	var desired fgaclient.ClientWriteAuthorizationModelRequest
	if err := json.Unmarshal([]byte(modelJSON), &desired); err != nil {
		return fmt.Errorf("parse model: %w", err)
	}

	existing, err := c.api.ReadAuthorizationModels(ctx).Execute()
	if err != nil {
		return fmt.Errorf("read models: %w", err)
	}
	if models := existing.GetAuthorizationModels(); len(models) > 0 {
		// ReadAuthorizationModels returns newest first.
		if sameTypeDefinitions(models[0].GetTypeDefinitions(), desired.TypeDefinitions) {
			return nil
		}
		slog.InfoContext(ctx, "authorization model differs from desired; writing a new version",
			"existing_model_id", models[0].GetId())
	}
	if _, err := c.api.WriteAuthorizationModel(ctx).Body(desired).Execute(); err != nil {
		return fmt.Errorf("write model: %w", err)
	}
	return nil
}

// sameTypeDefinitions compares two models by the set of type names and
// each type's relation names.
//
// It deliberately does not compare the full rewrite trees. A structural
// deep-compare of OpenFGA's generated types is brittle across SDK
// versions, and being wrong in the "they differ" direction is cheap:
// the model is re-written, which is idempotent in effect because
// OpenFGA versions rather than mutates. Being wrong in the "they match"
// direction is the expensive one, and adding a type or a relation —
// the only changes this repo has ever made — always changes a name.
func sameTypeDefinitions(existing, desired []openfga.TypeDefinition) bool {
	shape := func(defs []openfga.TypeDefinition) map[string][]string {
		out := make(map[string][]string, len(defs))
		for _, d := range defs {
			rels := make([]string, 0, len(d.GetRelations()))
			for name := range d.GetRelations() {
				rels = append(rels, name)
			}
			sort.Strings(rels)
			out[d.GetType()] = rels
		}
		return out
	}
	return reflect.DeepEqual(shape(existing), shape(desired))
}
```

Add `reflect` and `sort` to the imports if not present; `openfga` and `slog` already are.

- [ ] **Step 14: Run the whole package**

```bash
cd backend && go test -race ./pkg/authz/...
```

Expected: PASS, including the pre-existing store/model tests.

- [ ] **Step 15: Prove the upgrade test can fail**

Restore the old early-return (`if len(existing.GetAuthorizationModels()) > 0 { return nil }`), run `TestEnsureModelUpgradesAnExistingStore`, and confirm it fails. Restore.

Then make `sameTypeDefinitions` always return `true`, re-run, and confirm the same test fails. Restore. This proves the comparison is load-bearing rather than decorative.

- [ ] **Step 16: Commit**

```bash
cd backend && go vet ./... && cd .. && make lint-go
git add backend/pkg/authz/
git commit -m "feat: add a tenant type whose member relation derives from role assignment, and let ensureModel version an existing store (#781)"
```

---

## Task 2: The reconciler writes and prunes tenant→role edges

**Files:**
- Modify: `backend/internal/platform/module.go` (`TupleWriter`), `backend/internal/platform/reconcile.go`, `backend/internal/platform/reconcile_test.go`, `backend/internal/platform/reconcile_prune_test.go`
- Modify: `backend/internal/testutil/harness.go` (`noopWriter` gains the new methods)

**Interfaces:**
- Consumes: `authz.TenantObject`, `(*Client).GrantTenantRole`, `(*Client).RevokeTenantRole` from Task 1.
- Produces: `platform.TupleWriter` gains `GrantTenantRole(ctx, tenantID string, role authz.Role) error`; `platform.TupleReconciler` gains `RevokeTenantRole(ctx, tenantID string, role authz.Role) error`.

**Why this matters more than it looks.** An existing store has no `tenant:… granted_role …` tuples. The instant Task 3's membership check goes live, every `Check` returns false and **every user in every tenant is locked out**. `platform.Reconcile` already runs at `cmd/api/main.go:90`, before `ListenAndServe` at line 137, so the ordering that saves us exists — this task makes the reconciler actually write the edges, and the test asserts the whole rescue path.

- [ ] **Step 1: Write the failing test — a store with no tenant edges gains them**

Add to `backend/internal/platform/reconcile_test.go`:

```go
func TestReconcileWritesTenantRoleEdgesForEveryBackedMembership(t *testing.T) {
	reg, db, fga, ctx := reconcileHarness(t) // existing helper in this file

	const (
		tenantID = "44444444-4444-4444-4444-444444444444"
		subject  = "uid-reconcile"
	)
	seedMembership(t, db, tenantID, subject, string(authz.RoleNurse))

	require.NoError(t, Reconcile(ctx, reg, db, fga))

	member, err := fga.IsMember(ctx, subject, tenantID)
	require.NoError(t, err)
	require.True(t, member,
		"a membership row backed by Postgres must produce a working membership check after reconcile")
}
```

If `reconcileHarness` and `seedMembership` do not exist under those names, use whatever the existing tests in this file use and keep the assertion identical.

- [ ] **Step 2: Run it and confirm it fails**

```bash
cd backend && go test ./internal/platform/ -run TestReconcileWritesTenantRoleEdges -v
```

Expected: FAIL — `IsMember` returns false; the reconciler writes role and perm tuples but no tenant edge.

- [ ] **Step 3: Widen the writer interfaces**

In `backend/internal/platform/module.go`, add to `TupleWriter`:

```go
	// GrantTenantRole wires a role into its tenant so that assignees of
	// the role resolve as members. Written per (tenant, role), not per
	// member: membership for any number of subjects derives through this
	// one edge.
	GrantTenantRole(ctx context.Context, tenantID string, role authz.Role) error
```

In `backend/internal/platform/reconcile.go`, add to `TupleReconciler`:

```go
	// RevokeTenantRole is reconciler-only, alongside DeleteTuple, and for
	// the same reason: a module able to remove a tenant->role edge could
	// strip membership from every holder of that role in one call.
	RevokeTenantRole(ctx context.Context, tenantID string, role authz.Role) error
```

- [ ] **Step 4: Write the tenant edge during grant, and record it as desired**

In `backend/internal/platform/reconcile.go`, add the tuple constructor beside `roleTuple` and `permTuple`:

```go
// tenantRoleTuple mirrors exactly what authz.Client.GrantTenantRole
// writes, for the same lockstep reason as roleTuple and permTuple: a
// drift in relation name or object format would make every tenant edge
// look orphaned and get deleted on the next boot — which would take
// membership, and therefore every request, down with it.
func tenantRoleTuple(tenantID string, role authz.Role) authz.Tuple {
	return authz.Tuple{
		User:     authz.RoleObject(tenantID, role),
		Relation: "granted_role",
		Object:   authz.TenantObject(tenantID),
	}
}
```

Then in `applyGrants`, inside the `if !authz.KnownRole(role) { … }` guard's success path — immediately after the existing `w.GrantRole(...)` call and its `desired[...].add(roleTuple(...))`:

```go
		// The tenant edge is written per (tenant, role) and is what makes
		// membership derivable. Writing it here, keyed off a membership
		// row rather than off the registry, means a tenant only ever gains
		// edges for roles somebody actually holds — and the edge is added
		// to the desired set so prune keeps it.
		if err := w.GrantTenantRole(ctx, m.TenantID, role); err != nil {
			return nil, fmt.Errorf("grant tenant edge for role %s in %s: %w", role, m.TenantID, err)
		}
		desired[m.TenantID].add(tenantRoleTuple(m.TenantID, role))
```

- [ ] **Step 5: Run the test and confirm it passes**

```bash
cd backend && go test ./internal/platform/ -run TestReconcileWritesTenantRoleEdges -v
```

Expected: PASS.

- [ ] **Step 6: Write the failing prune test**

`ReadTuplesByTenant` must now surface `tenant:` tuples so prune can consider them, and prune must delete an edge for a role nobody holds any more — otherwise a retired role's edge lingers forever.

```go
func TestReconcilePrunesTenantEdgesForRolesNobodyHolds(t *testing.T) {
	reg, db, fga, ctx := reconcileHarness(t)

	const (
		tenantID = "55555555-5555-5555-5555-555555555555"
		subject  = "uid-prune"
	)
	seedMembership(t, db, tenantID, subject, string(authz.RoleNurse))
	require.NoError(t, Reconcile(ctx, reg, db, fga))

	// A stale edge for a role no membership row backs.
	require.NoError(t, fga.GrantTenantRole(ctx, tenantID, authz.RoleDoctor))

	require.NoError(t, Reconcile(ctx, reg, db, fga))

	tuples, err := fga.ReadTuplesByTenant(ctx)
	require.NoError(t, err)
	require.NotContains(t, keys(tuples[tenantID]),
		tenantRoleTuple(tenantID, authz.RoleDoctor).Key(),
		"an edge for a role no membership row backs must be pruned")
	require.Contains(t, keys(tuples[tenantID]),
		tenantRoleTuple(tenantID, authz.RoleNurse).Key(),
		"the edge for the role that IS backed must survive")
}
```

Add a small `keys(tuples []authz.Tuple) []string` helper in the test file mapping each tuple to `.Key()`.

- [ ] **Step 7: Run it and confirm it fails**

```bash
cd backend && go test ./internal/platform/ -run TestReconcilePrunesTenantEdges -v
```

Expected: FAIL — either the stale edge survives (prune never saw it) or the backed edge is deleted (prune saw it but `desired` did not record it).

- [ ] **Step 8: Teach `ReadTuplesByTenant` about tenant objects, and prune to delete them**

In `backend/pkg/authz/read.go`, wherever `ReadTuplesByTenant` filters which object types it reads, include `tenantType` alongside `role` and `perm`. Update its doc comment from "every role: and perm: tuple" to "every role:, perm: and tenant: tuple".

Prune itself needs no branching: `TenantOfObject` now parses tenant objects (Task 1), the bucket check still holds, and `DeleteTuple` deletes by user/relation/object regardless of type. Verify by reading `prune` rather than assuming — if `DeleteTuple` special-cases object types, extend it.

- [ ] **Step 9: Run the whole platform package**

```bash
cd backend && go test -race ./internal/platform/...
```

Expected: PASS, including the existing global-empty-read guard tests.

- [ ] **Step 10: Prove both tests can fail**

Comment out the `w.GrantTenantRole` call in `applyGrants`; confirm the Step 1 test fails. Restore.
Comment out the `desired[m.TenantID].add(tenantRoleTuple(...))` line; confirm the Step 6 test fails on the "must survive" assertion — this is the one that catches a desired-set omission silently deleting live membership. Restore.

- [ ] **Step 11: Update `noopWriter` in the test harness**

`backend/internal/testutil/harness.go`'s `noopWriter` must satisfy the widened interface:

```go
func (noopWriter) GrantTenantRole(context.Context, string, authz.Role) error { return nil }
```

- [ ] **Step 12: Commit**

```bash
cd backend && go test -race ./internal/platform/... ./pkg/authz/... && cd .. && make lint-go
git add backend/internal/platform/ backend/pkg/authz/read.go backend/internal/testutil/harness.go
git commit -m "feat: reconcile tenant-role edges so membership is derivable and prunes with the tuples that back it (#781)"
```

---

## Task 3: Membership becomes a first-class check, and `Public` stops meaning "unexamined"

**Files:**
- Create: `backend/pkg/authz/membership.go`, `backend/pkg/authz/membership_test.go`
- Modify: `backend/pkg/authz/authz.go`, `backend/pkg/authz/middleware.go`, `backend/internal/platform/router.go`, `backend/internal/modules/iam/me.go:65-67`, `backend/internal/archtest/arch_test.go`, `backend/internal/testutil/harness.go`

**Interfaces:**
- Consumes: `(*Client).IsMember` from Task 1.
- Produces: `authz.NoTenantMembership` (a `Permission`); `authz.MembershipChecker` interface with `IsMember(ctx context.Context, subject, tenantID string) (bool, error)`; `authz.RequireMembership(m MembershipChecker) gin.HandlerFunc`; `platform.NewRouter(g *gin.RouterGroup, m authz.MembershipChecker) *Router`.

**The harness gets restructured here**, because this task adds a fifth constructor parameter and `ModuleHarness*` is already four near-identical variants. It becomes one options struct. This is targeted improvement to a file the task must modify anyway, not unrelated refactoring.

- [ ] **Step 1: Write the failing regression test for the actual defect**

This is T2 from the spec — the literal test for the bug in #781. Add to `backend/internal/modules/reference/module_test.go` (the `reference` module owns the `Public` routes):

```go
func TestPublicRouteRefusesANonMember(t *testing.T) {
	// The caller authenticates fine and carries a valid tenant_id claim,
	// but holds no role in that tenant — the shape of an ex-employee
	// whose membership was revoked while their token was still live.
	r := harnessWithMembership(t, map[string]bool{"uid-ex-member": false})

	w := testutil.Do(r, http.MethodGet, "/v1/reference/pings", "tok-ex-member", "")

	require.Equal(t, http.StatusForbidden, w.Code,
		"a Public route declares no permission; it must still refuse a non-member")
}

func TestPublicRouteStillServesAMember(t *testing.T) {
	r := harnessWithMembership(t, map[string]bool{"uid-nurse": true})

	w := testutil.Do(r, http.MethodGet, "/v1/reference/pings", "tok-nurse", "")

	require.Equal(t, http.StatusOK, w.Code)
}

func TestMembershipInfrastructureFailureIsFailClosed(t *testing.T) {
	r := harnessWithFailingMembership(t, errors.New("openfga unreachable"))

	w := testutil.Do(r, http.MethodGet, "/v1/reference/pings", "tok-nurse", "")

	require.Equal(t, http.StatusServiceUnavailable, w.Code,
		"a membership check that cannot be answered must deny, never admit")
	require.Contains(t, w.Body.String(), "authz_unavailable")
}
```

- [ ] **Step 2: Run and confirm they fail**

```bash
cd backend && go test ./internal/modules/reference/ -run TestPublicRoute -v
```

Expected: FAIL to compile (`harnessWithMembership` undefined). After the harness exists, the first test fails with `200` — which is the defect.

- [ ] **Step 3: Add the marker and remove the `Has` short-circuit**

In `backend/pkg/authz/authz.go`:

```go
// Public marks a route as declaring no permission requirement. It is an
// explicit, greppable opt-out rather than an omission.
//
// Public does NOT opt out of tenant membership. A route that declares no
// permission still only serves members of the tenant its caller's token
// names — see NoTenantMembership for the narrower opt-out, and #781 for
// the defect that existed while these two were the same thing.
const Public Permission = "public"

// NoTenantMembership marks the handful of routes that must serve a
// caller who is not (or is no longer) a member of the tenant their token
// names: the self-service routes answering "where do I belong?" and
// sign-out. Every one of them gates itself.
//
// Deliberately awkward to type, and pinned by
// TestNoTenantMembershipAllowlist in internal/archtest to an explicit
// list — the safety of every other route depends on this set staying
// small, so growing it must require editing an allowlist a reviewer
// sees.
const NoTenantMembership Permission = "no_tenant_membership"
```

And replace `Has`:

```go
// Has reports whether the set carries p.
//
// It used to answer true unconditionally for Public, which meant an
// unguarded route never consulted the set at all — and therefore served
// a caller whose set was empty because their membership had been
// revoked. Public is now handled by Require, which skips the permission
// check explicitly rather than by asking a set a question it answers
// dishonestly.
func (s PermissionSet) Has(p Permission) bool {
	_, ok := s[p]
	return ok
}
```

- [ ] **Step 4: Make `Require` skip the markers explicitly**

In `backend/pkg/authz/middleware.go`:

```go
// Require denies with 403 unless the resolved set carries p. A caller
// who is not a member of the tenant resolves to an empty set, and is
// additionally refused by RequireMembership before reaching here.
//
// The two markers declare no permission, so there is nothing to check:
// Public still passes through RequireMembership, NoTenantMembership
// does not. Skipping explicitly here — rather than by having Has lie
// about what the set contains — keeps PermissionSet an honest set.
func Require(p Permission) gin.HandlerFunc {
	return func(c *gin.Context) {
		if p == Public || p == NoTenantMembership {
			c.Next()
			return
		}
		set, ok := PermissionsFrom(c)
		if !ok {
			respond.InternalErr(c, errors.New("permission set missing from context"), "authorization not initialized")
			return
		}
		if !set.Has(p) {
			respond.Forbidden(c, "missing permission "+string(p))
			return
		}
		c.Next()
	}
}
```

- [ ] **Step 5: Implement `backend/pkg/authz/membership.go`**

```go
package authz

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/tesserix/hms/internal/platform/respond"
	"github.com/tesserix/hms/pkg/authn"
)

// MembershipChecker answers whether a subject belongs to a tenant.
// *Client implements it; tests substitute fakes.
type MembershipChecker interface {
	IsMember(ctx context.Context, subject, tenantID string) (bool, error)
}

// RequireMembership refuses a caller who holds no role in the tenant
// their token names.
//
// It runs per route rather than once per request group, because whether
// membership is required is a property of the route's declared marker —
// and because resolving it lazily means a NoTenantMembership route
// never makes the call at all. That matters: sign-out is a
// NoTenantMembership route, and it must keep working during an OpenFGA
// outage, which it would not if membership were resolved for every
// request up front.
//
// 403, not 404: the caller is authenticated and the route exists. 404 is
// this codebase's answer for a cross-tenant *resource*, which is a
// different question from a cross-tenant *caller*.
func RequireMembership(m MembershipChecker) gin.HandlerFunc {
	return func(c *gin.Context) {
		p, ok := authn.PrincipalFrom(c)
		if !ok {
			respond.Unauthenticated(c, "missing principal")
			return
		}
		member, err := m.IsMember(c.Request.Context(), p.Subject, p.TenantID)
		if err != nil {
			slog.ErrorContext(c.Request.Context(), "membership check failed",
				"err", err, "subject", p.Subject, "tenant_id", p.TenantID)
			respond.Error(c, http.StatusServiceUnavailable,
				"authz_unavailable", "authorization is temporarily unavailable")
			return
		}
		if !member {
			respond.Forbidden(c, "not a member of this tenant")
			return
		}
		c.Next()
	}
}
```

- [ ] **Step 6: Wire it into the router**

In `backend/internal/platform/router.go`:

```go
type Router struct {
	group      *gin.RouterGroup
	declared   *[]authz.Permission
	membership authz.MembershipChecker
}

// NewRouter builds the root router. membership is required, not
// optional: a nil checker would mean every route silently skips the
// membership gate, which is the defect this parameter exists to close.
func NewRouter(g *gin.RouterGroup, membership authz.MembershipChecker) *Router {
	if membership == nil {
		panic("platform: NewRouter requires a MembershipChecker")
	}
	return &Router{group: g, declared: &[]authz.Permission{}, membership: membership}
}

func (r *Router) Group(prefix string) *Router {
	return &Router{group: r.group.Group(prefix), declared: r.declared, membership: r.membership}
}

func (r *Router) handle(method, path string, perm authz.Permission, h []gin.HandlerFunc) {
	*r.declared = append(*r.declared, perm)
	chain := make([]gin.HandlerFunc, 0, len(h)+2)
	// Membership first: a non-member gets the same answer on every route
	// regardless of what permission it declares, and a route that does
	// not need the check never pays for the call.
	if perm != authz.NoTenantMembership {
		chain = append(chain, authz.RequireMembership(r.membership))
	}
	chain = append(chain, authz.Require(perm))
	chain = append(chain, h...)
	r.group.Handle(method, path, chain...)
}
```

- [ ] **Step 7: Move the self-service routes to the new marker**

`backend/internal/modules/iam/me.go:65-67`:

```go
	// NoTenantMembership, not Public: a caller whose membership in the
	// tenant their token names has just been revoked must still be able
	// to discover the other tenants they belong to and switch to one.
	// Requiring membership here would lock them out of the endpoint whose
	// whole job is answering that question. Each of these gates itself —
	// switchTenant checks membership before minting, and permissions
	// reports only what the caller actually resolved to.
	g.GET("/me/permissions", authz.NoTenantMembership, permissions)
	g.GET("/me/tenants", authz.NoTenantMembership, me.tenants)
	g.POST("/me/tenant", authz.NoTenantMembership, me.switchTenant)
```

Leave `reference`'s three routes on `authz.Public` — they now correctly require membership.

- [ ] **Step 8: Collapse the harness variants into an options struct**

`backend/internal/testutil/harness.go` has four `ModuleHarness*` constructors differing only in which stub they pass. This task adds a fifth dependency and Task 4 adds a sixth. Replace them with:

```go
// HarnessOptions are the substitutable dependencies of a module
// harness. The zero value is valid: every field falls back to a stub
// that is inert but not permissive — an unset MembershipChecker reports
// every caller a member, because a test that has not opted into
// membership semantics should exercise the route it is actually about.
type HarnessOptions struct {
	Tokens     map[string]string
	Perms      map[string][]authz.Permission
	Writer     platform.TupleWriter
	Roles      platform.RoleLister
	Minter     authn.TokenMinter
	Membership authz.MembershipChecker
	Revocation authn.RevocationChecker // wired in Task 4
	Modules    []platform.Module
}

// NewHarness builds a router whose middleware chain matches
// cmd/api/main.go exactly, in the same order.
func NewHarness(t *testing.T, opts HarnessOptions) (*gin.Engine, *tenantdb.DB, *events.Bus, context.Context)
```

Update every existing caller. Keep the middleware-order comment from the current `moduleHarness` verbatim — it documents a real regression — and extend it to name the membership and revocation additions.

Add the two helpers the Step 1 tests use:

```go
// harnessWithMembership builds a reference-module harness whose
// membership answers come from a fixed map keyed by subject.
func harnessWithMembership(t *testing.T, members map[string]bool) *gin.Engine

// harnessWithFailingMembership builds one whose membership check always
// errors, for the fail-closed assertion.
func harnessWithFailingMembership(t *testing.T, err error) *gin.Engine
```

- [ ] **Step 9: Run the reference tests**

```bash
cd backend && go test ./internal/modules/reference/ -run TestPublicRoute -v
cd backend && go test ./internal/modules/reference/ -run TestMembershipInfrastructureFailure -v
```

Expected: PASS on all three.

- [ ] **Step 10: Write the lockout-edge test**

This is spec T4. Add to `backend/internal/modules/iam/me_test.go`:

```go
func TestSelfServiceRoutesServeACallerWithNoMembership(t *testing.T) {
	// Membership is false everywhere: the caller has just been removed
	// from the tenant their token names.
	r := newIAMHarness(t, testutil.HarnessOptions{
		Membership: fixedMembership(false),
		Roles:      rolesIn("66666666-6666-6666-6666-666666666666", authz.RoleNurse),
	})

	for _, path := range []string{"/v1/iam/me/permissions", "/v1/iam/me/tenants"} {
		w := testutil.Do(r, http.MethodGet, path, "tok-nurse", "")
		require.Equal(t, http.StatusOK, w.Code,
			"%s must answer a caller with no membership — it is the endpoint that tells them where they do belong", path)
	}
}
```

- [ ] **Step 11: Run it, then prove it can fail**

```bash
cd backend && go test ./internal/modules/iam/ -run TestSelfServiceRoutesServe -v
```

Expected: PASS. Then temporarily change one of the three routes from `NoTenantMembership` back to `Public` and confirm this test fails with 403. Restore.

- [ ] **Step 11a: Write the cross-tenant isolation test (spec T3)**

Losing membership in one hospital must not end a session in another. This is what proves membership stayed a tenant-scoped check rather than collapsing into the subject-scoped watermark.

```go
func TestMembershipRevokedInOneTenantLeavesTheOtherWorking(t *testing.T) {
	const (
		tenantA = "77777777-7777-7777-7777-777777777777"
		tenantB = "88888888-8888-8888-8888-888888888888"
	)
	// The same subject, two tokens, one per tenant. Membership survives
	// in B only.
	r := harnessWithTenantMembership(t, map[string]bool{
		"uid-locum@" + tenantA: false,
		"uid-locum@" + tenantB: true,
	})

	require.Equal(t, http.StatusForbidden,
		testutil.Do(r, http.MethodGet, "/v1/reference/pings", "tok-locum-a", "").Code,
		"membership was revoked in tenant A")

	require.Equal(t, http.StatusOK,
		testutil.Do(r, http.MethodGet, "/v1/reference/pings", "tok-locum-b", "").Code,
		"the same person is still employed at tenant B; revoking A must not touch B")
}
```

`harnessWithTenantMembership` keys its fixture on `subject@tenant` so the fake `IsMember` can answer differently per tenant — the whole point of the test.

- [ ] **Step 11b: Run it and prove it can fail**

```bash
cd backend && go test ./internal/modules/reference/ -run TestMembershipRevokedInOneTenant -v
```

Expected: PASS. Then change `RequireMembership` to ignore `p.TenantID` and consult only the subject — the collapse this test exists to catch — and confirm the second assertion fails. Restore.

- [ ] **Step 12: Add the arch test pinning the allowlist**

In `backend/internal/archtest/arch_test.go`:

```go
// noTenantMembershipAllowlist is every route permitted to skip the
// tenant-membership check. Adding an entry is a security decision:
// such a route serves a caller who is not a member of the tenant their
// token names, so it must gate itself.
var noTenantMembershipAllowlist = map[string]string{
	"GET /iam/me/permissions": "reports what the caller resolved to; reveals nothing they did not already hold",
	"GET /iam/me/tenants":     "answers 'where do I belong'; unusable if it required belonging",
	"POST /iam/me/tenant":     "gates on membership itself before minting (iam/me.go switchTenant)",
	"POST /iam/me/sign-out":   "a revoked member must still be able to end their session",
}

func TestNoTenantMembershipAllowlist(t *testing.T) {
	found := routesDeclaring(t, authz.NoTenantMembership)
	require.ElementsMatch(t, keysOf(noTenantMembershipAllowlist), found,
		"a route skipping the membership check must be added to noTenantMembershipAllowlist with a reason")
}
```

`routesDeclaring` walks the registered modules through a router that records `(method, path, permission)`. If `Router.Declared()` only returns permissions and not paths, extend it to return a `[]DeclaredRoute{Method, Path, Permission}` — the arch test and the adversarial matrix suite both already build on `Declared()`, so extend rather than duplicate.

- [ ] **Step 13: Run it and prove it can fail**

```bash
cd backend && go test ./internal/archtest/ -run TestNoTenantMembershipAllowlist -v
```

Expected: PASS (the sign-out entry will fail until Task 6 — if so, add it to the allowlist in Task 6 instead and note that here). Then temporarily switch a `reference` route to `NoTenantMembership` and confirm the test fails. Restore.

- [ ] **Step 14: Full backend suite**

```bash
cd backend && go test -race ./... && cd .. && make lint-go
```

Expected: green. Many call sites of `NewRouter` and the harness changed; fix every one.

- [ ] **Step 15: Commit**

```bash
git add backend/pkg/authz/ backend/internal/platform/router.go backend/internal/modules/ backend/internal/archtest/ backend/internal/testutil/
git commit -m "fix: require tenant membership on every route so authz.Public no longer serves a revoked member (#781)"
```

---

## Task 4: The revocation registry, checker and cache

**Files:**
- Create: `backend/pkg/authn/revocation.go`, `backend/internal/modules/iam/revocation.go`, `backend/internal/modules/iam/revocation_test.go`
- Modify: `backend/pkg/authn/authn.go`, `backend/pkg/authn/gip.go`, `backend/internal/modules/iam/module.go` (migration `0003_iam`), `backend/pkg/tenantdb/db.go` (`LintRLS` allowlist)

**Interfaces:**
- Produces: `authn.RevocationChecker` interface with `RevokedAfter(ctx context.Context, subject string) (time.Time, error)`; `authn.Principal` gains `AuthTime time.Time`; `iam.NewRevocationChecker(db *tenantdb.DB) *RevocationChecker`.
- Consumes: nothing from Tasks 1–3.

- [ ] **Step 1: Carry `auth_time` on the Principal**

The watermark cannot be compared without it. In `backend/pkg/authn/authn.go`:

```go
type Principal struct {
	Subject  string `json:"subject"`
	TenantID string `json:"tenant_id"`
	// AuthTime is when the user actually authenticated, not when this
	// token was issued. A token refresh mints a new token with a fresh
	// iat but carries the ORIGINAL auth_time, so this is the only claim
	// a revocation watermark can be compared against: comparing iat
	// would let any client holding a live refresh token walk through the
	// watermark simply by refreshing.
	AuthTime time.Time `json:"-"`
}
```

In `backend/pkg/authn/gip.go`, `principalFromToken`:

```go
	// A token with no auth_time cannot be evaluated against a revocation
	// watermark, and a credential that cannot be evaluated is not one
	// that can be trusted.
	if tok.AuthTime == 0 {
		return Principal{}, ErrNoAuthTime
	}
	return Principal{
		Subject:  tok.UID,
		TenantID: tenantID.String(),
		AuthTime: time.Unix(tok.AuthTime, 0).UTC(),
	}, nil
```

Add `var ErrNoAuthTime = errors.New("authn: token has no auth_time claim")`.

- [ ] **Step 2: Write the failing test — THE test (spec T1)**

Create `backend/pkg/authn/revocation_test.go`:

```go
// TestRefreshedTokenIsStillRevoked is the assertion this whole feature
// rests on. A refreshed token has a NEW iat and the ORIGINAL auth_time.
// If the comparison is ever "simplified" to iat, revocation silently
// degrades into a suggestion any client can ignore by refreshing — and
// this test is what fails.
func TestRefreshedTokenIsStillRevoked(t *testing.T) {
	signIn := time.Now().Add(-2 * time.Hour)
	revokedAt := time.Now().Add(-1 * time.Hour)
	refreshedAt := time.Now() // the new iat: AFTER the revocation

	checker := fixedRevocation{"uid-nurse": revokedAt}
	mw := Middleware(staticVerifier{
		"tok": Principal{Subject: "uid-nurse", TenantID: testTenant, AuthTime: signIn},
	}, checker)

	w := runWithMiddleware(t, mw, "tok")

	require.Equal(t, http.StatusUnauthorized, w.Code,
		"a refreshed token (iat %s) whose auth_time (%s) predates the watermark (%s) must be refused",
		refreshedAt, signIn, revokedAt)
}

func TestTokenIssuedAfterRevocationIsAccepted(t *testing.T) {
	revokedAt := time.Now().Add(-1 * time.Hour)
	signIn := time.Now() // signed in again after the revocation

	mw := Middleware(staticVerifier{
		"tok": Principal{Subject: "uid-nurse", TenantID: testTenant, AuthTime: signIn},
	}, fixedRevocation{"uid-nurse": revokedAt})

	require.Equal(t, http.StatusOK, runWithMiddleware(t, mw, "tok").Code,
		"revocation must not lock a user out permanently; signing in again works")
}

func TestRevocationLookupFailureIsFailClosed(t *testing.T) {
	mw := Middleware(staticVerifier{
		"tok": Principal{Subject: "uid-nurse", TenantID: testTenant, AuthTime: time.Now()},
	}, failingRevocation{errors.New("postgres unreachable")})

	w := runWithMiddleware(t, mw, "tok")

	require.Equal(t, http.StatusServiceUnavailable, w.Code,
		"a revocation state that cannot be read must deny, never admit")
	require.Contains(t, w.Body.String(), "authz_unavailable")
}
```

- [ ] **Step 3: Run and confirm failure**

```bash
cd backend && go test ./pkg/authn/ -run TestRefreshedTokenIsStillRevoked -v
```

Expected: FAIL to compile — `Middleware` takes one argument.

- [ ] **Step 4: Implement the interface and wire it into `Middleware`**

Create `backend/pkg/authn/revocation.go`:

```go
package authn

import (
	"context"
	"time"
)

// RevocationChecker reports the instant before which every credential
// for a subject is refused — the subject's revocation watermark.
//
// A zero time means "never revoked". Implementations MUST return an
// error rather than the zero time when they cannot answer: the two are
// indistinguishable to the caller, and only one of them should admit a
// request.
//
// Implemented by the iam module, which owns the table. Deliberately
// declared here and implemented there: pkg/ must not depend on
// internal/, and this package has no business knowing about Postgres.
type RevocationChecker interface {
	RevokedAfter(ctx context.Context, subject string) (time.Time, error)
}
```

Then in `authn.go`, after the existing `v.Verify` succeeds:

```go
		watermark, err := rev.RevokedAfter(c.Request.Context(), p.Subject)
		if err != nil {
			slog.ErrorContext(c.Request.Context(), "revocation lookup failed",
				"err", err, "subject", p.Subject)
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{
				"error": "authz_unavailable", "message": "authorization is temporarily unavailable"})
			return
		}
		// Not-after, deliberately: a token whose auth_time equals the
		// watermark to the second is refused. Second granularity means a
		// sign-in racing a revocation is ambiguous, and the safe reading
		// of an ambiguous credential is that it is revoked.
		if !watermark.IsZero() && !p.AuthTime.After(watermark) {
			slog.InfoContext(c.Request.Context(), "refused a revoked credential",
				"subject", p.Subject, "auth_time", p.AuthTime, "revoked_at", watermark,
				"path", c.Request.URL.Path)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error": "unauthenticated", "message": "credential revoked"})
			return
		}
```

- [ ] **Step 5: Run and confirm the three tests pass**

```bash
cd backend && go test -race ./pkg/authn/...
```

- [ ] **Step 6: Prove T1 can fail — do not skip this**

Change the comparison to use an `IssuedAt` field instead of `AuthTime` (add one to the test Principal if needed) and confirm `TestRefreshedTokenIsStillRevoked` fails while every other test still passes. Restore.

This is the single most important verification in the plan: it proves the test actually discriminates between the correct and the plausible-but-wrong implementation.

- [ ] **Step 7: Add migration `0003_iam`**

In `backend/internal/modules/iam/module.go`, append to `Migrations()`:

```go
	}, {
		ID: "0003_iam",
		// Not tenant-scoped, and therefore an explicit LintRLS allowlist
		// entry rather than a table that quietly has no tenant_id: a GIP
		// subject is global, so a revocation is global. It holds no
		// tenant data and no PHI.
		//
		// The watermark only ever moves forward (see the GREATEST upsert
		// in revocation.go): a retried or late write must never be able
		// to resurrect a revoked credential.
		SQL: `
			CREATE TABLE iam_credential_revocations (
			  subject    text PRIMARY KEY,
			  revoked_at timestamptz NOT NULL,
			  reason     text NOT NULL CHECK (reason IN ('sign_out','admin_revoke')),
			  actor      text NOT NULL,
			  updated_at timestamptz NOT NULL DEFAULT now()
			);`,
	}}
```

In `backend/pkg/tenantdb/db.go`, add to the `LintRLS` allowlist with the reason **at the entry**:

```go
	// A GIP subject is global, not tenant-scoped, so a revocation
	// watermark cannot carry a tenant_id and cannot be RLS-policied.
	// The table holds no tenant data and no PHI: subject, timestamp,
	// reason, actor.
	"iam_credential_revocations",
```

- [ ] **Step 8: Write the failing tests for the checker and cache**

Create `backend/internal/modules/iam/revocation_test.go`:

```go
// revokeNow is the test's wrapper around RevokeTx, which takes a tx
// because production callers publish the invalidation in the same
// transaction. Tests that only care about the watermark open their own.
func revokeNow(t *testing.T, db *tenantdb.DB, c *RevocationChecker, subject string, at time.Time, reason, actor string) {
	t.Helper()
	require.NoError(t, db.WithSystem(context.Background(), func(tx *gorm.DB) error {
		return c.RevokeTx(tx, subject, at, reason, actor)
	}))
}

func TestWatermarkOnlyMovesForward(t *testing.T) {
	db, ctx := revocationHarness(t)
	c := NewRevocationChecker(db)

	late := time.Now().Add(-time.Hour)
	current := time.Now()

	revokeNow(t, db, c, "uid-nurse", current, "sign_out", "uid-nurse")
	revokeNow(t, db, c, "uid-nurse", late, "sign_out", "uid-nurse")
	c.Invalidate("uid-nurse") // drop the cached read so the assertion sees Postgres

	got, err := c.RevokedAfter(ctx, "uid-nurse")
	require.NoError(t, err)
	require.WithinDuration(t, current, got, time.Second,
		"a late or replayed revoke must not move the watermark backwards and resurrect a credential")
}

func TestUnknownSubjectIsNotRevoked(t *testing.T) {
	db, ctx := revocationHarness(t)
	c := NewRevocationChecker(db)

	got, err := c.RevokedAfter(ctx, "uid-never-revoked")
	require.NoError(t, err)
	require.True(t, got.IsZero(), "a subject with no row has never been revoked")
}

func TestCacheServesRepeatLookupsWithoutQuerying(t *testing.T) {
	db, ctx := revocationHarness(t)
	c := NewRevocationChecker(db)

	_, err := c.RevokedAfter(ctx, "uid-nurse")
	require.NoError(t, err)
	before := c.QueriesForTest()
	for range 100 {
		_, err := c.RevokedAfter(ctx, "uid-nurse")
		require.NoError(t, err)
	}
	require.Equal(t, before, c.QueriesForTest(),
		"the negative case is the hot path and must not reach Postgres on every request")
}

func TestInvalidateForcesAReread(t *testing.T) {
	db, ctx := revocationHarness(t)
	c := NewRevocationChecker(db)

	_, err := c.RevokedAfter(ctx, "uid-nurse")
	require.NoError(t, err)

	// Write directly, bypassing this checker's own Invalidate, exactly
	// as another replica would.
	other := NewRevocationChecker(db)
	revokeNow(t, db, other, "uid-nurse", time.Now(), "sign_out", "uid-nurse")

	got, err := c.RevokedAfter(ctx, "uid-nurse")
	require.NoError(t, err)
	require.True(t, got.IsZero(), "precondition: the stale negative entry is still cached")

	c.Invalidate("uid-nurse")

	got, err = c.RevokedAfter(ctx, "uid-nurse")
	require.NoError(t, err)
	require.False(t, got.IsZero(), "invalidation must force a re-read")
}
```

- [ ] **Step 9: Run and confirm failure**

```bash
cd backend && go test ./internal/modules/iam/ -run TestWatermark -v
```

Expected: FAIL to compile.

- [ ] **Step 10: Implement `backend/internal/modules/iam/revocation.go`**

```go
package iam

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"gorm.io/gorm"

	"github.com/tesserix/hms/pkg/tenantdb"
)

const (
	// maxRevocationCacheEntries bounds memory at roughly a megabyte. A
	// single hospital's concurrent staff is two orders of magnitude
	// below this, so eviction should effectively never happen; when it
	// does, the evicted entry reads through and is correct, only slower.
	maxRevocationCacheEntries = 10_000

	// revocationCacheTTL is the BACKSTOP for a missed broadcast, not the
	// propagation mechanism — invalidation is (see broadcast.go). Five
	// minutes bounds the abnormal case to something an incident
	// responder can accept while keeping steady-state traffic off
	// Postgres.
	//
	// Both constants are code, not configuration, on purpose: a
	// deployment that tuned the TTL upward would silently widen a
	// security window, so changing it should require a reviewer.
	revocationCacheTTL = 5 * time.Minute
)

type revocationRow struct {
	Subject   string    `gorm:"primaryKey"`
	RevokedAt time.Time
	Reason    string
	Actor     string
	UpdatedAt time.Time
}

func (revocationRow) TableName() string { return "iam_credential_revocations" }

type cacheEntry struct {
	watermark time.Time // zero == never revoked
	readAt    time.Time
}

// RevocationChecker answers "has this subject's credential been
// revoked, and when" for the authentication path, and is the only
// writer of the watermark.
//
// The cache holds NEGATIVE entries too — "not revoked" is the
// overwhelmingly common answer and the one that must not reach
// Postgres on every request.
type RevocationChecker struct {
	db *tenantdb.DB

	mu      sync.RWMutex
	entries map[string]cacheEntry
	order   []string // insertion order, for bounded eviction

	queries atomic.Int64 // observability + the cache test's assertion
}

func NewRevocationChecker(db *tenantdb.DB) *RevocationChecker {
	return &RevocationChecker{db: db, entries: make(map[string]cacheEntry, 128)}
}

// RevokedAfter returns subject's watermark, or the zero time if it has
// never been revoked. It returns an error rather than the zero time
// when the answer cannot be read: the caller must be able to tell
// "definitely not revoked" from "cannot say".
func (r *RevocationChecker) RevokedAfter(ctx context.Context, subject string) (time.Time, error) {
	if e, ok := r.lookup(subject); ok {
		return e.watermark, nil
	}
	var row revocationRow
	r.queries.Add(1)
	// WithSystem, not WithTenant: this table is not tenant-scoped and
	// the authentication path has no tenant context yet — the tenant
	// claim has been read but nothing has authorized the caller to act
	// in it.
	err := r.db.WithSystem(ctx, func(tx *gorm.DB) error {
		return tx.Where("subject = ?", subject).First(&row).Error
	})
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		r.store(subject, time.Time{})
		return time.Time{}, nil
	case err != nil:
		return time.Time{}, fmt.Errorf("read revocation watermark for %s: %w", subject, err)
	}
	r.store(subject, row.RevokedAt.UTC())
	return row.RevokedAt.UTC(), nil
}

// Revoke moves subject's watermark to at, never backwards.
//
// It takes a tx so the caller can publish the invalidation event
// through the outbox in the same transaction: a watermark that
// committed without its event would propagate only at TTL, and an event
// published without its watermark would be a lie.
func (r *RevocationChecker) RevokeTx(tx *gorm.DB, subject string, at time.Time, reason, actor string) error {
	return tx.Exec(`
		INSERT INTO iam_credential_revocations (subject, revoked_at, reason, actor, updated_at)
		VALUES (?, ?, ?, ?, now())
		ON CONFLICT (subject) DO UPDATE SET
		  revoked_at = GREATEST(iam_credential_revocations.revoked_at, EXCLUDED.revoked_at),
		  reason     = EXCLUDED.reason,
		  actor      = EXCLUDED.actor,
		  updated_at = now()`,
		subject, at.UTC(), reason, actor).Error
}

// Invalidate drops subject's cached entry, forcing the next lookup to
// re-read. Called by the broadcast consumer on every replica.
func (r *RevocationChecker) Invalidate(subject string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, subject)
}

func (r *RevocationChecker) lookup(subject string) (cacheEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[subject]
	if !ok || time.Since(e.readAt) > revocationCacheTTL {
		return cacheEntry{}, false
	}
	return e, true
}

func (r *RevocationChecker) store(subject string, watermark time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.entries[subject]; !exists {
		if len(r.order) >= maxRevocationCacheEntries {
			delete(r.entries, r.order[0])
			r.order = r.order[1:]
		}
		r.order = append(r.order, subject)
	}
	r.entries[subject] = cacheEntry{watermark: watermark, readAt: time.Now()}
}

// QueriesForTest reports how many times the cache missed and read
// Postgres. Exported for the cache test, which cannot otherwise
// distinguish a cache hit from a fast query.
func (r *RevocationChecker) QueriesForTest() int64 { return r.queries.Load() }
```

If `tenantdb` has no `WithSystem`, use whatever non-tenant accessor exists; do **not** use `WithAdmin`, which is arch-test restricted to boot/ops. If no suitable accessor exists, add a narrow one and extend the arch test's allowlist with a reason.

- [ ] **Step 11: Run, then prove the monotonicity test can fail**

```bash
cd backend && go test -race ./internal/modules/iam/ -run 'TestWatermark|TestUnknownSubject|TestCache|TestInvalidate' -v
```

Expected: PASS. Then replace `GREATEST(...)` with `EXCLUDED.revoked_at` and confirm `TestWatermarkOnlyMovesForward` fails. Restore.

- [ ] **Step 11a: Write the concurrency test (spec T11)**

The cache is a mutable map read on every request and written by both the read path and the broadcast handler. `-race` needs something real to examine.

```go
func TestCacheIsRaceFreeUnderConcurrentReadsAndInvalidations(t *testing.T) {
	db, ctx := revocationHarness(t)
	c := NewRevocationChecker(db)

	subjects := []string{"uid-a", "uid-b", "uid-c", "uid-d"}
	var wg sync.WaitGroup

	for i := range 50 {
		wg.Add(3)
		s := subjects[i%len(subjects)]
		go func() { defer wg.Done(); _, _ = c.RevokedAfter(ctx, s) }()
		go func() { defer wg.Done(); c.Invalidate(s) }()
		go func() {
			defer wg.Done()
			revokeNow(t, db, c, s, time.Now(), "sign_out", s)
		}()
	}
	wg.Wait()

	// Eviction must also be exercised: the bounded-order slice is
	// mutated on every insert of an unseen subject.
	for i := range maxRevocationCacheEntries + 100 {
		_, err := c.RevokedAfter(ctx, fmt.Sprintf("uid-filler-%d", i))
		require.NoError(t, err)
	}
	require.LessOrEqual(t, c.EntriesForTest(), maxRevocationCacheEntries,
		"the cache must stay bounded; an unbounded map keyed by subject is a memory leak")
}
```

Add `EntriesForTest() int` beside `QueriesForTest()`, reading under the read lock.

- [ ] **Step 11b: Run it under the race detector**

```bash
cd backend && go test -race ./internal/modules/iam/ -run TestCacheIsRaceFree -v
```

Expected: PASS with no race report. Then temporarily drop the `r.mu.Lock()` from `store` and confirm `-race` reports a data race. Restore.

- [ ] **Step 12: Commit**

```bash
cd backend && go test -race ./... && cd .. && make lint-go
git add backend/pkg/authn/ backend/internal/modules/iam/ backend/pkg/tenantdb/db.go
git commit -m "feat: refuse credentials whose auth_time predates the subject's revocation watermark (#781)"
```

---

## Task 5: Broadcast subscriptions in `pkg/events`

**Files:**
- Create: `backend/pkg/events/broadcast.go`, `backend/pkg/events/broadcast_test.go`
- Modify: `docs/standards/backend.md`

**Interfaces:**
- Produces: `events.Broadcast{Subject string, Handle func(ctx context.Context, evt Event)}`; `(*Bus).StartBroadcasts(ctx context.Context, bs []Broadcast) error`.

**Why a new mechanism rather than a `Consumer`.** `Consumer` is a durable pull subscription with a shared name — replicas *compete*, and exactly one handles each message. That is right for work and wrong for cache invalidation, where **every** replica must hear. A broadcast is also not idempotency-claimed: dropping a duplicate invalidation would be harmless, and claiming one in Postgres would mean the second replica skips the invalidation it needs.

- [ ] **Step 1: Write the failing fanout test**

```go
func TestBroadcastReachesEverySubscriber(t *testing.T) {
	bus := newTestBus(t) // existing helper
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	const subscribers = 3
	received := make(chan string, subscribers)
	for range subscribers {
		require.NoError(t, bus.StartBroadcasts(ctx, []Broadcast{{
			Subject: "hms.in.iam.credential_revoked.v1",
			Handle:  func(_ context.Context, evt Event) { received <- string(evt.Data) },
		}}))
	}

	publishDirectly(t, bus, "hms.in.iam.credential_revoked.v1", Event{
		Type: "CredentialRevoked", Version: 1, Data: json.RawMessage(`{"subject":"uid-nurse"}`),
	})

	for i := range subscribers {
		select {
		case <-received:
		case <-time.After(10 * time.Second):
			t.Fatalf("only %d of %d subscribers received the broadcast; a durable consumer would give exactly 1", i, subscribers)
		}
	}
}
```

The failure message names the exact wrong behaviour — a competing durable consumer delivers to one subscriber — so a regression is self-diagnosing.

- [ ] **Step 2: Run and confirm it fails**

```bash
cd backend && go test ./pkg/events/ -run TestBroadcastReachesEverySubscriber -v
```

Expected: FAIL to compile.

- [ ] **Step 3: Implement `backend/pkg/events/broadcast.go`**

```go
package events

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/nats-io/nats.go"
)

// Broadcast is a fanout subscription: EVERY replica running one
// receives every message, unlike Consumer, where replicas sharing a
// durable name compete and exactly one wins.
//
// Handle returns nothing and gets no transaction. A broadcast is a hint
// — the durable truth is in Postgres — so there is nothing to ack
// meaningfully, nothing to claim for idempotency, and nothing a handler
// could usefully fail at. A dropped broadcast degrades to the
// consumer's own read-through and TTL, never to incorrectness.
type Broadcast struct {
	Subject string
	Handle  func(ctx context.Context, evt Event)
}

// StartBroadcasts subscribes to each broadcast with an ephemeral,
// unnamed JetStream consumer delivering only new messages.
//
// Ephemeral and unnamed is the point: a durable name would be shared
// across replicas (competing delivery) or would have to be made unique
// per replica and then leak a consumer per pod restart. An ephemeral
// consumer vanishes with the connection.
//
// DeliverNew, not DeliverAll: a replica starting up has an empty cache
// and reads through to Postgres for everything, so replaying historical
// invalidations would be pure noise.
func (b *Bus) StartBroadcasts(ctx context.Context, bs []Broadcast) error {
	for _, bc := range bs {
		bc := bc
		sub, err := b.js.Subscribe(b.Subject(bc.Subject), func(msg *nats.Msg) {
			var evt Event
			if err := json.Unmarshal(msg.Data, &evt); err != nil {
				slog.ErrorContext(ctx, "broadcast: undecodable message", "err", err, "subject", bc.Subject)
				return
			}
			// A panicking handler must not take the NATS callback
			// goroutine — and therefore the whole subscription — down
			// with it, exactly as the consumer path already guards.
			defer func() {
				if rec := recover(); rec != nil {
					slog.ErrorContext(ctx, "broadcast handler panicked", "panic", rec, "subject", bc.Subject)
				}
			}()
			bc.Handle(ctx, evt)
		}, nats.DeliverNew())
		if err != nil {
			return fmt.Errorf("broadcast subscribe %s: %w", bc.Subject, err)
		}
		go func() {
			<-ctx.Done()
			_ = sub.Unsubscribe()
		}()
	}
	return nil
}
```

- [ ] **Step 4: Run, then prove it can fail**

```bash
cd backend && go test -race ./pkg/events/ -run TestBroadcastReachesEverySubscriber -v
```

Expected: PASS. Then change `b.js.Subscribe(...)` to a durable `PullSubscribe` with a fixed name and confirm the test fails at "only 1 of 3 subscribers". Restore.

- [ ] **Step 5: Record the naming deviation in the standard**

`docs/standards/backend.md`, in the events section, after the consumer-naming rule:

```markdown
**Broadcast subscriptions are the one exception to consumer naming.** The
`<module>-<purpose>` rule assumes a work queue, where replicas sharing a
durable name compete and exactly one handles each message. A broadcast
(`events.Broadcast`, `Bus.StartBroadcasts`) is the opposite: every replica
must receive every message, so it uses an ephemeral, unnamed JetStream
consumer with `DeliverNew`. Use it only for state whose durable truth
lives elsewhere — the credential-revocation cache invalidation (#781) is
the model: Postgres holds the watermark, the broadcast only says "re-read
it". A broadcast handler gets no transaction and no idempotency claim,
because a dropped or duplicated hint must be harmless by construction.
```

- [ ] **Step 6: Commit**

```bash
cd backend && go test -race ./pkg/events/... && cd .. && make lint-go
git add backend/pkg/events/ docs/standards/backend.md
git commit -m "feat: add ephemeral broadcast subscriptions so every replica hears cache invalidations (#781)"
```

---

## Task 6: Sign-out, admin revoke, and full wiring

**Files:**
- Create: `backend/internal/modules/iam/signout.go`, `backend/internal/modules/iam/signout_test.go`
- Modify: `backend/internal/modules/iam/module.go`, `backend/internal/platform/module.go` (`Deps`), `backend/cmd/api/main.go`, `backend/internal/archtest/arch_test.go`

**Interfaces:**
- Consumes: `iam.NewRevocationChecker` (Task 4), `events.Broadcast` (Task 5), `authz.NoTenantMembership` (Task 3).
- Produces: `iam.SubjectCredentialRevoked = "hms.in.iam.credential_revoked.v1"`; `iam.CredentialRevokedData{Subject string}`; `iam.PermCredentialRevoke authz.Permission = "iam.credential.revoke"`.
- **`iam.New` changes signature** from `New() *Module` to `New(checker *RevocationChecker) *Module`, storing it as `m.checker`. Every caller changes: `bootstrap.NewRegistry()`, and every `iam` test. This is deliberate — the module and the authentication middleware must share **one** checker instance, because the middleware reads the cache the module's broadcast handler invalidates. Two instances compile, pass most tests, and silently never invalidate the cache the request path actually consults.
- `platform.Deps` deliberately does **not** gain the checker: `platform` must not import a module. It gains `TokenRevoker authn.TokenRevoker` only; the checker reaches `iam` through its constructor.
- `platform.Module` gains `Broadcasts(deps Deps) []events.Broadcast`. Every other module returns nil — add the method to each, in the same file as its `Consumers`, so the interface stays satisfied.

- [ ] **Step 1: Add the GIP revoke capability**

`pkg/authn` gains, beside `TokenMinter`:

```go
// TokenRevoker revokes a subject's refresh tokens at the identity
// provider, so GIP agrees with the HMS watermark instead of quietly
// disagreeing. One method wide, for the same reason TokenMinter is.
type TokenRevoker interface {
	RevokeRefreshTokens(ctx context.Context, uid string) error
}
```

with a `gipRevoker` implementation in `gip.go` forwarding to `client.RevokeRefreshTokens`, constructed by `NewGIPRevoker` following `NewGIPMinter` exactly — including the emulator refusal.

- [ ] **Step 2: Write the failing handler tests**

```go
func TestSignOutWritesTheWatermarkAndRevokesAtGIP(t *testing.T) {
	r, db, revoker := signoutHarness(t)

	w := testutil.Do(r, http.MethodPost, "/v1/iam/me/sign-out", "tok-nurse", "")
	require.Equal(t, http.StatusOK, w.Code)

	require.False(t, watermarkFor(t, db, "uid-nurse").IsZero(),
		"sign-out must write the watermark, not merely clear a cookie")
	require.Equal(t, []string{"uid-nurse"}, revoker.Calls(),
		"GIP must be told, so the identity provider does not disagree with us")
}

func TestSignOutWorksForARevokedMember(t *testing.T) {
	// NoTenantMembership: someone just removed from the tenant their
	// token names must still be able to end their session.
	r, _, _ := signoutHarnessWithMembership(t, false)

	require.Equal(t, http.StatusOK,
		testutil.Do(r, http.MethodPost, "/v1/iam/me/sign-out", "tok-nurse", "").Code)
}

func TestAdminRevokeRefusesASubjectOutsideTheActingTenant(t *testing.T) {
	// The target is a member of some other hospital only. Without this
	// gate any tenant admin could revoke any subject on the platform.
	r, db, _ := adminRevokeHarness(t, map[string]bool{"uid-stranger": false})

	w := testutil.Do(r, http.MethodPost, "/v1/iam/subjects/uid-stranger/revoke", "tok-admin", "")

	require.Equal(t, http.StatusNotFound, w.Code,
		"a subject outside the acting tenant is answered 404, the cross-tenant answer")
	require.True(t, watermarkFor(t, db, "uid-stranger").IsZero(),
		"and no watermark is written")
}

func TestAdminRevokeRequiresThePermission(t *testing.T) {
	r, _, _ := adminRevokeHarnessWithoutPermission(t)

	require.Equal(t, http.StatusForbidden,
		testutil.Do(r, http.MethodPost, "/v1/iam/subjects/uid-nurse/revoke", "tok-nurse", "").Code)
}

func TestRevocationPublishesInvalidationInTheSameTransaction(t *testing.T) {
	r, db, _ := signoutHarness(t)

	testutil.Do(r, http.MethodPost, "/v1/iam/me/sign-out", "tok-nurse", "")

	require.Eventually(t, func() bool {
		return outboxContains(t, db, SubjectCredentialRevoked, "uid-nurse")
	}, 10*time.Second, 100*time.Millisecond,
		"the invalidation must go through the outbox, so it cannot commit without the watermark or vice versa")
}
```

- [ ] **Step 3: Run and confirm failure**

```bash
cd backend && go test ./internal/modules/iam/ -run 'TestSignOut|TestAdminRevoke|TestRevocationPublishes' -v
```

- [ ] **Step 4: Implement `backend/internal/modules/iam/signout.go`**

```go
package iam

import (
	"encoding/json"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/tesserix/hms/internal/platform/requestid"
	"github.com/tesserix/hms/internal/platform/respond"
	"github.com/tesserix/hms/pkg/authn"
	"github.com/tesserix/hms/pkg/events"
	"github.com/tesserix/hms/pkg/tenantdb"
)

const SubjectCredentialRevoked = "hms.in.iam.credential_revoked.v1"

// CredentialRevokedData is the v1 payload. It carries only the subject:
// every replica needs to know which cache entry to drop, and nothing
// else about the revocation belongs on a bus.
type CredentialRevokedData struct {
	Subject string `json:"subject"`
}

type revocationHandlers struct {
	db      *tenantdb.DB
	bus     *events.Bus
	checker *RevocationChecker
	revoker authn.TokenRevoker
	roles   platform.RoleLister
}

// signOut ends every session for the calling subject, on every device.
//
// GIP ID tokens carry no session identifier, so revocation is
// necessarily by subject — see the design spec's D5. That is the
// correct answer for the shared-workstation case that motivates this
// work: a ward terminal's previous user must not be recoverable.
func (h *revocationHandlers) signOut(c *gin.Context) {
	p, ok := authn.PrincipalFrom(c)
	if !ok {
		respond.Unauthenticated(c, "missing principal")
		return
	}
	if err := h.revoke(c, p.Subject, time.Now().UTC(), "sign_out", p.Subject); err != nil {
		respond.InternalErr(c, err, "could not sign out")
		return
	}
	respond.OK(c, gin.H{"signed_out": true})
}

// adminRevoke lets a tenant admin cut off a compromised account
// immediately.
//
// The target must be a member of the acting admin's tenant. Without
// that gate any tenant admin could revoke any subject on the platform.
// A non-member is answered 404, this codebase's cross-tenant answer —
// 403 would confirm the subject exists somewhere.
//
// The effect is nonetheless cross-tenant by construction: a credential
// is global, so revoking it ends that subject's sessions everywhere.
// That is deliberate — forcing a hospital to leave a known-compromised
// credential alive elsewhere would be worse — and it is why every
// revoke is logged with actor, target and tenant.
func (h *revocationHandlers) adminRevoke(c *gin.Context) {
	p, ok := authn.PrincipalFrom(c)
	if !ok {
		respond.Unauthenticated(c, "missing principal")
		return
	}
	target := c.Param("subject")

	bindings, err := h.roles.ListRoles(c.Request.Context(), target)
	if err != nil {
		respondRolesUnavailable(c, err)
		return
	}
	if !hasBindingForTenant(bindings, p.TenantID) {
		respond.NotFound(c, "subject")
		return
	}
	if err := h.revoke(c, target, time.Now().UTC(), "admin_revoke", p.Subject); err != nil {
		respond.InternalErr(c, err, "could not revoke credentials")
		return
	}
	requestid.Logger(c).WarnContext(c.Request.Context(), "credentials revoked by administrator",
		"target_subject", target, "actor_subject", p.Subject, "tenant_id", p.TenantID,
		"cross_tenant_effect", "the target's sessions end in every tenant, not only this one")
	respond.OK(c, gin.H{"revoked": true, "subject": target})
}

// revoke writes the watermark and publishes the invalidation in ONE
// transaction. Splitting them would allow a watermark that propagates
// only at TTL, or an invalidation for a revocation that never happened.
//
// GIP is told after the commit: it is not transactional, and a GIP
// failure must not roll back a revocation HMS has already decided on.
// The HMS watermark is authoritative on the request path, so a GIP call
// that fails leaves the credential refused here regardless — it is
// logged loudly rather than surfaced as a failure the caller might
// retry into a double revoke.
func (h *revocationHandlers) revoke(c *gin.Context, subject string, at time.Time, reason, actor string) error {
	err := h.db.WithSystem(c.Request.Context(), func(tx *gorm.DB) error {
		if err := h.checker.RevokeTx(tx, subject, at, reason, actor); err != nil {
			return err
		}
		data, err := json.Marshal(CredentialRevokedData{Subject: subject})
		if err != nil {
			return err
		}
		return h.bus.Publish(tx, SubjectCredentialRevoked, events.Event{
			Type: "CredentialRevoked", Version: 1, Data: data,
		})
	})
	if err != nil {
		return err
	}
	// Drop our own entry immediately rather than waiting for our own
	// broadcast: the replica that served this request should never serve
	// the revoked credential again, not even for one more request.
	h.checker.Invalidate(subject)

	if h.revoker != nil {
		if err := h.revoker.RevokeRefreshTokens(c.Request.Context(), subject); err != nil {
			requestid.Logger(c).ErrorContext(c.Request.Context(),
				"HMS revoked the credential but GIP refresh-token revocation failed; the identity provider will keep issuing tokens this platform refuses",
				"err", err, "subject", subject)
		}
	}
	return nil
}
```

Note `events.Event.TenantID` is deliberately left empty: a revocation is not tenant-scoped. If `Publish` requires a non-empty `TenantID`, that is a real constraint to resolve during implementation — either relax it for non-tenant events or carry the acting tenant with a comment saying it is provenance, not scope.

- [ ] **Step 5: Register the routes, the permission and the broadcast**

In `module.go`:

```go
const PermCredentialRevoke authz.Permission = "iam.credential.revoke"

// Permissions declares iam.member.manage and iam.credential.revoke.
// Neither lists a role: tenant_admin is implicit, and revoking a
// person's credentials platform-wide is not an authority any clinical
// role should carry.
func (m *Module) Permissions() []authz.Grant {
	return []authz.Grant{
		{Permission: PermMemberManage},
		{Permission: PermCredentialRevoke},
	}
}
```

In `Routes`:

```go
	rev := &revocationHandlers{db: deps.DB, bus: deps.Bus, checker: m.checker, revoker: deps.TokenRevoker, roles: deps.Roles}
	g.POST("/me/sign-out", authz.NoTenantMembership, rev.signOut)
	g.POST("/subjects/:subject/revoke", PermCredentialRevoke, rev.adminRevoke)
```

Add `Broadcasts(deps platform.Deps) []events.Broadcast` to the module (and to the `platform.Module` interface, defaulting to nil in every other module):

```go
func (m *Module) Broadcasts(platform.Deps) []events.Broadcast {
	return []events.Broadcast{{
		Subject: SubjectCredentialRevoked,
		Handle: func(ctx context.Context, evt events.Event) {
			var data CredentialRevokedData
			if err := json.Unmarshal(evt.Data, &data); err != nil {
				slog.ErrorContext(ctx, "credential_revoked: undecodable payload", "err", err)
				return
			}
			m.checker.Invalidate(data.Subject)
		},
	}}
}
```

- [ ] **Step 6: Wire `cmd/api/main.go`**

Construct the revoker beside the minter; construct the checker after `db`; pass both into `Deps`; pass the checker to `authn.Middleware`; pass `fga` as the membership checker to `NewRouter`; start broadcasts alongside consumers:

```go
	revoker, err := authn.NewGIPRevoker(ctx, cfg.GIPProjectID, cfg.IsDev())
	if err != nil {
		return err
	}
	revocations := iam.NewRevocationChecker(db)
	…
	api := platform.NewRouter(srv.Engine.Group("/v1",
		authn.Middleware(verifier, revocations),
		requestid.PrincipalMiddleware(),
		authz.Middleware(fga),
	), fga)
	for _, m := range registry.All() {
		m.Routes(api, deps)
		if err := bus.StartConsumers(ctx, db, m.Consumers(deps)); err != nil {
			return err
		}
		if err := bus.StartBroadcasts(ctx, m.Broadcasts(deps)); err != nil {
			return err
		}
	}
```

`bootstrap.NewRegistry()` must hand `iam.New(revocations)` the checker so the module and the middleware share one cache instance. Passing two instances would mean the middleware's cache is never invalidated — add a test asserting they are the same pointer if the wiring makes that ambiguous.

- [ ] **Step 7: Add sign-out to the arch-test allowlist**

The `"POST /iam/me/sign-out"` entry written in Task 3 Step 12 now resolves. Run:

```bash
cd backend && go test ./internal/archtest/ -v
```

Expected: PASS.

- [ ] **Step 8: Write the two-replica propagation test (spec T9)**

```go
func TestRevocationPropagatesToAnotherReplica(t *testing.T) {
	// Two independently-constructed checkers sharing one Postgres and
	// one NATS — the shape of two pods.
	db, bus, ctx := sharedInfra(t)
	replicaA := NewRevocationChecker(db)
	replicaB := NewRevocationChecker(db)
	startBroadcastFor(t, ctx, bus, replicaB)

	// B caches the negative answer.
	got, err := replicaB.RevokedAfter(ctx, "uid-nurse")
	require.NoError(t, err)
	require.True(t, got.IsZero())

	// A revokes.
	require.NoError(t, db.WithSystem(ctx, func(tx *gorm.DB) error {
		if err := replicaA.RevokeTx(tx, "uid-nurse", time.Now().UTC(), "sign_out", "uid-nurse"); err != nil {
			return err
		}
		data, _ := json.Marshal(CredentialRevokedData{Subject: "uid-nurse"})
		return bus.Publish(tx, SubjectCredentialRevoked, events.Event{
			Type: "CredentialRevoked", Version: 1, Data: data,
		})
	}))

	require.Eventually(t, func() bool {
		w, err := replicaB.RevokedAfter(ctx, "uid-nurse")
		return err == nil && !w.IsZero()
	}, 15*time.Second, 100*time.Millisecond,
		"a revocation on one replica must invalidate the cache on another; without the broadcast this only heals at the 5-minute TTL")
}
```

- [ ] **Step 9: Run everything, prove two assertions can fail**

```bash
cd backend && go test -race ./... && cd .. && make lint-go && cd backend && ./scripts/coverage-gate.sh
```

Then: remove `hasBindingForTenant` from `adminRevoke` and confirm `TestAdminRevokeRefusesASubjectOutsideTheActingTenant` fails. Restore.
Then: make `Broadcasts` return nil and confirm `TestRevocationPropagatesToAnotherReplica` fails on the `Eventually`. Restore.

- [ ] **Step 10: Commit**

```bash
git add backend/
git commit -m "feat: sign-out and admin revoke write the watermark, revoke at GIP and invalidate every replica (#781)"
```

---

## Task 7: The shell actually signs out

**Files:**
- Rewrite: `apps/shell/app/logout/route.ts`
- Create: `apps/shell/app/logout/logout.test.ts`, `e2e/tests/signout.spec.ts`
- Modify: `packages/ui/src/hms-shell.tsx:115,197`, `docs/standards/frontend.md`

**Interfaces:**
- Consumes: `POST /v1/iam/me/sign-out` from Task 6.

- [ ] **Step 1: Write the failing E2E test — the shared-workstation guarantee (spec T10)**

This is the assertion that would have caught the original bug. It deliberately does **not** assert "the cookie was cleared" — that is the proxy assertion that let the defect exist.

Create `e2e/tests/signout.spec.ts`:

```ts
test("a signed-out session cannot be reconstructed from the browser", async ({ page }) => {
  await signIn(page, "nurse@hms.dev", "password123");
  await expect(page.getByRole("link", { name: "MediCore" })).toBeVisible();

  await page.getByRole("button", { name: "Sign out" }).click();
  await expect(page).toHaveURL(/\/login/);

  // The attack: the previous user's Firebase SDK session used to survive
  // in IndexedDB, so this rebuilt a working session from the console.
  const restored = await page.evaluate(async () => {
    const { getAuth, getIdToken } = await import("firebase/auth");
    const user = getAuth().currentUser;
    if (!user) return "no-user";
    const idToken = await getIdToken(user, true);
    const res = await fetch("/api/session", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ idToken }),
    });
    if (!res.ok) return "session-refused";
    const check = await fetch("/api/v1/iam/me/permissions");
    return check.ok ? "RESTORED" : "api-refused";
  });

  expect(restored).not.toBe("RESTORED");
});
```

- [ ] **Step 2: Run it and confirm it fails**

```bash
make up && make seed
pnpm --filter e2e exec playwright test tests/signout.spec.ts
```

Expected: FAIL with `restored === "RESTORED"` — the current defect, reproduced.

If the port set is busy, use the shifted ports: `HMS_PG_PORT=15432 HMS_NATS_PORT=14222 HMS_NATS_MONITOR_PORT=18222 HMS_REDIS_PORT=16379 HMS_OPENFGA_PORT=18090 HMS_GIP_PORT=19099 HMS_API_PORT=18080 make up`.

- [ ] **Step 3: Rewrite the logout route**

```ts
import { NextRequest, NextResponse } from "next/server";

const SESSION_COOKIE = "hms_session";

/**
 * POST, not GET, and same-origin checked.
 *
 * While logout only cleared a cookie, CSRF against it was an annoyance:
 * `<img src="/logout">` logged someone out and they signed back in. Now
 * that it revokes every session for the subject on every device, the
 * same trivial attack is a remote denial of service against a clinician
 * mid-shift. So it takes the same same-origin check /api/session uses.
 */
export async function POST(req: NextRequest) {
  const fetchSite = req.headers.get("sec-fetch-site");
  if (fetchSite && fetchSite !== "same-origin") {
    return NextResponse.json({ error: "invalid_request" }, { status: 403 });
  }

  // Server-side revocation first. If this fails the session stays live,
  // and telling the user they are signed out when they are not is the
  // failure this endpoint exists to prevent.
  const session = req.cookies.get(SESSION_COOKIE)?.value;
  if (session) {
    const res = await fetch(`${process.env.API_URL}/v1/iam/me/sign-out`, {
      method: "POST",
      headers: { Authorization: `Bearer ${session}` },
    });
    if (!res.ok) {
      return NextResponse.json({ error: "sign_out_failed" }, { status: 502 });
    }
  }

  const out = NextResponse.json({ ok: true });
  out.cookies.set(SESSION_COOKIE, "", { path: "/", maxAge: 0 });
  return out;
}
```

The Firebase `signOut()` and `clearPermissionsCache()` must run **client-side** — the SDK session lives in the browser, and a route handler cannot reach IndexedDB. Add a small client component owning the sequence: `POST /logout` → `await signOut(firebaseAuth())` → `clearPermissionsCache()` → `router.replace("/login")`, with the Firebase step still running if the POST fails, so a user can always get out of a shared workstation.

- [ ] **Step 4: Convert the two sidebar links**

`packages/ui/src/hms-shell.tsx:115` and `:197` become a submit button in a form posting to the sign-out client component, styled to match the existing link. Both instances (desktop rail and mobile sheet) change.

- [ ] **Step 5: Document the frontend exception**

`docs/standards/frontend.md`, in the navigation section:

```markdown
**Sign-out is the one exception to "cross-zone links are plain `<a>`".** It
is a state change, not navigation: it revokes every session for the
subject, so it must be a POST with a same-origin check rather than a GET
any page can trigger with an `<img>` tag (#781). It renders as a form
submission. Do not convert it back to a link.
```

- [ ] **Step 6: Write the route unit test**

`apps/shell/app/logout/logout.test.ts`:

```ts
it("refuses a cross-site POST", async () => {
  const res = await POST(request({ "sec-fetch-site": "cross-site" }));
  expect(res.status).toBe(403);
});

it("does not clear the cookie when server-side revocation fails", async () => {
  fetchMock.mockResolvedValue({ ok: false });
  const res = await POST(request({ "sec-fetch-site": "same-origin" }, "a-session-token"));
  expect(res.status).toBe(502);
  expect(res.cookies.get("hms_session")?.value).toBeUndefined();
});
```

- [ ] **Step 7: Run everything**

```bash
pnpm turbo lint type-check test build
pnpm --filter e2e exec playwright test tests/signout.spec.ts
```

Expected: all green; the E2E now returns `session-refused` or `api-refused`.

- [ ] **Step 8: Prove the E2E can fail**

Comment out the `await fetch(.../sign-out)` call in the route handler so only the cookie is cleared — the exact original defect — and confirm `signout.spec.ts` fails with `RESTORED`. Restore.

- [ ] **Step 9: Commit**

```bash
git add apps/shell packages/ui e2e docs/standards/frontend.md
git commit -m "fix: sign-out revokes the session server-side and clears the Firebase SDK state so a shared workstation cannot restore it (#781)"
```

---

## Task 8: Verification sweep and PR

- [ ] **Step 1: Full gates**

```bash
make lint-go
cd backend && go test -race ./... && ./scripts/coverage-gate.sh
cd .. && pnpm turbo lint type-check test build
```

- [ ] **Step 2: End-to-end against the running stack**

```bash
make up && make seed && make verify-local
pnpm --filter e2e exec playwright test
```

Then manually: sign in, revoke via `POST /v1/iam/subjects/<uid>/revoke` as an admin in another browser, and confirm the first browser's next request is refused without waiting for expiry.

- [ ] **Step 3: Confirm the outage scenario is actually covered**

Point the stack at a store created before this branch (or delete the tenant edges from the store), boot, and confirm requests succeed — the reconciler must rebuild the edges before the listener starts. If this fails, the boot ordering is not what `cmd/api/main.go:90` suggests and must be fixed before merge.

- [ ] **Step 4: Update the spec's status**

Change the header of `docs/superpowers/specs/2026-08-13-credential-revocation-design.md` from `approved` to `implemented`, and correct anything the implementation decided differently. A spec describing a design the code no longer has is worse than no spec.

- [ ] **Step 5: Commit and open the PR**

```bash
git add -A && git commit -m "docs: mark the credential revocation design implemented"
git push -u origin feat/781-credential-revocation
```

PR body must carry: link to #781, the three defects and how each is closed, the T1 `auth_time`-vs-`iat` reasoning, the migration hazard and how boot ordering avoids it, the stated limitations (GIP-console-only disable, subject-level granularity, the 5-minute propagation backstop, admin revoke's cross-tenant effect), and test evidence naming which assertions were observed failing. Close with `Closes #781`.

---

## Known limitations (carry into the PR body)

- A GIP-console-only account disable is not honoured until an HMS revoke is issued. There is no reconciliation job; that option was considered and declined (spec D1).
- Revocation is per subject, not per session or device. Signing out anywhere signs out everywhere (spec D5). Per-device revocation is #424.
- Propagation is sub-second normally and bounded at 5 minutes when a replica misses the broadcast.
- Admin revoke crosses tenant boundaries by construction, bounded by the acting-tenant membership gate and logged.
- No user-administration UI; the revoke endpoint is the control.
- Membership adds a second OpenFGA call per request alongside `Resolve`'s `ListObjects`. Accepted here because membership must be immediate; optimising permission resolution generally is a #774 Tier 2 item in its own right.
- `Resolve`'s uncapped `ListObjects` and the hardcoded `SetMaxOpenConns(5)` are untouched.
