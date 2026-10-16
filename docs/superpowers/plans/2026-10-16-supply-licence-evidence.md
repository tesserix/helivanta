# Licence evidence implementation

Spec: [supply-licence-evidence](../specs/2026-10-16-supply-licence-evidence.md). Parent stories remain open.

1. Prove organisation object scope fails, then add the non-clinical opaque key branch; retain patient/staff key compatibility.
2. Generate bounded evidence intent input/types from the Supply contract. Add immutable source-version-bound evidence schema, role-restricted RLS and isolated worker grants/policies. Reproduce HTTP denial/version/retry/concurrency failures before handlers.
3. Add quarantine byte intake and atomic scan-request outbox, clean-only generation-bound download, revoke and retry operations. Reuse shared storage/inspection and retain current authorization checks around external I/O.
4. Add the dedicated processor, Temporal workflow/validated durable-start consumer, sanitized retry states and exact release provenance; test rollback/copy ambiguity and role isolation against real disposable PostgreSQL/GCS/scanner protocol fixtures. Extract common infrastructure only with both consumers covered.
5. Add accessible reusable evidence controls in the private licence UI, query refresh and clear historical/unverified status. Test against the local API and synthetic bytes, including mobile and revocation.
6. Prepare dedicated CNPG/OpenBao/workload-IAM/network/worker GitOps resources and recovery/runbooks, without changing live credentials or policies. Run all required checks and submit reviewable app/IaC PRs. Request only the new named production changes once ready.
7. After the evidence slice is released, build independent maker/checker dossiers bound to exact current profile, premise, licence and clean file generations; enforce contributor separation, expiry and country policy gates before any trading eligibility. This is not included in a clean-file verdict.
