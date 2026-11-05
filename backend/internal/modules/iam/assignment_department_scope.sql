SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- ADR-0055: expand only; existing rows keep hospital scope.
ALTER TABLE iam_assignment_proposals ADD COLUMN scope_kind text NOT NULL DEFAULT 'hospital', ADD COLUMN department_id uuid;
ALTER TABLE iam_assignment_snapshots ADD COLUMN scope_kind text NOT NULL DEFAULT 'hospital', ADD COLUMN department_id uuid;
ALTER TABLE iam_assignment_proposals
 ADD CONSTRAINT iam_assignment_proposals_scope_check CHECK(scope_kind IN('hospital','department') AND (scope_kind='hospital')=(department_id IS NULL)
  AND (department_id IS NULL OR (substring(department_id::text,15,1)='7' AND substring(department_id::text,20,1) IN('8','9','a','b') AND department_id<>hospital_id))) NOT VALID,
 ADD CONSTRAINT iam_assignment_proposals_template_scope_check CHECK(iam_assignment_template_scope_kinds(template_key) @> ARRAY[scope_kind]) NOT VALID;
ALTER TABLE iam_assignment_proposals VALIDATE CONSTRAINT iam_assignment_proposals_scope_check;
ALTER TABLE iam_assignment_proposals VALIDATE CONSTRAINT iam_assignment_proposals_template_scope_check;
ALTER TABLE iam_assignment_snapshots
 ADD CONSTRAINT iam_assignment_snapshots_scope_check CHECK(scope_kind IN('hospital','department') AND (scope_kind='hospital')=(department_id IS NULL)) NOT VALID;
ALTER TABLE iam_assignment_snapshots VALIDATE CONSTRAINT iam_assignment_snapshots_scope_check;
-- Restrictive policies AND with tenant_isolation, so unaware or rolled-back code never sees department rows.
CREATE POLICY department_scope ON iam_assignment_proposals AS RESTRICTIVE
 USING(scope_kind='hospital' OR current_setting('app.assignment_department_scope',true)='enabled')
 WITH CHECK(scope_kind='hospital' OR current_setting('app.assignment_department_scope',true)='enabled');
CREATE POLICY department_scope ON iam_assignment_snapshots AS RESTRICTIVE
 USING(scope_kind='hospital' OR current_setting('app.assignment_department_scope',true)='enabled')
 WITH CHECK(scope_kind='hospital' OR current_setting('app.assignment_department_scope',true)='enabled');
CREATE TABLE iam_assignment_department_verifications (
 tenant_id uuid NOT NULL, proposal_id uuid NOT NULL, request_id uuid NOT NULL,
 owner text NOT NULL CHECK(owner IN('workforce','organization')), eligible boolean NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,proposal_id,request_id,owner),
 FOREIGN KEY(tenant_id,proposal_id) REFERENCES iam_assignment_proposals(tenant_id,id) ON DELETE RESTRICT
);
ALTER TABLE iam_assignment_department_verifications ENABLE ROW LEVEL SECURITY;
ALTER TABLE iam_assignment_department_verifications FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON iam_assignment_department_verifications USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.assignment_access',true)='authorized') WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.assignment_access',true)='authorized');
CREATE FUNCTION iam_assignment_department_verification_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF current_setting('app.assignment_scope_result',true) IS DISTINCT FROM NEW.request_id::text OR NOT EXISTS(SELECT 1 FROM iam_assignment_proposals p
  WHERE p.tenant_id=NEW.tenant_id AND p.id=NEW.proposal_id AND p.request_id=NEW.request_id AND p.scope_kind='department' AND p.status IN('pending_scope','pending_decision_scope')) THEN
  RAISE EXCEPTION 'current department proposal request required' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER iam_assignment_department_verification_guard BEFORE INSERT ON iam_assignment_department_verifications FOR EACH ROW EXECUTE FUNCTION iam_assignment_department_verification_guard();
