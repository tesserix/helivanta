# Manage Helivanta feature flags

Product operators control released features in the
[Tesserix Console](https://console.tesserix.app/platform/feature-flags/helivanta).
Hospital and pharmacy owners manage staff permissions in Helivanta; they do not
control these platform switches. Product-wide flags affect **all Helivanta tenants in production**. Entries
marked **Pilot only** affect only explicitly configured pilot tenants while their
operator policy remains unexpired; enabling them never releases them globally.
See [the scoped pilot procedure](../design/1689-qa-retail-pilot.md). A flag never grants membership, professional authority, staff
permissions or access to another employer or outlet.

## Before changing availability

1. Sign in to Console with a platform account. Editing additionally requires
   `manage-feature-flags`; hospital ownership does not imply this role.
2. Open **Feature flags → Helivanta**. Check the product and production environment.
3. Select **Refresh** and wait for completion. Resolve any unavailable management
   state or unresolved command before making another change.
4. Find the feature by its title, Staff/PharmaConnect group or full flag key.
   Review its description, release eligibility and prerequisites.
5. Prepare the tested release or incident reference. Keep credentials, patient
   details and staff personal information out of the change reason.

**Provider enabled** means the desired GrowthBook value. **Available in Helivanta**
means released, enabled and all prerequisites available. **Release locked**
features require a reviewed application release; provider edits cannot release them.

## Enable a released feature

1. Enable the feature's prerequisites first, using the dependency tree below.
2. Select its **Enable** switch. Locked or dependency-blocked switches cannot be enabled.
3. Review the confirmation dialog and the dependent features that will resume.
   Their desired values are preserved, so one prerequisite can restore several features.
4. Enter **Reason for this change**, including the release reference (8–500 characters).
5. Select **Confirm enable** once and wait for the acknowledged result.
6. Refresh Console. Confirm desired and effective availability and inspect the
   recorded command in the history.
7. Wait for a fresh Helivanta capability snapshot, then refresh the affected page.
   Check navigation, the direct page URL and the corresponding Global API request
   with an authorized account. A provider write alone does not prove app acceptance.

## Disable and restore a feature

1. Find the feature and select **Disable**.
2. Review the dependent-feature impact. Disabling a prerequisite also makes its
   dependents unavailable; their desired values remain stored.
3. Enter the release or incident reason and select **Confirm disable** once.
4. Refresh Console and verify the acknowledged command and effective values.
5. After a fresh Helivanta snapshot, confirm the feature disappears from navigation,
   bookmarked business pages are unavailable and new gated API work is refused.
6. To restore, follow the enable procedure with a new reason referencing the
   original change, and recheck the dependents.

Disabling does not delete records, revoke IAM assignments or sign users out.
Baseline authentication, verification, password setup, session recovery and
tenant-security routes keep their existing authorization checks.

## Current MVP and dependency tree

The dated production baseline on **2026-11-17** has 22 definitions and six enabled
capabilities. Future desired values may differ. The canonical application source
is [`contracts/release-capabilities.json`](../../contracts/release-capabilities.json).
All suffixes below use the prefix `helivanta.release.`.

| Suffix             | Prerequisites                  | Enabled MVP purpose                                                      |
| ------------------ | ------------------------------ | ------------------------------------------------------------------------ |
| `onboarding`       | None                           | Hospital and independent pharmacy signup                                 |
| `team_accounts`    | None                           | Staff accounts, invitations and reviewed access                          |
| `staff_core`       | `team_accounts`                | Staff directory, person/employment review, account links and credentials |
| `department_teams` | `staff_core`                   | Department structure, team membership and reviewed postings              |
| `pharmacy_setup`   | `team_accounts`                | Pharmacy employer, outlets, contacts and hospital affiliations           |
| `pharmacy_staff`   | `staff_core`, `pharmacy_setup` | Pharmacy staff and reviewed identity/employment evidence                 |

```mermaid
flowchart TD
    ON[onboarding]
    TA[team_accounts] --> SC[staff_core]
    TA --> PS[pharmacy_setup]
    SC --> DT[department_teams]
    SC --> PST[pharmacy_staff]
    PS --> PST
```

The other 16 definitions remain release locked: `pharmacy_catalogue`, `pharmacy_stock`,
`pharmacy_prescriptions`, `pharmacy_returns`, `pharmacy_reports`, `pharmacy_counter`,
`pharmacy_payables`, `workforce_scheduling`, `workforce_payroll`,
`workforce_advanced`, `supplier_collaboration`, `clinical`, `lab`, `emergency`,
`billing_workspace` and `unreleased`. Do not turn on stock, supplier fulfilment,
receiving, financial or advanced Staff workflows as a shortcut to MVP testing.

### Retained pharmacy stocktakes

The **Retained stocktakes** page uses `pharmacy_setup`, independently of Stock
and Reports. It requires stock-read permission, the existing pharmacy directory
access and the selected outlet's current grant. Ordinary stock staff do not need
access-management authority. Managers also have recovery in an opened outlet's
**Outlets & access** detail. These continuations can inspect retained status and
original outcomes; cancellation additionally needs correction permission and an
outlet editor grant. They cannot start a count, change quantities, approve
adjustments or export inventory while Stock is disabled.

### Reviewed stocktake duties — release candidate

The candidate **My stocktakes** page (`/pharmacy/working-stocktakes`)
uses `pharmacy_setup` and current-user reviewed-duty discovery. The navigation
continuation offers a personal dashboard shortcut only after current-account
discovery; it also preserves filtered-page continuations without background
scanning. This requires no general
Stock/Catalogue access. With `pharmacy_stocktake` unavailable it offers retained
status/recovery and current-counter cancellation only. The worker-command
continuation connects operational controls under `pharmacy_stocktake`: counters
start/count/submit/cancel, independent reviewers approve/reject and readers have
no command controls. Release refresh failure removes those new-work controls
while retaining recovery. The reviewed source/capacity/browser gates have passed. This release candidate
makes the capability eligible; protected CI, matching deployed revisions and
Console activation remain required before production availability.

The reviewed `/stocktakes/product-choices` and `/stocktakes/quarantine-choices`
collections use the same stocktake capability as new work. They do not expose
general catalogue/receiving access and are unavailable when that capability is
withdrawn. Existing retained-work discovery remains a baseline continuation.

`pharmacy_stocktake` is a separate capability, dependent on `pharmacy_setup`
and compiled **eligible in this release candidate**. Its provider key is
`helivanta.release.pharmacy_stocktake`; the scheduled catalogue job creates it
disabled. Deploy and verify the approved revision before enabling it in Console. Do not enable broad Stock/Catalogue to reach this work.

The canonical release panels classify each operating template as
`staff.operating.<template-key>`. Existing duties retain their baseline
visibility. The new `pharmacy.stocktake_reader.v1`,
`pharmacy.stocktake_counter.v1` and `pharmacy.stocktake_reviewer.v1` all bind to
`pharmacy_stocktake`. The duty picker filters these bindings using the current
account's release snapshot. IAM independently checks availability for new
proposals and approval; unknown bindings or unavailable policy fail closed.
Rejection, withdrawal, revocation and exact recorded-result recovery retain
their existing current-authority checks.

When adding a template, update its canonical panel binding in
`contracts/release-capabilities.json`, then run both
`generate-release-capabilities.mjs` and `generate-operating-assignments.mjs`.
The operating generator refuses a missing or unknown release binding. These
bindings control availability, and do not assign Staff or confer stock authority.

The reviewed independent/chain path includes start, line reads,
counting, submission, independent posting, rejection, cancellation and original
command recovery with retained Staff/link/duty evidence. Current reviewed
header/list/register reads also passed hosted scope, audit-failure and rollback
tests. Clean independent reviewed capacity also passed in hosted 45075494219.
Reviewed header/list and original count/decision receipts are baseline
continuations. A dedicated `PUT /stocktakes/{stocktake}/cancellations/{command}`
accepts only cancellation under a current counter duty; it cannot admit submit,
approval or rejection. These continuations do not need a release-provider
snapshot, but still require the current account, Staff/link, exact outlet,
command ownership and audit evidence. New starts/counts/decisions, line browsing
and CSV export remain gated; retrying a blocked write is not the recovery path.
The SDK requires explicit `authority: "operating"` to use reviewed routes and
separate recovery/cache namespaces. Existing legacy references remain readable
through the legacy flow; they are not adopted into reviewed work. A scope change
removes confirmed results from view until the new scope is checked. This SDK
support does not release a page or replace current server authorization.

Hospital source recovery and maximum capacity passed hosted 45337524268; the
built hospital browser passed 45338199508. Independent reviewed browser/capacity
evidence is separate. Current protected checks, Kargo rollout and both-model
authenticated live acceptance remain required by #1686. A passing hosted gate
does not enable the provider value by itself.

### Changed hospital stocktake source

A retained unfinished count can show **Source changed** when its original
hospital department revision or approved outlet binding no longer matches.
Its observations and original state/version remain evidence; its counting,
submission and review actions are unavailable. Do not grant broad outlet access
or enable another feature to adopt that count.

1. Have the responsible department administrators establish the correct current
   department and independently approve its replacement outlet binding through
   the existing binding workflow.
2. Select the current named assignment, department and outlet in **My stocktakes**.
3. Start a fresh count. A successful start records source invalidation and the
   replacement atomically. An invalid start or unavailable audit leaves neither
   change committed; recover the same command before retrying an uncertain result.
4. Inspect the retained original observations and current fresh count separately.
   Recovery creates no stock movement. Only a later independent approved
   difference posts adjustments through the normal stocktake workflow.

The CSV column `source_invalidated` conveys this condition without rewriting
state, version or quantities. Completed counts retain their ledger. A replacement
department does not inherit receipt access from a retired department. Eligibility
and live acceptance gates above still apply to this development candidate.

### Pending Staff authenticator recovery control

Policy `2026-11-19-r1` adds `staff_mfa_recovery`, dependent on `staff_core` and
`team_accounts`, for the Staff account panel and reset commands. Its Console
flag is `helivanta.release.staff_mfa_recovery`. The recovery candidate makes it
eligible after isolated native acceptance, with the final release gates pending.
Its provider value stays disabled until approved activation. The Helm-managed catalogue creates its
missing provider definition disabled after the deployed application contract
matches; neither catalogue synchronization nor provider configuration can bypass
compiled eligibility. A future activation uses the normal product-wide Console
procedure above and does not grant Staff account authority.

Hospital-owned pharmacy staff stay within their hospital tenant and employer.
Independent pharmacy staff belong to their pharmacy tenant and employer. Shared
email addresses and declared affiliations do not create cross-tenant authority.

## Dashboard cards follow the same flags

Dashboard shortcuts use the same canonical page classifications and effective
capability snapshot as the sidebar and destination pages. They also require the
account's existing page permissions. There is no separate dashboard flag list to
maintain. A new page must be classified in the canonical contract before it ships.

| Dashboard cards in this MVP                                                              | Required flag suffix                                                           |
| ---------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------ |
| Staff access; Branding                                                                   | `team_accounts`                                                                |
| Staff directory; Employment reviews; Person reviews; Credential register                 | `staff_core`                                                                   |
| Department teams; Posting preparation; Posting reviews; HR setup; Organization structure | `department_teams`                                                             |
| Setup in PharmaConnect                                                                   | `pharmacy_setup`                                                               |
| Staff in PharmaConnect; Pharmacy Staff; Pharmacy relationships                           | `pharmacy_staff`                                                               |
| Workspace security                                                                       | Baseline authenticated account security; independent of optional release flags |

`onboarding` controls public signup and has no dashboard shortcut. Flags control
capability groups, so the six enabled flags can produce 16 cards for an owner
with all required permissions. For example, disabling `pharmacy_staff` removes
its three cards while leaving pharmacy setup and hospital Staff cards available.
Disabling `team_accounts` also closes its dependent features, leaving baseline
account security. Account switching immediately withholds the previous tenant's
business cards until the new account has a valid snapshot.

Automated dashboard acceptance lives in
`apps/shell/app/(workspace)/dashboard/page.release.test.tsx`. It checks the exact
accepted MVP card set against the canonical eligibility list, each enabled
flag's disabled/dependency state, a refreshed off/on transition, unavailable and
wrong-account snapshots, and tenant switching. New eligibility needs a reviewed
update to this acceptance baseline, not an automatic opt-in of untested pages.
The release generator and navigation tests additionally enforce page coverage.

### Reviewer choices follow the release catalogue

In **Staff → Staff access → Reviewer appointments**, account-link, person and
employment roles require Staff Core. Hospital scoped-access reviewer requires
Department Teams. Governance auditor requires Team Accounts. Named pharmacy
scopes additionally require Pharmacy Staff. The operating reviewer and supplier
scope are not part of the released MVP. Turning off a dependency also withholds
its choices; owners cannot use a reviewer appointment to enable a locked product.
A successful appointment receipt may still be provisional or awaiting review.
It does not grant clinical authority or pharmacy outlet operations.

### Snapshot expiry and automatic refresh

Capabilities remain bound to the current tenant/account and the provider's original
60-second lifetime. The client refreshes every 15 seconds, including background
polling, refreshes on focus and makes one deduplicated recheck when its snapshot
expires. A missed poll must not strand the user until a full page reload. Expired
or wrong-account data never permits a business action. Continuing provider/source
failure still makes the feature unavailable; refresh is not a stale-data bypass.
Browser scheduling can delay background work, so resolve an unavailable source
before preparing a new command. Keep retained command references for recovery.

## Troubleshooting and uncertain commands

| Observation                           | Action                                                                                                                                                  |
| ------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Read-only Console                     | Follow the authorized platform access process; do not grant customers platform administration.                                                          |
| Prerequisite unavailable              | Check its desired value, eligibility and dependencies; enable only the reviewed chain.                                                                  |
| Release locked                        | Implement, deploy and accept the application release first.                                                                                             |
| Pending or uncertain command          | Retain its reference. Use the displayed recovery control or **Retry same request**; do not submit a second intent. Editing stays paused until resolved. |
| State changed while dialog was open   | Close it, refresh, review the new revision and reopen.                                                                                                  |
| Availability cannot be verified       | Refresh after recovery. This does not mean every flag is off; do not reset provider data.                                                               |
| Provider enabled but app refuses work | Check effective availability, prerequisites, running policy version, snapshot freshness and the actor's permissions and scope.                          |

Console refreshes visible pages every 30 seconds. App snapshots have a maximum
60-second lifetime; changes are not instantaneous across all open tabs. Unknown,
expired or unavailable provider state refuses new gated work with 503; explicitly
unavailable features return 404. Identity and object authorization still apply.
See the [API contract](../api/release-capabilities.md).

## Add future flags through Helm and the reconciler

Maintainers can add reviewed definitions through the chart. Console's manual
creation supports missing reviewed entries, not arbitrary unbound feature names.

1. Implement the feature and tests. Update the canonical contract with its owner,
   dependencies, route/page/panel bindings and release eligibility. Do not ship
   unclassified routes or hand-edit generated outputs.
2. Regenerate and verify in Helivanta:

   ```sh
   node scripts/generate-release-capabilities.mjs
   node scripts/generate-release-capabilities.mjs --check
   node --test scripts/release-capabilities.test.mjs
   ```

3. Update the reviewed catalogue/schema in `tesserix/tesserix-k8s` using its
   `scripts/sync-helivanta-feature-catalogue.py` (canonical contract path, with
   `--check` supported). Follow that repository's
   `docs/adr/2026-11-17-helivanta-feature-catalogue.md` for chart ownership and checks.
4. The onboarding-service chart publishes `helivanta-feature-catalogue` ConfigMap
   in `helivanta-growthbook`. The dedicated GrowthBook chart owns the CronJob of
   the same name, scheduled every five minutes (`*/5 * * * *`). Deploy through
   its owning GitOps/Kargo lane.
5. The reconciler validates the entire catalogue, schema, dependencies and published
   release-contract parity before writing. It creates missing definitions
   **disabled**, preserving existing values, rules and versions. It never enables
   features, resets Console choices or deletes flags.
6. Wait for a successful scheduled Job and refresh Console. For a missing reviewed
   entry, operators can instead select **Create disabled flag**, enter a reason
   and **Confirm create**. Both paths converge on the same definition.
7. Deploy the compatible application and complete acceptance tests. Enable the
   eligible feature separately using the Console procedure above.

GrowthBook stays version/digest pinned, with upgrades through its platform-tools
Kargo lane. Its deployment is API-only; operators use Console. Credentials come
from OpenBao/ESO and stay server-side, outside catalogues, chart values, browser
code, screenshots and this public repository.

## Evidence for an acceptance run

Record the running commit/digest, flag and command references, before/after
desired and effective values, tenant/actor test labels, UI result, API status and
restoration result. Keep real contacts and credentials in private evidence.
Distinguish hosted CI, live browser and backend checks. Do not call a workflow
accepted while independent review, recipient verification, scope denial or
recovery checks remain unexecuted.

## Outlet low-stock warnings

`helivanta.release.pharmacy_low_stock` depends only on `pharmacy_setup`, which
requires `team_accounts`. Its workflow is **PharmaConnect → Inventory → Low
stock** (`/pharmacy/low-stock`). It provides an exact outlet count, warning list
and accountable product/outlet minimum editor. Readers need stock-read permission
and the exact outlet reader grant; workspace discovery also needs catalogue-read.
Editors additionally need catalogue-write and
the outlet editor grant. The same checks apply to hospital-owned and independent
pharmacies. This flag grants neither stock movement nor dispensing authority.

1. Check #1685 acceptance evidence and the deployed application policy version
   against the canonical contract and chart catalogue. Keep this feature release
   locked until owner, Global API, built-browser and 10,000-product gates pass.
2. Deploy the eligible signed revision through the normal Release/Kargo flow.
3. Let the chart-owned scheduled catalogue sync create the definition disabled.
   Review it in Console; preserve all other desired feature values.
4. Enable only **Low stock** with a release reference using the procedure above.
5. Refresh Helivanta, choose an authorized test outlet and check the count, named
   list, zero/equality threshold, saved minimum and reload. Verify reader write
   denial and inaccessible-scope refusal. Never enable broad Stock/Catalogue to
   prepare fixtures or bypass identity review.
6. Verify a later scheduled catalogue sync preserves its acknowledged desired
   value. Record actual release, command and browser evidence in #1685.

Disabling Low stock hides the workspace and denies its APIs. Broad Stock also
depends on it, preventing legacy list/count exposure if Stock is enabled later.
Configuration history remains retained. General Catalogue, Reports, receiving,
ordering and financial capabilities remain governed by their own release gates.

### Low-stock read-policy migration gate

`0071_pharmacy_low_stock_scope_read` is a forward-only read-policy optimization
for catalogue products, reorder levels, stock batches, movements and locations.
It retains exact row tenant/outlet checks and evaluates the existing current
outlet grant once per statement. It does not cache authority between requests.
No write, alternate read, restrictive purpose or policy-role clause is replaced.
The migration also centralizes relational lot-reason and
all-product sellable-bucket calculation. Existing scalar/product functions
delegate to those owner projections with the same signatures, rule priorities,
hold arithmetic and exceptions. Approval must explicitly include these function
delegations because existing sales, balance and supplier guards call them.
It also validates and re-pins the existing hospital-stocktake source seal for
the one controlled movement-read clause; every other source field and the
runtime choice/TRUNCATE restriction remain guarded. Unrelated predecessor drift
refuses the migration before any authority can be re-sealed.

Before production application, retain the exact-head hosted predecessor/new
visibility tests, write/purpose metadata checks, denial/revocation, both-owner
browser acceptance, affected pharmacy/lot/bucket consumer regressions and original 10,000-product p99 ≤500 ms evidence. Obtain
explicit approval naming this migration and the signed release commit. Capture
the five current policies' expressions, roles and purpose-integrity result in
protected operational evidence before release; no credentials or private data
belong in the public repository.

The migration refuses an unexpected predecessor and uses five-second lock and
60-second statement limits. If it refuses or times out, investigate the exact
policy discrepancy; do not weaken its guard or edit an applied migration. It has
no automatic down migration. Rolling back an application image leaves the
compatible policy optimization in place; reverting the policy requires a new
reviewed forward migration with separately approved production application.

### Low-stock activation record — 2026-11-24 UTC

Signed application revision `95285c5b` and migration 0071 were deployed through
[Release/Kargo](https://github.com/tesserix/helivanta/actions/runs/45497399513)
after explicit approval. The new Freight was Ready/Healthy/Verified and all seven
application deployments were 2/2 ready at approved digests. Catalogue revision
`969f3055` then synchronized through the chart-owning onboarding Application.

The 22:20 scheduled reconciliation reported `created: 1`, `preserved: 24` for
policy `2026-11-24.pharmacy-low-stock.v1`; the new definition was disabled.
An audited Console command enabled only `helivanta.release.pharmacy_low_stock`
at 22:21. The 22:30 scheduled reconciliation reported `created: 0`,
`preserved: 25`, and a later Console refresh confirmed Low stock enabled with
nine available features and all 16 unreleased features still locked.

The two scheduled jobs during the release-first/old-ConfigMap interval failed
the intended contract-parity check and changed no provider values. They were not
replaced with an imperative provider edit. The approved chart ConfigMap sync
restored normal scheduled reconciliation. See the
[deployed acceptance record](../superpowers/specs/2026-11-24-low-stock-activation.md#production-acceptance--2026-11-24-utc)
for the live owner save/edit/disable tests, audited QA-fixture provenance and
separate hosted evidence for both ownership models. This record grants no roles
and does not release Catalogue, Stock, Reports, ordering or financial workflows.
