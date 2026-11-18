# R0 safe error references — deployed acceptance

Issue: [#1706](https://github.com/tesserix/helivanta/issues/1706).
Application PR: [#1742](https://github.com/tesserix/helivanta/pull/1742).
Evidence was collected on 18 November 2026 UTC (19 November in Melbourne).

## Release and checks

- Application revision: `1cfae666ca4d31e003fe3e82709cafe5bfbe748e`.
- All required [PR checks](https://github.com/tesserix/helivanta/actions/runs/44287393317)
  passed, including Go race/coverage, Web and E2E. Hosted Web exercised the real
  production boundary, collector, log correlation and retry, plus startup
  unhandled-rejection reporting.
- Local Shell suite: 2,140 tests across 238 files passed. Lint, formatting,
  types, offline contractdocs generation/check and applicable inventory checks
  passed. One existing signup-form hook warning remains.
- The exact merged tree, including the independently checked dependency update,
  passed a local production build and all 55 focused regressions. Task-generated
  build artifacts were removed. No local application or container was started.
- [Main image publication](https://github.com/tesserix/helivanta/actions/runs/44305512454)
  and the normal reviewer-approved [release](https://github.com/tesserix/helivanta/actions/runs/44306332078)
  succeeded. No approval or signature bypass was used.
- Freight: `4f2265332aa6e3edd44ea13da9124152a1f52d08`.
- Promotion: `prod.01m7qb6rmxe4yak6x86q9qbqxs.4f22653`, succeeded.
- API digest: `sha256:7472e500d4c8b7f3e9fa74994babc582ef2db87007efee70b746ed3f76dba1cf`.
- Shell digest: `sha256:e0f8e991180c25243db4c75a4e2ada8af9b95720e618e5ac400f8834bc249c1f`.
- API, Global API and Shell each reached two updated/ready replicas on those
  exact revision/digests; all three Argo Applications were Synced/Healthy.
  The migration job completed and logged `migrations applied`.

## Live browser and report evidence

A temporary browser-only listbox rendering fault was installed in a separate
Chrome tab on the public ambulance page. It created no emergency call, identity,
permission grant or business record.

- The real boundary showed generic recovery copy, an opaque support reference
  and Try again. The synthetic private marker was absent from the page.
- Reference `HV-b1332edd-4b54-4a92-8f23-bd197ce9b985` appeared in both the screen
  and the actual `frontend_error` Shell pod log. The browser's resource timing
  showed collector HTTP 204. The record contained only the fixed severity/event
  fields and canonical version/reference/source/kind/area fields.
- The fault was removed and its cleanup function was confirmed absent. Try again
  restored the normal page; opening and closing the country selector then worked.
- A separate synthetic runtime error produced
  `HV-be4e65fe-30a9-4886-8f64-8bc343b0bacd`, source `runtime`, kind `TypeError`.
  Its collector status was 204 and the same safe record appeared in the pod log.
- A synthetic unhandled rejection produced
  `HV-741fc84e-cb3e-4376-a566-05cf27bbc4f5` in the sanitised browser record.
  Its request hit the client deadline (resource status 0) and did not appear in
  the pod log. The page remained usable. This verified the documented browser
  fallback; it is not claimed as successful server delivery.

## Operational limits

The deployed cluster has GKE `loggingService: none`. The existing OTel filelog
receiver excludes Helivanta. These reports were verified in operator-readable
Kubernetes pod logs, **not** Cloud Logging or ClickHouse. Pod logs can rotate or
be removed; this release provides no durable delivery or retention guarantee.
Central monitoring/alert delivery and India retention/residency remain #1700 and
#1701. See the corrected [support runbook](../../runbooks/frontend-errors.md).

Root/workspace/global boundary unit tests and hosted acceptance supplement the
live root/runtime checks; this record does not claim every boundary was manually
faulted in production. Both repositories remained public.
