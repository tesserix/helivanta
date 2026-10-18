# Directed receiving and transport completion

Status: implemented and locally verified, programme #1220.

Use the existing planning envelope of 20 commands/s, 200 metadata reads/s, 8 KiB
commands, p 99 below 500 ms and 99.9% monthly availability. For sizing only, assume
1, 000 bookings/day, 12 ledger records/booking: about 365, 000 cases and 4.4 million
ledger records at 12 months;1.1 million cases and 13 million ledger records at 36
months, approximately 4/12 GiB before protected payload retention. These are
unmeasured planning assumptions. A 10 x increase remains a single indexed Postgres
workload; no new service, datastore or cache is justified.

Receiving capacity, clinical reports and crew assignments are protected assets.
Untrusted citizens, another tenant, an unassigned staff member and a compromised
workflow worker must not approve a destination or release occupied capacity.
Verified sessions, exact provider-base/facility grants and current immutable
approval revisions define the trust boundaries. Tenant administration alone is
not receiving authority. Independent, expiring verification is explicit; software
records attestations and must not claim to verify a professional licence.

A receiving facility is an operational endpoint owned by its hospital tenant;
separate endpoints can represent distinct hospital campuses or branches. It does
not create or replace the canonical hospital/clinical record. Configuration and
subject grants retain all revisions. A provider supervisor proposes a service
link. A separately authorized receiving manager approves it under a recorded
policy and expiry. Either side may revoke; reactivation needs a new proposal.

Provider and recipient share only explicitly directed records in this module:
link, pre-alert and attributed decisions. Forced RLS permits the named provider
base or the named receiving facility, not tenant-wide administration. Other cases
and protected intake payloads remain inaccessible to the recipient. Pre-alert
metadata contains IDs and assessed transport class, never contact or exact
location. Clinical pre-alert and acceptance require the authorized humans to
complete their approved clinical communication and record its reference.

This module has one database and one data owner. Atomic directed records are
chosen over asynchronous approval projections: a delayed revocation must not
permit a new transport/arrival/handover command. A narrow boolean owner function
may check only the selected dispatch's latest directed receipt, current facility,
service link and receiving delegation; purpose scope is restored before return.
It has no BYPASSRLS and returns no recipient or patient data. NATS notifications
and Temporal carry only operational IDs, versions, states and deadlines.

A pre-alert has a bounded response window. Acceptance has an explicit lease.
Rejection, revocation, expiry and a newer destination invalidate the old
acceptance. Destination changes preserve old requests and require an attributed
reason. Transport and hospital arrival require the latest live acceptance.
An independent receiving subject confirms physical handover; assigned crew then
record their matching handover. A scoped supervisor clears the trip only after
both receipts and a new post-handover readiness check. Ordinary cancellation
cannot release capacity after transport has started. Uncertainty retains it.

Temporal owns durable receiving waits; a deadline creates an operational
escalation and keeps the current trip phase/capacity. It never invents an
acceptance. Postgres failure rejects commands. OpenBao failure prevents protected
intake/read. NATS failure retains committed intent. Temporal failure leaves
visible pending work and held capacity. Telephony/routing failure provides no ETA
or acceptance claim; staff follow the approved fallback.

Add forward-only migration `0006_emergency` after the unpublished dispatch migrations. Test
against disposable Postgres, including cross-tenant and unassigned denial,
concurrent decisions, lost-response replay, revoked/expired delegation and
acceptance, destination supersession and both handover signatures. Rollback stops
new entry through GitOps while preserving ledgers and reconciliation-capable
code. No deletion, credential rotation or schema contraction is needed. Reuse the
existing API/database/Temporal capacity; no new worker process is required.

Production public activation still requires real provider/receiving owners and
approved policies. Synthetic test attestations cannot activate a hospital.