CREATE TRIGGER iam_assignment_department_verification_immutable BEFORE UPDATE OR DELETE ON iam_assignment_department_verifications FOR EACH ROW EXECUTE FUNCTION iam_assignment_proposal_preserve();
CREATE TRIGGER iam_assignment_department_verification_no_truncate BEFORE TRUNCATE ON iam_assignment_department_verifications FOR EACH STATEMENT EXECUTE FUNCTION iam_assignment_proposal_preserve();
-- Extends the 0012 body: scope is immutable, and a department proposal advances only on both owners' eligible answers.
CREATE OR REPLACE FUNCTION iam_assignment_proposal_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE d iam_assignment_decisions%ROWTYPE;
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.status<>'pending_scope' OR NEW.version<>1 OR NEW.proposed_by IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'') THEN
   RAISE EXCEPTION 'invalid proposal origin' USING ERRCODE='23514';
  END IF;
 ELSE
  IF ROW(NEW.tenant_id,NEW.id,NEW.hospital_id,NEW.subject,NEW.template_key,NEW.reason,NEW.valid_from,NEW.expires_at,NEW.proposed_by,NEW.created_at,NEW.scope_kind,NEW.department_id) IS DISTINCT FROM ROW(OLD.tenant_id,OLD.id,OLD.hospital_id,OLD.subject,OLD.template_key,OLD.reason,OLD.valid_from,OLD.expires_at,OLD.proposed_by,OLD.created_at,OLD.scope_kind,OLD.department_id) OR NEW.version<>OLD.version+1 THEN
   RAISE EXCEPTION 'immutable proposal or stale version' USING ERRCODE='23514';
  END IF;
  IF OLD.status='approved_pending_activation' AND NEW.status='active' AND NEW.request_id=OLD.request_id THEN
   IF current_setting('app.assignment_projected_version',true) IS DISTINCT FROM OLD.version::text THEN
    RAISE EXCEPTION 'projected snapshot required' USING ERRCODE='23514';
   END IF;
   PERFORM 1 FROM iam_subject_membership_epochs e WHERE e.tenant_id=NEW.tenant_id AND e.subject=NEW.subject AND e.epoch=NEW.membership_epoch AND e.member_count>0 FOR SHARE;
   IF NOT FOUND THEN RAISE EXCEPTION 'current membership required for activation' USING ERRCODE='23514'; END IF;
   PERFORM 1 FROM iam_assignment_scope_epochs e WHERE e.tenant_id=NEW.tenant_id AND e.hospital_id=NEW.hospital_id AND e.subject=NEW.subject AND e.epoch::text=current_setting('app.assignment_activation_epoch',true) FOR SHARE;
   IF NOT FOUND OR NEW.expires_at<=clock_timestamp() OR NOT EXISTS(SELECT 1 FROM iam_assignment_snapshots s WHERE s.tenant_id=NEW.tenant_id AND s.id=NEW.id AND s.version=OLD.version AND s.membership_epoch=NEW.membership_epoch) THEN
    RAISE EXCEPTION 'current projection scope required for activation' USING ERRCODE='23514';
   END IF;
  ELSIF NEW.request_id<>OLD.request_id THEN
   SELECT * INTO d FROM iam_assignment_decisions WHERE tenant_id=NEW.tenant_id AND proposal_id=NEW.id AND expected_version=OLD.version AND request_id=NEW.request_id;
   IF NOT FOUND OR d.actor_subject IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'') OR
    (d.decision='withdraw' AND NEW.status<>'withdrawn') OR
    (d.decision IN('approve','reject') AND NEW.status<>'pending_decision_scope') THEN
    RAISE EXCEPTION 'matching decision required' USING ERRCODE='23514';
   END IF;
  ELSE
   IF current_setting('app.assignment_scope_result',true) IS DISTINCT FROM NEW.request_id::text OR NOT(
    (OLD.status='pending_scope' AND NEW.status IN('pending_review','scope_rejected')) OR
    (OLD.status='pending_decision_scope' AND NEW.status IN('pending_review','scope_rejected','approved_pending_activation','rejected'))) THEN
    RAISE EXCEPTION 'invalid owner response transition' USING ERRCODE='23514';
   END IF;
   IF NEW.scope_kind='department' AND ((OLD.status='pending_scope' AND NEW.status='pending_review') OR NEW.status='approved_pending_activation') AND
    (SELECT count(*) FROM iam_assignment_department_verifications v WHERE v.tenant_id=NEW.tenant_id AND v.proposal_id=NEW.id AND v.request_id=NEW.request_id AND v.eligible)<>2 THEN
    RAISE EXCEPTION 'both scope owners must confirm the department' USING ERRCODE='23514';
   END IF;
  END IF;
  NEW.updated_at:=GREATEST(clock_timestamp(),OLD.updated_at+interval '1 microsecond');
 END IF;
 RETURN NEW;
