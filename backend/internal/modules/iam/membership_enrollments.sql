SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE TABLE iam_membership_enrollments (
 tenant_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 subject text NOT NULL CHECK(length(subject) BETWEEN 1 AND 200),
 reason text NOT NULL CHECK(length(btrim(reason)) BETWEEN 1 AND 500),
 membership_id uuid NOT NULL CHECK(substring(membership_id::text,15,1)='7'),
 role_key text NOT NULL CHECK(role_key='staff_member'),
 membership_epoch bigint NOT NULL CHECK(membership_epoch=1),
 actor_subject text NOT NULL CHECK(length(actor_subject) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,subject), UNIQUE(tenant_id,membership_id),
 FOREIGN KEY(tenant_id,subject) REFERENCES iam_subject_membership_epochs(tenant_id,subject) ON DELETE RESTRICT
);
ALTER TABLE iam_membership_enrollments ENABLE ROW LEVEL SECURITY;
ALTER TABLE iam_membership_enrollments FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON iam_membership_enrollments
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid);
CREATE INDEX iam_membership_enrollments_history ON iam_membership_enrollments(tenant_id,created_at DESC,id DESC);
CREATE TRIGGER iam_membership_enrollments_immutable BEFORE UPDATE OR DELETE ON iam_membership_enrollments FOR EACH ROW EXECUTE FUNCTION iam_membership_preserve();
CREATE TRIGGER iam_membership_enrollments_no_truncate BEFORE TRUNCATE ON iam_membership_enrollments FOR EACH STATEMENT EXECUTE FUNCTION iam_membership_preserve();
CREATE FUNCTION iam_membership_enrollment_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF pg_trigger_depth()<2
  OR NEW.actor_subject IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'')
  OR NEW.id::text IS DISTINCT FROM NULLIF(current_setting('app.membership_enrollment',true),'')
  OR NOT EXISTS(SELECT 1 FROM iam_members WHERE tenant_id=NEW.tenant_id AND subject=NEW.actor_subject AND role_key='tenant_admin')
  OR NOT EXISTS(SELECT 1 FROM iam_members WHERE tenant_id=NEW.tenant_id AND id=NEW.membership_id AND subject=NEW.subject AND role_key='staff_member') THEN
  RAISE EXCEPTION 'first enrollment requires an attributed membership transition' USING ERRCODE='23514';
 END IF;
 IF NOT EXISTS(SELECT 1 FROM iam_subject_membership_epochs WHERE tenant_id=NEW.tenant_id AND subject=NEW.subject AND epoch=1 AND member_count=1) THEN
  RAISE EXCEPTION 'first enrollment cannot replace membership continuity' USING ERRCODE='23505';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER iam_membership_enrollment_guard BEFORE INSERT ON iam_membership_enrollments FOR EACH ROW EXECUTE FUNCTION iam_membership_enrollment_guard();
CREATE FUNCTION iam_membership_enrollment_record() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE command_id text;
BEGIN
 command_id:=NULLIF(current_setting('app.membership_enrollment',true),'');
 IF command_id IS NULL THEN RETURN NULL; END IF;
 INSERT INTO iam_membership_enrollments(tenant_id,id,subject,reason,membership_id,role_key,membership_epoch,actor_subject)
 VALUES(NEW.tenant_id,command_id::uuid,NEW.subject,current_setting('app.membership_reason'),NEW.id,NEW.role_key,1,current_setting('app.actor_subject'));
 RETURN NULL;
END $$;
-- Run after continuity and any existing command receipt triggers.
CREATE TRIGGER zzz_iam_membership_enrollment_record AFTER INSERT ON iam_members FOR EACH ROW EXECUTE FUNCTION iam_membership_enrollment_record();
