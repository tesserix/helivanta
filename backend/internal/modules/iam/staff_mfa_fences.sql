SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- Retain only the canonical reset intent; never native factor or session secrets.
CREATE TABLE iam_staff_mfa_commands (
 tenant_id uuid NOT NULL,
 command_id uuid NOT NULL CHECK(command_id::text ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'),
 actor_subject text NOT NULL CHECK(actor_subject ~ '^[A-Za-z0-9_-]{1,200}$'),
 employer_kind text NOT NULL CHECK(employer_kind IN('hospital','pharmacy','supplier')),
 employer_id uuid NOT NULL CHECK(employer_id<>'00000000-0000-0000-0000-000000000000'),
 staff_id uuid NOT NULL CHECK(staff_id<>'00000000-0000-0000-0000-000000000000'),
 fingerprint text NOT NULL,
 request jsonb CHECK(request IS NULL OR octet_length(request::text)<=8192),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,command_id),
 CHECK((request IS NULL AND fingerprint='') OR (request IS NOT NULL AND fingerprint ~ '^[0-9a-f]{64}$' AND jsonb_typeof(request)='object')),
 CHECK(request IS NULL OR COALESCE(
 request->>'actorSubject'=actor_subject AND
 request->'target'->>'tenantId'=tenant_id::text AND
 request->'target'->>'employerKind'=employer_kind AND
 request->'target'->>'employerId'=employer_id::text AND
 request->'target'->>'staffId'=staff_id::text AND
 request->'intent'->>'commandId'=command_id::text,false))
);
CREATE TABLE iam_staff_mfa_cancellations (
 tenant_id uuid NOT NULL,
 command_id uuid NOT NULL,
 actor_subject text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,command_id),
 FOREIGN KEY(tenant_id,command_id) REFERENCES iam_staff_mfa_commands(tenant_id,command_id)
);
ALTER TABLE iam_staff_mfa_commands ENABLE ROW LEVEL SECURITY;
ALTER TABLE iam_staff_mfa_commands FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON iam_staff_mfa_commands
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid);
ALTER TABLE iam_staff_mfa_cancellations ENABLE ROW LEVEL SECURITY;
ALTER TABLE iam_staff_mfa_cancellations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON iam_staff_mfa_cancellations
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid);
CREATE FUNCTION iam_staff_mfa_intent_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM pg_advisory_xact_lock(hashtextextended('iam.staff-mfa-fence:'||NEW.tenant_id::text||':'||NEW.command_id::text,0));
 IF NEW.actor_subject IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'')
 OR NOT EXISTS(SELECT 1 FROM iam_members WHERE tenant_id=NEW.tenant_id AND subject=NEW.actor_subject AND role_key='tenant_admin') THEN
  RAISE EXCEPTION 'attributed current administrator required' USING ERRCODE='23514';
 END IF;
 IF TG_TABLE_NAME='iam_staff_mfa_cancellations' AND NOT EXISTS(
  SELECT 1 FROM iam_staff_mfa_commands WHERE tenant_id=NEW.tenant_id AND command_id=NEW.command_id AND actor_subject=NEW.actor_subject
 ) THEN RAISE EXCEPTION 'original initiator required' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER iam_staff_mfa_intent_guard BEFORE INSERT ON iam_staff_mfa_commands FOR EACH ROW EXECUTE FUNCTION iam_staff_mfa_intent_guard();
CREATE TRIGGER iam_staff_mfa_cancellation_guard BEFORE INSERT ON iam_staff_mfa_cancellations FOR EACH ROW EXECUTE FUNCTION iam_staff_mfa_intent_guard();
CREATE FUNCTION iam_staff_mfa_tombstone_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.request IS NULL AND NOT EXISTS(
  SELECT 1 FROM iam_staff_mfa_cancellations WHERE tenant_id=NEW.tenant_id AND command_id=NEW.command_id AND actor_subject=NEW.actor_subject
 ) THEN RAISE EXCEPTION 'an empty intent must be cancelled atomically' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
CREATE CONSTRAINT TRIGGER iam_staff_mfa_tombstone_guard AFTER INSERT ON iam_staff_mfa_commands DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION iam_staff_mfa_tombstone_guard();
CREATE TRIGGER iam_staff_mfa_intent_no_change BEFORE UPDATE OR DELETE ON iam_staff_mfa_commands FOR EACH ROW EXECUTE FUNCTION iam_membership_preserve();
CREATE TRIGGER iam_staff_mfa_intent_no_truncate BEFORE TRUNCATE ON iam_staff_mfa_commands FOR EACH STATEMENT EXECUTE FUNCTION iam_membership_preserve();
CREATE TRIGGER iam_staff_mfa_cancellation_no_change BEFORE UPDATE OR DELETE ON iam_staff_mfa_cancellations FOR EACH ROW EXECUTE FUNCTION iam_membership_preserve();
CREATE TRIGGER iam_staff_mfa_cancellation_no_truncate BEFORE TRUNCATE ON iam_staff_mfa_cancellations FOR EACH STATEMENT EXECUTE FUNCTION iam_membership_preserve();

-- Append-only: a committed reset must retain its original revocation cutoff.
-- Reversal after accepted native writes requires evidence, not table removal.
CREATE TABLE iam_staff_mfa_fences (
 tenant_id uuid NOT NULL,
 command_id uuid NOT NULL CHECK(command_id::text ~ '^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$'),
 target_subject text NOT NULL CHECK(target_subject ~ '^[A-Za-z0-9_-]{1,200}$'),
 actor_subject text NOT NULL CHECK(actor_subject ~ '^[A-Za-z0-9_-]{1,200}$' AND actor_subject<>target_subject),
 fingerprint text NOT NULL CHECK(fingerprint ~ '^[0-9a-f]{64}$'),
 revoked_at timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,command_id),
 FOREIGN KEY(tenant_id,command_id) REFERENCES iam_staff_mfa_commands(tenant_id,command_id)
);
ALTER TABLE iam_staff_mfa_fences ENABLE ROW LEVEL SECURITY;
ALTER TABLE iam_staff_mfa_fences FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON iam_staff_mfa_fences
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid);
CREATE FUNCTION iam_staff_mfa_fence_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM pg_advisory_xact_lock(hashtextextended('iam.staff-mfa-fence:'||NEW.tenant_id::text||':'||NEW.command_id::text,0));
 IF NOT EXISTS(SELECT 1 FROM iam_staff_mfa_commands WHERE tenant_id=NEW.tenant_id AND command_id=NEW.command_id AND actor_subject=NEW.actor_subject AND fingerprint=NEW.fingerprint AND request IS NOT NULL AND request->'target'->>'subject'=NEW.target_subject)
 OR EXISTS(SELECT 1 FROM iam_staff_mfa_cancellations WHERE tenant_id=NEW.tenant_id AND command_id=NEW.command_id) THEN
 RAISE EXCEPTION 'an uncancelled original intent is required' USING ERRCODE='23514';
 END IF;
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
