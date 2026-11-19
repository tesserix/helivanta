# Implementation plan: #1683

1. Add canonical shared source-product/ingredient and import command models.
2. Test and implement bounded current pack search and locked owner resolution.
3. Add nullable immutable outlet source links, scoped search/import commands,
   replay/conflict checks and prevention of linked identity edits.
4. Add catalogue search/select/import UI with provenance and manual entry retained.
5. Test and implement bounded licensed HTTPS release fetch/staging with hash replay;
   document six-hour scheduling, failure recovery and activation prerequisites.
6. Regenerate contracts/release classifications; run Go/SDK/UI tests, lint and builds.
7. Open and validate the PR, merge after green CI, and deploy only to a verified
   test environment. No live production mutations or fabricated drug records.
