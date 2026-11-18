# Frontend error support references

Applies to #1706. Shell emits JSON with `event=frontend_error`, severity `ERROR`,
and the safe fields in the [contract](../api/frontend-errors.md). No new vendor,
secret, database or deployment is required. Existing GKE container logging is
the operator destination. Monitoring/alert routing and residency/retention are
separate R0 issues #1700 and #1701; this change does not establish those policies.

## Investigate a user report

1. Obtain the `HV-<UUIDv4>` support reference and approximate occurrence time.
   Do not request credentials, patient details, full URLs or an exported browser
   session. Confirm the value follows the contract before using it in a query.
2. In Cloud Logging, select the authorised project and time range. Use:

   ```text
   resource.type="k8s_container"
   resource.labels.namespace_name="helivanta"
   resource.labels.container_name="shell"
   jsonPayload.event="frontend_error"
   jsonPayload.reference="HV-<UUIDv4>"
   ```

   Verify the actual container label with read-only discovery if deployment
   naming changes. Search `textPayload` for the same reference if the logging
   agent has not parsed the JSON. For recent pod-local evidence, read Shell logs
   across all replicas with timestamps and match the exact reference.

3. Check `source`, `kind`, `area` and timestamp, the deployed image revision,
   release health and nearby aggregate error signals. These fields are an
   unauthenticated observation: they do not identify an actor or prove authority.
   Do not infer a patient, account or tenant from the reference.
4. No server match can mean offline delivery, a blocked request, 429, aborted
   delivery, process restart, or log ingestion delay. The safe browser console
   entry may be the only correlation source. A reference is not a receipt of
   durable server storage. Do not collect arbitrary console output; copy only
   the fixed-shape `frontend_error` record.
5. If the reference is unavailable, retry/reload and gather the approximate time,
   product area and recovery outcome. Do not enable raw error rendering.

## Capacity and failure investigation

Browser delivery has a 1.5-second abort and five attempts/minute. Collector body
reads stop at two seconds/1 KiB and capacity is 60 attempts/minute/process.
Two replicas admit at most 120 attempts/minute; load balancing can produce 429
before that aggregate is reached. The diagnostic cap does not block business
operations. Check 403 for Origin/Host/proxy preservation, 415 for media type, 429
for volume and 503 for log-sink failure. Never relax origin or schema validation
to recover telemetry. Invalid bodies are discarded without logging their content.

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
safe reference, deployed revision, collector status, matching live log and retry
outcome. Restore the browser override in all outcomes and reload if necessary.
Never commit screenshots containing patient/staff details or credentials.

Rollback is a reviewed code revert through the normal release workflow. There
is no data migration or diagnostic database to reconcile. Do not bypass signed
images or change live Deployment parameters.
