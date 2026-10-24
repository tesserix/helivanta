# Running and releasing the Global API

The executable `backend/cmd/global-api` connects the existing UI to the existing
product modules. Read [the contract](../api/global-connector.md) before adding APIs.

## Local development

`make up` / `make dev` starts backend :8080, Global API :8081 and UI :4301.
For separate terminals, start `make dev-api`, `make dev-global-api`, then
`make dev-web` after the existing infrastructure/bootstrap setup. `make down`
includes the connector port. `HELIVANTA_GLOBAL_API_PORT` changes its port;
`API_URL` must point to that origin for the shell. Existing custom `API_URL`
values must be updated explicitly; they are not silently overridden.

```sh
curl http://localhost:8081/healthz
curl -i http://localhost:8081/v1/pharmacy/medications
```

Without credentials the second call must return the owning API's 401. The UI
continues using `/api/v1/*`; login, tenant changes, renewal and logout traverse
the same connector. Its `/healthz` reports process health; `/readyz` additionally checks platform
readiness when configured, without making every product a global dependency. Downstream failures are returned per request.

The production-build E2E harness routes main, idle and renewal stacks through
ports 8081, 8199 and 8198 respectively. Overrides are
`HELIVANTA_GLOBAL_API_PORT`, `HELIVANTA_IDLE_GLOBAL_API_PORT` and
`HELIVANTA_RENEWAL_GLOBAL_API_PORT`. Caddy and shell builds use the same values.

## Configuration

| Variable                         | Contract                                                                                  |
| -------------------------------- | ----------------------------------------------------------------------------------------- |
| `GLOBAL_API_PORT`                | Listener port, default 8081                                                               |
| `GLOBAL_API_UPSTREAMS`           | Explicit JSON object of product owner keys to HTTPS origins; required outside development |
| `HELIVANTA_ENV=dev`              | Allows loopback HTTP and local monolith defaults; never use in production                 |
| `GLOBAL_API_UPSTREAM_URL`        | Development-only monolith origin used when the explicit map is absent                     |
| `GLOBAL_API_TRUSTED_PROXY_CIDRS` | Comma-separated exact proxy CIDRs, or `none`; explicit value required outside development |
| Shell `API_URL`                  | Global API origin, set identically at build and runtime                                   |

Example configuration shape, with illustrative DNS only:

```json
{
  "platform": "https://platform.internal.example",
  "medicore": "https://medicore.internal.example",
  "workforce": "https://medicore.internal.example",
  "pharmacy": "https://pharmacy.internal.example",
  "supply": "https://supply.internal.example",
  "lab": "https://lab.internal.example",
  "emergency": "https://emergency.internal.example"
}
```

Each destination must implement the current path, authentication and contract;
no placeholder API is supplied. An omitted product returns 503. Unknown keys or
unsafe origins refuse startup. Do not put URL credentials in configuration.
Requests are limited to 16 MiB, 30 seconds, 32 concurrent requests per product and 16 total in-flight
requests per replica to bound buffered-body memory.
Long-running domain work requires the owner's asynchronous operation contract.

Set `GLOBAL_API_TRANSPORT_SECURITY=istio` only with STRICT mesh mTLS and
workload policies. This permits HTTP solely to Kubernetes service DNS names;
the default remains HTTPS with certificate verification. The Global API owns no
identity-provider or application-secret credentials; existing Zitadel/session
authority stays at the product owner.

## Production handoff

`Dockerfile.api` includes `/app/global-api`; deploy it as a separate workload
using that command. The existing API entrypoint remains unchanged. The owning
`tesserix-k8s` GitOps repository must supply the workload/service, encrypted
upstreams, restricted ingress/egress and trusted proxy configuration. Set resource
requests/limits, replicas, probes, termination grace above 35 seconds, an outage
alert and approved capacity evidence. Expose no product API directly to clients.

Update the public `/api/v1/*` route and shell `API_URL` together. The image workflow and image build default now target the Global API. Deploy
the connector before promoting a shell built with this destination. This source change does not silently repoint the deployed UI at a missing
service. No live rollout, external repository edit or database migration occurred.

Verify login/MFA, cookie security, renewal, tenant switching, logout revocation,
permissions, wrong-tenant/facility denial, exports and request correlation through
the deployed boundary. Verify exact trusted proxy chains and rate limits at both
layers. Production TLS/workload identity, load/availability and domain completion
remain release checks; local proxy tests cannot establish them.

For rollback, restore the previous compatible route and shell build/runtime
origin together. No data ownership moves in this change. Future extraction after
accepted writes requires its own reconciliation and approved recovery procedure.

## Validation recorded for this change

Executed locally on 2026-10-24; test credentials and smoke requests were synthetic.

- `go test -race ./internal/globalapi ./cmd/global-api`: passed. Includes actual
  registry coverage, real session signature/revocation middleware through the
  proxy, cookie preservation, timeout cancellation, unsafe paths/configuration,
  forged headers and body bounds. These are transport/auth tests, not full domain
  persistence or OpenFGA availability tests.
- `go vet ./internal/globalapi ./cmd/global-api`,
  `go build -o /tmp/helivanta-global-api ./cmd/global-api`, and
  `golangci-lint run ./internal/globalapi/... ./cmd/global-api/...`: passed.
- `go run ./cmd/contractdocs` and `go run ./cmd/contractdocs -check`: passed;
  existing generated domain inventory did not change.
- On Node 24, `pnpm --filter @helivanta/shell test`: 634 tests passed across 93
  files. Additional focused Global API configuration, rewrite and logout checks
  passed. The initial Node 26 run was stopped and replaced by the supported run.
- `pnpm --filter @helivanta/shell type-check` and
  `pnpm --filter @helivanta/shell lint`: passed.
- `bash scripts/preflight.test.sh`: 103 checks passed. Changed shell scripts also
  passed `bash -n` and `shellcheck`; authored Markdown links and `git diff --check`
  passed. New behavior tests were first run before implementation and failed for
  the missing Go/TypeScript entry points, then passed after implementation.
- An actual local Next development server and the compiled Global API process
  were started on isolated ports and stopped after the check. Both login and
  pharmacy UI URLs reached Global API and returned its expected 502 when their
  configured downstream was absent. This proves the connection and honest failure
  path, not successful clinical persistence.

`pnpm --filter @helivanta/shell build` compiled, typechecked and generated pages,
then exited nonzero with Next.js `SecurityFatalError` reporting a known
vulnerability in the installed Next.js version. Upgrade to a reviewed fixed
version and rerun the build before release; the warning was not bypassed.

The default local Docker socket is unavailable. Full database-backed API tests,
three-stack Playwright journeys, container build and live rollout were not run.
No production or independent product-service readiness is claimed. The native
AmbulanceConnect app currently uses bundled emergency-call data without an API;
future authenticated mobile capabilities must use this same Global API boundary.

The release checkout is based on current main with Next.js 16.4.2. Its production
build passes, resolving the earlier checkout's Next.js security blocker. Earlier
validation records above describe the original snapshot, not the release result.
