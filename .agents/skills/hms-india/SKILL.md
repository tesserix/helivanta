---
name: hms-india
description: Scope India-specific Helivanta HMS domain work and identify applicable hospital workflows, state/service policies, owners and primary sources. Use for healthcare product or integration design and review; ordinary styling or unrelated tooling does not require this domain workflow.
---

# India HMS domain routing

Read root `AGENTS.md`, the [India knowledge index](../../../docs/domain/india/README.md)
and the affected current [API contract](../../../docs/api/README.md). Select the
domain references needed for the task; do not load every domain by default.

Establish the hospital/customer context: owning tenant and hospital/legal entity,
state/UT, service type, patient/care context, payer if relevant and effective date.
An India country code does not select every state law, licence or payer contract.

Separate three kinds of statements in the proposed change:

- Existing behavior, verified against owning handlers, migrations and contracts.
- Proposed Helivanta acceptance criteria, identified by the relevant `IN-*` IDs.
- External obligations, with current primary text, scope, effective date and
  accountable applicability review. Use the [source register](../../../docs/domain/india/sources.md).

Route clinical identity/care/order work to
[hms-india-clinical](../hms-india-clinical/SKILL.md), revenue/insurance work to
[hms-india-revenue](../hms-india-revenue/SKILL.md), and pharmacy/supply/facility
controls to [hms-india-regulated-operations](../hms-india-regulated-operations/SKILL.md).
Reuse existing workforce and emergency references from the knowledge index. Route
roles, departments, staff access, approvals and public/private sector work to
[hms-organization-access](../hms-organization-access/SKILL.md).

Before implementation, identify the single data owner, state transition, current
actor/object authority, source version, retry/concurrency behavior and failure
recovery. Record unresolved domain decisions without inventing a clinical protocol
or treating a draft as deployed. For a small change, put the result in its existing
specification or implementation summary; do not create a new process or document
solely to satisfy this skill.

Use synthetic edge cases from the domain reference to verify consequential
behavior. Preserve original clinical/financial evidence and cross-product lineage.
Link the contracts and `IN-*` requirements used, report actual validation and state
what still requires review. This skill authorizes no live patient, national-ID,
payment, emergency, regulatory filing or production action, and creates no
certification claim.
