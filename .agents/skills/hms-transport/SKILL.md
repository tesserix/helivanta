---
name: hms-transport
description: Design or review Helivanta AmbulanceConnect and EmergencyConnect calling, dispatch, crew, receiving handover and scheduled patient transport. Use for transport state, permissions, integrations and safety; never to perform live dispatch or invent triage or eligibility rules.
---

# Ambulance and patient transport

Use [hms-domain](../hms-domain/SKILL.md) for scope and read the
[transport knowledge](../../../docs/domain/transport.md) and
[current emergency contract](../../../docs/api/domains/emergency.md). For India,
also use [hms-india](../hms-india/SKILL.md) and only the affected source leads in
[India transport research](../../../docs/research/2026-10-18-india-emergency-transport.md).
Research is not current operating authority or a government adapter contract.

First classify the task: public emergency calling, authorized provider dispatch,
clinical transport review, receiving-facility handover or scheduled non-emergency
transport. Keep their permissions, clinical authority, funding and confirmed
milestones distinct. Scheduled transport is not an existing generic booking API;
check its owner and accepted contract before adding it.

Trace verified provider tenant/base, case and version, active crew/duty/vehicle,
reviewed capability/readiness and destination tenant/facility authority. Verify
exact commands and event payloads in the owning canonical contracts and handlers.
Do not replace dispatch/receiving lifecycles with a generic appointment status or
infer receiving acceptance from a hospital directory, GPS arrival or HTTP success.

For each changed handoff, specify durable request identity, atomic reservation,
acknowledgement/deadline, duplicate and out-of-order handling, bounded retry,
reconciliation/escalation owner and the safe unavailable state. A cancellation
request is not confirmed stand-down; preserve physical-action and handover history.

Never claim a public dispatch connection, crew acceptance, response time or
completed transfer without the authorized source's evidence. Keep call-first
access usable when location permission, login, identity exchange, data connectivity
or payment is unavailable. Use currently verified local emergency-service sources;
do not hardcode a universal number or activate regional coverage from old research.
No autonomous clinical triage, priority downgrade, cancellation or dispatch.

Review synthetic cases for cross-base/tenant access, caller/patient mismatch,
stale readiness, competing reservation, revoked receiving authority, lost
acknowledgement, retry after commit, late cancellation, dependency outage and
physical handover disagreement. Link actual validation and unresolved pilot/safety
gates. Never call emergency numbers, send real dispatch requests or collect live
incident data to validate this skill.