END $$;
CREATE OR REPLACE FUNCTION iam_assignment_proposal_record() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE actor text;
BEGIN
 INSERT INTO iam_assignment_proposal_history(tenant_id,id,version,status,created_at) VALUES(NEW.tenant_id,NEW.id,NEW.version,NEW.status,NEW.updated_at);
 IF TG_OP='INSERT' THEN actor:=NEW.proposed_by;
 ELSIF NEW.status='active' THEN actor:='system:iam-assignment-projector';
 ELSIF NEW.request_id<>OLD.request_id THEN
  SELECT actor_subject INTO actor FROM iam_assignment_decisions WHERE tenant_id=NEW.tenant_id AND proposal_id=NEW.id AND result_version=NEW.version;
 ELSIF current_setting('app.assignment_scope_verifier',true)='organization' THEN actor:='system:organization-department-verifier';
 ELSE actor:='system:workforce-scope-verifier'; END IF;
 INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id,changed_fields)
 VALUES(NEW.tenant_id,actor,'proposal_'||NEW.status,'iam_assignment_proposals',NEW.id,ARRAY['status','version']);
 RETURN NULL;
END $$;
CREATE OR REPLACE FUNCTION iam_assignment_snapshot_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM iam_assignment_proposals p JOIN iam_assignment_decisions d ON d.tenant_id=p.tenant_id AND d.proposal_id=p.id AND d.request_id=p.request_id
  WHERE p.tenant_id=NEW.tenant_id AND p.id=NEW.id AND p.version=NEW.version AND p.status='approved_pending_activation' AND d.decision='approve' AND d.actor_subject=NEW.approved_by
  AND ROW(p.hospital_id,p.subject,p.template_key,p.valid_from,p.expires_at,p.scope_kind,p.department_id) IS NOT DISTINCT FROM ROW(NEW.hospital_id,NEW.subject,NEW.template_key,NEW.valid_from,NEW.expires_at,NEW.scope_kind,NEW.department_id)) THEN
  RAISE EXCEPTION 'reviewed snapshot required' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE OR REPLACE FUNCTION iam_assignment_snapshot_record() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.status='approved_pending_activation' THEN
  INSERT INTO iam_assignment_snapshots(tenant_id,id,version,hospital_id,subject,template_key,permissions,approved_by,valid_from,expires_at,scope_kind,department_id)
   SELECT NEW.tenant_id,NEW.id,NEW.version,NEW.hospital_id,NEW.subject,NEW.template_key,iam_assignment_template_permissions(NEW.template_key),d.actor_subject,NEW.valid_from,NEW.expires_at,NEW.scope_kind,NEW.department_id
   FROM iam_assignment_decisions d WHERE d.tenant_id=NEW.tenant_id AND d.proposal_id=NEW.id AND d.request_id=NEW.request_id AND d.decision='approve';
 END IF;
 IF NEW.status='approved_pending_activation' OR (NEW.status='withdrawn' AND OLD.status IN('approved_pending_activation','active')) THEN
  PERFORM set_config('app.assignment_epoch_change','authorized',true);
  INSERT INTO iam_assignment_scope_epochs(tenant_id,hospital_id,subject,epoch) VALUES(NEW.tenant_id,NEW.hospital_id,NEW.subject,1)
   ON CONFLICT(tenant_id,hospital_id,subject) DO UPDATE SET epoch=iam_assignment_scope_epochs.epoch+1;
 END IF;
 RETURN NULL;
END $$;
