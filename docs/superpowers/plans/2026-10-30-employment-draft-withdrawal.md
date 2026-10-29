# Employment draft withdrawal delivery

1. Reproduce missing terminal command against real PostgreSQL.
2. Add canonical command/status contracts and additive migration 0052 with
   trigger-enforced immutable terms, finality and history.
3. Implement current-authority idempotent withdrawal and opt-in lifecycle reads;
   preserve draft-only reads for existing clients.
4. Add the protected UI form, required reason, exact retry and terminal history.
5. Run owner/database/consumer/contract/web checks and normal local UI QA; update
   the Staff tracker, then commit/push/merge after CI. Review exact deployment and
   obtain its named production approval before rollout/migration.
