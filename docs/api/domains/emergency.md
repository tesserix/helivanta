# EmergencyConnect and AmbulanceConnect contracts

[Contract index](../README.md) · current provider workflows; public booking remains separate

## Public versus staff boundary

AmbulanceConnect provides citizen call-first access through `/ambulance`, read-only `GET /api/emergency/access?country=IN|OTHER` and the Expo application. These are web/application surfaces outside the generated Go `/v1` inventory. They do not establish government dispatch integration, live vehicle booking, guaranteed response or automatic clinical triage.

EmergencyConnect is the provider/staff domain: verified tenant → base → case/fleet/duty/dispatch, plus receiving-tenant → authorized facility. See [operations](../../architecture/emergencyconnect-operations.md), [fleet](../../architecture/emergencyconnect-fleet.md), [transport review](../../architecture/emergencyconnect-transport-review.md), [dispatch](../../architecture/emergencyconnect-dispatch.md) and [receiving](../../architecture/emergencyconnect-receiving.md). These existing documents retain detailed request/status rules; [runtime reference](../reference/README.md#emergency) enumerates all current operations.

## Current contract families

| Family                            | Authority and critical semantics                                                                                                                                                   |
| --------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Bases/access and case intake      | Explicit current base grant; encrypted contact/location/incident payloads through Transit/OpenBao; versioned case evidence, scoped lists/detail/timeline and supervisor closure    |
| Fleet, crew, duties and readiness | Vehicle capability, active crew duty membership, expiring credentials/readiness and constraints; resource availability is checked for dispatch, not inferred from an old dashboard |
| Clinical transport assessment     | Current authorized reviewer and reviewed transport capability/evidence; a dispatcher's classification is not a clinical sign-off                                                   |
| Dispatch reservation and commands | Case version, assessment, vehicle/duty, acknowledgement deadline, resource commitment, deterministic workflow and immutable command identity                                       |
| Crew surface                      | Assigned duty/dispatch context; bounded incident disclosure, lifecycle commands and evidence; crew role alone does not authorize every provider case                               |
| Receiving links                   | Source provider and destination facility, bilateral link review, expiry and revocation; destination UUID is not authorization                                                      |
| Receiving requests/commands       | Pending/accepted/rejected/revoked/handover-received states; physical handover remains an explicit fact, not an inference from arrival                                              |

`contracts/emergency-dispatch.json` generates dispatch/receiving models and lifecycle vocabularies. Intake has an additional module-local `contract/intake.json`; do not omit it from schema discovery. The event envelope's `type` convention differs across older and newer modules: dispatch/receiving checks the full subject string. Read the producing contract, not a guessed PascalCase event name.

The dispatch lifecycle includes `workflow_pending`, `offered`, `acknowledged`, `en_route`, `on_scene`, `transporting`, `arrived`, `handover_complete`, `cleared`, reconciliation and cancellation states. It is not a single generic case status. The JSON catalogue and current handlers own exact legal commands; do not duplicate their enum in a new frontend.

## Failure and delivery

Reservation and command state must commit locally before a workflow acts. Workflow bridge starts/signals require a matching durable execution; duplicate starts verify history/scope rather than assume success. Timeout/no acknowledgement creates escalation/reconciliation, not a silent reassignment. Emergency worker configuration and dispatch flags gate runtime availability; source-level registration is not proof that production dispatch is enabled.

If Transit is unavailable, protected case reads/writes fail closed rather than expose ciphertext or accept unencrypted payloads. If Temporal is unavailable, show pending/unavailable state and recovery ownership; do not label an unstarted workflow dispatched. Receiving acceptance must be current and facility-specific; a later revocation requires operator reconciliation. Physical arrival/handover cannot be undone by deleting a record.

`receiving_changed.v1` is explicitly directed back to the provider tenant. The receipt consumer checks origin, destination, request identity, local lineage and version before waking the provider workflow. Do not generalize this path into unrestricted cross-tenant event writes. Other published emergency events carry minimized references; encrypted incident payloads do not belong in broker data or CRM exports.

## Remaining gates

Programme [#1220](https://github.com/tesserix/helivanta/issues/1220) separates public calling, dispatch, safety/pilot, nearby choice and operational CRM. [#1224](https://github.com/tesserix/helivanta/issues/1224) requests callback/escalation/complaint/tariff workflows but does not authorize PHI in sales CRM. [#1222](https://github.com/tesserix/helivanta/issues/1222) owns the safety case and pilot launch gates. No contract document closes those gates.

Review missing fleet, expired readiness, duplicate reservation, conflicting duty, wrong crew, patient/caller mismatch, repeated handover, revoked receiving link, deadline expiration, cross-provider visibility, dependency failure, clock ordering and command retry after commit. Emergency access, triage and dispatch must not wait for payment, ABHA, insurance, CRM completion or marketing consent. Scheduled private transport quotation is a separate reviewed workflow.
