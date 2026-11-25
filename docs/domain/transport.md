# Ambulance and scheduled patient transport

[Domain index](README.md) · [owning emergency contract](../api/domains/emergency.md)

## Three different boundaries

| Journey                           | Helivanta boundary                                                                                                   | Completion evidence                                                                               |
| --------------------------------- | -------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------- |
| Citizen emergency access          | AmbulanceConnect call-first web/Expo information; public booking remains separate                                    | A visible call action is not dispatch acceptance or an integration                                |
| Provider operations               | EmergencyConnect case intake, reviewed transport capability, resource reservation, crew dispatch and receiving links | Current owner-confirmed milestones, crew acknowledgement and explicit receiving/handover evidence |
| Scheduled non-emergency transport | A distinct proposed workflow requiring an accepted owner/contract and provider arrangements                          | Provider booking confirmation, agreed service/funding and eventual trip/handover evidence         |

Verify actual handlers, feature configuration, contracts and operating acceptance;
routes and documents do not establish a live government dispatch adapter. Existing
research is a source lead, not authority to activate a service. See the current
[dispatch](../architecture/emergencyconnect-dispatch.md),
[transport review](../architecture/emergencyconnect-transport-review.md) and
[receiving](../architecture/emergencyconnect-receiving.md) specifications.

## Scheduled transport design inputs

Before implementing a scheduled transport slice, establish the owning provider,
patient and authorized requester, care/appointment reference, pickup/destination,
timezone and time window, mobility/accessibility, escort/equipment needs,
eligibility/funding source, provider service type and cancellation rules. Minimize
location and clinical disclosure to the assigned trip and authorized actors.
Define who reviews capability/eligibility and who confirms the booking. Do not
assume every request needs or qualifies for an ambulance.

Design explicit requested/reviewed/scheduled/physical-progress/terminal concepts
and exceptions against the accepted contract. These concepts are not approved
enum values or a new endpoint. Do not reuse emergency dispatch commands or an
appointment's fulfilled state as evidence of transport completion.

## Dispatch and handover invariants

Use `contracts/emergency-dispatch.json`, the emergency module's intake contract
and current handlers for exact lifecycle commands. Intake, clinical assessment,
reservation, crew acceptance, arrival, transport, receiving acceptance, physical
handover and resource clearance are separate facts with different actors.
Provider/base grants and receiving-tenant/facility grants are distinct; a destination
UUID, affiliation or directory capability does not authorize cross-tenant data.

Reserve the vehicle/compatible duty atomically using current readiness, privileges,
equipment and reviewed capability. GPS proximity is not suitability or availability.
Pin case/assessment versions and command identity. Commit durable intent before
starting the workflow. Retry with the same identity after ambiguous timeout;
changed payloads cannot masquerade as the original command.

Record provider reference, acknowledgement level/deadline, event version and last
successful synchronization. Handle delayed/duplicated/out-of-order receipts and
surface conflicts for accountable reconciliation. Bounded retries and operational
escalation must survive process restart. No acknowledgement must not become a
success, automatic reassignment or indefinite silent waiting.

A caller cancellation request is not crew recall. Late acknowledgement after
cancellation, destination change, revoked receiving acceptance and loss of contact
need approved operator/clinical reconciliation. Preserve actual physical actions
and handover history; deleting a row does not reverse a trip.

## Safe public and degraded operation

Use current official local contact/coverage information. Do not add universal
emergency numbers, triage scripts, vehicle classification rules or response-time
promises from memory. Do not let an AI independently triage, downgrade, dispatch
or stand down a response. Qualified authorities approve clinical/capability,
eligibility, escalation, funding and emergency-service operating rules.

Call-first access must remain usable without login, OTP, GPS, ABHA, payment or
marketing consent. Support landmarks/manual location when GPS is unavailable and
accessible, local-language communication. Do not expose fleet/caller coordinates
to public maps or clinical details to sales CRM. A caller helping another person
does not gain access to that person's records.

Make unavailable/unattended intake, stale telemetry, no suitable resource,
dependency failures and unsupported regions explicit. Respect encryption and
authorization failure boundaries; do not accept unprotected incident payloads as
a fallback. Staff need an approved manual process and post-outage reconciliation.
Do not enable live intake before staffed coverage, queue-age/escalation monitoring,
provider agreements and the [safety/pilot gates](../api/domains/emergency.md#remaining-gates)
are evidenced.

## Acceptance evidence

Use isolated synthetic cases: wrong tenant/base/duty, mismatched patient/caller,
expired capability/readiness, competing reservation, revoked destination link,
duplicate retry after commit, lost/late acknowledgement, reordered receipts,
cancel/arrival race, workflow/encryption outage and disputed handover. Also check
GPS denial, keyboard/screen-reader access, timezone/DST for scheduled trips and
an approved downtime drill. Never dial emergency services, create real responses
or use live patient/location data for routine validation.

Operational metrics need explicit endpoints (receipt-to-assignment,
assignment-to-movement, receipt-to-scene, destination-to-handover), denominators,
freshness and missing-data state. Technical API latency does not prove operational
response. Report actual implementation/test/deployment evidence and unresolved
qualified-review gates separately.
