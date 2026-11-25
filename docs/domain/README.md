# Helivanta healthcare domain knowledge

This index routes coding agents to maintained Helivanta knowledge. It is a design
and review guide, not proof of deployed functionality, legal applicability,
clinical approval or standards conformance. Start with the
[product ownership and coverage](../api/portfolio.md) and the affected
[API contract](../api/README.md); inspect its current implementation before changing
behavior.

## Choose the knowledge and skill

| Task                                                                                | Knowledge / owner contract                                                                                                                                                                    | Skill                                                                                                                                                                  |
| ----------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Healthcare scope, hazards, care setting and cross-domain journey                    | This index; [patient and care workflows](patient-care.md)                                                                                                                                     | [hms-domain](../../.agents/skills/hms-domain/SKILL.md)                                                                                                                 |
| India applicability and source review                                               | [India index](india/README.md), [source register](india/sources.md)                                                                                                                           | [hms-india](../../.agents/skills/hms-india/SKILL.md)                                                                                                                   |
| Patient identity, encounters, appointments, nursing, orders, diagnostics, discharge | [Patient/care guide](patient-care.md), [patient/MediCore](../api/domains/patients-and-clinical.md), [beds](../api/domains/beds-and-admissions.md), [laboratory](../api/domains/laboratory.md) | [hms-domain](../../.agents/skills/hms-domain/SKILL.md); for India, [hms-india-clinical](../../.agents/skills/hms-india-clinical/SKILL.md)                              |
| Ambulance calling, provider dispatch, receiving, scheduled transport                | [Transport guide](transport.md), [EmergencyConnect](../api/domains/emergency.md)                                                                                                              | [hms-transport](../../.agents/skills/hms-transport/SKILL.md)                                                                                                           |
| Prescribing/dispensing/administration boundary, catalogue, stock, procurement       | [Pharmacy](../api/domains/pharmacy.md), [supply](../api/domains/supply.md), [drug master](../drugmaster.md)                                                                                   | [hms-domain](../../.agents/skills/hms-domain/SKILL.md); India controls: [hms-india-regulated-operations](../../.agents/skills/hms-india-regulated-operations/SKILL.md) |
| Tariffs, patient liability, insurance, claims and settlement                        | [Ownership](../api/portfolio.md), [India billing](india/billing-and-insurance.md)                                                                                                             | [hms-domain](../../.agents/skills/hms-domain/SKILL.md); India funding: [hms-india-revenue](../../.agents/skills/hms-india-revenue/SKILL.md)                            |
| Workforce, departments, privileges, consent access, proxy, break-glass, approvals   | [Organization index](organization/README.md), [workforce](../api/domains/workforce.md), [MediConnect](../api/mediconnect.md)                                                                  | [hms-organization-access](../../.agents/skills/hms-organization-access/SKILL.md)                                                                                       |
| Equipment, maintenance, quality, waste, facility readiness                          | [Coverage and gaps](../api/portfolio.md), [India facility/quality](india/facility-and-quality.md)                                                                                             | [hms-domain](../../.agents/skills/hms-domain/SKILL.md); India controls: [hms-india-regulated-operations](../../.agents/skills/hms-india-regulated-operations/SKILL.md) |
| Terminology, FHIR/HL7/DICOM, external systems, reporting and downtime               | [Patient/care guide](patient-care.md), [integration contracts](../api/integration-contracts.md), [schema ownership](../api/schemas.md)                                                        | [hms-domain](../../.agents/skills/hms-domain/SKILL.md) plus the affected domain skill                                                                                  |

Load only the rows affected by the task. Domain skills complement the applicable
engineering, security, distributed-systems, UX, testing and stack guidance; they
do not require a full hospital audit for every edit.

## Jurisdiction and review

Record country/state, provider type, care setting, service, legal data owner,
effective policy/version and accountable reviewer. Distinguish hospital policy,
professional authority, law, payer agreement and external technical contract.
Each has different applicability and evidence. India-specific acceptance criteria
are in the existing `IN-*` references, not universal regulations.

For a proposed Australian deployment, evaluate ICD-10-AM/ACHI, AMT, healthcare
identifiers, Medicare/private funding and applicable Commonwealth/state/territory
privacy and health-record requirements with current primary sources and qualified
review. These are discovery topics, not activated Helivanta defaults. Do not
substitute them for India profiles or assume an Australian deployment is supported.
Likewise, terminology availability does not establish licensing or redistribution
rights; verify the actual edition, territory and intended use.

## Before implementing a consequential workflow

Use the existing issue/specification to record:

1. Users and care setting, including patient/carer, registrar, clinician, nurse,
   allied health, pharmacist, diagnostics, finance, operations and external actors
   actually affected by this change.
2. Owning records, consumers, canonical schemas and patient/encounter/resource
   references; current scope and purpose authority for each read and command.
3. Legal transitions and what evidence proves completion; version/concurrency,
   idempotency and external acknowledgements.
4. Hazards from wrong identity, missing/stale/corrected information or failed
   handover; prevention/detection, safe downtime fallback and recovery owner.
5. Synthetic acceptance cases and required clinical, pharmacy, terminology,
   privacy/security, operational or payer reviewers. Keep unapproved decisions
   explicitly unresolved; configuration alone does not make them safe to activate.

## Agent discovery and maintenance

Canonical skills live in `.agents/skills/`; `.claude/skills/` uses relative symlinks
to the same definitions. Root [AGENTS.md](../../AGENTS.md) routes by workflow even
when an agent does not discover repository skills automatically. Do not claim
that a tool loaded a skill unless it was actually read. New conversations may be
needed for native discovery after installation; the explicit file links work now.

For changes to these instructions, validate skill frontmatter, local links and
Claude symlinks, then review the scenarios in
[skill acceptance](skill-acceptance.md). No app build is needed when no application
target or executable behavior changes. Application changes still use the full
affected-target gates in root instructions.
