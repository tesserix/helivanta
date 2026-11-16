# Schedule class lowering needs a Helivanta reviewer — design

- **Issue:** #1681 (milestone R1 — Hospital pharmacy pilot)
- **Date:** 2026-11-16
- **Status:** implemented (branch `feat/1681-schedule-class-lowering`)

## Problem

A product's schedule class (`none`, `G`, `H`, `H1`, `X`, plus the NDPS flag)
is declared and reviewed inside the tenant
(`backend/internal/modules/pharmacy/schedule_class.sql`). The only guard is
`pharmacy_schedule_four_eyes`: the reviewer is not the declarer. Two staff in
one pharmacy, such as a pharmacist and the owner, can move an approved H1
product to `none`. That removes the counter's prescription requirement
(`sale.go`) and the product's H1 register entries.

The class is a fact about the formulation under the Drugs and Cosmetics Rules,
not a pharmacy setting. The pharmacy owner earns from sales a lower class makes
easier, so the owner cannot be the one who approves it.

## Decision (user, 2026-11-16)

- Inside the tenant, a class can only be made stricter.
- Lowering an approved class is a correction request. A pharmacist files it
  with evidence, the pharmacy owner countersigns it, and a Helivanta platform
  reviewer (the user or Sam) decides it.
- The platform reviewer decides from a command run as a platform-only database
  role, following `cmd/drugmaster-release`. No in-app review queue for now.
- Taking the class from the drug master for linked products is #1683, the next
  issue, and is not part of this slice.

## Goals

1. No tenant member or pair of tenant members can make an approved class less
   strict. The database enforces it, not only the handler.
2. A lowering takes effect only after a recorded countersign by a pharmacy owner
   and a recorded platform decision by someone other than the declarer and the
   countersigner.
3. While a lowering request waits or after it is rejected, the stricter approved
   class stays in force and the product stays sellable under it.
4. Raising a class works as today: one person declares, another reviews, and a
   pending stricter declaration blocks the product.
5. The schedule screen shows a lowering request's state, who countersigned it
   and the platform decision.
