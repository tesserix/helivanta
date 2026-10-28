# Hospital receiving and transport

Use the current production web application in `apps/shell`, served at
`/emergency`, `/emergency/crew` and `/emergency/receiving`. Hospital preference,
a pre-alert, hospital acceptance and physical patient receipt are distinct facts.

## Configure an authorized receiving campus

1. A hospital administrator with `emergency.receiving.manage` creates the campus
   under Hospital receiving, using its approved policy reference and expiry.
2. Verify the staff member through the normal identity process. Enter that
   member's verified identity subject and load the existing delegation.
3. Record an independently verified, expiring receiver or manager delegation.
   A member cannot enable their own delegation. The `emergency_receiver` role
   grants receiving read/action permissions; it does not grant administration.
4. Share the receiving tenant and campus identifiers with the provider supervisor.
   The supervisor proposes a directed service link with an agreement reference
   and expiry. A receiving manager reviews and approves it from Hospital receiving.
   Administration alone cannot approve a link or accept a patient.

A hospital tenant can have several independently scoped campuses. Use distinct
hospital tenants when their administrative/data ownership is separate; do not
create a project per building to simulate isolation. Shared professionals retain
one verified identity with explicit memberships and campus delegations. No user
inherits access to another hospital's private cases through a service link.

## Complete a trip

1. Dispatch records a clinically reviewed requirement and reserves eligible fleet
   capacity. Driver and clinical crew independently acknowledge their assignment.
2. Crew record departure and arrival on scene. Dispatch selects an approved
   receiving partner and sends a pre-alert with a 15–300 second response window.
3. The authorized receiver completes the agreed clinical communication with
   dispatch and records acceptance with a bounded lease of up to two hours.
   The software shares transport metadata and references; it does not send the
   caller's name, contact details or exact incident location to the receiving inbox.
4. Crew confirm the current accepted campus and begin transport. Transport and
   arrival require the exact latest receiving request, current clinical/fleet
   eligibility and live hospital acceptance. Expiry or revocation retains capacity
   and raises an escalation. Follow the approved dispatch fallback.
5. Crew record hospital arrival. The independent hospital receiver confirms
   physical receipt. The assigned crew then record their matching handover.
6. Record a fresh vehicle readiness check after handover. A supervisor can then
   clear the trip. Failed equipment checks continue to block the vehicle.

A new destination supersedes the old acceptance and requires a new attributed
pre-alert. Ordinary cancellation cannot release a vehicle after transport begins.
Timeouts never invent acceptance, cancel a trip or release its capacity.

## Retry, security and service suspension

Commands retain their original URL, ID, actor, version and body across an uncertain
response. Retry the unchanged command; refresh before attempting another action.
No clinical mutation is queued for reconnect. Access failures remove protected
content. Staff should use the approved voice fallback when connectivity fails.

`EMERGENCY_DISPATCH_ENABLED=false` suspends new reservations.
`EMERGENCY_WORKFLOWS_ENABLED=true` keeps existing trips and hospital notifications
processing. GitOps exposes these as `emergencyDispatch.acceptNew=false` while
`emergencyDispatch.enabled=true`. Never stop workers to suspend new intake.

Forced row security scopes every object to the verified provider base or receiving
campus. The workflow database identity has narrow metadata grants and cannot read
private incidents, credential evidence or application/owner roles. Acceptance,
revocation and handover retain attributed immutable receipts.

## Validation and activation

The receiving browser fixture runs at a loopback-only origin with disposable
Postgres, OpenBao and NATS and separate supervisor, driver and receiver sessions:

```sh
go -C backend test -tags emergencybrowser ./internal/modules/emergency \
  -run 'Test(EmergencyConsole|ReceivingTransport)Browser' -count=1 -v
```

Set `HELIVANTA_BROWSER_ORIGIN` to the locally running production web build first.
Synthetic approvals prove software behavior only. A real YES Hospital dispatch
and clinical lead must approve receiving/crew protocols, fallback and pilot
readiness before clinical or public activation. This work is not a compliance
certification or evidence of an emergency-services integration.

## Architecture review records

The independent permission matrix records six additional permissions: dispatch
management for dispatchers/supervisors, own-assignment crew read/action and
campus-delegated receiver read/action. Campus configuration/delegation is
administrator-only; base/campus row grants remain independent and mandatory.

Single-resource GET declarations identify the exact case, trip, request, link,
campus or current delegation. All queue/history/inbox collections retain cursor
pagination. Case eligibility returns one assessment with at most eight fixed
blocker codes. Published workflow notifications contain only scoped opaque IDs
and revision counters; their ten fields are classified individually, with no
automatic exemption for identifier-shaped fields.

The `emergencyplans` performance fixture alone has a reviewed owner-access
exception to generate 100,000 synthetic terminal history rows and analyze indexes
in its disposable database. Its query-plan assertions run under the application
role and forced RLS. No request handler, runtime worker, general test-file pattern
or production identity receives this exception.
