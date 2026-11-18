# R0 safe error reporting — #1706

## Scope and current evidence

Both `apps/shell/app/error.tsx` and `(workspace)/error.tsx` render
`error.message`. The workspace test currently requires that disclosure; it must
change with the new behaviour. Root layout failures have no application-owned
global error screen. `instrumentation-client.ts` configures CSP-compatible Zod
validation but installs no error reporters. The application already owns a
bounded `/api/csp-report` collector that writes structured container logs.

Acceptance: a plain error screen with a support reference, the same reference in
a server or browser log, and frontend reports delivered to existing application
logs without personal data. Root, workspace, global layout, unhandled runtime
errors and rejected promises are covered. This does not implement the wider
Sentry/source-map work in #608 or complete monitoring/retention issues #1700/1701.

## Ownership, threat model and contract

The Shell owns these operational diagnostics and its error UI. This collector
reads no business record, accepts no tenant or account identity, creates no
grant and forwards no credential. It is a web-owned operational endpoint like
the CSP collector, not a direct client-to-product business API bypass. Business
traffic remains on the Global API. One shared Shell Zod contract is used by the
browser and collector; no Go wire model or clinical contract is duplicated.

Raw errors can contain patient/staff data, credentials, SQL, URLs and private
identifiers. Both accidental disclosures and an anonymous adversary submitting
forged or excessive telemetry are in scope. The browser is untrusted. The
collector strictly accepts only version, a generated support reference, fixed
source/kind enums and an allowlisted product area. Messages, stacks, arbitrary
names, URLs, query strings, bodies, cookies and identity assertions are excluded
by construction. Unknown fields are rejected without logging the rejected data.
Client reports are labelled as client observations, never security audit proof.

`POST /api/client-errors` is public because sign-in/root failures can precede a
session. Only same-origin JSON posts are accepted. Validate Origin against the
request host, reject non-same-origin Fetch Metadata when supplied, require HTTPS
outside development/loopback, and ignore forwarding headers as authority. The
browser omits credentials and referrers. Body reading is bounded to 1 KiB and two
seconds, including undeclared/chunked bodies. Responses are non-cacheable; 204
accepts a report, 400 rejects malformed/schema-invalid data, 403 rejects origin,
408 rejects a stalled read, 409 rejects conflicting reuse, 413 rejects oversize,
415 rejects media type, and 429 includes rate-limit/retry headers. No response
reflects raw input.

## Reporting and UI behaviour

Generate an opaque `HV-<UUIDv4>` support reference, independent of any account,
patient, backend resource or raw error digest. The same Error object shares one
report/reference across runtime listeners and error boundaries. Use a WeakMap,
not an unbounded retained error list. Classify error kind and product area only
through fixed allowlists; hostile getters and unknown values degrade to unknown.

Write a fixed-shape browser log, then attempt a same-origin report with a bounded
timeout and no automatic retry. A failed/rate-limited/offline server report keeps
the safe reference correlated to the browser log. If neither logging destination
works, the screen honestly says the support reference is unavailable. Reporting
must never throw into the recovery UI or prevent retry. The reference is prepared
asynchronously after hydration; no random server/client markup mismatch is added.
Do not render a stale reference for a different error.

Use one provider-independent screen for root/workspace/global errors. Retain a
workspace section and h2 inside the existing main landmark; root/global use main
and h1, and global errors supply their own html/body. Keep the plain message,
reference status and retry accessible. The global runtime/rejection listeners
install before application code without weakening CSP or cancelling native
browser error handling.

## Capacity, consistency and failure behaviour

Reuse planning targets of 1,000/3,000 application peak RPS at 12/36 months. At a
0.1% failure assumption this is 1/3 diagnostic events per second across the app;
these are sizing assumptions, not measured error rates. A report is below 1 KiB
with a fixed small log line. Cap browser network reports at five/minute and each
collector process at 60 attempts/minute. Two replicas therefore bound admitted
collector traffic to 120 attempts/minute. Excess failures remain local diagnostic
references. A fixed-window, bounded per-process reference map prevents duplicate
log entries locally and rejects conflicting reference reuse. Replica/restart
duplicates remain possible and correlate by reference; no exactly-once audit or
durable delivery is promised. There is no database, queue or new service.

The user-facing screen/retry does not wait indefinitely for diagnostics. A
collector read times out after two seconds; browser reporting has a short abort
deadline. Collector failure must not fail care workflows. Privacy validation
fails closed by dropping the entire invalid report. Rate limits protect optional
diagnostic capacity, not business authority. Log visibility uses existing GKE
container logging; its actual live delivery must be verified. Retention/residency
remain #1701 and no regulatory readiness is asserted here.

## Alternatives and rollout

Raw error strings/stacks and partial regex masking are rejected: they cannot
prove personal-data exclusion. Browser-only console logging is insufficient for
normal operator visibility, but is the truthful fallback on transport failure.
A new Sentry account/service/secret is unnecessary when existing logs meet this
issue. A telemetry database, durable business saga or product-API extraction is
unnecessary for bounded best-effort diagnostics.

Extract the CSP collector's bounded-body read into a shared web helper and keep
its existing limits/contract, with tests for any added deadline handling. No data
migration or new permission is needed. The normal image/Kargo release ships all
changes together. A code revert restores the prior implementation without data
reconciliation; the collector stores no business state. Volume/cost is bounded
by admission limits, with no new pod or vendor subscription.

Production acceptance uses a browser-only synthetic rendering fault in the
authorised test session, not a production fault endpoint or persistent test flag.
Restore the browser override/reload after observing the safe screen, find the
same reference in the sanitised server log, and verify successful retry/normal
navigation. Do not record real patient information or credentials in evidence.
