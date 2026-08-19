# Cross-tenant reads run on a role that may cross tenants, and nothing else may

**Issue:** [#894](https://github.com/tesserix/helivanta/issues/894)
**Widens:** #894 as filed, which describes the reconciler alone. The outbox
drainer and the retention pruner are blind for the identical reason and are
included here — see D1.
**Adjacent:** [#893](https://github.com/tesserix/helivanta/issues/893) wrote the
membership row this makes visible. That row is the only thing in
`iam_members` today, and it is why the destructive case has not yet fired.

## The problem, stated precisely

`iam_members` is `FORCE ROW LEVEL SECURITY`. **FORCE** binds the table owner
too — that is the whole point of choosing it over plain `ENABLE`. The three
whole-system code paths therefore cannot see any row, because the pool they
use connects as the owner:

```
helivanta | rolsuper=f | rolbypassrls=f     <- admin pool, owns every table
hms_app   | rolsuper=f | rolbypassrls=f     <- request path
postgres  | rolsuper=t | rolbypassrls=t     <- not used by Helivanta
```

`WithAdmin`'s doc comment says it "connects as the migration role and
therefore bypasses RLS entirely — it sees every tenant's rows in every
table." In production that sentence is false. It is true in dev, where the
owner is the Postgres image's superuser, and true in the test harness, which
reproduces dev. **The assumption was never wrong anywhere it was tested.**

## D1 — This is three bugs, not one

Every entry in `withAdminAllowlist` is there because it must cross tenants,
and each one reads a FORCE-RLS table:

| Call site | Purpose | Table | Consequence in production |
|---|---|---|---|
| `platform.Reconcile` | memberships → OpenFGA tuples | `iam_members` | no tuple ever written; nobody holds a permission |
| `events.drainOnce` | publish outbox rows | `outbox_events` | outbox never drains |
| `events.Prune` | trim outbox/ledger | `outbox_events` | deletes nothing, reports success |

Confirmed live: 9 of 13 public tables are forced, including both tables above.
Neither failure has cost anything yet only because `outbox_events` holds 0
rows and OpenFGA holds 0 tuples.

`Prune` is the sharpest: `retention.go` already warns that "a DELETE under
`WithSystem` matches zero rows across every tenant and reports success" — and
that is precisely what `WithAdmin` now does. The hazard was identified, and
the mitigation chosen for it has the same defect.

## D2 — Split the two privileges that `WithAdmin` conflates

`WithAdmin` today means both "is the table owner" and "sees every tenant".
Those are different privileges with different blast radii, and only one of
them is needed by any given caller.

- **`WithAdmin` keeps owner semantics.** DDL, `Migrate`, `LintRLS`. These need
  to *own* the tables; they do not need to see other tenants' rows. Its doc
  comment is corrected to say so.
- **`WithSystemPool` (new) holds `BYPASSRLS` and no DDL.** The three call
  sites in D1 move to it.

The privilege becomes auditable: one role, granted one attribute, reachable
from one method, allowlisted to three files.

**Rejected: `ALTER ROLE helivanta BYPASSRLS`.** One statement, no code change,
and it makes `WithAdmin`'s existing comment true. It also removes FORCE's
binding on the owner across all 9 forced tables for every admin-pool
connection — including migrations — to fix three reads. FORCE was chosen over
ENABLE deliberately; this un-chooses it globally.

**Rejected: iterate tenants under `WithTenant`.** Needs a tenant list.
Enumerating tenants means reading `iam_members`, which is the blocked read.
A `tenants` table does not exist (#893). A `SECURITY DEFINER` function is the
same privilege wearing a different hat, with the grant harder to audit.

## D3 — The harness must reproduce production's roles, not dev's

`testinfra.StartPostgres` creates `hms_app` as `NOSUPERUSER NOBYPASSRLS` and
leaves the owner as the image superuser. That single omission is why every
test passed while production was broken: the harness rebuilt dev's wiring
instead of production's.

The owner role becomes `NOSUPERUSER NOBYPASSRLS` in the harness, and the new
system role is created alongside it. This is the load-bearing change — it is
what makes the class of divergence *testable* rather than merely fixed once.

`TestWithAdminBypassesRLSAcrossTenants` asserts the false claim directly. It
is expected to fail the moment D3 lands, and that failure is the proof D3
works. It is replaced by a pair: `WithAdmin` sees only what RLS permits,
`WithSystemPool` sees every tenant.

## D4 — Fail closed on a missing system DSN

`cmd/api` refuses to boot without the system DSN rather than falling back to
the admin pool. A fallback would restore exactly today's silent-zero-rows
behaviour, and the operator would learn about it as "permissions stopped
working" weeks later. `cmd/migrate` and `cmd/bootstrap` do **not** need it —
neither crosses tenants.

## Out of scope

- Rotating the system role's password. Same posture as every other Helivanta
  DB credential; no new mechanism.
- The `tenants` table (#893's third gap). Nothing here needs one.
- Any change to `WithSystem` or `WithTenant`.

## How this is proven

Not by "the reconciler logs a non-zero count" — that is a proxy. The claim is
that a membership row becomes a tuple **on a database whose owner cannot
bypass RLS**, so the harness change in D3 is what makes every assertion below
meaningful:

1. Owner role in the harness has `rolbypassrls=false` — asserted, not assumed.
2. `WithAdmin` cannot see another tenant's row; `WithSystemPool` can.
3. `Reconcile` writes the expected tuple with the owner unable to read the
   table directly.
4. `drainOnce` publishes an outbox row written under a different tenant.
5. Each assertion is mutation-proven: revert the call site to `WithAdmin`,
   watch it fail.
