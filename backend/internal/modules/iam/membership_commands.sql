SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE TABLE iam_membership_commands (
 tenant_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 operation text NOT NULL CHECK(operation IN('remove','recover')),
 basis_id uuid NOT NULL,
 subject text NOT NULL CHECK(length(subject) BETWEEN 1 AND 200),
 role_key text NOT NULL CHECK(length(role_key) BETWEEN 1 AND 100),
 expected_membership_epoch bigint NOT NULL CHECK(expected_membership_epoch>0),
 reason text NOT NULL CHECK(length(btrim(reason)) BETWEEN 1 AND 500),
 result_membership_id uuid NOT NULL,
 result_membership_epoch bigint NOT NULL CHECK(result_membership_epoch>0),
 actor_subject text NOT NULL CHECK(length(actor_subject) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,id),
 UNIQUE(tenant_id,operation,basis_id),
 FOREIGN KEY(tenant_id,subject) REFERENCES iam_subject_membership_epochs(tenant_id,subject) ON DELETE RESTRICT,
 CHECK((operation='remove' AND basis_id=result_membership_id AND result_membership_epoch IN(expected_membership_epoch,expected_membership_epoch+1))
    OR (operation='recover' AND role_key='staff_member' AND result_membership_epoch=expected_membership_epoch))
);
ALTER TABLE iam_membership_commands ENABLE ROW LEVEL SECURITY;
ALTER TABLE iam_membership_commands FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON iam_membership_commands
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid);
CREATE INDEX iam_membership_commands_history ON iam_membership_commands(tenant_id,created_at DESC,id DESC);
CREATE TRIGGER iam_membership_commands_immutable BEFORE UPDATE OR DELETE ON iam_membership_commands FOR EACH ROW EXECUTE FUNCTION iam_membership_preserve();
CREATE TRIGGER iam_membership_commands_no_truncate BEFORE TRUNCATE ON iam_membership_commands FOR EACH STATEMENT EXECUTE FUNCTION iam_membership_preserve();
CREATE FUNCTION iam_membership_command_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.actor_subject IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'')
  OR current_setting('app.membership_command',true) IS DISTINCT FROM NEW.id::text
  OR NOT EXISTS(SELECT 1 FROM iam_members WHERE tenant_id=NEW.tenant_id AND subject=NEW.actor_subject AND role_key='tenant_admin') THEN
  RAISE EXCEPTION 'current attributed membership command required' USING ERRCODE='23514';
 END IF;
 IF NEW.operation='remove' THEN
  IF pg_trigger_depth()<2 OR NEW.role_key='tenant_admin' OR EXISTS(SELECT 1 FROM iam_members WHERE tenant_id=NEW.tenant_id AND id=NEW.basis_id) THEN
   RAISE EXCEPTION 'exact non-governance membership removal required' USING ERRCODE='23514';
  END IF;
 ELSE
  IF pg_trigger_depth()<2 OR NOT EXISTS(SELECT 1 FROM iam_membership_commands WHERE tenant_id=NEW.tenant_id AND id=NEW.basis_id AND operation='remove' AND subject=NEW.subject)
   OR NOT EXISTS(SELECT 1 FROM iam_members WHERE tenant_id=NEW.tenant_id AND id=NEW.result_membership_id AND subject=NEW.subject AND role_key='staff_member') THEN
   RAISE EXCEPTION 'recorded removal and fresh ordinary membership required' USING ERRCODE='23514';
  END IF;
 END IF;
 IF NOT EXISTS(SELECT 1 FROM iam_subject_membership_epochs WHERE tenant_id=NEW.tenant_id AND subject=NEW.subject AND epoch=NEW.result_membership_epoch) THEN
  RAISE EXCEPTION 'current membership epoch required' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER iam_membership_command_guard BEFORE INSERT ON iam_membership_commands FOR EACH ROW EXECUTE FUNCTION iam_membership_command_guard();
CREATE FUNCTION iam_membership_command_record() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE command_id text; resulting_epoch bigint;
BEGIN
 command_id:=NULLIF(current_setting('app.membership_command',true),'');
 IF command_id IS NULL THEN RETURN NULL; END IF;
 IF TG_OP='INSERT' THEN
  SELECT epoch INTO STRICT resulting_epoch FROM iam_subject_membership_epochs WHERE tenant_id=NEW.tenant_id AND subject=NEW.subject;
  INSERT INTO iam_membership_commands(tenant_id,id,operation,basis_id,subject,role_key,expected_membership_epoch,reason,result_membership_id,result_membership_epoch,actor_subject)
  VALUES(NEW.tenant_id,command_id::uuid,'recover',current_setting('app.membership_recovery_basis')::uuid,NEW.subject,NEW.role_key,current_setting('app.membership_expected_epoch')::bigint,current_setting('app.membership_reason'),NEW.id,resulting_epoch,current_setting('app.actor_subject'));
  RETURN NULL;
 END IF;
 SELECT epoch INTO STRICT resulting_epoch FROM iam_subject_membership_epochs WHERE tenant_id=OLD.tenant_id AND subject=OLD.subject;
 INSERT INTO iam_membership_commands(tenant_id,id,operation,basis_id,subject,role_key,expected_membership_epoch,reason,result_membership_id,result_membership_epoch,actor_subject)
 VALUES(OLD.tenant_id,command_id::uuid,'remove',OLD.id,OLD.subject,OLD.role_key,current_setting('app.membership_expected_epoch')::bigint,current_setting('app.membership_reason'),OLD.id,resulting_epoch,current_setting('app.actor_subject'));
 RETURN NULL;
END $$;
-- Membership continuity is recorded by iam_membership_record first.
CREATE TRIGGER zz_iam_membership_command_record AFTER INSERT OR DELETE ON iam_members FOR EACH ROW EXECUTE FUNCTION iam_membership_command_record();
