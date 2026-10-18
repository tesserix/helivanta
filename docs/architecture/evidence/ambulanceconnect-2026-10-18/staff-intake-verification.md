# EmergencyConnect staff intake verification

18 October 2026. Implementation #1227; programme #1220.
Application branch: `feat/122-ambulanceconnect`. Infrastructure branch:
`feat/helivanta-emergency-transit`. Both remain local and uncommitted.
No production migration, policy change, workload rollout or clinical activation
was performed. All incident data in tests/screenshots is synthetic.

## Evidence

| Check                                                                                                                 | Observed result                                                                                                                                                                                                                       |
| --------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Full frontend gate, `pnpm turbo lint type-check test build format:check --concurrency=1`                              | 29/29 passed under Node 24 with public OIDC placeholders; shell 507, API 284, UI 93, shared emergency 9, native 5 tests                                                                                                               |
| Final module / architecture check, `go -C backend test -race -cover ./internal/modules/emergency ./internal/archtest` | Passed; emergency 77.5% coverage; includes the final database timeline guard                                                                                                                                                          |
| Whole backend `GOFLAGS=-p=2 ./scripts/coverage-gate.sh`                                                               | Passed: full race suite and 70% per-package gate; final emergency module rerun also passed after the last integrity guard                                                                                                             |
| `make lint-go`                                                                                                        | 0 issues; includes gofmt                                                                                                                                                                                                              |
| `go -C backend vet ./...`, `go -C backend build ./cmd/...`                                                            | Passed; final rerun after database integrity guard is recorded in local logs                                                                                                                                                          |
| `make lint-shell`                                                                                                     | Clean over all 11 discovered shell scripts                                                                                                                                                                                            |
| `go -C backend test -tags=emergencybrowser ./internal/modules/emergency -run TestEmergencyConsoleBrowser -count=1 -v` | Three Playwright tests passed against a production-built local shell: signed-out call access with private API blocked, exact public route protections, and mobile staff setup → grant → receive → detail → close → revoke → 404       |
| `go -C backend test -tags=emergencyplans ./internal/modules/emergency -run TestQueueAndExpiryAccessPaths -count=1 -v` | Passed against 100,000 counted synthetic cases; queue index 51 rows in 2.634 ms, expiry index 200 rows in 6.821 ms in this local run                                                                                                  |
| Infrastructure `pytest` for Transit and contact retention                                                             | 11 passed; real Helm-rendered resources, preserved staff policy, exact emergency paths, namespace/identity boundary, suspended schedule, separate service labels, GAR image, bounded job, no schema-owner/token/key credential, alert |
| `helm lint charts/apps/helivanta-api charts/thirdparty/openbao`                                                       | Two charts passed                                                                                                                                                                                                                     |
| Ruff check on changed infrastructure tests                                                                            | Passed                                                                                                                                                                                                                                |
| `git diff --check`                                                                                                    | Passed in both worktrees                                                                                                                                                                                                              |
| GitHub identity / repository visibility                                                                               | `Sam123ben`; both Helivanta and tesserix-k8s PUBLIC                                                                                                                                                                                   |

The whole-backend first run caught the missing IAM labels for the new roles;
these were added and both role-catalogue assertions then passed. That run also
reported an existing test-container subprocess cleanup failure. Its unchanged
standalone race suite subsequently passed in 15.484 seconds; final validation uses
bounded package concurrency without weakening assertions or internal race tests.

The first frontend run used unsupported Node 26 and produced storage-related
failures in existing API tests. Verification uses the required Node 24. An
initial build omitted required public OIDC settings; the recorded final build
uses the checked-in example's valid-shaped placeholders. Concurrent heavy checks
also caused existing long interaction tests to time out; the final frontend gate
runs tasks sequentially with the same assertions and time limits.

The first browser attempt reached a stale preview process with missing runtime
OIDC settings and returned 500 before loading a form. The preview was rebuilt and
restarted with example settings. A later test captured the workspace URL before
client navigation completed; it was corrected to await the case URL, then verified
revocation against that exact protected case. Final browser tests run with no
retries. No emergency dial action was clicked.

