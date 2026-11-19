# Native identity runtime baseline

Status: candidate CI alignment for #1703; no live identity configuration change.

Read-only discovery on 19 November 2026 UTC used the existing authenticated
`tesseracthub-480811` GKE context. The `zitadel/zitadel` Deployment had three Ready
replicas. Its executable reported:

```text
zitadel version v4.19.5+tesserix.29986e1
```

The running image is the Tesserix-patched build at digest
`sha256:22ab1fb4abe6a5f279c5509fa2bbb10358a2fc9fb479af013235add8af1188ac`.
The separate production login Deployment had two Ready replicas and its own
patched image. No rollout, policy update, factor change or credential read was
performed during this discovery; only the core version command was executed.

The isolated Compose core and login fixtures now pin upstream `v4.19.5`, replacing
`v4.15.3`. The [upstream release](https://github.com/zitadel/zitadel/releases/tag/v4.19.5)
and its pinned login Dockerfile were checked. The upstream login image retains
the baked healthcheck used by the preflight guard.

This matches the deployed core's upstream version, not its exact patchset. Passing
these CI cases cannot replace deployed browser/API acceptance against the real
patched core and login services. Older `v4.15.3` run results remain evidence for
that older fixture; they are not current-provider release acceptance.

The pinned `v4.19.5` `cmd/defaults.yaml` still uses 12h access-token lifetimes at
both system-default and first-instance configuration layers. Both isolated
fixture settings explicitly use 1h, preserving the product credential client's
existing one-hour maximum. The public-client fixture compatibility regression
exercises the configured lifetimes through the real credential client. Existing
persistent development instances retain their initialized native policy; the
settings apply to the clean CI instance. No local application, database or
container was started to validate this change.

The deployed ConfigMap's first-instance lifetime was read as 1h. This is config
presence, not proof of the instance's current effective policy or native machine
permissions. The live recovery flow must establish those during approved QA.

Before release, require the current candidate's native password reset/new-password
login, hospital and independent-pharmacy factor reset/re-enrollment/login,
lockout guidance, full required Go/web gates and actual deployed acceptance.
Keep the recovery capability disabled and #1703 open until those gates pass.

## Current-provider hosted evidence

At combined HMS revision `629b97715d400d355e72f5731c221083e79db0b9`, run
`44555086918`, E2E job `135669112951` passed all three phases: 109 main
tests passed, one configured live security-key test skipped, then one idle and
one renewal test passed. The mandatory native hospital and independent-pharmacy
recovery cases are selected by the `native-recovery` project dependency and have
no skip path. Native password reset/new-password sign-in is also selected.
This establishes isolated upstream v4.19.5 acceptance at that revision, not live
patched-provider acceptance or a full Go check result.

The password-only parent `e26e46a9`, run `44553331011`, failed three ordinary
pharmacy cases: licence evidence timed out, a report setup received
`identity_unavailable`, and stock reconciliation could not finish sign-in.
Its result was 101 passed, one configured skip and two cases not run.
Retained failure traces contain 21 API 503 responses taking approximately
758–800 ms. The owner organisation verifier has a shared 750 ms source deadline;
provider logs include cancelled queries. These observations identify the failed
scope-proof path but do not establish which source or resource constraint caused
the latency. The combined branch's passing run does not erase that evidence.

The CI diagnostics filter now retains the owner's existing fixed scope phases,
integer elapsed milliseconds in the range 0–60,000 and boolean parent-context
cancellation/deadline fields. It continues to discard identity fields, arbitrary
messages, raw upstream errors and payloads. The new regression first failed
because all scope records were dropped, then all six diagnostics tests passed.
No timeout, retry, cache or authorization behavior changed.

Full hosted Go gates, a passing final accepted revision, approved schema/owner/HMS
rollout and deployed browser/API verification remain required. No recovery PR
has been merged or deployed on the strength of this evidence.

## Main integration after hosted acceptance

Main advanced to `b30a8bb83dba053e0d279ae9d8d16632d86d17c1`, including the
credential-freshness prerequisite and pharmacy lot-block rule. The password
parent integration `2d9ae9ec` retains its owner SDK revision and includes those
main changes. The combined Staff branch retains its newer owner SDK and both
recovery implementations. Registry and Staff source inventories were regenerated
from the merged canonical module registrations. The additional pharmacy migration
does not replace the existing IAM migration identifiers.

Local integration validation passed backend builds, identity-client and
organization-identity race suites, affected-package vet, contractdocs generation
and checking, and Staff source-inventory generation and checking. The parent
fixture/inventory selection passed 18 Node tests; the combined diagnostics,
fixture and inventory selection passed 25. These integrations need fresh hosted
checks before release. Necessary main and catalogue integration updates advance
the PR branches through normal CI concurrency. Any older Go run superseded by
those updates remains unproven; only the final accepted revision can satisfy the
full Go release gate.

## Catalogue and live QA readiness

The owning infrastructure candidate now projects this release contract through
the onboarding-service Helm chart, including `staff_mfa_recovery`. Its ConfigMap
regression reproduced the missing definition before
regeneration. The render/schema selection passed 11 Python tests, five native
reconciler tests passed, and Helm lint/render and Ruff checks passed. The rendered
validation artifact was removed. Catalogue parity prevents writes until the
deployed application contract matches; provisioning creates missing flags disabled.
The supported Console activation is product-wide, not a tenant-targeted override.

Read-only Chrome inspection on 19 November 2026 UTC found the existing owner
signed in to Yes Hospital with a display name and the Staff directory available.
QA Staff was active but its account link still awaited independent review by
someone other than its proposer. No access, account link, credential or factor
was changed. This is baseline readiness evidence, not deployed recovery acceptance.

After the isolated v4.19.5 native acceptance above, the candidate now makes
`staff_mfa_recovery` compiled-eligible while preserving its disabled provider
default. The new public-service regression first failed because provider approval
could never make recovery available. After canonical regeneration, the full
release-service race suite passed: provider off still denies, provider approval
permits only with both prerequisites, and either disabled prerequisite denies.
This prepares the real Console-controlled release path; it is not live activation.
Final hosted checks and rollout approval remain mandatory.