6. A test proves a tenant pharmacist and owner together cannot move an H1
   product to `none` (the issue's acceptance test).

## Non-goals

- Drug master linkage and taking the class from the master (#1683).
- An in-app platform review queue or notifications. The command lists waiting
  requests.
- Provisioning the platform database role in production infrastructure. The
  role is not provisioned in this repo or `tesserix-k8s` today; #1683 needs it
  too. This slice names it as a deployment prerequisite.
- Rewriting existing declarations. History stays as recorded.

## Anti-goals

- Any path by which a tenant API call, under any role or pair of roles, makes a
  lowering take effect.
- A lowering request that blocks sales of the product while it waits. That
  would turn a correction into an outage, so staff would avoid filing one.
- A platform reviewer acting through a tenant session or a tenant permission.

## Design

### What counts as lowering

The strictness rank is `none < G < H < H1 < X`, and NDPS `true` is stricter than
`false`. A declaration lowers the class when the class in force at insert time
exists, and either its rank is lower or it drops NDPS. Raising the rank while
dropping NDPS still counts as lowering. A declaration with no class in force is
a first declaration, handled as today.

### Database (migration `0050_pharmacy_schedule_lowering`)

1. **Recorded at insert.** Add `lowers boolean NOT NULL DEFAULT false` to
   `pharmacy_product_schedule_declarations`. A `BEFORE INSERT` trigger sets it
   from `pharmacy_product_schedule_in_force` and ignores any client value. It
   serializes concurrent declarations for one product, for example by locking
   the product's catalogue row, so two racing inserts cannot both read a stale
   class. Existing rows stay `false`, which is correct because they were all
   reviewed under the old rule.
2. **Tenant reviews refused.** `pharmacy_schedule_review_guard` refuses a tenant
   review of a lowering declaration with constraint
   `pharmacy_schedule_platform_review`. _Fail closed:_ the database refuses
   whatever the handler does, so a future route or a direct insert under
   `hms_app` cannot approve a lowering.
3. **Countersign.** A new table `pharmacy_product_schedule_countersigns` holds
   one row per lowering declaration: the countersigner and a reason. It uses the
   tenant RLS boilerplate, is append-only, has an audit trigger, and records the
   subject from `app.pharmacy_subject`. A guard refuses a countersign by the
   declarer and a countersign on a non-lowering declaration.
4. **Platform decision.** The platform role
   (my call: reuse `helivanta_drug_approver`, see Calls below) may insert into
   `pharmacy_product_schedule_reviews` only for lowering declarations that have
   a countersign. Its reviewer name must differ from the declarer's and the
   countersigner's, compared case-insensitively and trimmed as the existing
   four-eyes check does. It reads only the columns it needs to decide. It holds
   no tenant session, and every other tenant table stays closed to it.
5. **Class in force.** `pharmacy_product_schedule_in_force` skips lowering
   declarations that are not approved, then applies today's rule: the latest
   remaining declaration counts if approved. A pending or rejected lowering
   leaves the earlier stricter class in force. A pending raising declaration
   still blocks the product.

### API (pharmacy module)

- `POST /catalogue/products/:id/schedule/:declaration/review` answers `409
  platform_review_required` for a lowering declaration.
- New `POST /catalogue/products/:id/schedule/:declaration/countersign`, with
  body `{reason}`, under a new permission `pharmacy.schedule.countersign`
  granted only to `RolePharmacyOwner`. It is idempotent per declaration, with a
  repeat by the same person answering 200 and a different body answering 409,
  matching the declaration PUT. A countersign by the declarer answers `409
  four_eyes`. A non-lowering declaration answers 409.
- `GET /catalogue/products/:id/schedule` gains, on each history item, `lowers`,
  the countersign (by, reason, at) and whether the decision came from the
  platform. Its `reason` explains a waiting or rejected lowering while
  `in_force` shows the class still in effect.

### Platform command

A new command, `cmd/pharmacy-schedule-review` (my call), connecting as the
platform role, like `drugmaster-release`:

- `list` shows lowering requests that are countersigned and waiting: tenant,
  outlet, product, current and requested class, source, declarer and
  countersigner.
- `decide -declaration ID -by NAME (-approve|-reject) -reason TEXT` records the
  decision. The database rules above govern it.

### Shell (`apps/shell/components/pharmacy/schedule-class.tsx`)

- A lowering declaration shows "Waiting for Helivanta review", the
  countersign, and once decided, the platform decision.
- The tenant review action is not offered for a lowering declaration. An owner
  holding the countersign permission sees a countersign action with a required
  reason.
- The screen keeps showing the class still in force while a lowering waits.

### Tests

- Go:
  - A pharmacist declares `none` on an approved H1 product and the owner
    reviews it. The review is refused, and the in-force class is still H1. This
    is the issue's acceptance test.
  - A direct `hms_app` insert of an approving review for a lowering is refused
    by the database.
  - The client cannot set `lowers`. Concurrent declarations against the same
    in-force class are classified correctly.
  - Countersign: by the declarer refused, on a raise refused, idempotent,
    without the permission 403, and cross-tenant 404.
  - Platform decision: refused without a countersign, refused when the
    reviewer is the declarer or the countersigner, and refused under `hms_app`.
    An approval puts the lower class in force, and a sale then follows it. A
    rejection leaves the old class in force.
  - Raising still works with tenant four-eyes, and a pending raise still
    blocks.
  - Mixed change (raise the rank, drop NDPS) counts as lowering.
  - Each new assertion is checked to fail against the old code.
- Command: `list` and `decide` against a test database, as
  `drugmaster-release` is tested.
- Vitest (`renderWithProviders`): the waiting state, countersign action and
  gating, no review action on a lowering, and the platform decision in history.

### Contracts and docs

- `docs/api/domains/pharmacy.md`: the lowering rule, the countersign route, the
  409 codes and the platform command.
- OpenAPI for the schedule routes, wherever they are described.
- `go run ./cmd/contractdocs` and `-check`, and the Staff operational access
  inventory `--check` (new permission).

## Constraints

- Branch from main. Migration ID `0050_pharmacy_schedule_lowering`: #1477 holds
  `0049`. Re-check `main` and `codex/*` before merge.
- Gates run one after another.

## Calls made in the design (not stated by the user)

- Reuse `helivanta_drug_approver` as the platform reviewer role, because it is
  already the drug-data steward and production must provision it for #1683
  anyway. A separate role would add a second production database role for the
  same two people.
- The owner's countersign is required before the platform can decide, and only
  `RolePharmacyOwner` holds it.
- A mixed change that raises the rank but drops NDPS counts as lowering.
- A pending or rejected lowering leaves the old class in force rather than
  blocking the product.
- The command is named `pharmacy-schedule-review`.

## Left to the builder

- How the insert trigger serializes concurrent declarations.
- Whether the platform role's audit trail needs an extra grant for
  `audit_record_change`. Verify under that role, don't assume.
- Exact copy on the screen and in error messages.

## Implementation notes

- Lowering is compared with the latest **approved** declaration, not the class
  in force: while a raise waits, nothing is in force, and a lowering filed
  behind it must still count as one.
- The review guard also refuses a tenant approval of a declaration that is
  lower than the class approved by review time. Two declarations filed before
  either was approved would otherwise let the tenant approve the stricter one,
  then the laxer one.
- Tenant members may still reject a lowering; rejecting only keeps the stricter
  class.
- The platform role reads through each table's single `tenant_isolation`
  policy, widened with `CASE` for it alone (the RLS lint allows one permissive
  policy), and through the restrictive `operating_choice_purpose` policy on the
  same tables. Its column grants bound what it reads.
- The command reads `SCHEDULE_REVIEW_DATABASE_URL`. A tenant administrator
  also holds `pharmacy.schedule.countersign`, as with every permission.
- No OpenAPI file describes the schedule routes; `docs/api/domains/pharmacy.md`
  is the contract.

## Not covered by this slice

- **First declarations.** A product with no class in force can still be
  declared `none` under tenant four-eyes, including a duplicate of an existing
  H1 product. #1683 closes this by taking the class from the drug master for
  matching products.
- Provisioning `helivanta_drug_approver` in production, a deployment
  prerequisite before the command can be used there.
- Any in-app platform queue or notification.
