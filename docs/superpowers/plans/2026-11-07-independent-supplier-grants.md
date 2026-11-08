# Independent supplier implementation plan

See the [scope, ownership and acceptance specification](../specs/2026-11-07-independent-supplier-grants.md)
and [delivery ledger](../../design/staff-and-rbac/independent-collaboration-delivery.md).

1. Reproduce canonical typed recipient, action ceiling, jurisdiction, validity,
   tenant isolation and exact-recovery failures before implementation.
2. Implement shared IAM grant preparation with canonical Go/Zod generation,
   forced-RLS migration, immutable proposal, current governance admission,
   audited reads and signed bounded pagination. No operating grant is emitted.
3. Add the Global API consumer, SDK, accessible owner UI and lost-response
   recovery acceptance; update architecture registries and generated contracts.
4. Implement the remaining relationship, disclosure, two-stock-owner fulfilment
   and financial stages in dependency order, keeping owner facts separate.
5. Run actual owner/HTTP/browser tests, race tests, lint, generators, typecheck,
   production build and full CI. Record limitations and unresolved prerequisites.
6. Merge exact verified revisions and use the existing Kargo production stage;
   capture exact freight, schema, image and healthy application evidence.

Production discovery on 7 November found the API at `62f3f7fd`, no configured
organization-identity environment group, and onboarding-service #12 still open.
No live mutation or trade approval is implied by this record.
