# EmergencyConnect staff intake

Programme #1220; parent #121; scoped implementation #1227.
This extends the AmbulanceConnect design rather than activating live citizen booking.

## Contract and capacity

Pilot target (not yet load-tested): 10 creates/s, <=8 KiB request, 100 queue readers,
read:write 10:1, p99 server <1s while dependencies are healthy. Planning volume is
36,500 cases at 12 months / 109,500 at 36 months; this fits indexed Postgres without
another datastore. No maps, fleet telemetry or patient-master call is in this slice.
OpenBao encryption is a critical dependency: unavailable/refusing Transit returns
503 before a write; the public call-first experience remains independent. Postgres
is critical; NATS may be down because the transaction still stores its outbox.
No new cloud service or paid third-party provider is introduced.

## Identity and ownership

`emergency` owns bases, base access, cases and timeline. Register it through the
shared bootstrap/archtest registries. Use the existing tenantdb transaction gateway
(backed by GORM/pgx), not a new pool that bypasses its forced-RLS invariants.
Emergency dispatcher and supervisor are distinct seeded system roles; no doctor,
nurse or receptionist inherits either permission. OpenFGA checks tenant membership
and named route permissions. PostgreSQL additionally enforces base-level subject
scope through SET LOCAL context and a grant table, mirroring workforce's established
hospital boundary. Tenant admin may configure bases/access but cannot read cases
without an explicit base grant. A subject can hold independent tenant memberships.
No cross-tenant records or implicit service agreements are introduced.

Base metadata (ID, display name, timezone, policy reference and retention days) is
visible only to emergency base-read holders in the active tenant. Sensitive cases
need a separate explicit base grant: reader, dispatcher or supervisor. Creating or
revoking a grant requires base-manage permission and is audited. Only tenant admin
holds base management; the matrix declares this intentional exception. Case read,
intake and close are separate route permissions; object grants narrow them further.

## Case lifecycle and API

Routes under `/v1/emergency` (same-origin edge `/api/v1/emergency`):

- `GET /bases`: cursor metadata directory, including the caller's explicit level.
- `GET /bases/:base/access`: the current subject's single live base grant; no administrator inheritance.
- `PUT /bases/:base`: tenant-admin configuration; UUIDv7 ID, timezone, reviewed
  privacy-policy reference and caller-contact retention days (1..365).
- `PUT|DELETE /bases/:base/access/:subject`: explicit scoped grants/revocation.
- `PUT /bases/:base/cases/:id`: canonical staff intake, client UUIDv7 operation ID.
- `GET /bases/:base/cases`: cursor queue; metadata only, no contact/incident text.
- `GET /bases/:base/cases/:id`: audited detail access, decrypted only after scope.
- `GET /bases/:base/cases/:id/timeline`: cursor immutable history, no PHI payload.
- `POST /bases/:base/cases/:id/close`: version and coded reason, supervisor only;
  only received/untriaged cases may close. This never stands down a vehicle.

Intake channel is phone, walk_in or inter_facility. Requested service may be unknown,
BLS, ALS, neonatal, patient_transport, mortuary or home_visit: it is the caller's
request, not a clinical suitability decision. Patient identity may be completely
unknown. Priority remains `untriaged` until the separately approved structured
triage workflow lands. No inferred diagnosis or priority, no message saying an
ambulance is on the way, no public POST request endpoint.

References are base-scoped, including uniqueness and idempotency. Deliberate client
ID reuse in another base must not expose case existence through a global reference
constraint. Any future cross-base search must retain explicit base context.

Creation returns 202 with a case reference, `received` status and a version; this
means durable staff intake only. Identical retries return the original metadata,
including timestamps. HMAC over normalized payload plus base/case binds idempotency;
changed content at the same ID returns 409. Unique composite ownership/operation
constraints resolve races. One transaction inserts case, received timeline and
metadata-only outbox. The dispatcher never waits for event delivery.

Closure is guarded by version + status in the database. Reason is from an explicit
list (duplicate_report, caller_withdrew, no_response_required, entered_in_error),
with mandatory encrypted explanation. It cannot imply no clinical need, merge a
patient, or recall an assigned crew. Timeline entries retain actor, occurrence and
server receipt timestamps; no update/delete/truncate. Case removal is prohibited.

## Privacy, retention and threat model

Assets: caller phone/name, precise incident location, incident narrative, case
existence and immutable provenance. Adversaries: other tenants/bases, revoked
staff, unauthorized administrators, compromised clients and dependencies. Every
lookup is tenant/base scoped; authorization errors fail closed. SQL errors are
sanitized because PostgreSQL diagnostics can embed input values.

Use derived non-exportable OpenBao keys `helivanta-emergency-<tenant UUID>`, with
context `helivanta/emergency/case/v1|tenant|base|case|contact|incident|closure`.
Caller and incident JSON are encrypted separately before acquiring database locks.
The key's v1 HMAC protects request comparisons across key rotation. Only IDs,
channel, requested-service, status, version and policy references leave the case
store in timeline/outbox. No raw request payload, phone, coordinates or names in
logging/audit. Decryption requires an audited authorized read; vault failure never
falls back to ciphertext or plaintext caches.

The India processing basis (`medical_emergency` or `consent`) and the configured
reviewed policy reference are recorded, not interpreted as a compliance certificate.
Medical-emergency basis is invalid for non-emergency transport without the operator's
reviewed legal policy; the API constrains that combination. Notices, legal approvals
and clinical retention remain launch gates #1222. No Aadhaar/ABHA prerequisite.
Contact expiry is frozen from base policy at creation. Read-time masking is immediate
at expiry; maintenance purges contact ciphertext on a bounded batch without deleting
case history. Original retry fingerprints remain key-protected and non-public.
Incident content and closure evidence have separate clinical retention governance;
this slice does not invent a legally sufficient clinical retention period.

## Delivery and failure handling

Additive migration `0001_emergency`: new tables/functions only, no existing data
changed. Include forced tenant+base RLS, composite tenant/base/case foreign keys,
UUIDv7 checks and immutable identity/timeline triggers. Timeline inserts must match
the current case version/state/reason; a dispatcher cannot append a fictitious future
closure. Expired contact purging is reserved to the operational system DB identity. Reverse migration is not
supported after case intake: reverting code leaves the additive tables intact;
never delete clinical history for rollback. Scratch test DBs are disposable.
New role grants are additive through the existing reconciler; do not patch live
membership to test features. Contact purging is separately bounded and never erases
case/timeline. Its operational schedule and OpenBao policy changes must be reviewed
through GitOps before live use; no imperative production change in this code task.

Update the owning OpenBao policy and test-policy mirror together for encrypt,
decrypt and HMAC on the emergency key prefix only, with no export/rotate/delete/KV.
Until that policy is deployed the feature fails closed. The existing staff key
scope must remain unchanged. Documentation provides the maintenance command and
release checklist. Clinical/government integration, fleet/crew dispatch, live
citizen booking and clinical triage are dependent slices, not simulated here.

Simpler alternative rejected: a tenant-only unencrypted case table would expose
other bases' patients and caller contacts. A new database/service is rejected for
this bounded workflow; reliability isolation is still required and measured before
live emergency intake activation under #1222.