The final browser recheck encountered a failed default Colima host-socket forward.
Docker remained healthy inside the existing VM. A temporary local SSH Unix-socket
forward to that daemon allowed all three tests to pass without a VM/container
restart. The forward was removed after verification; the default host socket
requires local environment repair if it remains unreachable. No production
configuration or default Docker context was changed.

## Security and failure assertions

Real Postgres, OpenBao and NATS verify:

- Deliberately reusing a client case ID in an independent base does not reveal a private case through a uniqueness conflict; both records and encryption contexts remain base-scoped.
- Tenant/base scope is mandatory, including for tenant administrators; current
  grant revocation affects subsequent reads and writes.
- Plain caller/incident/closure content is absent from persisted case envelopes,
  queue responses, immutable timeline and events. Audited detail decrypts only
  with its tenant/base/case/field context; unrelated key paths and wrong context
  fail closed.
- Eight concurrent identical intake requests commit one case, received timeline
  and outbox event. A divergent same-ID report returns 409.
- Closure needs both the route permission and live supervisor base grant. Wrong
  versions and changed replay content conflict; identical replay does not add
  another transition. Review explanation is encrypted.
- Timeline/audit changes and clinical case removal are rejected by SQL guards.
  Inserting a fictitious future timeline entry was observed succeeding (red test),
  then rejected after adding a current-case version/state/reason database guard.
- Expired caller contact is hidden even before maintenance; system maintenance
  purges once with an audit in the same transaction and retains incident/history.
  Request credentials were observed able to purge an expired envelope (red test),
  then denied after restricting that transition to the operational system role.
- Nil/missing system pool and invalid batch limits fail. Nil/unavailable Transit
  refuses case creation without writing plaintext. Malformed/extra/oversized
  input, invalid service/basis/phone/control data and unauthorized grants fail.
- Browser retries preserve the original report/ID after an ambiguous failure;
  definite validation rejection allows correction rather than replaying rejected
  content. Revoke confirmation handles an actual empty HTTP 204 response.
- The maintenance pod was initially selected by the API Service (red render
  assertion); distinct labels now prevent routing API traffic to the job.

Observed red logs include `/tmp/helivanta-emergency-{base-red,intake-red,close-red,retention-red,purge-boundary-red,forged-history-red,correction-red,cron-service-red}.log`.
Final logs use `/tmp/helivanta-emergency-{web-final,backend-final,browser-final,integrity-final,go-lint-final,go-vet-final,go-build-final,infra-green,helm-lint}.log`.

## Limits and release boundary

The browser fixture substitutes **synthetic identity and local API routing**.
Domain responses come from the real Go handlers and disposable services. This is
not a production Zitadel/session/proxy end-to-end test. Backend architecture tests
separately exercise real OpenFGA permission resolution. The full unrelated
clinical, bulk, idle-timeout and renewal browser suites were not run here.

The scale fixture uses representative metadata queue columns and an expiry selector under application RLS, not the privileged purge update or a full HTTP load. PostgreSQL chose the `(tenant_id,base_id,created_at,id)` queue index and `(contact_retain_until,tenant_id,base_id,id)` partial expiry index, without a case-table sequential scan. Batched fixture commits retain all real RLS, foreign-key and timeline checks and allow ordinary autovacuum statistics maintenance; the benchmark receives no schema-owner access. An earlier single-transaction fixture was interrupted after cold statistics selected the queue index for repeated point lookups and made fixture creation slow. No index hints or constraint disabling were used.

The module's 10-create/s, p99-under-one-second target remains a planning target,
not a completed load test or emergency response SLO. Native target/screen-reader,
regional-language, signed app/store, operator/medical lead, consent/legal,
clinical retention/backup restore and operational drill evidence remain launch
gates. No SOC 2, HIPAA or DPDP compliance certification is implied.

Fleet/crew readiness, actual reservation/dispatch, citizen live booking/status,
receiving acceptance, telemetry/offline handover and CRM are separate open
programme deliverables. A `received / untriaged` record promises none of those.
The new schema has only run against disposable databases; migration and rollout
require separate specific approval. Never delete the clinical tables as rollback.

Mobile staff screenshot: [staff-mobile.png](staff-mobile.png).
Operations: [guide](../../emergencyconnect-operations.md).
