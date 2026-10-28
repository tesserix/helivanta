# Registrar review implementation

1. Write a real multi-module submission/worklist test and observe missing routes.
2. Add submission storage and typed requested/withdrawn contracts; implement own
   submit/read and atomic cancellation on the existing withdrawal command.
3. Add hospital-scoped review worklist/decision storage, consumer handlers and
   registrar endpoints using the current hospital grant/registration boundary.
4. Apply verified decision projections with immutable identity checks and local
   withdrawal precedence. Test real asynchronous round trips and hostile cases.
5. Update canonical contracts, classifications, registry reference and delivery
   records; run package/integration/architecture/race/build/lint and full CI.
6. Rehearse migration and record rollback limits. Merge/publish/promote the exact
   reviewed source through Kargo; verify health, isolation and route integration.
