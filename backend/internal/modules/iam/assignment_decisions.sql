SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
ALTER TABLE iam_assignment_proposals DROP CONSTRAINT iam_assignment_proposals_status_check;
ALTER TABLE iam_assignment_proposals ADD CONSTRAINT iam_assignment_proposals_status_check CHECK(status IN('pending_scope','pending_review','scope_rejected','pending_decision_scope','approved_pending_activation','rejected','withdrawn'));
CREATE TABLE iam_assignment_decisions (
 tenant_id uuid NOT NULL, id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 proposal_id uuid NOT NULL, request_id uuid NOT NULL,
 expected_version bigint NOT NULL CHECK(expected_version>0), result_version bigint NOT NULL CHECK(result_version=expected_version+1),
 decision text NOT NULL CHECK(decision IN('approve','reject','withdraw')),
 actor_subject text NOT NULL CHECK(length(actor_subject) BETWEEN 1 AND 200),
 reason text NOT NULL CHECK(length(btrim(reason)) BETWEEN 1 AND 500),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,proposal_id,expected_version),UNIQUE(tenant_id,request_id),
 FOREIGN KEY(tenant_id,proposal_id) REFERENCES iam_assignment_proposals(tenant_id,id) ON DELETE RESTRICT
);
ALTER TABLE iam_assignment_decisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE iam_assignment_decisions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON iam_assignment_decisions USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.assignment_access',true)='authorized') WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.assignment_access',true)='authorized');
CREATE INDEX ON iam_assignment_decisions(tenant_id,proposal_id,created_at,id);
CREATE FUNCTION iam_assignment_decision_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE p iam_assignment_proposals%ROWTYPE;
BEGIN
 SELECT * INTO p FROM iam_assignment_proposals WHERE tenant_id=NEW.tenant_id AND id=NEW.proposal_id;
 IF NOT FOUND OR p.version<>NEW.expected_version OR NEW.actor_subject IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'') THEN
  RAISE EXCEPTION 'invalid proposal decision' USING ERRCODE='23514';
 END IF;
 IF NEW.decision='withdraw' THEN
  IF NEW.actor_subject<>p.proposed_by OR p.status NOT IN('pending_scope','pending_review','pending_decision_scope','approved_pending_activation') OR current_setting('app.assignment_command',true) IS DISTINCT FROM 'withdraw' THEN
   RAISE EXCEPTION 'invalid proposal withdrawal' USING ERRCODE='23514';
  END IF;
 ELSE
  IF NEW.actor_subject IN(p.proposed_by,p.subject) OR p.status<>'pending_review' OR current_setting('app.assignment_command',true) IS DISTINCT FROM 'review' THEN
   RAISE EXCEPTION 'independent review required' USING ERRCODE='23514';
  END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER iam_assignment_decision_guard BEFORE INSERT ON iam_assignment_decisions FOR EACH ROW EXECUTE FUNCTION iam_assignment_decision_guard();
CREATE TRIGGER iam_assignment_decision_immutable BEFORE UPDATE OR DELETE ON iam_assignment_decisions FOR EACH ROW EXECUTE FUNCTION iam_assignment_proposal_preserve();
CREATE TRIGGER iam_assignment_decision_no_truncate BEFORE TRUNCATE ON iam_assignment_decisions FOR EACH STATEMENT EXECUTE FUNCTION iam_assignment_proposal_preserve();
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
  IF NEW.request_id<>OLD.request_id THEN
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
 ELSIF NEW.request_id<>OLD.request_id THEN
  SELECT actor_subject INTO actor FROM iam_assignment_decisions WHERE tenant_id=NEW.tenant_id AND proposal_id=NEW.id AND result_version=NEW.version;
 ELSE actor:='system:workforce-scope-verifier'; END IF;
 INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id,changed_fields)
 VALUES(NEW.tenant_id,actor,'proposal_'||NEW.status,'iam_assignment_proposals',NEW.id,ARRAY['status','version']);
 RETURN NULL;
END $$;
