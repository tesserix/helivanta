---
name: hms-domain
description: Scope Helivanta healthcare workflows, patient and care-data ownership, safety hazards, terminology and integration acceptance. Use for HMS feature design, implementation and review; unrelated tooling and purely visual edits do not require domain review.
---

# Helivanta healthcare domain

Read root `AGENTS.md` and the [domain index](../../../docs/domain/README.md).
Choose only the affected references and current owner contracts; do not load all
domain skills for every task. Documentation describes both targets and implemented
slices: verify current handlers, canonical schemas, migrations and relevant issues
before claiming a capability exists.

Establish country/state, provider and care setting, tenant/legal entity and narrower
hospital/outlet/base/facility scope. India work additionally uses
[hms-india](../hms-india/SKILL.md); another country needs its own verified sources
and accountable applicability review. Do not activate Australian coding, medicines,
privacy or funding rules merely because they appear in a generic HMS guide.

For the affected journey, identify users (including carers and external providers),
owner records and consumers, legal transitions, current object authority, evidence
of completion, freshness, retries/concurrency and the recovery owner. Read
[patient and care workflows](../../../docs/domain/patient-care.md) when identity,
scheduling, clinical records, medication, results or handovers are involved.

Route emergency/ambulance/scheduled transport work to
[hms-transport](../hms-transport/SKILL.md), and roles, consent-related access,
delegation, approvals or break-glass to
[hms-organization-access](../hms-organization-access/SKILL.md). The domain index
routes pharmacy, diagnostics, revenue, facilities and workforce work to existing
specialists. Domain guidance supplements the engineering and stack skills required
by the working agreements; it does not replace their validation gates.

Before a consequential change, record a short hazard/acceptance note in the existing
issue, specification or implementation summary: harm from wrong identity, missing,
late or misleading data; prevention/detection; safe fallback; accountable reviewer;
and synthetic adverse cases. No separate document is needed for routine changes.
Unapproved clinical/legal/eligibility rules remain unresolved and inactive. A
configurable field is not approval to enable a hazardous workflow.

Preserve single owners, original evidence, correction history and purpose-limited
disclosure. Never invent treatment, dose, triage, dispatch priority, eligibility or
funding rules. Report actual tests and distinguish implemented, deployed,
integration-acknowledged and qualified-review states. This skill grants no live
patient, emergency-service, financial, regulatory or production authorization.
