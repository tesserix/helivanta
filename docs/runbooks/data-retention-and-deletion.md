# Data retention and deletion

Control: SOC 2 C1.2, P4.2. The automated lifecycle is tracked in
[#65](https://github.com/tesserix/helivanta/issues/65) and [#480](https://github.com/tesserix/helivanta/issues/480);
medical record retention periods are in [#69](https://github.com/tesserix/helivanta/issues/69).

| Data | Where | Retention today | Deletion |
|---|---|---|---|
| Clinical and workforce records | Postgres, per-tenant RLS | Kept for the tenant's contract | On tenant offboarding, by a reviewed SQL change under the tenant |
| `audit_events`, `workforce_audit` | Postgres, append-only | Kept with the records they describe | Never by the application: a trigger rejects UPDATE and DELETE. Offboarding removal is a reviewed schema-owner change |
| Outbox / processed events | Postgres | Pruned automatically after publication | Automatic |
| Backups | GCS | 30-day recovery window | Barman prunes base backups together with their WAL |
| Application logs | Cloud Logging | The project's log bucket retention | Expire automatically |

## Rules

- Deleted data stays recoverable from backups for up to 30 days. Tenant contracts must say so.
- Offboarding a tenant: export on request, delete under the tenant's RLS scope, record the date in the offboarding issue, and confirm deletion again after the 30-day backup window.
- Logs must not contain patient data. `pkg/logging` redacts phone, ABHA and Aadhaar numbers, and audit rows record column names, never values.
