# Backup and restore

Control: SOC 2 A1.2–A1.3. Infrastructure lives in `tesserix-k8s` (`charts/apps/helivanta-postgres`);
the general CNPG procedure is `tesserix-k8s/docs/cnpg-backup-restore.md`.

## What runs

| Item | Value |
|---|---|
| Cluster | CNPG `helivanta-postgres`, namespace `helivanta`, 2 instances in prod |
| Continuous WAL archive + base backup | Barman object store, `gs://tesseracthub-480811-helivanta-backups-prod/cnpg` |
| Base backup schedule | Every 6 hours |
| Retention (recovery window) | 30 days |
| Credentials | Workload Identity (`helivanta-db-backup@…`); no static keys |

## Verify (monthly, and after any infrastructure change)

```bash
kubectl get cluster helivanta-postgres -n helivanta \
  -o jsonpath='{.status.firstRecoverabilityPoint}{"  "}{.status.lastSuccessfulBackup}{"\n"}'
kubectl get backups.postgresql.cnpg.io -n helivanta
```

## Restore test (quarterly)

1. Choose a completed backup ID (Barman timestamp, `YYYYMMDDTHHMMSS`).
2. In `tesserix-k8s`, set `restoreCheck.enabled: true` and `restoreCheck.backupID` for `helivanta-postgres`, then commit and let ArgoCD sync. The check restores into a separate cluster and leaves production untouched.
3. Record in the restore-test issue: the backup ID, the restore time (RTO), the recovery point (RPO), and row counts for `medicore_visits`, `pharmacy_dispenses`, `lab_orders` and `audit_events`.
4. Set `restoreCheck.enabled: false` again.

Real restores follow `cnpg-backup-restore.md`: bootstrap a new cluster from the object store, verify it, then cut over. Never restore in place.
