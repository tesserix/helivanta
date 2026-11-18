# Frontend error support references

Applies to #1706. Shell emits JSON with `event=frontend_error`, severity `ERROR`,
and the safe fields in the [contract](../api/frontend-errors.md). The verified
operator destination is the Shell's Kubernetes container log. Monitoring/alert
routing and residency/retention remain separate R0 issues #1700 and #1701.

## Current logging boundary

Verified against the production cluster on 18 November 2026 UTC:

- GKE has `loggingService: none`; Cloud Logging has no copy of these reports.
- The existing OTel agent's filelog include list excludes Helivanta. Its
  gateway/Kafka/ClickHouse pipeline therefore has no copy either.
- Successful reports are readable from the running Shell pods with authorised
  Kubernetes log access. These logs can be lost on rotation or pod removal.
- A browser-only reference remains useful when network delivery is aborted or
  unavailable. Neither a reference nor HTTP 204 proves durable storage.

Central forwarding must preserve the diagnostic allowlist and the applicable
India residency/retention policy. Whole-container forwarding needs separate
privacy review: existing CSP reports and other application logs have different
payloads. This release does not enable a central collector or establish retention.
See the [deployed acceptance record](../deployment/releases/2026-11-19-r0-safe-error-references.md).

## Investigate a user report

1. Obtain the `HV-<UUIDv4>` support reference and approximate occurrence time.
   Do not request credentials, patient details, full URLs or an exported browser
   session. Confirm the value follows the contract before using it in a query.
2. Confirm the authorised kubeconfig context and namespace. Query all current
   Shell replicas and emit only the matching fixed-shape report. Replace the
   final argument with the actual reference:

   ```sh
   kubectl logs deployment/helivanta-shell -n helivanta \
     -c shell --all-pods=true --prefix=true --since=15m |
     python3 -c '
   import json, sys
   reference = sys.argv[1]
   fields = ("event", "version", "reference", "source", "kind", "area")
   for line in sys.stdin:
       if reference not in line:
           continue
       try:
           row = json.loads(line[line.index("{"):])
       except (ValueError, json.JSONDecodeError):
           continue
       if row.get("event") == "frontend_error" and row.get("reference") == reference:
           print(json.dumps({key: row.get(key) for key in fields}))
   ' 'HV-<UUIDv4>'
   ```

   Adjust the time range to the occurrence. Verify deployment/container naming
   with read-only discovery if it changes. Do not export unrelated container
   output. This command does not retrieve removed pods or rotated log segments.

3. Check `source`, `kind`, `area`, the occurrence time and deployed image revision.
   These fields are unauthenticated observations, not proof of actor authority.
   Do not infer a patient, account or tenant from the reference.
4. No server match can mean offline delivery, a blocked request, 429, an aborted
   request, rotation or pod replacement. Copy only the fixed-shape
   `frontend_error` browser record when that is the remaining correlation source;
   raw browser/framework errors have a separate payload and may contain private
   details. Do not collect arbitrary console output.
5. If the reference is unavailable, retry/reload and gather the approximate time,
   product area and recovery outcome. Keep raw error rendering disabled.

## Capacity and failure investigation

Browser delivery has a 1.5-second abort and five attempts/minute. Collector body
reads stop at two seconds/1 KiB and capacity is 60 attempts/minute/process.
Two replicas admit at most 120 attempts/minute; load balancing can produce 429
before that aggregate is reached. The diagnostic cap does not block business
operations. Check 403 for Origin/Host/proxy preservation, 415 for media type, 429
for volume and 503 for log-sink failure. Preserve origin and schema validation
when investigating delivery. Invalid bodies are discarded without logging input.

Existing CSP reports have a separate contract and may retain URL paths. This
runbook's privacy guarantee applies to `frontend_error` records only.

## Verify a release

CI runs `e2e/playwright.frontend-errors.config.ts` against the production Shell
build. It introduces a browser-only synthetic listbox rendering failure on the
public ambulance page, checks safe UI, exact reference in the actual server log,
Origin preservation, absent Cookie/Referer, then restores the browser override
and verifies Try again. A second case checks unhandled rejection reporting.
No emergency call, business record or production test endpoint is created.

After normal Kargo release, repeat a controlled browser-only rendering failure
in the authorised session without submitting any business command. Record the
safe reference, deployed revision, actual collector status, matching log and
retry outcome. Report browser fallback separately from server delivery. Restore
the browser override in all outcomes and reload if necessary. Never commit
screenshots containing patient/staff details or credentials.

Rollback is a reviewed code revert through the normal release workflow. There
is no data migration or diagnostic database to reconcile. Use signed images
and the normal GitOps release path.
