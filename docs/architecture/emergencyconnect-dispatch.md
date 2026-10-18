# Dispatch reservations and crew acknowledgement

This implementation extends the protected EmergencyConnect module in the actual
`apps/shell` web application. Public emergency calling remains independent.
Production activation is disabled by default. This document describes implemented
reservation/offer/acknowledgement/scene stages; hospital acceptance, transport,
handover and citizen booking are not implied by the presence of future status labels.

## Authority and tenancy

Assets are protected caller/location/incident data, physical fleet capacity and
attributed operational decisions. Threats include cross-tenant staff access,
unassigned crew commands, untrusted event hints, retries and compromised workflow
credentials. Every HTTP operation derives tenant and subject from the verified
session. Base operations additionally require explicit object grants. Tenant
administrator metadata authority does not imply case access.

`emergency.dispatch.manage` belongs to dispatchers/supervisors; the base grant
still decides whether the operation is allowed. `emergency.crew.read` and
`emergency.crew.act` belong to the assignable `emergency_crew` system role.
Assigned crew can see only their offered assignments; they do not acquire base
reader or fleet-manager authority. A never-offered reservation exposes no case
reference to crew, including when eligibility failed and reconciliation is needed.

The product trusts one configured identity namespace. A shared verified subject
may belong to multiple provider tenants. Reservation holds capacity within its
provider; actual crew acknowledgement acquires a global active-subject commitment.
This prevents a person acknowledging two active provider assignments. It cannot
reveal which other tenant holds a commitment. Unconfirmed offers do not allow a
provider to take a global commitment on another person's behalf.

## Commands and durable processing

| Endpoint under `/v1/emergency`                           | Authority                                                                         |
| -------------------------------------------------------- | --------------------------------------------------------------------------------- |
| `PUT /bases/:base/dispatches/:id`                        | Dispatch permission plus base intake grant                                        |
| `GET /bases/:base/cases/:case/dispatch-eligibility`      | Dispatch permission plus base intake grant                                        |
| `GET /bases/:base/dispatches/:id`                        | Case-read permission plus base reader grant                                       |
| `GET /bases/:base/cases/:case/dispatches`                | Same scoped read, cursor paginated                                                |
| `PUT /bases/:base/dispatches/:id/commands/:command`      | Operation-specific base grant and actor-bound receipt                             |
| `GET /crew/assignments`                                  | Crew permission, own offered active assignments only, cursor paginated            |
| `GET /crew/bases/:base/dispatches/:id`                   | Crew permission plus assigned identity and actual offer; own acknowledgement flag |
| `GET /crew/bases/:base/dispatches/:id/incident`          | Same assignment, audited/decrypted protected read, no-store                       |
| `PUT /crew/bases/:base/dispatches/:id/commands/:command` | Same assignment, legal state/version and crew command                             |

Reservation binds the current case version, latest clinical assessment, reviewer
delegation, vehicle and duty. It locks the base without conflicting with foreign-key
checks, then case/vehicle/duty and sorted crew. Forced RLS and partial unique
indexes enforce exclusivity. Closing a case, ending a duty or updating allocation
state directly cannot release an active commitment.

The `PUT` response is an immutable receipt (`workflow_pending`, version 1), not a
statement about the current crew state. Read the dispatch for current status.
The web form freezes the command ID/body on an uncertain response and retries
that same request. It never replaces uncertainty with another booking.

The crew workspace is `/emergency/crew` in the production shell. It discovers
only the verified subject’s actually offered active assignments, with no base
reader grant. Selecting an assignment reads the protected incident through a
separate audited endpoint. Unmount discards the protected query immediately; a
failed or revoked read hides any previously displayed detail. Reconciled stand-down
removes private access. Both HTTP and browser requests use `no-store`.

Crew actions record acknowledgement, departure and arrival on scene. They use
current assignment versions and retain the original ID/body after an uncertain
response, including when polling observes a newer version. Commands are not
queued for later reconnection. Offline action attempts show a contact-dispatch
message; connectivity restoration alone cannot initiate a command. An encrypted
native offline queue and transport/hospital stages are still separate programme
work, not delivered by this screen.

Only the dedicated `helivanta_emergency_worker` database identity runs workflow
activities. It is non-superuser, non-bypass, NOINHERIT, has no other role memberships, and has selected metadata
privileges, not private incident columns. Role-specific restrictive outbox/audit
policies cannot be overwritten by another module's canonical tenant policy.
Temporal and NATS retain identifiers, versions and operational timing/state only.
Activity errors are sanitized before durable recording.

The worker revalidates current evidence before offering. Crew acknowledgement and
departure revalidate again through a narrowly scoped boolean database function.
That function checks the original caller's object authority before using its
owner context, keeps forced RLS, pins `search_path`, and restores its scope on
return. Setting a purpose flag on an application connection cannot elevate its
SQL role. This permits an assigned crew member to receive an eligibility decision
without receiving private reviewer/credential records.

Both the driver and clinical crew member must acknowledge before departure.
An unanswered offer becomes `reconciliation_required`; capacity remains held.
A cancellation request also keeps capacity. A scoped supervisor must record a
confirmation reference before cancellation releases the vehicle and crew.
Never use case closure or a Temporal cancellation as a stand-down signal.

Outbox delivery starts or signals a matching workflow. Duplicate starts verify
workflow type, task queue and canonical tenant/base/dispatch input. Signals are
version hints; activities read committed state. Timers do not trust incoming
state. A broker outage retains intent; a workflow/storage failure does not free
capacity. Workflow history is bounded with Continue-As-New.

## Deployment and evidence

Deploy the companion infrastructure in dependency order, following the owning
`tesserix-k8s` runbook `docs/runbooks/helivanta-emergency-workflows.md`.
Migrations `0004_emergency` and `0005_emergency` are additive and are not yet a
production migration approval. Keep retention suspended and public booking off
until their separate operational gates are satisfied.

The backend integration suite uses real Postgres/OpenBao and a dedicated worker
role. It covers concurrent allocation, replay/actor binding, cross-tenant denial,
post-offer clinical revocation, two-person acknowledgement, shared identities
across provider tenants and safe cancellation. The Temporal time-skipping test
runs the real SQL processor through expiry and a subsequent real supervisor
reconciliation. The web panel uses shared generated request contracts and tests
uncertain retries and disabled activation. The mobile-sized browser suite uses
the production Next build and real disposable Postgres/OpenBao/NATS API, verifying
separate crew identities, acknowledgement/departure/scene transitions and private
access removal after real supervisor reconciliation. These synthetic checks are not proof
of professional licensing, real receiving acceptance, device operation or a
production citizen journey.

Named provider leads, approved qualification/receiving policies and a tested
retention/fallback operation are still activation prerequisites. No SOC 2 or
HIPAA certification is claimed by these code controls.
