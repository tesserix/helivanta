# ADR-0072: Release availability is independent of staff authority

Date: 2026-11-16. Status: accepted for incremental implementation; application activation pending.

## Context

Owners currently see many permission-eligible Staff and product screens. Those
permissions do not establish that a workflow is accepted for the first release.
Mahesh's pharmacy plans define a broad retail acceptance scope; their blank
sign-off record cannot establish operational readiness. The user requests
consistent onboarding, Staff and PharmaConnect release slices, using GrowthBook
and exact image pins with Kargo platform-tool upgrade automation.

## Decision

Use the existing shared self-hosted GrowthBook with an isolated Helivanta
project/environment. A canonical manifest binds known release groups to API
methods, pages, panels, dependencies and evidence. The backend evaluates trusted
context locally and applies release admission in addition to existing identity,
permission and object-scope admission. The UI receives only an account-bound
effective snapshot. Provider uncertainty closes new gated work; essential
identity/recovery and explicitly classified authorized continuations remain
available. Flags never bypass professional policy, independent review or audit.

Deliver R1 owner onboarding, team/staff and pharmacy department setup first;
activate retail only after Mahesh's acceptance and required reviewers pass.
Other modules and advanced Staff operations start off. Do not infer global
release readiness from any individual merged/deployed fix.

GrowthBook's image is version/digest pinned through a dedicated lane in the
existing infra platform-tools Kargo project, using protected-main-compatible PR
write-back. The existing bundled stage is failing, so it cannot be assumed to
provide working automation. Automate stable supported patch update discovery and
verified promotion; stop for failed verification or unapproved irreversible
schema/major upgrades. Shared provider data and other products are preserved.

## Alternatives

UI-only hiding leaves APIs open. Permission changes conflate authority with
release status. Independent browser evaluation drifts across tenants and stale
sessions. A separate flag service duplicates existing infrastructure. Floating
tags cannot provide reproducible upgrade/recovery evidence. All are rejected.

## Consequences

Every API/UI surface needs a classification and CI parity checks. Core Staff
must retain required reviewer workflows. Retail activation needs safe completion
and recovery semantics before entry is enabled. The shared GrowthBook footprint
is reused; no new data service is proposed. Provider isolation, installed-edition
support, backup/restore and SDK compatibility remain verification gates.

See the [design](../superpowers/specs/2026-11-16-first-release-capabilities-design.md)
and [implementation plan](../superpowers/plans/2026-11-16-first-release-capabilities.md)
for contracts, performance/freshness targets, failure behavior and acceptance.

## Implementation evidence (2026-11-16)

The dedicated GrowthBook infrastructure lane is merged in
[tesserix-k8s #1678](https://github.com/tesserix/tesserix-k8s/pull/1678) and
[kargo-manifests #74](https://github.com/tesserix/kargo-manifests/pull/74).
Read-only inspection found GrowthBook Healthy/Synced on `5.2.0` at digest
`sha256:74844e961f4fb65785ddb351bb2417a33a9cbcf0b1aa73774709aee986324afb`;
`kargo-infra/growthbook-prod` reports Healthy/Ready, verified Freight
`8006e0409c0b829cfebbad10e9366273a67b52d3`. The separate legacy bundled
platform-tools stage still fails on a missing sealed-secrets path; this does
not establish its repair.

Backend admission and browser page/navigation/signup candidates are implemented.
The [API contract](../api/release-capabilities.md) records remaining nested-panel,
provider isolation, configuration and live acceptance gates. Infrastructure health
does not mean Helivanta capabilities are configured, accepted or enabled.

## Proposed scoped acceptance pilot

The #1689 candidate adds an independently compiled `pilot_eligible` designation
without changing global eligibility or the membership/permission boundary.
Activation additionally needs a named tenant/deadline policy and provider
approval, with the same dependencies and product guards. This permits synthetic
live acceptance without a general retail release. It is disabled by default and
is not the Mahesh/general-release sign-off described above. See the
[design, limits and rollout evidence](../design/1689-qa-retail-pilot.md). No pilot
is activated by this ADR addendum.
