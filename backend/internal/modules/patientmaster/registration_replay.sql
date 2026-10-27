SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- Registration replays are audited with the ids they announce (#1377).
ALTER TABLE patient_access_audit DROP CONSTRAINT patient_access_audit_action_check;
ALTER TABLE patient_access_audit ADD CONSTRAINT patient_access_audit_action_check
 CHECK (action IN ('read', 'list', 'match', 'register', 'grant', 'revoke', 'replay')) NOT VALID;
ALTER TABLE patient_access_audit VALIDATE CONSTRAINT patient_access_audit_action_check;
