# SupplyConnect draft-profile local validation

Validated 16 October 2026 for the profile slice of #1135, #1137 and #1005 under epic #1133. This does not complete those stories or authorise a production migration.

## Commands and results

| Local command                                                                                                            | Observed result                                                                                                                                                                                     |
| ------------------------------------------------------------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GOTOOLCHAIN=go1.27.2 go test -v -race ./... -count=1` from `backend`                                                    | Passed; 874 top-level tests, including architecture, IAM, Pharmacy, Supply and tenant database packages. Disposable PostgreSQL/NATS/OpenFGA connections were forwarded from the local container VM. |
| `GOTOOLCHAIN=go1.27.2 go vet ./...` and `go build ./...` from `backend`                                                  | Passed.                                                                                                                                                                                             |
| `golangci-lint run ./...` from `backend`                                                                                 | Passed, zero issues.                                                                                                                                                                                |
| `pnpm -r type-check`                                                                                                     | Passed, including generated contract drift checks.                                                                                                                                                  |
| `pnpm -r lint`                                                                                                           | Passed.                                                                                                                                                                                             |
| `pnpm -r test`                                                                                                           | Passed: API 135 tests, shared UI 90 tests, shell 224 tests, and the scripts tests.                                                                                                                  |
| `pnpm --filter @helivanta/shell exec vitest run components/supply/parties-workspace.test.tsx` after extending acceptance | Passed, all five Supply UI tests, including auditor read-only access and structured-array identity retention.                                                                                       |
| `pnpm --filter @helivanta/shell build`                                                                                   | Passed; `/supply/parties` included in production output. Existing middleware-to-proxy deprecation remains unrelated to this slice.                                                                  |

The last failure-injection regression also requires an internal outbox constraint failure to return a generic 500 rather than an invalid-input 400. The affected Supply package passed again with race detection after that correction; the full-repository run above preceded this one-line error-classification change.

## API and database evidence

The tests exercise the public HTTP routes against real PostgreSQL and durable NATS consumers. They cover private draft creation/read/update; identical retries; optimistic conflicts; concurrent retries producing one revision, audit mutation and event; revoked grants; cross-tenant 404 responses; private revision pagination; bounded literal-prefix directory search; country-specific declared tax kinds; malformed/oversized input; duplicate component IDs; duplicate business/tax identity rollback; component reordering without fake delete/insert history; immutable revisions; direct RLS even if the general visibility function is widened; and rollback of profile/access/audit/history when outbox writes fail. Event payload assertions allow only opaque references, version and grant/revoke operation.

## Real local browser acceptance

The Next.js UI was exercised against a standalone local API using the same module stack, real disposable PostgreSQL and NATS, and a test-only signed synthetic identity. The temporary authentication fixture and its entry route remain outside tracked source. This is local functional acceptance; it is distinct from production Zitadel/OIDC and external-provider acceptance.

Through the UI, a synthetic Pakistan supplier was created with a contact, registered address and NTN declaration, updated to revision 2, and reloaded from persisted API data. Both revisions were visible. Country and tax dropdowns showed Pakistan-specific choices. The new-profile action focused its form. The navigation expansion control displayed zone labels. Desktop and 390px mobile layouts had no horizontal overflow, and browser error/warning logs were empty. Reserved example domains, fictitious phone values and `TEST-` identifiers were used throughout; no customer rows were copied from legacy.

## Remaining acceptance

Licence/evidence review, premise/warehouse relationships, restricted finance metadata, approval/suspension/offboarding, country statutory rules, complete access administration and transaction-domain workflows remain open. Product drafts cannot be approved through these endpoints. No production schema or rollout was performed by this validation.
