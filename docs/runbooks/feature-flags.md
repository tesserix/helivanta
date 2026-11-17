# Manage Helivanta feature flags

Product operators control released features in the
[Tesserix Console](https://console.tesserix.app/platform/feature-flags/helivanta).
Hospital and pharmacy owners manage staff permissions in Helivanta; they do not
control these platform switches. Changes affect **all Helivanta tenants in
production**. A flag never grants membership, professional authority, staff
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

The other 16 definitions are release locked: `pharmacy_catalogue`, `pharmacy_stock`,
`pharmacy_prescriptions`, `pharmacy_returns`, `pharmacy_reports`, `pharmacy_counter`,
`pharmacy_payables`, `workforce_scheduling`, `workforce_payroll`,
`workforce_advanced`, `supplier_collaboration`, `clinical`, `lab`, `emergency`,
`billing_workspace` and `unreleased`. Do not turn on stock, supplier fulfilment,
receiving, financial or advanced Staff workflows as a shortcut to MVP testing.

Hospital-owned pharmacy staff stay within their hospital tenant and employer.
Independent pharmacy staff belong to their pharmacy tenant and employer. Shared
email addresses and declared affiliations do not create cross-tenant authority.

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
