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
service. The staged connector is deployed through the GitOps repository; the routing and owner-boundary rollout stages are tracked separately below. No product API image or database change is part of this connector rollout.

Verify login/MFA, cookie security, renewal, tenant switching, logout revocation,
permissions, wrong-tenant/facility denial, exports and request correlation through
the deployed boundary. Verify exact trusted proxy chains and rate limits at both
layers. Production TLS/workload identity, load/availability and domain completion
remain release checks; local proxy tests cannot establish them.

For rollback, restore the previous compatible route and shell build/runtime
origin together. No data ownership moves in this change. Future extraction after
accepted writes requires its own reconciliation and approved recovery procedure.

## Validation and rollout evidence

Release source: `ede362a567dce01f6eaf31d8598cfe4eb95c4903`, based on current main
with Next.js 16.4.2. Recorded on 2026-10-24:

- `go test -race ./internal/globalapi ./cmd/global-api`, `go vet`, Go build and
  `golangci-lint` for the connector passed. Tests include real registered routes,
  session signature/revocation, cookies, forged identity headers, unsafe paths,
  body bounds, missing products, deadline cancellation and upstream failure.
- Source CI's full Go/coverage job passed, including database-backed tests.
- `make e2e` in CI passed all three production-build browser phases through
  Global API: 54 main journeys, 1 idle-session journey and 1 renewal journey.
- On supported Node 24, `pnpm --filter @helivanta/shell test` passed 827 tests
  across 100 files. Shell production build, typecheck and lint passed.
- `go run ./cmd/contractdocs` and its `-check` passed. The domain inventory is
  unchanged; the connector route-registry tests cover every registered route.
- API and shell image build, vulnerability scan, signing and publication passed.
  The new shell image is not promoted in this rollout.
- GitOps [staging PR #1519](https://github.com/tesserix/tesserix-k8s/pull/1519)
  passed all 14 checks and deployed two ready connector replicas with their
  dedicated service account and native Istio sidecars. Runtime image digest:
  `sha256:5dea64c9593f6f0b601ff8d0f4c1e69401cb291738d312b041a13f0bc84f0269`.
- From the live shell, connector readiness returned 200; protected routes for all
  seven owners rejected missing credentials and forged identity headers with
  401; an unknown product returned 404; exact internal enrolment without workload
  credentials returned 401. Responses included `Via: 1.1 helivanta-global-api`.

Follow [routing PR #1520](https://github.com/tesserix/tesserix-k8s/pull/1520) and
[owner-boundary PR #1521](https://github.com/tesserix/tesserix-k8s/pull/1521) for
activation and public/direct-access verification. The
[GitOps runbook](https://github.com/tesserix/tesserix-k8s/blob/main/docs/helivanta-global-api-rollout.md)
records staged rollout, recovery order, capacity and repeatable acceptance probes.
Do not treat private connector readiness as public cutover evidence.

The Zod advisory `GHSA-mrhx-c7xc-m3w5` is addressed with Zod 4.6.6 and a
compatible form resolver. Collection-bound regression tests cover the actual
pharmacy schema and resolved Expo CLI dependency. Existing UUID wire shapes and
explicit UUIDv7 restrictions are preserved; the advisory is not suppressed.

Routing PR #1520 and owner-boundary PR #1521 are merged and live. The shell's
direct product API request returns 403; the same protected request through Global
API returns 401 with the connector Via header. Public login returns 200.

The unrelated India-only migration preflight found 15 incompatible rows across
eight tables. No data was changed. The restricted preflight role lacks a required
SELECT grant; administrator read-only counts are recorded in the
[release hold](https://github.com/tesserix/kargo-manifests/pull/60).
That PR is merged and live `autoPromotionEnabled: false` was verified on
2026-10-24 before the source merge. Product API, connector and shell promotion now uses the same
source tag in order. Keep production promotion held until data owners approve a
remediation plan and a complete fresh preflight passes. Never bypass migration
guards or delete business records to make a rollout succeed.

Local Docker was unavailable, so database/browser/container validation ran in CI.
These results establish the implemented routing slice, not all clinical workflows,
production load/availability targets, independent service extraction or regulatory
readiness. MediConnect still has no registered backend API. The native
AmbulanceConnect app currently uses bundled emergency-call data; future
authenticated mobile capabilities must use this same Global API boundary.
