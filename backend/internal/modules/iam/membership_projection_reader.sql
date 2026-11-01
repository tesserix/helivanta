SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- Startup reconciliation reads only IAM continuity metadata through its system pool.
GRANT SELECT ON iam_subject_membership_epochs TO helivanta_system;
