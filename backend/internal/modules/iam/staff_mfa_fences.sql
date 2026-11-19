SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- Append-only: a committed reset must retain its original revocation cutoff.
-- Reversal after accepted native writes requires evidence, not table removal.
CREATE TABLE iam_staff_mfa_fences (
 tenant_id uuid NOT NULL,
 command_id uuid NOT NULL CHECK(command_id::text ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'),
 target_subject text NOT NULL CHECK(target_subject ~ '^[A-Za-z0-9_-]{1,200}$'),
 actor_subject text NOT NULL CHECK(actor_subject ~ '^[A-Za-z0-9_-]{1,200}$' AND actor_subject<>target_subject),
 fingerprint text NOT NULL CHECK(fingerprint ~ '^[0-9a-f]{64}$'),
 revoked_at timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,command_id)
);
ALTER TABLE iam_staff_mfa_fences ENABLE ROW LEVEL SECURITY;
ALTER TABLE iam_staff_mfa_fences FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON iam_staff_mfa_fences
 USING(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid);
CREATE FUNCTION iam_staff_mfa_fence_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.actor_subject IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'')
 OR NOT EXISTS(SELECT 1 FROM iam_members WHERE tenant_id=NEW.tenant_id AND subject=NEW.actor_subject AND role_key='tenant_admin')
 OR NOT EXISTS(SELECT 1 FROM iam_members WHERE tenant_id=NEW.tenant_id AND subject=NEW.target_subject)
 OR EXISTS(SELECT 1 FROM iam_members WHERE tenant_id=NEW.tenant_id AND subject=NEW.target_subject AND role_key='tenant_admin')
 OR NOT EXISTS(SELECT 1 FROM iam_credential_revocations WHERE subject=NEW.target_subject AND revoked_at>=NEW.revoked_at) THEN
  RAISE EXCEPTION 'attributed current administrator and committed ordinary-account revocation required' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER iam_staff_mfa_fence_guard BEFORE INSERT ON iam_staff_mfa_fences FOR EACH ROW EXECUTE FUNCTION iam_staff_mfa_fence_guard();
CREATE TRIGGER iam_staff_mfa_fence_no_change BEFORE UPDATE OR DELETE ON iam_staff_mfa_fences FOR EACH ROW EXECUTE FUNCTION iam_membership_preserve();
CREATE TRIGGER iam_staff_mfa_fence_no_truncate BEFORE TRUNCATE ON iam_staff_mfa_fences FOR EACH STATEMENT EXECUTE FUNCTION iam_membership_preserve();
