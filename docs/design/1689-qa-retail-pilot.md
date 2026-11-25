# Tenant-scoped pharmacy retail acceptance pilot

Status: implementation candidate; not activated. Completes the remaining live
acceptance path for [#1689](https://github.com/tesserix/helivanta/issues/1689).
The IGST implementation itself is deployed as `16eab119` with migration 0072.

## Decision and ownership

Keep general pharmacy retail unreleased. Permit an explicitly approved,
time-limited pilot for Yes Hospital so its authorized users can exercise the
actual deployed Global API and UI. This is a release-availability restriction,
not a permission grant, an impersonation mechanism, a medication authority or a
claim of regulatory readiness. Other tenant accounts remain blocked.

Application owners retain authorization and records. Global API remains the
entry point. Console owns audited provider changes; GitOps owns the pilot policy.
The canonical release contract owns which capabilities can ever participate.
No request header, query parameter, supplied outlet ID or browser flag can
supply pilot membership.

## Three independent gates

1. `contracts/release-capabilities.json` retains `eligible: false` and adds
   `pilot_eligible: true` only to catalogue, stock, prescriptions, returns,
   reports, counter and payables. All other unreleased features stay blocked.
2. The API reads an optional operator-mounted policy at startup through
   `RELEASE_PILOT_POLICY_FILE`. It accepts at most 20 distinct nonzero UUIDs and
   an absolute RFC3339 expiry no more than seven days ahead. Unknown fields,
   duplicate JSON keys, malformed IDs, unreadable files and oversized input
   prevent startup. An absent policy or an expired valid policy enables no pilot.
3. GrowthBook must independently enable each required flag. Dependencies still
   resolve recursively. A missing, invalid or stale provider fails closed.

The service compares the policy against the authenticated principal's tenant.
Policy data is copied on construction. Expiry is checked on every evaluation;
pilot snapshot expiry is capped by the policy expiry. Ordinary onboarding and
released capabilities continue after the pilot expires. This does not bypass
membership, hospital/outlet scope, operating assignments, licensing,
prescription checks, stock rules, idempotency or financial history.

The scope is the approved tenant and its **already authorized members**, not
only the owner. All seven pilot capability areas become eligible there; this is
not an IGST-only endpoint exception. Prescription and financial controls remain
mandatory and no clinical authority is inferred from pilot membership.

## Console behavior and rollout order

Console reads the contract from `release/prod`. Pilot entries show **Pilot only**
and, when enabled with prerequisites, **Pilot enabled**. This describes provider
readiness within the pilot scope; it does not attest that a tenant policy exists
or has not expired. Operators must inspect the approved GitOps policy and the
actual account-bound Helivanta snapshot. The existing revision check, reason,
dependency review, idempotency journal and provider outcome recovery still apply.

Deploy compatible Console/API and catalogue tooling first. Release the Helivanta
candidate with pilot configuration absent. Publish the matching chart catalogue;
reconciliation remains create-only and never changes existing desired values.
Then, following approval of the named policy and exact release revision:

1. Resolve Yes Hospital's tenant ID from the canonical IAM owner and verify the
   target with the signed-in owner. Do not use a hospital, pharmacy or outlet ID
   as the tenant ID. Keep IDs out of user-facing labels.
2. Set `releaseCapabilities.pilot.enabled: true`, `tenantIDs: [<verified UUID>]`
   and `expiresAt: <approved UTC deadline>` in the owning GitOps values. Choose
   a short test window within the seven-day limit; the chart default is disabled.
3. Helm-render and review the read-only policy ConfigMap, API-only mount and
   checksum-triggered rollout. Apply via GitOps, never an imperative pod edit.
4. Enable the seven pilot flags in Console in dependency order, recording #1689
   and the approved test window. Enable counter last; payables additionally
   covers Tally export and remains subject to its own authorization.
5. Wait for the exact release Freight and new API configuration to be verified.
   Check the tenant-bound UI snapshot and server route admission, not only the
   provider's desired value.

Changing/removing the startup policy rolls API replicas. During that rolling
update old instances retain their previous policy until drained; the absolute
expiry remains enforced even if reconciliation stalls. Do not remove the expiry
or weaken authorization to recover a failed test.

## Live acceptance and evidence

Use conspicuously labelled synthetic fixtures only; never use patient care,
real dispensing or real payment activity as test data. Do not invent prescribing
or facility authority to satisfy a guard. If prerequisite evidence is absent,
record the specific missing fixture and use the owner's supported setup flow.

Verify in the signed-in Yes Hospital account:

- Same-state baseline and Maharashtra-to-Odisha recipient-state quote with one
  IGST component; omission preserves the seller-state default.
- Synthetic checkout, persisted invoice and original-component partial/full
  credit; retries do not duplicate stock or financial consequences.
- Daily/item reports and CSV reconciliation; Tally requires explicit IGST ledger
  mapping and produces balanced sale/credit vouchers.
- An account outside the allowlist cannot obtain the pilot in either UI or API;
  spoofed tenant headers do not change this result. Test missing permissions and
  narrower outlet scope using the existing authorization tests and test accounts.
- Provider disable and policy expiry block new pilot work. Existing retained
  evidence and already released recovery operations stay available.

Retain exact revisions, policy deadline, synthetic references, totals and test
outcomes in the issue. Do not put credentials, patient details or private staff
data in public evidence. Counter/credit documents preserve attributable history;
complete synthetic reversals through supported commands instead of deleting
posted stock or financial records.

## End the pilot

Disable counter first in Console, then the remaining pilot provider flags.
Remove/disable the GitOps pilot policy and verify the rollout and account-bound
snapshot. Existing provider flags are preserved by catalogue reconciliation.
Expiry provides an independent server-enforced end even if an operator forgets.
A general release requires a separate reviewed change to global eligibility;
this pilot is not that sign-off.
