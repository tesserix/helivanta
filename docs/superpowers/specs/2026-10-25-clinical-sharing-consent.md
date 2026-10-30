# Purpose-scoped clinical sharing consent

Status: implementation in progress for #60/#552/#556. No real policy, recipient,
notice or consent is seeded. This does not implement treatment/procedure consent,
marketing permission, statutory access, emergencies or ABDM certification.

Patientmaster owns reviewed identity, self-capacity, sharing notices, named
recipient bindings and this consent declaration ledger. MediConnect owns the
current original intake claim, observed through the workload-authenticated HTTP
contract in ADR-0041. Record owners retain control of signed/released versions.
Global API remains the entry point for every patient/recipient/staff API.

## Named facility recipients

Hospital reviewers with a separate permission and a current registrar grant
propose an immutable recipient binding: display name, external organization
reference, exact verified-identity subject, policy/evidence references and explicit
valid_until. A second reviewer confirms identity evidence and approves it; the
proposer and recipient cannot approve their own binding. Revocation is terminal.
This is an authorization binding to a reviewed representative, not a duplicate
facility/licence master or automatic certification of an external organization.
The approved organization reference and exact subject remain immutable. Actual
recipient requests must pass current verified tenant membership before object
checks; neither affiliation nor a client-supplied subject confers authority.

A patient selects only an approved current binding in the source hospital; no
arbitrary grantee string is accepted in a grant. Historical recipient access is
scoped to its exact principal and consent ID. There is no unrestricted patient or
consent search. Separate unattended facility-machine delegation remains an
identity-provider integration; user sessions are not advertised as machine tokens.

## Patient grant and withdrawal

A grant names exact accepted self authority v2, approved notice v2 plus its hash,
approved recipient v2, care_coordination purpose, an explicit bounded unique list
of resource kinds and an explicit valid_until. Patient ID, original claim and
recipient subject are derived from canonical owner rows, never supplied as
independent authority by the client. Explicit acknowledgement is required.
Expiry may not exceed authority/recipient validity. No duration is a legal default.

The immutable receipt binds tenant, hospital, patient/subject, authority/review
versions, exact notice hash, recipient/organization binding, purpose, resource
kinds, expiry, recorded time and command fingerprint. A dedicated
helivanta-consent-<tenant> HMAC key protects the canonical receipt; it never reuses
staff/emergency keys. The fixed key version is retained for existing receipts.
The receipt is server integrity evidence, not a qualified patient e-signature or
a self-contained record-access capability.

Prepare a scoped audited snapshot in SQL, close the transaction, sign outside
SQL, check current claim over HTTP, then recheck all local versions/state/expiry
before atomic insert+audit+metadata-only outbox. Concurrent identical ID retries
return the existing current receipt; changed input conflicts. A retry never
reactivates withdrawal or quarantine. Current patient registration version and
review decision must still match the accepted authority.

Withdrawal is a terminal attributable SQL transition and outbox intent. It does
not depend on OpenBao, the original claim, notice/recipient activity or authority
still being valid. Historical signed grant plus withdrawal evidence is not
advertised as an independently signed withdrawal PDF; signed rendering/provider
and later receipt-signing work must be completed separately. Retain all history.

## Recipient consent gate

The authenticated named recipient supplies consent ID, exact purpose and resource
kind. The server checks the immutable receipt HMAC, live claim and current local
owner state. It records the decision/audit before returning. There is no positive
decision cache. Withdrawal, expiry, authority/review/registration change, recipient
revocation or notice retirement blocks subsequent checks. Retirement blocks new
processing but preserves the historical declaration and wording; no silent
migration to another notice or recipient is allowed.

The result is a consent gate only. It neither authorizes an object nor substitutes
for requester clinical authority, source-hospital isolation, subject/object match,
signed/released record version and reauthorization at file release. Record owners
must enforce all gates in an actual release operation before #556 can close.
Dependency failure is unavailable, never fabricated ALLOW/false success. A malformed
or tampered receipt is quarantined and raises a durable security incident with
opaque identifiers only; quarantine never reactivates an earlier grant.

## Boundaries and failures

Server-verified tenant, current hospital grants and exact subject/recipient object
scope are mandatory. Use one forced-RLS policy/table and immutable SQL lineage
checks. Patient and recipient readers cannot act as hospital reviewers. Purpose
consent cannot be inferred from registration receipts, account notices, identity
review, shared ownership or this predicate alone. Audit sensitive reads and writes.

Postgres, workload verification and the record/consent owners fail closed. OpenBao
failure blocks new signing and cryptographic validation; withdrawal stays available.
An unconfigured claim client yields unavailable. The client uses an explicit
origin, strict TLS or verified cluster mTLS, refuses redirects/cookies, bounds
request/response/deadline and reloads its projected credential for every call.
No remote operation is performed while SQL locks are held.

Assumptions, not measured certification: 20 peak consent checks/s, <=8 KiB commands,
<=16 KiB immutable receipts, p99 check target <=750 ms, 99.9% availability. At 500k
grants/year and 1.5m by 36 months, receipt/index storage is approximately 12/36 GiB
at 24 KiB/record. Audited read volume is a separate and potentially larger storage
budget; reviewed capacity/retention must precede activation. Exact owner lookup,
subject/recipient cursor pages and active-state indexes need real non-bypass plans
at >=100k rows. No legal retention duration is invented.

