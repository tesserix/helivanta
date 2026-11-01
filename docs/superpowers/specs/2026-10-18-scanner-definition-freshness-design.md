# Scanner definition freshness

Issue: #1242

## Problem

Scans and worker start-up both refused ClamAV definitions older than 24 hours. On 2026-10-18 upstream published daily 28157 24h04m after 28156, while freshclam only checked every two hours. Both workers crash-looped, ArgoCD reported `helivanta-api` Degraded, and the credential reminder worker (#1108) went down with them.

## Decision

- **Scan gate (fail closed):** `ClamAV.Status` refuses definitions older than `MaxDefinitionAge` (48h), or more than 5 minutes in the future. Every scan already checks the gate before and after streaming, so no file is released on stale definitions.
- **Readiness:** `ClamAV.Ready` fails from `ReadyDefinitionAge` (36h). Readiness never restarts a pod. `HelivantaDocumentWorkerUnavailable` and the supply-evidence equivalent fire at least 12 hours before scans would refuse.
- **Start-up:** workers no longer exit on scanner state. An unreachable or stale scanner fails readiness and scans; it does not stop Temporal workers that do not scan.
- **Updates:** freshclam checks hourly (`Checks 24`, tesserix-k8s), so a new publish lands within an hour.

## Not covered

- Definition age is not exported as a metric. The workers have no ingress (`ingress: []`), and opening a scrape path into a PHI worker needs its own review. The readiness-based alert provides the warning instead.


## Workforce database enforcement

The Workforce document trigger was still enforcing the historical 24-hour bound after the scanner adopted this policy. Forward migration `0067_workforce_document_scanner_freshness` aligns the trigger with the same 48-hour maximum. It preserves the latest recovery, identity, scope, immutable provenance, transition and five-minute future-date checks; historical migration SQL is unchanged. This migration is forward-only: retain its ledger entry and use a separately reviewed forward correction if policy changes.

Regression coverage uses the real narrow worker database role and PostgreSQL: 30-hour and 47-hour definitions release the exact uploaded bytes, while 49-hour and six-minute-future definitions refuse promotion and download. The database separately rejects invalid provenance even through the scoped worker role.
