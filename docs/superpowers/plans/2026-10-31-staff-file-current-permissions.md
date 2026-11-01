# Current Staff file permissions implementation

Follow the matching design and the full A01–A12 acceptance ledger. This fixes
current permission loss across existing file I/O; it does not activate scoped HR
roles or settle employment/current-person policy.

1. Reproduce stale IAM permission release through real IAM, PostgreSQL, OpenFGA,
   a signed session and actual released bytes. Retain the hospital manager grant
   and stale graph proof to isolate the permission-source defect.
2. Make each existing document gate declare its read/write requirements. Refresh
   the original authenticated permission resolver once before each owner phase;
   require both employment-evidence permissions from that one current result.
3. Preserve owner/purpose/release/generation checks and keep storage outside SQL
   transactions. Deny without private bytes, success audit or new scan receipt
   when authority ends or becomes unavailable during I/O.
4. Test upload loss after quarantine, each employment permission independently,
   dependency failure, and normal Staff/benefit/tax/evidence behavior. The graph
   outage fixture must actually return an error; soft store deletion is not a
   reliable unavailability mechanism. Audit reads need the real file RLS context.
5. Correct the older Global API lock observer to match the current proposal-first
   lock query, retaining blocker-PID observation and the required 403/no-write
   result. Do not hide the existing CI failure or relax concurrency assertions.
6. Run Workforce, journey and Global API race suites, vet/build/lint and contract
   freshness. Record results, invocation/fixture corrections and remaining source-
   lock, HR owner/UI, current-person and lifecycle/cross-module gates. Update the
   draft only after its earlier CI head is terminal.
