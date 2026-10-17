# SOC 2 control map

Where each control lives, so evidence can be gathered quickly. Tracking issue: [#1112](https://github.com/tesserix/helivanta/issues/1112).

| Criterion | Control | Evidence |
|---|---|---|
| CC6.1 Logical access | Every route declares a permission; authz fails closed (503); forced RLS on every tenant table | `internal/archtest`, `LintRLS` at boot |
| CC6.1 Sessions | Signed session tokens with idle deadline and revocation watermark; HttpOnly, Secure, SameSite=Lax cookie | `pkg/authn`, `iam/signout.go` |
| CC6.2–6.3 Access reviews | Quarterly review | [access-review.md](access-review.md) |
| CC6.6 Transport | HSTS, nosniff, frame denial and referrer policy on every response | `internal/httpserver`, `apps/shell/lib/security-headers.ts` |
| CC6.7 Data at rest | CNPG volumes on Google-encrypted persistent disks; backups in GCS with Workload Identity | [backup-and-restore.md](backup-and-restore.md) |
| CC6.7 Field encryption | Staff bank account, PAN and UAN encrypted in OpenBao Transit with a non-exportable per-tenant key bound to hospital, staff and field; masked reads, permissioned and audited reveal; fail closed with 503 | `pkg/transit`, `workforce/staff_identifiers.go`, [secrets.md](secrets.md#runtime-transit-access) |
| CC7.1 Vulnerabilities | govulncheck, pnpm audit, gosec, Trivy, Dependabot | [vulnerability-management.md](vulnerability-management.md) |
| CC7.2 Monitoring and audit | Append-only `audit_events` for clinical changes; `audit_event` tagged auth logs | `pkg/audit`, [incident-response.md](incident-response.md) |
| CC7.3–7.5 Incidents | Severity, containment, notification | [incident-response.md](incident-response.md) |
| CC8.1 Change management | PRs with required CI checks; `CODEOWNERS`; actions pinned to SHAs; signed images with SBOM | `.github/` |
| A1.2–1.3 Availability | 6-hourly backups, WAL archive, quarterly restore test | [backup-and-restore.md](backup-and-restore.md) |
| C1.2 Disposal | Retention and deletion rules | [data-retention-and-deletion.md](data-retention-and-deletion.md) |

## Known gaps and compensating controls

| Gap | Compensating control | Owner decision |
|---|---|---|
| PRs need 0 approvals, so maintainers can self-merge | Required CI checks, `CODEOWNERS` review requests, squash history | Raise the ruleset to 1 approval and code-owner review |
| Database connections use `sslmode=require`, not `verify-full` | Connections are TLS-encrypted and never leave the cluster network; only the server identity is unverified | Mount the CNPG CA and switch to `verify-full` in `tesserix-k8s` |
| Patient DOB, phone and email have no field-level encryption | Forced RLS, a separate non-BYPASSRLS app role, storage encryption, log redaction, audit trail | Extend the Transit field encryption already used for staff identifiers (#30) |
| Session and idle lifetime are 12 hours (#1099) | Server-enforced idle deadline, revocation on sign-out, admin revoke | Confirm the clinical-shift rationale or shorten |
| Auth events are logs, not database rows | Tagged, structured and centrally collected | Durable store in [#54](https://github.com/tesserix/helivanta/issues/54) |
| Script CSP not enforced | Framing, base-uri and object-src restricted | [#1113](https://github.com/tesserix/helivanta/issues/1113) |
