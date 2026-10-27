SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
ALTER TABLE iam_assignment_proposals DROP CONSTRAINT iam_assignment_proposals_status_check;
ALTER TABLE iam_assignment_proposals ADD CONSTRAINT iam_assignment_proposals_status_check CHECK(status IN('pending_scope','pending_review','scope_rejected','pending_decision_scope','approved_pending_activation','active','rejected','withdrawn'));
CREATE OR REPLACE FUNCTION iam_assignment_decision_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE p iam_assignment_proposals%ROWTYPE;
BEGIN
 SELECT * INTO p FROM iam_assignment_proposals WHERE tenant_id=NEW.tenant_id AND id=NEW.proposal_id;
 IF NOT FOUND OR p.version<>NEW.expected_version OR NEW.actor_subject IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'') THEN
  RAISE EXCEPTION 'invalid proposal decision' USING ERRCODE='23514';
 END IF;
 IF NEW.decision='withdraw' THEN
  IF NEW.actor_subject<>p.proposed_by OR p.status NOT IN('pending_scope','pending_review','pending_decision_scope','approved_pending_activation','active') OR current_setting('app.assignment_command',true) IS DISTINCT FROM 'withdraw' THEN
   RAISE EXCEPTION 'invalid proposal withdrawal' USING ERRCODE='23514';
  END IF;
 ELSE
  IF NEW.actor_subject IN(p.proposed_by,p.subject) OR p.status<>'pending_review' OR current_setting('app.assignment_command',true) IS DISTINCT FROM 'review' THEN
   RAISE EXCEPTION 'independent review required' USING ERRCODE='23514';
  END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE OR REPLACE FUNCTION iam_assignment_proposal_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE d iam_assignment_decisions%ROWTYPE;
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.status<>'pending_scope' OR NEW.version<>1 OR NEW.proposed_by IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'') THEN
   RAISE EXCEPTION 'invalid proposal origin' USING ERRCODE='23514';
  END IF;
 ELSE
  IF ROW(NEW.tenant_id,NEW.id,NEW.hospital_id,NEW.subject,NEW.template_key,NEW.reason,NEW.valid_from,NEW.expires_at,NEW.proposed_by,NEW.created_at) IS DISTINCT FROM ROW(OLD.tenant_id,OLD.id,OLD.hospital_id,OLD.subject,OLD.template_key,OLD.reason,OLD.valid_from,OLD.expires_at,OLD.proposed_by,OLD.created_at) OR NEW.version<>OLD.version+1 THEN
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
 ELSE actor:='system:workforce-scope-verifier'; END IF;
 INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id,changed_fields)
 VALUES(NEW.tenant_id,actor,'proposal_'||NEW.status,'iam_assignment_proposals',NEW.id,ARRAY['status','version']);
 RETURN NULL;
END $$;
CREATE OR REPLACE FUNCTION iam_assignment_snapshot_record() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.status='approved_pending_activation' THEN
  INSERT INTO iam_assignment_snapshots(tenant_id,id,version,hospital_id,subject,template_key,permissions,approved_by,valid_from,expires_at)
   SELECT NEW.tenant_id,NEW.id,NEW.version,NEW.hospital_id,NEW.subject,NEW.template_key,'["workforce.leave.approve"]'::jsonb,d.actor_subject,NEW.valid_from,NEW.expires_at
   FROM iam_assignment_decisions d WHERE d.tenant_id=NEW.tenant_id AND d.proposal_id=NEW.id AND d.request_id=NEW.request_id AND d.decision='approve';
 END IF;
 IF NEW.status='approved_pending_activation' OR (NEW.status='withdrawn' AND OLD.status IN('approved_pending_activation','active')) THEN
  PERFORM set_config('app.assignment_epoch_change','authorized',true);
  INSERT INTO iam_assignment_scope_epochs(tenant_id,hospital_id,subject,epoch) VALUES(NEW.tenant_id,NEW.hospital_id,NEW.subject,1)
   ON CONFLICT(tenant_id,hospital_id,subject) DO UPDATE SET epoch=iam_assignment_scope_epochs.epoch+1;
 END IF;
 RETURN NULL;
END $$;
