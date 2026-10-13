# Security, HIPAA and SOC 2 readiness

[Architecture index](README.md) · Proposed controls, not an attestation

## Scope and interpretation

“SOC” is interpreted as **SOC 2**. Propose Security, Availability and Confidentiality as the initial examination scope; include Processing Integrity and Privacy according to customer commitments and auditor scoping. A Type I examination addresses design at a point in time; Type II also addresses operating effectiveness over a period. No deployment choice confers a SOC 2 report by itself. See the [AICPA SOC overview](https://www.aicpa-cima.com/resources/landing/system-and-organization-controls-soc-suite-of-services) and [Trust Services Criteria](https://www.aicpa-cima.com/resources/download/2017-trust-services-criteria-with-revised-points-of-focus-2022).

HIPAA applies where Helivanta's role and processing fall within the regulated relationship. Establish covered-entity/business-associate roles and appropriate agreements before processing US ePHI. The Security Rule spans administrative, physical and technical safeguards; architecture covers only part of that program ([HHS Security Rule summary](https://www.hhs.gov/hipaa/for-professionals/security/laws-regulations/index.html)). India-first customers also require applicable local legal/privacy/clinical obligations to be assessed; this document does not treat HIPAA as a replacement for them.

The HHS page reviewed for this design describes the cybersecurity update as a proposed rule and says the current rule remains in effect during rulemaking. Track its actual final/effective status with counsel before launch; do not label proposed requirements as current law ([HHS rulemaking status](https://www.hhs.gov/hipaa/for-professionals/security/hipaa-security-rule-nprm/index.html)). The safeguards below are proposed Helivanta requirements regardless of whether a specific technique is mandated by a particular rule.

## Threat model

Protect clinical records, patient identity links, hospital CRM/contact history, staff/payroll data, financial and stock ledgers, identity credentials, encryption keys and audit evidence. Threats include anonymous attackers, authenticated users of other tenants, staff in a sibling hospital with no sharing grant, excessive support privileges, compromised workloads/dependencies, malicious integrations and ransomware. Trust boundaries are browser→BFF, BFF→API, service→service, API→policy/data, hospital→hospital, tenant→tenant and platform→external processor. Validate identity, scope and purpose at every crossing.

| Threat                                            | Required prevention/detection                                                                                                                            |
| ------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Cross-tenant or sibling-hospital IDOR             | Tenant + hospital/grant query scope, RLS, object authorization, masked existence, negative tests on detail/search/count/export                           |
| Shared identity mistaken for shared record access | Separate registration/link/discovery/clinical/CRM grants; source ownership and current policy at every read                                              |
| Stolen workstation session                        | MFA, idle lock, absolute expiry, current revocation checks, CSRF protection and contextual hospital banner                                               |
| Compromised product pod                           | Per-product credentials and workload policy; no session signing key, migration key or unrestricted system pool                                           |
| Accidental PHI disclosure                         | Allowlisted log attributes, generic errors, protected audit references, no body/query/token capture, no third-party replay/analytics on clinical screens |
| Insider bulk export                               | Separate export permission, step-up and justification, scope preview, review where required, durable audit and anomaly alerts                            |
| Ransomware / evidence destruction                 | Restricted immutable archives and backups, separated recovery identities, tested clean restore, integrity verification                                   |
| CRM misuse                                        | Channel/purpose permission, suppression at send time, hospital-sharing rules, recipient verification and audited campaign execution                      |
| Malicious AI document                             | Source access checks, prompt-injection boundaries, no model-selected tenant scope and human approval for consequential tools                             |

## Control and evidence matrix

Mappings are an engineering crosswalk, not an auditor/legal determination that every requirement is satisfied. Owners below are accountable roles to assign before release; none is implied to be staffed today.

| Control                             | Implementation requirement                                                                           | HIPAA / SOC 2 relevance                                 | Evidence and owner                                                                                |
| ----------------------------------- | ---------------------------------------------------------------------------------------------------- | ------------------------------------------------------- | ------------------------------------------------------------------------------------------------- |
| Risk management                     | Inventory data flows/assets, evaluate tenant/hospital threats and vendors, track treatment decisions | 164.308(a)(1); CC3/CC9                                  | Risk register and review after material changes; Security                                         |
| Unique identity and access          | Zitadel, MFA, least privilege, expiring hospital assignment, joiner/mover/leaver process             | 164.308(a)(3–4), 164.312(a)/(d); CC6                    | Access review, offboarding drills and provisioning audit; IAM + hospital admin                    |
| Minimum necessary / purpose         | Field/action restrictions, sharing agreements and documented exceptions appropriate to use           | Privacy Rule where applicable; Confidentiality/Privacy  | Policy decisions and sample reviews; Privacy + clinical governance                                |
| Isolation                           | Tenant and hospital RLS, object permissions, scoped caches/events/files/AI                           | 164.312(a); CC6                                         | Automated adversarial matrix and independent penetration test; Engineering                        |
| Audit controls                      | Durable read/write/disclosure/export evidence, tamper-evident archive, restricted access             | 164.312(b), 164.308(a)(1)(ii)(D); CC7                   | Completeness checks, alert drills and review records; Security operations                         |
| Integrity                           | Signed record versions, checksums, immutable ledger entries, idempotency and reconciliation          | 164.312(c); Processing Integrity                        | Duplicate/reorder/failure tests, ledger/result reconciliation; product owners                     |
| Transmission and storage            | TLS, strict mesh mTLS, native datastore TLS, managed encryption and key lifecycle                    | 164.312(e), risk-based safeguards; CC6/C1               | Policy checks, encryption inventory, key/recovery review; Platform                                |
| Resilience                          | HA, bounded queues, tested PITR, hospital-specific restore, downtime care procedures                 | 164.308(a)(7); Availability                             | Timed restore/DR exercise and clinical downtime drill; SRE + clinical owner                       |
| Incident and breach response        | Triage, containment, forensic preservation and notification workflow under applicable rules          | 164.308(a)(6), applicable Breach Notification Rule; CC7 | Tabletop, incident record and counsel-reviewed decision trail; incident commander/privacy officer |
| Vendor governance                   | Appropriate BAAs/contracts, subprocessors, residency and retention assessment                        | 164.308(b), 164.314; CC9                                | Signed agreements and service eligibility register; Legal/procurement                             |
| Change and vulnerability management | Reviewed GitOps, supply-chain controls, scans, remediation SLAs and rollback                         | 164.308 risk management; CC8/CC7                        | Reviews, SBOM, signed build provenance and release evidence; Engineering/SRE                      |
| Physical/workforce safeguards       | Device controls, facility access, training, acceptable use and access removal                        | 164.310, 164.308(a)(5); CC1/CC2/CC6                     | Training and device/access records; organization and hospital owners                              |

Cloud encryption does not remove business-associate obligations. Review every provider that creates, receives, maintains or transmits ePHI, including backups, observability, support, email and AI; a “HIPAA eligible” service still needs appropriate configuration and agreements ([HHS cloud guidance](https://www.hhs.gov/hipaa/for-professionals/special-topics/health-information-technology/cloud-computing/index.html)). Self-hosting Zitadel, Valkey or Qdrant does not remove infrastructure or operational responsibilities.

## Audit without making logs a second clinical database

Record actor ID and credential reference, tenant, acting and owning hospitals, action, resource reference/version, purpose, decision, policy/sharing agreement version, outcome, timestamp and correlation ID. Sensitive before/after values belong in the access-controlled clinical change history; the security event references that version/digest instead of copying diagnoses and contact data into generic logs. Clinical record reads, denied reads, searches, result views, downloads, shares, merges, emergency access and administrative actions are within scope. Query terms can themselves be PHI.

For writes, commit the domain change and append-only audit staging row in the same product transaction. For reads, durably commit an access/disclosure intent for the authorized resource set before releasing sensitive bytes, then record the response outcome. A read-intent record does not prove the client received every byte. A queue of unpersisted in-memory logs is insufficient. Streaming exports record a scoped manifest/chunks and final delivery status.

A security-owned collector transfers staged records to a retention-locked archive with signed manifests/checkpoints; product identities cannot alter archived evidence. Database ACLs prevent the normal application role from updating/deleting staged audit entries. Alert on collector lag and completeness gaps. A hash chain alone is not immutable if an attacker can rewrite the full chain; separate ownership and locked retention are required.

If the archive is down but bounded durable staging is healthy, continue under the approved buffering policy and alert. If required audit cannot be persisted, refuse the protected operation. The clinical downtime plan must cover this failure; never silently drop audit to keep an endpoint green.

## Retention, legal hold and patient rights

| Data class                                    | Proposed policy decision                                                                                                                                              |
| --------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Clinical, pharmacy, lab and financial records | Jurisdiction/customer-specific retention schedule, legal hold and signed-record preservation; clinical owner/legal approval required                                  |
| HIPAA-required Security Rule documentation    | Retain for six years from creation or last effective date, whichever is later, where applicable; this is not a blanket medical-record or all-log retention period     |
| Security audit                                | Propose 90 days searchable plus a restricted long-term archive; exact archive period derives from assessed obligations/contracts, not an assumed universal HIPAA rule |
| General operational logs                      | Propose 30 days with PHI excluded; security events follow their own policy                                                                                            |
| CRM enquiry/campaign history                  | Purpose-specific schedule, opt-out/suppression preservation where justified; do not inherit medical-record retention automatically                                    |
| Backups and WAL                               | Proposed 35-day PITR window plus policy-driven longer backups; deletion requests account for expiry and restore suppression                                           |
| Vectors/caches/exports                        | Short, purpose-bound lifetimes; prompt deletion/tombstones following source policy and access withdrawal                                                              |

Maintain a rights-request workflow for access, amendment, permitted disclosure accounting, restriction and deletion where applicable. A request to delete a CRM contact does not authorize deletion of legally retained clinical records. Legal holds are explicit, scoped and reviewed. Restores reapply deletions/tombstones, holds, revocations and sharing epochs before the environment can serve users. Cite the retention basis on policy records; the six-year documentation rule is described in the [HHS summary](https://www.hhs.gov/hipaa/for-professionals/security/laws-regulations/index.html).

## Keys and operational access

Application, tenant and user secrets belong in **OpenBao**, with environment/product/hospital scope where appropriate and least-privilege runtime readers. Use GCP Secret Manager only for critical platform/bootstrap/recovery secrets, consistent with the parent repository policy. Key-encryption keys live in the approved KMS; define encryption/key versions, rotation and recovery responsibilities separately. Logs never expose secret values.

Platform administrators use separate privileged identities, phishing-resistant MFA where supported, time-bound elevation and recorded reason/approval. Production clinical access is not inherited from Kubernetes or database administration. Support impersonation is disabled by default; use attributable delegated access that preserves both operator and affected user identities. Quarterly access reviews are a proposed operating cadence, not a substitute for immediate removal when staff leave.

## Required assurance before PHI production use

Complete the legal/applicability and vendor assessment; assign security/privacy/clinical owners; approve data flows and retention; run cross-tenant **and cross-hospital** penetration tests; prove identity recovery and revocation; validate audit completeness and independent restore; exercise incident and clinical downtime response. Production readiness requires recorded evidence, not checked boxes in this design. Formal SOC 2 scope and examination are agreed with a qualified independent auditor.
