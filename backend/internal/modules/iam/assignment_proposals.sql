SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE TABLE iam_assignment_proposals (
 tenant_id uuid NOT NULL, id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 hospital_id uuid NOT NULL CHECK(hospital_id<>'00000000-0000-0000-0000-000000000000'),
 subject text NOT NULL CHECK(length(subject) BETWEEN 1 AND 200 AND subject !~ '[[:space:]*#:]'),
 template_key text NOT NULL CHECK(template_key='workforce.leave_reviewer.v1'),
 reason text NOT NULL CHECK(length(btrim(reason)) BETWEEN 1 AND 500),
 valid_from timestamptz NOT NULL, expires_at timestamptz NOT NULL,
 proposed_by text NOT NULL CHECK(length(proposed_by) BETWEEN 1 AND 200),
 request_id uuid NOT NULL, status text NOT NULL CHECK(status IN('pending_scope','pending_review','scope_rejected')),
 version bigint NOT NULL CHECK(version>0),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(), updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,id), CHECK(expires_at>valid_from AND expires_at<=valid_from+interval '366 days')
);
CREATE TABLE iam_assignment_proposal_history (
 tenant_id uuid NOT NULL, id uuid NOT NULL, version bigint NOT NULL,
 status text NOT NULL, created_at timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,id,version), FOREIGN KEY(tenant_id,id) REFERENCES iam_assignment_proposals(tenant_id,id) ON DELETE RESTRICT
);
ALTER TABLE iam_assignment_proposals ENABLE ROW LEVEL SECURITY;
ALTER TABLE iam_assignment_proposals FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON iam_assignment_proposals USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.assignment_access',true)='authorized') WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.assignment_access',true)='authorized');
ALTER TABLE iam_assignment_proposal_history ENABLE ROW LEVEL SECURITY;
ALTER TABLE iam_assignment_proposal_history FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON iam_assignment_proposal_history USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.assignment_access',true)='authorized') WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.assignment_access',true)='authorized');
CREATE INDEX ON iam_assignment_proposals(tenant_id,created_at,id);
CREATE INDEX ON iam_assignment_proposal_history(tenant_id,id,created_at);
CREATE FUNCTION iam_assignment_proposal_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.status<>'pending_scope' OR NEW.version<>1 OR NEW.proposed_by IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'') THEN
   RAISE EXCEPTION 'invalid proposal origin' USING ERRCODE='23514';
  END IF;
 ELSE
  IF ROW(NEW.tenant_id,NEW.id,NEW.hospital_id,NEW.subject,NEW.template_key,NEW.reason,NEW.valid_from,NEW.expires_at,NEW.proposed_by,NEW.request_id,NEW.created_at) IS DISTINCT FROM ROW(OLD.tenant_id,OLD.id,OLD.hospital_id,OLD.subject,OLD.template_key,OLD.reason,OLD.valid_from,OLD.expires_at,OLD.proposed_by,OLD.request_id,OLD.created_at)
   OR OLD.status<>'pending_scope' OR NEW.status NOT IN('pending_review','scope_rejected') OR NEW.version<>OLD.version+1
   OR current_setting('app.assignment_scope_result',true) IS DISTINCT FROM NEW.request_id::text THEN
   RAISE EXCEPTION 'invalid proposal transition' USING ERRCODE='23514';
  END IF;
  NEW.updated_at:=GREATEST(clock_timestamp(),OLD.updated_at+interval '1 microsecond');
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER iam_assignment_proposal_guard BEFORE INSERT OR UPDATE ON iam_assignment_proposals FOR EACH ROW EXECUTE FUNCTION iam_assignment_proposal_guard();
CREATE FUNCTION iam_assignment_proposal_record() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO iam_assignment_proposal_history VALUES(NEW.tenant_id,NEW.id,NEW.version,NEW.status,NEW.updated_at);
 INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id,changed_fields)
 VALUES(NEW.tenant_id,CASE WHEN TG_OP='INSERT' THEN NEW.proposed_by ELSE 'system:workforce-scope-verifier' END,'proposal_'||NEW.status,'iam_assignment_proposals',NEW.id,ARRAY['status','version']);
 RETURN NULL;
END $$;
CREATE TRIGGER iam_assignment_proposal_record AFTER INSERT OR UPDATE ON iam_assignment_proposals FOR EACH ROW EXECUTE FUNCTION iam_assignment_proposal_record();
CREATE FUNCTION iam_assignment_proposal_preserve() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'proposal history is immutable' USING ERRCODE='23514'; END $$;
CREATE TRIGGER iam_assignment_proposal_no_delete BEFORE DELETE ON iam_assignment_proposals FOR EACH ROW EXECUTE FUNCTION iam_assignment_proposal_preserve();
CREATE TRIGGER iam_assignment_proposal_no_truncate BEFORE TRUNCATE ON iam_assignment_proposals FOR EACH STATEMENT EXECUTE FUNCTION iam_assignment_proposal_preserve();
CREATE TRIGGER iam_assignment_proposal_history_immutable BEFORE UPDATE OR DELETE ON iam_assignment_proposal_history FOR EACH ROW EXECUTE FUNCTION iam_assignment_proposal_preserve();
CREATE TRIGGER iam_assignment_proposal_history_no_truncate BEFORE TRUNCATE ON iam_assignment_proposal_history FOR EACH STATEMENT EXECUTE FUNCTION iam_assignment_proposal_preserve();

CREATE FUNCTION iam_assignment_proposal_history_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM iam_assignment_proposals p WHERE (p.tenant_id,p.id,p.version,p.status,p.updated_at)=(NEW.tenant_id,NEW.id,NEW.version,NEW.status,NEW.created_at)) THEN
  RAISE EXCEPTION 'history must match proposal' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER iam_assignment_proposal_history_guard BEFORE INSERT ON iam_assignment_proposal_history FOR EACH ROW EXECUTE FUNCTION iam_assignment_proposal_history_guard();
