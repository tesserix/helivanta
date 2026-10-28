# Accessibility preference implementation

1. Add a public PATCH/GET round-trip test and observe unknown-field rejection.
2. Add migration 0003 and typed, optional nested settings to account responses and
   the existing guarded UPDATE; preserve omitted/null settings in SQL.
3. Verify legacy/reset/validation, migration upgrade, isolation, competing writes,
   audit rollback and real Global API forwarding.
4. Update API and delivery contracts, including the preceding deployed release;
   regenerate/check the contract inventory and run local checks plus full CI.
5. Commit/push, merge the checked revision (approved admin bypass if needed),
   publish and manually promote that exact freight, then verify migration,
   matching image digests, replica health and public boundary behavior.
