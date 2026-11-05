# Current chain review implementation plan

1. Reproduce missing protected current-chain view through real Global API test.
2. Add opt-in canonical request/response models; regenerate Go/Zod.
3. Reuse guarded approval-source traversal for a minimal audited read projection;
   verify scopes, stale/current source, rollback and no approval side effects.
4. Add strict SDK/hook and reject malformed/disconnected/cyclic/foreign chains.
5. Enable independent departmental review UI with explicit chain attestation;
   retain root behavior and exact uncertain-command recovery.
6. Run affected full suites, generation/inventory, build/lint/types and browser QA;
   update contracts/tracker, commit and push a bounded stacked draft PR.
