# Pharmacy evidence worker acceptance — 11 November 2026

The designated synthetic India pharmacy passed the processing gate in #1406.
The [machine-readable evidence](2026-11-11-pharmacy-evidence-acceptance.json)
records the initial deployed images, actual scanner definitions and individual
checks. Exact synthetic resource bindings and operator receipts remain in the
protected acceptance handoff; credentials, image bytes and private object paths
are not part of this release record.

The retained licence failure recovered through its owner retry command, from
`scanner_unavailable` to attempt 2 and a genuine clean release. A fresh
prescription image passed upload-pending, scan-pending and available. Authorized
downloads matched the original PNG bytes. A valid passive PNG containing a
harmless standard antivirus test archive reached the real worker's terminal
`rejected / malware_detected` state; it was not released, downloaded or retried
into approval. Unsupported image bytes were also refused at upload admission.

Normal tenant switching established a second verified tenant before testing
cross-tenant refusal. Cross-outlet requests returned 404, an unpermitted licence
reader received 403, and an anonymous prescription download received 401. A
temporary reader at the isolation outlet received the exact file only while its
grant was current; the same authenticated session received 404 after revocation.
The counter's original access grants were preserved.

A bounded read-only operational query verified the non-inheriting worker role,
zero memberships/elevated attributes, retained download audits and scanner
provenance. All four inspected clean releases used definitions younger than
24 hours at scan time. Six matching Temporal histories had application payloads
containing only scoped IDs, attempts and closed lifecycle results. The actual ClamAV client also passed its
clean, standalone antivirus-test, compressed-test and size-bound acceptance.

The initial antivirus string in plain PNG text was not a validated detection
fixture for this engine and was excluded from rejection acceptance. No scanner
configuration, definitions policy or lifecycle row was altered to manufacture a
verdict. The final rejection fixture was independently admitted by the real
image inspector and detected by the real scanner before owner-API use.

This accepts worker processing only. The licence review/UI gate (#1581) and
prescription-sale evidence gate (#1477) still require their own migration,
build, browser and production release checks. TEST declarations remain
unreviewed; this record certifies no pharmacist, prescription, medicine or
licence. No real dispensing or payment transfer was performed. Alert delivery
and the broader country/regulatory acceptance matrix remain outside this proof.
