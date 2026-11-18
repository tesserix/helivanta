# Frontend error reports

Shell owns `POST /api/client-errors`, an anonymous operational endpoint needed
before sign-in. It reads no business records, grants no authority and accepts no
user or tenant assertion. Business APIs continue through the Global API.

## Canonical schema

[`frontend-errors-contract.ts`](../../apps/shell/lib/frontend-errors-contract.ts)
is the single Zod source imported by browser and collector. Version 1 is strict:

| Field       | Accepted value                                                  |
| ----------- | --------------------------------------------------------------- |
| `version`   | `1`                                                             |
| `reference` | `HV-` followed by a lowercase UUIDv4                            |
| `source`    | `root`, `workspace`, `global`, `runtime`, `unhandled_rejection` |
| `kind`      | Fixed error-class allowlist; otherwise `unknown`                |
| `area`      | Fixed product-area allowlist; otherwise `other`                 |

No error message, stack, arbitrary error name, URL, query, credential, identity,
request body or header is included in the report. Unknown keys are rejected.
Reports are untrusted client observations, not authenticated audit evidence.

## Request and response

Use JSON with an exact Origin matching Host. Non-same-origin Fetch Metadata is
rejected when present. Production requires HTTPS except loopback. Forwarding
headers convey no authority. The browser uses CORS fetch mode to preserve Origin,
omits credentials and referrers, and aborts after 1.5 seconds. The collector
rejects query strings, limits bodies to 1 KiB and reads to two seconds.

| Status | Meaning                                                                    |
| ------ | -------------------------------------------------------------------------- |
| 204    | Logged, or identical reference already accepted within this process window |
| 400    | Invalid JSON, UTF-8, schema, content length or query                       |
| 403    | Origin/Host/Fetch Metadata refused                                         |
| 408    | Body read exceeded its deadline                                            |
| 409    | Reference reused with a different valid payload                            |
| 413    | Body exceeds limit                                                         |
| 415    | Content type is not JSON                                                   |
| 429    | Process capacity exceeded; includes Retry-After and rate-limit headers     |
| 503    | Collector could not write its log                                          |

Responses use `Cache-Control: no-store` and never reflect rejected input. No CORS
permission is granted to other origins. The exact route is public in middleware;
neighbouring routes retain their authentication checks.

## Delivery and failure semantics

The same Error object shares a reference and delivery attempt across observers.
Browser network attempts are capped at five/minute. Collector attempts after
origin/media checks are capped at 60/minute/process. Its reference map is bounded
by that window. Duplicate observations across replicas/restarts can be logged;
there is no durable delivery or global exactly-once guarantee. No automatic
network retry occurs. Offline/rate-limited delivery retains the sanitised browser
log reference; when neither sink works the UI says reference unavailable.

Root, workspace and global-layout screens show a generic message and retry.
Global runtime errors and unhandled rejections use the same reporter. See the
[operator runbook](../runbooks/frontend-errors.md) for correlation and acceptance.
