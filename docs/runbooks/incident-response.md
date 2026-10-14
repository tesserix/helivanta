# Incident response

Manual procedure until the automated incident lifecycle in
[#53](https://github.com/tesserix/helivanta/issues/53) exists. Control: SOC 2 CC7.3–CC7.5.

## Severity

| Severity | Meaning | Response |
|---|---|---|
| SEV1 | Patient data exposed or altered across a tenant boundary, credential forgery, or production down | Start immediately, any hour |
| SEV2 | Suspected exposure not yet confirmed, a compromised account, or a major feature down | Within 1 hour |
| SEV3 | A vulnerability with no evidence it was exploited, or a degraded feature | Next business day |
| SEV4 | A hardening finding | Normal backlog |

## Roles

- **Incident commander (IC):** the first maintainer to respond (`CODEOWNERS`). The IC owns decisions and the timeline.
- **Scribe:** the second maintainer. They keep the timeline in the incident record.

## Procedure

1. **Declare.** This repository is public, so the incident record is a private draft security advisory (`Security → Advisories → New draft`), never an issue. Record the time the incident was first noticed; every regulatory deadline runs from that time.
2. **Contain.**
   - Compromised user: `POST /v1/iam/subjects/<subject>/revoke` (permission `iam.credential.revoke`). This ends every session for that subject in every tenant.
   - Leaked boot secret: rotate it per [secrets.md](secrets.md). Rotating `SESSION_SIGNING_KEY` invalidates every session.
   - Compromised build or image: stop ArgoCD auto-sync for `helivanta-api` in `tesserix-k8s` and pin the last known-good image digest.
3. **Preserve evidence** before you change any data:
   - Audit trail: `SELECT * FROM audit_events WHERE tenant_id = '<tenant>' AND occurred_at >= '<t0>' ORDER BY occurred_at;`. Run it under the tenant (RLS); the table is append-only.
   - Auth events: filter logs on `jsonPayload.audit_event` (`auth.login.*`, `auth.session.*`, `auth.tenant.switched`, `auth.credentials.revoked`).
   - Database: note the latest backup ID ([backup-and-restore.md](backup-and-restore.md)) so the recovery point is fixed.
4. **Assess scope:** affected tenants, data classes (PHI, contact details, credentials) and the time window.
5. **Notify.**
   - Each affected hospital (the Data Fiduciary) without delay, so it can meet its own DPDP Act 2023 duty to notify the Data Protection Board and data principals.
   - CERT-In within 6 hours of noticing a reportable cyber incident.
6. **Eradicate and recover:** fix through a normal PR (review, green CI) and deploy through ArgoCD.
7. **Review** within 5 business days: write the root cause, a timeline, and follow-up issues. Publish the advisory only once the fix is deployed and tenants have been told.
