# Supply tenant-precondition repair

Issue #1170; spec ../specs/2026-10-16-supply-tenant-precondition.md.

1. Add public-route regression tests and observe the production-equivalent read
   failure locally. Prove mismatched valid writes do not create side effects.
2. Validate the optional expected tenant once in the shared Supply principal
   guard; preserve authoritative sessions, object grants and RLS.
3. Run the complete affected module and repository gates. Attach the observed
   failure, passing results and scope to a PR assigned Sam123ben.
4. Request specific approval for the concrete patch's production application
   rollout, then repeat the negative UI/API and database checks on production.
