# Current MediConnect claim check for clinical consent

Status: owner-boundary implementation; prerequisite for #60/#556, not clinical
consent or disclosure authority. Builds on the unchanged freshness requirement in
the self-authority specification. It does not replace hospital evidence review.

MediConnect owns the unverified intake claim and its withdrawal. Patientmaster
owns independent identity review and capacity evidence. A queued withdrawal can
lag at another owner; clinical consumers therefore need a live owner observation
in addition to current reviewed authority, purpose consent and record release.

Reuse the existing Kubernetes TokenReview workload verifier, with a dedicated
audience and exactly configured patientmaster caller service-account identity.
POST /internal/v1/mediconnect/current-claim accepts only a dedicated workload
credential header. User sessions and arbitrary tenant headers confer no access.
The internal route is not a Global API/UI route; all patient-facing APIs still
enter Global API. No machine credential is returned to a client.

The canonical request contains tenant_id, request_id, hospital_id, patient_id and
subject. The caller derives these from an authorized canonical authority receipt,
never from an unverified external facility request. Response is only {current:
boolean}. Current means the exact self claim remains pending_verification and
has not been withdrawn; it does not mean identity or clinical authority approved.
Unknown, wrong-tenant/subject/hospital/patient and withdrawn claims all return
current=false, without disclosing demographics, account details or other claims.

A dedicated, exact-tuple read-only RLS branch permits the authenticated system
operation. It does not change subject-owned writes or result-delivery authority.
Every attempted lookup is audited under the exact verified caller service-account
identity, without impersonating the patient. A transaction-only fixed operation
marker selects the narrow read branch; it does not replace audit attribution. Audit failure prevents a successful response. No
positive or negative claim observation is cached. The first request beginning
after committed owner withdrawal must observe current=false without broker wait.

Planning envelope: 20 peak checks/s, request <=4096 bytes, response <=1024 bytes,
read-only business state with one audit write/check, 500k claims at 12 months and 1.5m at 36 months (approximately 1.2 GiB including
indexes). At a sustained upper envelope of 500k checks/day, audits reach 182.5m
rows/year; at 0.5-1 KiB/row including indexes that is approximately 90-180 GiB/year
or 270-540 GiB over 36 months without archival. This requires measured capacity
and a reviewed storage/retention plan before traffic activation; no retention
period or automatic deletion is invented here. Target p99 <=500 ms and 99.9% availability; these are assumptions
requiring measurement. Existing Postgres and Kubernetes TokenReview are critical:
unavailability returns 503/401 and never current=true. The future consuming service client
must have a bounded deadline, refuse redirects, use an explicit origin and re-read
the projected token on each call for rotation. Cross-pod transport must use strict
authenticated TLS/Istio mTLS; credentials must not traverse unprotected remote HTTP. No network call occurs under SQL
locks or inside a tenant transaction.

Assets are patient-link privacy and current revocation evidence. Threats include
an ordinary user, another tenant, wrong/audience-confused service identity and a
compromised caller. TokenReview authenticates the dedicated owner identity before
parsing the body; exact tenant/object scope and bounded strict JSON protect the
next boundary. A compromised canonical consent owner remains a trusted service;
this endpoint exposes only a predicate for an exact already-known tuple, not an
account search or patient-master mutation.

The migration is additive/forward-only with bounded DDL timeouts and one forced-
RLS policy. The new read branch remains inert without its exact transaction flags.
Startup enablement is explicit and fails if workload identity is unconfigured;
no production enablement or token/RBAC deployment is inferred from source. API
rollback retains history and can leave the stricter read policy installed.

Rejected: cross-module SQL violates ownership; async projections cannot establish
fresh withdrawal state; forwarding a patient's browser credential cannot support
a recipient decision; a new custom HMAC/JWT service protocol is unnecessary while
the existing audience-bound workload verifier supplies this trust boundary.

Consumer acceptance must exercise actual HTTP owner check, committed withdrawal,
wrong tuple/credential, dependency and audit failure before clinical grant or
release integration. A current=true response never replaces consent/HMAC checks,
recipient authorization, source-hospital scope or signed/released object version.
IN-ID-04/06/07 apply. No legal policy, notice or consent is seeded.
