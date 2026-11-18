# R0 — Pilot foundations

[Milestone 16](https://github.com/tesserix/helivanta/milestone/16), reviewed
18 November 2026. Work proceeds one issue at a time, with implementation, local
validation, required CI, deployment and applicable live acceptance before the
next issue starts. Issue descriptions are requirements and historical findings;
their current implementation and infrastructure claims must be verified.

| Order | Issue                                                      | Required outcome and acceptance                                                                                                                                           | Dependency or decision                                                                                                                                       |
| ----- | ---------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 1     | [#1706](https://github.com/tesserix/helivanta/issues/1706) | Safe error UI, correlated support reference and personal-data-free frontend reports in existing logs                                                                      | Verified application release `1cfae666`; [live evidence](../deployment/releases/2026-11-19-r0-safe-error-references.md) and pod-log delivery limits recorded |
| 2     | [#1700](https://github.com/tesserix/helivanta/issues/1700) | Running, sized monitoring; a monitored human receiver; tested database, backup, scanner, API/pod and certificate alerts with response runbooks                            | Verify current GitOps/runtime state and existing receiver configuration before changing infrastructure or sending test alerts                                |
| 3     | [#1701](https://github.com/tesserix/helivanta/issues/1701) | Terraform-owned India log bucket with at least 180 days retention, correct application/ingress/identity sinks and default-bucket exclusions; recorded Cloudflare decision | Verify current primary legal sources and actual bucket locations/retention; do not claim compliance from a source definition                                 |
| 4     | [#1702](https://github.com/tesserix/helivanta/issues/1702) | Protected 30-day backups, an independent copy, timed scratch restore and scheduled restore verification                                                                   | Confirm destination, cost and approved retention/restore actions; preserve existing data and capture state before hard-to-recover changes                    |
| 5     | [#1703](https://github.com/tesserix/helivanta/issues/1703) | Organisation-bound password recovery, audited administrator MFA recovery with re-enrolment, honest lockout guidance and a real recovery/sign-in E2E journey               | Identity-owner contracts and current account authority; no global-account reset based on an email match                                                      |
| 6     | [#1704](https://github.com/tesserix/helivanta/issues/1704) | Named staff role grant/revoke UI, attributable reasons/audit, cashier/reception separation, supported employment-record path and effective-access E2E                     | Inspect current governance and operating-role contracts before extending legacy membership endpoints; preserve narrower hospital/outlet authority            |
| 7     | [#1705](https://github.com/tesserix/helivanta/issues/1705) | Patient-linked clinical read audit, system-read audit/metric, bounded authorised access history and an architectural audit gate                                           | Verify each data owner and existing audited-read pattern; maintain patient/tenant/resource isolation and fail-closed audit consequences                      |
| 8     | [#1672](https://github.com/tesserix/helivanta/issues/1672) | Fresh, normally onboarded pharmacy acceptance tenant; protected sign-in and retained-tenant denial; independent Staff/licence journeys                                    | Follow #1677 and the accepted fresh-tenant runbook. Do not migrate/adopt the retained pre-ADR-0070 tenant or bypass unavailable identity                     |
| 9     | [#1707](https://github.com/tesserix/helivanta/issues/1707) | Evidence-backed DPA, incident contacts/process, versioned notices, grievance ownership, pilot exclusions, commercial/tax documents and two owners                         | Named hospital/platform owners and qualified legal/tax review are required; software cannot manufacture signed agreements, identities or legal approval      |

This is a delivery sequence, not pilot or regulatory approval. Monitoring,
retention, recovery and human/legal requirements remain release conditions for
real patient data even when an earlier application issue is complete. The
sequence may change when source verification reveals a dependency, with the
reason recorded here.

For #1706, see the [design](../superpowers/specs/2026-11-18-r0-safe-error-reporting-design.md)
and [implementation plan](../superpowers/plans/2026-11-18-r0-safe-error-reporting.md).
An issue stays open until its actual acceptance evidence is recorded.

Priority update, 19 November 2026: after #1706 is complete, follow the user’s
sequence **#1703 → #1705 → #1704 → #1707 → milestone 12**. The table above retains
the initial review inventory; remaining R0 requirements still gate real patient
data use. Close each issue after its actual acceptance evidence is complete.
