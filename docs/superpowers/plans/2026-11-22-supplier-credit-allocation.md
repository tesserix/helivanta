# Supplier-credit allocation delivery

Spec: [allocation and reversal](../specs/2026-11-22-supplier-credit-allocation.md).

1. Capture hosted missing-allocation endpoint failure after normal owning setup.
2. Review all payable-balance/settlement consumers and source query plans.
3. Generate canonical allocation/reversal/choice contracts; red/green input checks.
4. Add concurrent source indexes separately from immutable journal DDL; extend
   credit proof, payable balances and the existing settlement guard under one lock.
5. Wire actor-bound writes/history/recovery/selectors, atomic audit/outbox and
   release/inventory classifications. Preserve strict existing payable payloads.
6. Prove hosted native concurrency, paid-source/future-bill, reversal after payment,
   isolation, stale state, idempotency, private disclosure and failure rollback.
7. Complete finance selectors/SDK/UI and actual report/export explanations; test
   the whole Global API/browser journey, including response loss and read refusal.
8. Pass local checks/builds and hosted gates, remove validation outputs, merge,
   request the exact named rollout/activation and perform live acceptance. Keep
   unfinished work open and do not substitute outbox delivery for financial effect.
