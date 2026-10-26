SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
LOCK TABLE iam_members IN SHARE ROW EXCLUSIVE MODE;
CREATE TABLE iam_subject_membership_epochs (
 tenant_id uuid NOT NULL, subject text NOT NULL,
 epoch bigint NOT NULL CHECK(epoch>0), member_count bigint NOT NULL CHECK(member_count>=0),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(), updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,subject)
);
-- The schema-owner pool has no BYPASSRLS; the transaction-held table lock keeps
-- this owner-only backfill atomic while application-role RLS remains enabled.
ALTER TABLE iam_members NO FORCE ROW LEVEL SECURITY;
INSERT INTO iam_subject_membership_epochs(tenant_id,subject,epoch,member_count)
 SELECT tenant_id,subject,1,count(*) FROM iam_members GROUP BY tenant_id,subject;
ALTER TABLE iam_members FORCE ROW LEVEL SECURITY;
ALTER TABLE iam_subject_membership_epochs ENABLE ROW LEVEL SECURITY;
ALTER TABLE iam_subject_membership_epochs FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON iam_subject_membership_epochs USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid) WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid);
CREATE INDEX ON iam_assignment_scope_epochs(tenant_id,subject,hospital_id);
ALTER TABLE iam_assignment_proposals ADD COLUMN membership_epoch bigint NOT NULL DEFAULT 0 CHECK(membership_epoch>=0);
ALTER TABLE iam_assignment_snapshots ADD COLUMN membership_epoch bigint NOT NULL DEFAULT 0 CHECK(membership_epoch>=0);
CREATE FUNCTION iam_membership_epoch_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF pg_trigger_depth()<2 THEN
  RAISE EXCEPTION 'membership epoch requires a membership transition' USING ERRCODE='23514';
 END IF;
 IF TG_OP='UPDATE' THEN
  IF ROW(NEW.tenant_id,NEW.subject,NEW.created_at) IS DISTINCT FROM ROW(OLD.tenant_id,OLD.subject,OLD.created_at)
   OR NOT((NEW.member_count=OLD.member_count+1 AND NEW.epoch=OLD.epoch)
   OR (NEW.member_count=OLD.member_count-1 AND NEW.epoch=OLD.epoch+CASE WHEN OLD.member_count=1 THEN 1 ELSE 0 END)) THEN
   RAISE EXCEPTION 'invalid membership epoch transition' USING ERRCODE='23514';
  END IF;
  NEW.updated_at:=GREATEST(clock_timestamp(),OLD.updated_at+interval '1 microsecond');
 ELSIF NEW.epoch<>1 OR NEW.member_count<>1 THEN
  RAISE EXCEPTION 'invalid initial membership epoch' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER iam_membership_epoch_guard BEFORE INSERT OR UPDATE ON iam_subject_membership_epochs FOR EACH ROW EXECUTE FUNCTION iam_membership_epoch_guard();
CREATE FUNCTION iam_membership_preserve() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'membership continuity evidence is immutable' USING ERRCODE='23514'; END $$;
CREATE TRIGGER iam_membership_epoch_no_delete BEFORE DELETE ON iam_subject_membership_epochs FOR EACH ROW EXECUTE FUNCTION iam_membership_preserve();
CREATE TRIGGER iam_membership_epoch_no_truncate BEFORE TRUNCATE ON iam_subject_membership_epochs FOR EACH STATEMENT EXECUTE FUNCTION iam_membership_preserve();
CREATE TRIGGER iam_members_no_truncate BEFORE TRUNCATE ON iam_members FOR EACH STATEMENT EXECUTE FUNCTION iam_membership_preserve();
CREATE TABLE iam_membership_revocations (
 tenant_id uuid NOT NULL, subject text NOT NULL, epoch bigint NOT NULL CHECK(epoch>0),
 removed_member_id uuid NOT NULL, role_key text NOT NULL, actor_subject text NOT NULL CHECK(actor_subject<>''),
 occurred_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,subject,epoch), UNIQUE(tenant_id,removed_member_id),
 FOREIGN KEY(tenant_id,subject) REFERENCES iam_subject_membership_epochs(tenant_id,subject) ON DELETE RESTRICT
);
ALTER TABLE iam_membership_revocations ENABLE ROW LEVEL SECURITY;
ALTER TABLE iam_membership_revocations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON iam_membership_revocations USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid) WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid);
CREATE FUNCTION iam_membership_revocation_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF pg_trigger_depth()<2 OR NOT EXISTS(SELECT 1 FROM iam_subject_membership_epochs e WHERE e.tenant_id=NEW.tenant_id AND e.subject=NEW.subject AND e.epoch=NEW.epoch+1 AND e.member_count=0)
  OR NEW.actor_subject IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'') THEN
  RAISE EXCEPTION 'membership removal evidence requires an attributed transition' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER iam_membership_revocation_guard BEFORE INSERT ON iam_membership_revocations FOR EACH ROW EXECUTE FUNCTION iam_membership_revocation_guard();
