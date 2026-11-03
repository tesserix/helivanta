# Recoverable posting rejection and withdrawal — 2026-11-04

PR [#1547](https://github.com/tesserix/helivanta/pull/1547) is merged and deployed
as `e7588e65c7507144c361fb34b6c866ac88a6a07b`. An independent reviewer can reject
a pending posting proposal; its original submitter can withdraw it. Both require
a rationale, source-examination attestation, current protected context and server
reauthorization. Uncertain outcomes retain an immutable command for exact retry
or receipt recovery. A missing receipt independently rechecks protected context;
revoked access clears private controls. Account/tenant changes abort the client
request and clear private state; an aborted request may already have committed.

## Verified rollout

- All required exact-head checks passed before the normal admin merge. GitHub
  initially refused merging while required checks were running; no ruleset was
  altered and auto-merge was not enabled.
- The [merged image workflow](https://github.com/tesserix/helivanta/actions/runs/41736641888)
  passed API/shell builds, scans, SBOMs, signing and deploy-branch advancement.
- Kargo Freight `30bf8bac6ab5e9a834897995494db74d8bf32535`; Promotion
  `prod.01m6hv877kavh7g8drw4w4z5s7.30bf8ba` Succeeded through the unchanged Stage
  template. No imperative Deployment image patch was used.
- The merged-image migration hook `helivanta-api-migrate-wsbpq` exited 0. No
  migration was added: the complete sorted ledger is byte-identical, 223→223.
- Seven application deployments are updated/ready/available at 2/2. All 14
  application containers match signed image digests and have zero restarts.
  API, Global API and shell Argo applications are Synced/Healthy.
- Public landing/login GETs return 200. Unauthenticated posting review discovery
  returns 401 through Global API. This is an authentication-negative smoke check,
  not authenticated decision acceptance.

The verified boundary was account `unidevidp@gmail.com`, project
`tesseracthub-480811`, context
`gke_tesseracthub-480811_asia-south1_tesseract-prod-in-gke`, application namespace
`helivanta`, Kargo stage `kargo-helivanta/prod`. The repository remained PUBLIC.
Replica counts and infrastructure services are unchanged.

## Scope and remaining acceptance

Local validation for the released source passed nine reviewer checks, 1,440 shell
tests, 633 SDK tests, types, lint, format and webpack builds. Backend/contracts
were byte-identical to the previously race-tested #1544 source; this slice adds
no API, schema, OpenFGA model or permission change. Full Staff acceptance is not
inferred from these tests or a healthy rollout.

Normal real-account browser acceptance remains at owner sign-in. Automatic safety
review previously rejected Terminal credential-entry setup; no alternative route
around that rejection was used. The local fixture preserves its existing QA
identities and approved assignments; new role grants were not fabricated.

Normal proposal submission UI is the next implementation slice. Positive
operating approval remains blocked by `posting_supervision_required`. Supervision,
transfers, contractors, suspension, exit/rehire and departmental/Pharma operating
consumers remain open. Staff Management remains **10 partial, 10 awaiting audit,
1 research, 0 fully accepted**.

See the [machine record](2026-11-04-posting-decisions.json) and
[Staff tracker](../../qa/staff-management-progress.md). Private captures are under
`~/.helivanta/staff-management-20261027/posting-decisions-release-20261104/`.