Use additive forward-only migrations with bounded DDL timeouts. Old binaries may
ignore new tables; rollback retains declarations/withdrawals, never drops history.
Signatures and receipts do not make a stale authorization projection current.

Rejected: a duplicated patient master, cross-module SQL, asynchronous-only claim
checks, mutable consent wording, a global unscoped grantee role, default legal
policy and a cached ALLOW reused for later record release. IN-ID-04/06/07 and
current primary-source limits remain applicable. Actual hospital/privacy review
of policy and notice content is required before publication.

## Local validation evidence (2026-10-25)

Actual Global API → owner routes, PostgreSQL forced RLS, JetStream delivery,
OpenBao and HTTPS claim-owner checks cover grant/replay, unrelated/foreign
principals, withdrawal, source-claim withdrawal, authority revocation, notice
retirement, recipient revocation, malformed signer receipts/quarantine, dependency
outage, expired historical declarations during outages, stale/invalid capture
input, conflicting replay and outbox/audit rollback. Expiry uses the database
clock before remote calls; already-expired declarations deny without a signer or
claim-service dependency. Only external Kubernetes TokenReview and faulted
OpenBao/owner HTTP responses are simulated; business repositories remain real.
Owner summaries are cursor pages without clinical identifiers or signatures;
they are not integrity-verified receipts or current eligibility assertions.

A tagged `performance` test seeds 100,000 synthetic recipient drafts through the
application role and measures the real patient-scope RLS catalog query. Before
indexing it discarded 100,000 rows (126.619 ms sample). The approved-only partial
index selected one row and discarded none (2.965 ms sample). This is a single
local plan sample, not a load/SLO result. The new index belongs to the new table;
no production populated-table index is built by this change. Audit-retention capacity remains an activation review item; ledger lookup and
owner pagination now have the larger plan checks described below.

Reproduce the plan with `go test -race -p=2 -tags=performance
./internal/modules/patientmaster -run '^TestClinicalRecipientCatalogPlan' -count=1
-v` from backend with the disposable test Docker socket configured. Production
policy content, proxy authority, actual record-owner disclosure and booking
integrations are outside this declaration slice and remain open stories.

## Infrastructure activation review (not yet implemented)

Repository inspection at `tesserix/tesserix-k8s@beb23aa0` confirms an integration
boundary that must be changed explicitly: `helivanta-api-ingress` denies traffic
on 8080 from every principal except Global API. A new ALLOW policy cannot
override that DENY. The internal claim path must be carved out exactly and receive
its own DENY for every principal except the API owner workload, including Global
API. Permit only POST of `/internal/v1/mediconnect/current-claim`; preserve the
Global API boundary for other public paths and verify actual enforcement before
activation. Do not merely enable the environment variables.

The owning GitOps change must also provide a 600-second audience-bound claim
projection, explicit Kubernetes API credential/CA for TokenReview with automount
disabled, a create-only TokenReview ClusterRole bound to the exact API service
account, and bounded control-plane egress using a discovered approved CIDR.
Dedicated strict mTLS and actual mesh readiness are mandatory for cluster HTTP.
The existing Transit role must retain staff/emergency functionality while gaining
only the tested nonexportable 32-byte HMAC-key create/update and HMAC operations
for `helivanta-consent-*`; no key read, rotation, export or deletion capability.

These are source review facts and planned controls, not evidence of live
configuration or successful deployment. The isolated infrastructure worktree is
`tesserix-k8s-consent-workload` on `feat/helivanta-consent-workload`; it has no
committed activation change yet. Keep production activation gated until rendered
and negative policy tests, the final application image, policy/credential delivery
and an authenticated isolated-tenant production journey are verified.

## Consent-ledger scale finding and correction

The tagged ledger plan test populates 100,000 consent rows and 100,000 recipient
bindings through the application role with forced RLS, audit and history guards
retained. Bulk filler declarations deliberately have invalid MACs and are used
only for structural plan measurements; all actual eligibility assertions use the
separate real API-created signed declaration. No filler is production data or
proof of a valid clinical consent.

Returned-row counts alone missed a non-leading-ID index scan inside the recipient
binding check. The stricter test bounds root shared-buffer visits as well as rows:
the failing sample touched 4,510 shared blocks (157.864 ms). Exact-scope indexes
on both new tables, `(tenant_id,hospital_id,id)`, reduced the final recipient query
to 17 shared blocks (7.117 ms sample); owner pagination touched five blocks
(0.114 ms). The completed race test then exercised the actual signed declaration
through Global API and obtained consent-gate allow, with record authorization
still explicitly false. These are local plan samples, not latency or load SLOs.

Run `go test -race -p=2 -timeout=20m -tags=performance
./internal/modules/patientmaster -run '^TestClinicalConsentLedgerPlans' -count=1 -v`
with the test Docker socket configured. Both new indexes are part of the still
unpublished `0008_patientmaster` creating their tables; no populated live table
index is built by this change.
