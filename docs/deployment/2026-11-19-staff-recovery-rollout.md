# R0 Staff account recovery: rollout and acceptance

Status: candidate preparation. The native hospital and independent-pharmacy
browser cases must pass before this plan becomes a rollout request. This document
is not permission to merge a change that automatically migrates or deploys the
shared production/QA environment. Issue #1703 remains open.

## Ownership and release units

| Owner                               | Change                                                                                                          | Required evidence                                                                                                 |
| ----------------------------------- | --------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- |
| Onboarding service, PRs #44 and #45 | Password-recovery prerequisite, native MFA ledger, factor operations and session proof; workload authentication | Current owner contracts, Go quality, secret scan and image checks; matching Go/TypeScript consumer pins           |
| Infrastructure, PR #1713            | Additive owner schema in `charts/apps/db-schema-bootstrap/schemas/global/global/onboarding_db.sql`              | Schema tests, reviewed runtime grants, successful approved global schema reconciliation                           |
| Helivanta, PR #1748                 | Tenant-scoped password recovery and lockout guidance                                                            | Password-reset-to-login acceptance and a resolved GitGuardian finding; normal required checks                     |
| Helivanta, PR #1749                 | Reviewed Staff MFA reset, retained application intent, credential fencing and UI                                | Full Go/web/native browser checks, both employer models, denial cases, local builds and generated contract checks |

Native identities, factors and password/session evidence remain owned by Zitadel
through the onboarding service. Workforce owns current Staff and reviewed account
links. IAM owns application membership, command intent and revocation fences. An
OAuth client login name is not the native user ID: the owner authorizes the JWT
subject against the registered machine ID, independently of the login name used
to obtain that JWT.

## Read-only baseline, 19 November 2026

Discovery verified the active operator account privately, project `tesseracthub-480811`, and
context `gke_tesseracthub-480811_asia-south1_tesseract-prod-in-gke`. The default
namespace was unset; every following observation used explicit namespaces.
Refresh this boundary and Kubernetes RBAC before any approved mutation.

- Deployments in `helivanta` and `onboarding` were Ready. Argo applications
  `helivanta-api`, `helivanta-shell`, `onboarding-service`,
  `global-db-schema-bootstrap` and `helivanta-db-schema-bootstrap` were
  Healthy/Synced. These observations do not prove the candidate is deployed.
- Kargo `prod` stages in `kargo-onboarding` and `kargo-helivanta` reported Healthy
  with Ready/Healthy/Verified conditions. Use those conditions; this CRD does not
  expose a `status.phase` field. Both stages had automatic promotion enabled, so
  approval must precede the merge/release that triggers production promotion.
- `global/global-db-schema-bootstrap` is an unsuspended CronJob, scheduled every
  30 minutes. Merging its new SQL can cause a live migration at the next run.
- The API chart at observed infrastructure revision
  `f35ff0f6b592494d7db3bb56d13b4138ae3d6227` uses a PreSync
  `helivanta-api-migrate` Job. It runs `/app/migrate` from the same API image as the
  application. BeforeHookCreation removes the previous hook Job. There are no
  API Deployment init containers that replace this migration gate.

No live schema, identity, factor, feature flag, deployment or Kargo promotion was
changed during this discovery. Readiness of the existing workloads is a baseline,
not candidate acceptance.

### Read-only refresh, 20 November 2026 AEDT

The operator account, project and context above remain active. API and shell each
have two Ready replicas at `65899b98365063acf6828d8b697ad58a914f993a`; the
`onboarding-api` Deployment has two Ready replicas at
`f47d44dae55e7e8ab62b657f21ff8aadb139e8a5`. These are the current sign-in releases,
not the recovery candidate. The five Argo applications listed above remain
Healthy/Synced, and both Kargo production stages report Ready, Healthy and
Verified. The global schema CronJob remains unsuspended at `*/30 * * * *`.

Schema PR #1713 at `7bd817e6fdfec553ee95499faf6076f155a4907d` contains all of
password-schema PR #1712 at `363dce21216c5e4830a33465d988f043b5ee960d` and adds the
MFA schema and its tests. The GitHub comparison reports two commits ahead and
zero behind. The approval request must identify this combined schema release;
do not schedule two independent applications of its shared SQL.

## Gates before the approval request

1. Keep the owner and application branches current through normal review. Pin
   both consumers to the same tested owner revision. Do not merge a branch with
   failed or pending required checks and do not cancel healthy runs to replace
   them with another revision.
