# ADR-0039: Reviewed privacy case deadlines

Status: API/schema deployed on 2026-10-25; follows ADR-0038. Worker activation
remains pending; no tenant deadline policy was activated.

Casework needs durable accountability without treating an elapsed deadline as
permission to disclose or erase records. Tenant-reviewed service deadlines have
no seeded statutory duration. Policies are immutable, explicitly activated and
pinned at intake. Existing cases are not silently reinterpreted by a new policy.
Policy proposals require an explicit true/false pause-on-information choice; omission
or null is rejected rather than choosing a default for the tenant.
No policy means deadline information is explicitly unavailable, not zero/overdue.

Keep the case owner in the modular backend. A separate restricted Temporal worker
handles only deadline metadata and escalation evidence, not descriptions, contact
information, clinical records or case responses. The existing API image carries
the worker binary so schema/API/worker source versions remain one artifact. The
worker has its own database role and task queue. Unconfigured workflow support
refuses policy activation; an outage after intake retains the committed outbox
intent and is visible as lag/unavailable monitoring, never successful delivery.

Sizing assumptions: up to 10 intakes/s peak, <512-byte workflow/event payload,
100k/300k cases at 12/36 months; 99.9% API and p99 <300ms design targets remain
unmeasured targets. No network call is made inside a database transaction.
Postgres owns due times, pause intervals, version and escalation receipts;
Temporal owns durable waits/retries. Notifications will have separate provider
receipts; emitting an event does not mean a message was sent.

A case's human status and deadline metadata remain separate. Acknowledgement is
recorded at review start. A reviewed pause-on-information policy pauses only the
resolution clock; replies retain elapsed pause and extend the due time. Terminal
cases stop timers. Timer activities recheck committed state and database time,
so stale signals, early timers and duplicate delivery cannot escalate a terminal
or already-escalated case. Continue-as-new bounds history. All read/write outcomes
are audited and use the same verified tenant/case scope.

A current accepted escalation officer is explicit tenant configuration, not an
implicit admin or a cross-tenant platform role. Missing/expired recipients remain
visible as unresolved routing, never auto-resolved cases. Public/proxy and
platform-wide oversight require separately verified authority.

Use additive owner migrations and deny unsupported old-worker operations. Rollback
must preserve cases, policy and deadline evidence; pending workflows need compatible
worker code until drained. Do not enable policies before worker readiness, exact
source/digest verification and failure/replay/time-skipping tests. A browser timer
or periodic in-memory sweep was rejected because neither survives process loss.

## Development verification checkpoint — 2026-10-25

Durable clock persistence, atomic outbox writes, restricted database activities,
Temporal start/signal consumption, expiry routing receipts and the worker binary
are implemented. Real PostgreSQL/JetStream/Temporal acceptance verifies delivery,
worker restart, terminal transition and duplicate replay without a new execution.
Targeted privacy and pharmacy race suites, worker/configuration tests, full backend
lint/build and generated-contract drift checks pass. An isolated rehearsal of
the previously captured production schema also passed upgrade, idempotent
reapplication and previous-migrator compatibility while preserving synthetic
account data and forced RLS. The full backend race/coverage gate, PR CI and
post-merge CI passed. Production API/schema verification is recorded in the
[MediConnect delivery ledger](../delivery/mediconnect.md#deadline-release-verification--2026-10-25);
the separate worker activation is still pending.

The earlier pharmacy 400-versus-404 failure has not recurred in isolated repeats
or the latest complete pharmacy package run. Its assertion now includes the
actual response body for diagnosis. No production pharmacy behavior was changed
and a root cause is not claimed. Infrastructure tests cover dedicated credentials,
create-only initialization, worker probes and combined namespace network policies.

Clinical/proxy consent, owner-authorized disclosure, booking/reminders, public
intake, provider-confirmed notifications and approved notice content remain
separate stories. No statutory policy or notice is seeded by this implementation.
