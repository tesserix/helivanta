SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE TABLE iam_assignment_scope_epochs (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, subject text NOT NULL,
 epoch bigint NOT NULL CHECK(epoch>0), acknowledged_epoch bigint NOT NULL DEFAULT 0 CHECK(acknowledged_epoch>=0 AND acknowledged_epoch<=epoch),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(), updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,hospital_id,subject)
);
CREATE TABLE iam_assignment_snapshots (
 tenant_id uuid NOT NULL, id uuid NOT NULL, version bigint NOT NULL CHECK(version>0),
 hospital_id uuid NOT NULL, subject text NOT NULL, template_key text NOT NULL CHECK(template_key='workforce.leave_reviewer.v1'),
 permissions jsonb NOT NULL CHECK(permissions='["workforce.leave.approve"]'::jsonb),
 approved_by text NOT NULL, valid_from timestamptz NOT NULL, expires_at timestamptz NOT NULL CHECK(expires_at>valid_from),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,id),
 FOREIGN KEY(tenant_id,id,version) REFERENCES iam_assignment_proposal_history(tenant_id,id,version) ON DELETE RESTRICT
);
ALTER TABLE iam_assignment_scope_epochs ENABLE ROW LEVEL SECURITY;
ALTER TABLE iam_assignment_scope_epochs FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON iam_assignment_scope_epochs USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.assignment_access',true)='authorized') WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.assignment_access',true)='authorized');
ALTER TABLE iam_assignment_snapshots ENABLE ROW LEVEL SECURITY;
ALTER TABLE iam_assignment_snapshots FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON iam_assignment_snapshots USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.assignment_access',true)='authorized') WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.assignment_access',true)='authorized');
CREATE INDEX ON iam_assignment_snapshots(tenant_id,hospital_id,subject,id);
CREATE FUNCTION iam_assignment_snapshot_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM iam_assignment_proposals p JOIN iam_assignment_decisions d ON d.tenant_id=p.tenant_id AND d.proposal_id=p.id AND d.request_id=p.request_id
  WHERE p.tenant_id=NEW.tenant_id AND p.id=NEW.id AND p.version=NEW.version AND p.status='approved_pending_activation' AND d.decision='approve' AND d.actor_subject=NEW.approved_by
  AND ROW(p.hospital_id,p.subject,p.template_key,p.valid_from,p.expires_at)=ROW(NEW.hospital_id,NEW.subject,NEW.template_key,NEW.valid_from,NEW.expires_at)) THEN
  RAISE EXCEPTION 'reviewed snapshot required' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER iam_assignment_snapshot_guard BEFORE INSERT ON iam_assignment_snapshots FOR EACH ROW EXECUTE FUNCTION iam_assignment_snapshot_guard();
CREATE TRIGGER iam_assignment_snapshot_immutable BEFORE UPDATE OR DELETE ON iam_assignment_snapshots FOR EACH ROW EXECUTE FUNCTION iam_assignment_proposal_preserve();
CREATE TRIGGER iam_assignment_snapshot_no_truncate BEFORE TRUNCATE ON iam_assignment_snapshots FOR EACH STATEMENT EXECUTE FUNCTION iam_assignment_proposal_preserve();
CREATE FUNCTION iam_assignment_epoch_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.epoch<>1 OR NEW.acknowledged_epoch<>0 OR current_setting('app.assignment_epoch_change',true) IS DISTINCT FROM 'authorized' THEN
   RAISE EXCEPTION 'invalid initial scope epoch' USING ERRCODE='23514';
  END IF;
 ELSE
  IF ROW(NEW.tenant_id,NEW.hospital_id,NEW.subject,NEW.created_at) IS DISTINCT FROM ROW(OLD.tenant_id,OLD.hospital_id,OLD.subject,OLD.created_at) OR NOT(
   (NEW.epoch=OLD.epoch+1 AND NEW.acknowledged_epoch=OLD.acknowledged_epoch AND current_setting('app.assignment_epoch_change',true) IS NOT DISTINCT FROM 'authorized') OR
   (NEW.epoch=OLD.epoch AND NEW.acknowledged_epoch=NEW.epoch AND current_setting('app.assignment_projection_ack',true) IS NOT DISTINCT FROM 'authorized')) THEN
   RAISE EXCEPTION 'invalid scope epoch transition' USING ERRCODE='23514';
  END IF;
  NEW.updated_at:=GREATEST(clock_timestamp(),OLD.updated_at+interval '1 microsecond');
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER iam_assignment_epoch_guard BEFORE INSERT OR UPDATE ON iam_assignment_scope_epochs FOR EACH ROW EXECUTE FUNCTION iam_assignment_epoch_guard();
CREATE TRIGGER iam_assignment_epoch_no_delete BEFORE DELETE ON iam_assignment_scope_epochs FOR EACH ROW EXECUTE FUNCTION iam_assignment_proposal_preserve();
CREATE TRIGGER iam_assignment_epoch_no_truncate BEFORE TRUNCATE ON iam_assignment_scope_epochs FOR EACH STATEMENT EXECUTE FUNCTION iam_assignment_proposal_preserve();
CREATE FUNCTION iam_assignment_snapshot_record() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.status='approved_pending_activation' THEN
  INSERT INTO iam_assignment_snapshots(tenant_id,id,version,hospital_id,subject,template_key,permissions,approved_by,valid_from,expires_at)
   SELECT NEW.tenant_id,NEW.id,NEW.version,NEW.hospital_id,NEW.subject,NEW.template_key,'["workforce.leave.approve"]'::jsonb,d.actor_subject,NEW.valid_from,NEW.expires_at
   FROM iam_assignment_decisions d WHERE d.tenant_id=NEW.tenant_id AND d.proposal_id=NEW.id AND d.request_id=NEW.request_id AND d.decision='approve';
 END IF;
 IF NEW.status='approved_pending_activation' OR OLD.status='approved_pending_activation' THEN
  PERFORM set_config('app.assignment_epoch_change','authorized',true);
  INSERT INTO iam_assignment_scope_epochs(tenant_id,hospital_id,subject,epoch) VALUES(NEW.tenant_id,NEW.hospital_id,NEW.subject,1)
   ON CONFLICT(tenant_id,hospital_id,subject) DO UPDATE SET epoch=iam_assignment_scope_epochs.epoch+1;
 END IF;
 RETURN NULL;
END $$;
-- Runs after the proposal history trigger, so the snapshot references retained evidence.
CREATE TRIGGER iam_assignment_z_snapshot_record AFTER UPDATE ON iam_assignment_proposals FOR EACH ROW EXECUTE FUNCTION iam_assignment_snapshot_record();
