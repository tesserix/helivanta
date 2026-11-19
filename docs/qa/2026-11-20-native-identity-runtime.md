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