CREATE TRIGGER iam_membership_revocation_immutable BEFORE UPDATE OR DELETE ON iam_membership_revocations FOR EACH ROW EXECUTE FUNCTION iam_membership_preserve();
CREATE TRIGGER iam_membership_revocation_no_truncate BEFORE TRUNCATE ON iam_membership_revocations FOR EACH STATEMENT EXECUTE FUNCTION iam_membership_preserve();
CREATE FUNCTION iam_membership_record() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE remaining bigint; ended_epoch bigint; actor text; prior_access text; prior_change text;
BEGIN
 IF TG_OP='UPDATE' THEN
  IF NEW IS DISTINCT FROM OLD THEN
   RAISE EXCEPTION 'replace membership through grant and revoke' USING ERRCODE='23514';
  END IF;
 ELSIF TG_OP='INSERT' THEN
  INSERT INTO iam_subject_membership_epochs(tenant_id,subject,epoch,member_count) VALUES(NEW.tenant_id,NEW.subject,1,1)
   ON CONFLICT(tenant_id,subject) DO UPDATE SET member_count=iam_subject_membership_epochs.member_count+1;
 ELSE
  UPDATE iam_subject_membership_epochs SET member_count=member_count-1,epoch=epoch+CASE WHEN member_count=1 THEN 1 ELSE 0 END
   WHERE tenant_id=OLD.tenant_id AND subject=OLD.subject RETURNING member_count,epoch-1 INTO remaining,ended_epoch;
  IF NOT FOUND THEN RAISE EXCEPTION 'missing membership continuity evidence' USING ERRCODE='23514'; END IF;
  IF remaining=0 THEN
   actor:=NULLIF(current_setting('app.actor_subject',true),'');
   IF actor IS NULL THEN RAISE EXCEPTION 'membership removal actor required' USING ERRCODE='23514'; END IF;
   INSERT INTO iam_membership_revocations(tenant_id,subject,epoch,removed_member_id,role_key,actor_subject)
    VALUES(OLD.tenant_id,OLD.subject,ended_epoch,OLD.id,OLD.role_key,actor);
   INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id,changed_fields)
    VALUES(OLD.tenant_id,actor,'membership_ended','iam_members',OLD.id,ARRAY['role_key','membership_epoch']);
   prior_access:=current_setting('app.assignment_access',true);
   prior_change:=current_setting('app.assignment_epoch_change',true);
   PERFORM set_config('app.assignment_access','authorized',true),set_config('app.assignment_epoch_change','authorized',true);
   UPDATE iam_assignment_scope_epochs SET epoch=epoch+1 WHERE tenant_id=OLD.tenant_id AND subject=OLD.subject;
   PERFORM set_config('app.assignment_access',COALESCE(prior_access,''),true),set_config('app.assignment_epoch_change',COALESCE(prior_change,''),true);
  END IF;
 END IF;
 RETURN NULL;
END $$;
CREATE TRIGGER iam_membership_record AFTER INSERT OR UPDATE OR DELETE ON iam_members FOR EACH ROW EXECUTE FUNCTION iam_membership_record();
CREATE FUNCTION iam_proposal_membership_bind() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  SELECT epoch INTO NEW.membership_epoch FROM iam_subject_membership_epochs WHERE tenant_id=NEW.tenant_id AND subject=NEW.subject AND member_count>0 FOR SHARE;
  IF NOT FOUND THEN RAISE EXCEPTION 'current membership required' USING ERRCODE='23514'; END IF;
 ELSIF NEW.membership_epoch<>OLD.membership_epoch THEN
  RAISE EXCEPTION 'proposal membership binding is immutable' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER iam_proposal_membership_bind BEFORE INSERT OR UPDATE ON iam_assignment_proposals FOR EACH ROW EXECUTE FUNCTION iam_proposal_membership_bind();
CREATE FUNCTION iam_snapshot_membership_bind() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 SELECT p.membership_epoch INTO NEW.membership_epoch FROM iam_assignment_proposals p
  JOIN iam_subject_membership_epochs e ON e.tenant_id=p.tenant_id AND e.subject=p.subject AND e.epoch=p.membership_epoch AND e.member_count>0
  WHERE p.tenant_id=NEW.tenant_id AND p.id=NEW.id AND p.version=NEW.version FOR SHARE OF e;
 IF NOT FOUND THEN RAISE EXCEPTION 'reviewed membership continuity required' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER iam_snapshot_membership_bind BEFORE INSERT ON iam_assignment_snapshots FOR EACH ROW EXECUTE FUNCTION iam_snapshot_membership_bind();
ANALYZE iam_subject_membership_epochs;
ANALYZE iam_assignment_scope_epochs;