2. Run the real disposable native hospital and independent-pharmacy tests. They
   must use separate actual organizations/accounts, the Global API, real reviewed
   Staff links, native factors and workload JWTs. Test rejected stale and foreign
   scope, old-access/renewal denial, forced replacement enrollment, fresh accepted
   access/renewal and the retained completed receipt. A typed route, mock, unit
   test or compiled integration package cannot substitute for this evidence.
   The explicit Staff MFA fixture opt-in makes the seed retain
   `dev/zitadel/secrets/native-staff-setup.pat` as an exclusive `0600` copy of
   the disposable bootstrap credential. Retries reuse only the same value;
   symlinks, broader permissions and conflicts fail closed. CI validates this
   private file before owner/browser startup and never exports its value.
   The native Staff cases use the two fixed UUIDv7/RFC 4122 tenants in
   `scripts/lib/native-staff-fixture-tenants.json`, each with its own actual
   native organization. The legacy sample tenant IDs remain for baseline tests;
   they cannot satisfy the canonical Staff workload client's UUID requirements.
3. Verify password recovery, single-use verification, password-reset-to-login and
   clear locked-account guidance against the actual changed behavior. Resolve
   the parent PR's security finding through the legitimate security workflow;
   do not dismiss it merely because other checks pass.
4. Complete local affected builds/checks and remove their generated artifacts.
   Preserve dependencies, caches, source and unrelated work. Record commands and
   hosted results against exact revisions.
5. Prepare the exact named QA tenants and ordinary Staff accounts. Do not reset
   the hospital owner's authenticator or another real user's factors as a test.
   List account creation, messages and any reset in the approval request. A broad
   earlier approval does not identify an arbitrary new destructive QA reset.
6. Present the concrete schema and release revisions with the named live actions
   below. Obtain approval before a merge that triggers their automatic rollout.

## Named actions to approve and execute in order

1. Approve the additive owner schema reconciliation through PR #1713 and
   `global/global-db-schema-bootstrap`. Verify the actual run's success and
   retained schema/grants before advancing owner code. Use the owning GitOps
   repository; do not execute ad hoc SQL through a pod.
2. Approve the onboarding-service prerequisite PR #44 and recovery PR #45 releases and their Kargo `prod` stage
   in `kargo-onboarding`. Observe the released image, promotion and
   `onboarding-service` Argo health. Do not advance HMS against an unverified
   owner/schema combination.
3. Approve the normal Helivanta stack merge and Kargo `prod` rollout in
   `kargo-helivanta`, including the API PreSync migration. Its new IAM phase is
   `0054_iam_staff_mfa_fences`, after the published `0053_iam_login_account_choice`;
   retain commands, cancellations and fences under
   exact tenant RLS and deny their use by Staff selectors. Capture the previous
   hook Job metadata and required recovery evidence privately before the
   controller removes it. Record the archive location before that deletion.
4. Verify both API and shell images belong to the accepted release and all
   prerequisite applications remain healthy. A successful image build is not a
   deployment; a successful promotion is not application or browser acceptance.
5. After hosted native acceptance, use the canonical release catalogue and
   existing console integration to make recovery eligible for an explicitly
   approved QA tenant cohort while retaining a disabled global default. An
   ineligible capability cannot be enabled by a provider override. Do not reuse
   the development fixture's all-capabilities mode in the shared live
   environment. Review and approve any eligibility or tenant targeting change.
6. Execute only the approved QA identity/factor actions, then test the deployed
   owner and Staff flows in Chrome and through authorized API checks. Keep
   passwords, codes, factor keys, tokens and private identity payloads out of
   screenshots, traces, logs and committed evidence.

## Acceptance, failure and recovery

Record the exact freight, image revisions, migration outcomes, feature policy,
tenant cohort and actual browser/API results. Reauthorize each narrower employer
scope: an affiliation or shared email is not authority across tenants. Verify a
normal new hospital and an independent pharmacy use their own organization and
Staff ownership; a verified workload machine does not replace an administrator's
current membership or the independent account-link review.

If any owner/schema, command, audit, revocation or native proof step is unavailable,
keep new resets disabled and preserve the retained request. Use authenticated
status/cancellation according to its legal state; do not blindly create a new
command, erase a fence or treat an uncertain native write as failed. Do not drop
recovery tables, undo accepted factor changes, remove audit history or restore old
credentials to make a deployment appear successful. A rollout rollback is a
separately approved live action and does not reverse completed native actions.

Close #1703 only when its password, MFA, lockout and deployed acceptance criteria
are proven. Continue #1705, #1704 and #1707, then milestone 12;
this rollout does not mark those issues complete.
