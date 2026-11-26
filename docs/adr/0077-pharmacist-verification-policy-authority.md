# ADR-0077: Clinical approval of pharmacist-verification policy

Date: 2026-11-26. Status: **proposed — product-owner decision required**.

This proposal addresses ORG-D5 in the [organization decision register](../domain/organization/README.md#open-decisions) for [pharmacist verification #169](https://github.com/tesserix/helivanta/issues/169). It is not an accepted clinical policy, an operating grant, a registration attestation or permission to activate supply. The [full implementation and acceptance design](../design/pharmacist-verification.md) remains authoritative for the remaining work.

## Problem

Verification categories, second-check conditions, escalation and emergency configuration change a medication process. Tenant ownership and an HR or professional-record permission do not establish clinical authority. Allowing the same administrator to write and activate these rules would bypass the accepted separation in ADR-0044 and ORG-AU-04/08. The existing registration-verification API expressly returns `clinical_authority: not_evaluated`.

## Proposed decision

Require an independently authorized **clinical-governance approver**, in addition to the policy publisher, before any new pharmacist-verification policy version can become effective. The author/publisher and clinical approver must be different professional people, including when one person has multiple accounts.

1. The publisher may prepare the non-patient policy configuration only within a current scoped assignment. Publishing or owning the tenant grants no patient-record access, pharmacist decision authority, professional attestation or clinical privilege.
2. The clinical approver needs a separate current governance assignment covering the exact hospital-owned pharmacy/outlet or independently owned pharmacy/outlet. Pharmacy-policy review also requires the owner's current professional identity, registration and the explicitly reviewed local privilege/designation for this act. Being a registered pharmacist alone does not make someone a clinical-governance approver.
3. The policy owner must bind the approval to the exact immutable proposed policy version and the approver's independently verified professional person. A material change needs a new version and independent approval. An older approval cannot activate changed content.
4. Activation rechecks the publisher's current scope, professional source/assignment dependencies, approver eligibility, approval content and effective dates. Unavailable authority denies activation. Revoked/expired authority cannot be replaced with tenant ownership, a cached role, an HR review or the author's self-approval.
5. Clinical decisions and supply bind the effective policy version and recheck current source/actor authority at their consequential boundary. A policy change that invalidates a review requires a new review; it never rewrites immutable historical evidence.
6. Default proposed behavior requires verification for all prescription supplies. Exceptions are not inferred from a product label, an empty flag list, an outage or a broadly privileged owner. The policy cannot override a stock/licence/drug-owner prohibition or make an otherwise unsupported controlled-medicine transaction supported.
7. Emergency supply starts disabled. Activation additionally needs an accountable qualified policy defining eligible actors and scope, prohibited cases, reasons, the retrospective-review window and escalation/recovery. Approval of this ADR alone supplies none of those values.

## Bootstrap and implementation gates

The initial clinical-governance appointment must have a separately reviewed authoritative source. This ADR does not create that source or permit an administrator to invent it. Until the person/account linkage, professional verification, local privilege/designation and independently authorized appointment can be established, setup shows an explicit missing-authority state and clinical activation remains unavailable.

Implement the accepted decision through the existing IAM/owner boundaries and canonical contracts. Do not add module-private grants, a second practitioner master, direct consumer queries of professional tables or a blanket system-role grant. Declare and review any new platform permission/template through its normal versioned migration process. No new permission vocabulary or executable migration is established by this proposal.

Retain immutable policy versions, attributable proposals/approvals/activation, reasons and source/version references. Define retained command receipts, concurrency conflicts, effective-time rules, invalidation, revocation and audit-failure behavior in the implementation contract. Policy editors need names and roles, not opaque account IDs; policy access does not disclose patient data.

## Alternatives

- **Owner or administrator approval alone:** rejected because organizational ownership is not clinical authority.
- **Every registered pharmacist can publish clinical policy:** rejected because professional registration and local clinical-governance authority are separate.
- **One person approves through a second account:** rejected because account independence is not professional-person independence.
- **Nightly reconciliation as the only eligibility check:** rejected because an expired/revoked credential or assignment must block a consequential action immediately.

## Acceptance after a decision

Prove that an owner without clinical authority cannot activate policy; author/self approval and second-account self approval fail; wrong tenant/outlet, stale content, changed policy, expired/revoked source, concurrent activation and authorization/audit outages fail closed; exact retries recover the original result without duplicate activation. Prove correct behavior for hospital-owned and independent pharmacy scopes, with no authority transfer through supplier grants. Test default all-prescription review, preservation of existing source prohibitions, history after later policy changes and emergency-disabled behavior.

Update the organization decision register only after product-owner acceptance. Retain separate accountable clinical/legal applicability review and full #169 acceptance before activation or closure.
