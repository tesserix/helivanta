SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
ALTER TABLE privacy_officers ADD COLUMN role text NOT NULL DEFAULT 'primary' CHECK(role IN ('primary','escalation'));
ALTER TABLE privacy_officers DROP CONSTRAINT privacy_officers_status_check;
ALTER TABLE privacy_officers ADD CONSTRAINT privacy_officers_status_check CHECK((role='primary' AND status IN ('nominated','active','revoked')) OR (role='escalation' AND status IN ('nominated','escalation_active','revoked')));
DROP INDEX privacy_current_officer;
CREATE UNIQUE INDEX privacy_current_officer ON privacy_officers(tenant_id,role) WHERE status IN ('active','escalation_active');
CREATE OR REPLACE FUNCTION privacy_officer_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'officer history is retained' USING ERRCODE='23514'; END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.status<>'nominated' OR NEW.version<>1 OR current_setting('app.privacy_manage',true) IS DISTINCT FROM 'true' THEN RAISE EXCEPTION 'invalid nomination' USING ERRCODE='23514'; END IF;
 ELSE
  IF (NEW.tenant_id,NEW.id,NEW.role,NEW.subject,NEW.display_name,NEW.contact_email,NEW.review_reference,NEW.valid_until,NEW.created_at) IS DISTINCT FROM (OLD.tenant_id,OLD.id,OLD.role,OLD.subject,OLD.display_name,OLD.contact_email,OLD.review_reference,OLD.valid_until,OLD.created_at) OR NEW.version<>OLD.version+1 OR OLD.status='revoked' OR NOT ((OLD.status='nominated' AND NEW.status IN ('active','escalation_active','revoked')) OR (OLD.status IN ('active','escalation_active') AND NEW.status='revoked')) THEN RAISE EXCEPTION 'invalid officer transition' USING ERRCODE='23514'; END IF;
  IF NEW.status='revoked' AND current_setting('app.privacy_manage',true) IS DISTINCT FROM 'true' THEN
   IF NEW.ended_reason<>'superseded' OR NEW.ended_reference::text IS DISTINCT FROM current_setting('app.privacy_accept',true) OR NOT EXISTS(SELECT 1 FROM privacy_officers WHERE tenant_id=NEW.tenant_id AND id=NEW.ended_reference AND role=NEW.role AND subject=current_setting('app.actor_subject',true) AND status='nominated' AND valid_until>clock_timestamp()) THEN RAISE EXCEPTION 'officer management authority required' USING ERRCODE='23514'; END IF;
  END IF;
  IF NEW.status IN ('active','escalation_active') AND (NEW.subject IS DISTINCT FROM current_setting('app.actor_subject',true) OR NEW.valid_until<=clock_timestamp()) THEN RAISE EXCEPTION 'invalid acceptance' USING ERRCODE='23514'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE OR REPLACE FUNCTION privacy_command_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE target_subject text; target_version bigint; target_status text;
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'case history is immutable' USING ERRCODE='23514'; END IF;
 SELECT subject,version,status INTO target_subject,target_version,target_status FROM privacy_requests WHERE tenant_id=NEW.tenant_id AND id=NEW.request_id FOR UPDATE;
 IF NOT FOUND OR target_version<>NEW.expected_version OR target_status IN ('responded','withdrawn') OR NEW.actor_subject IS DISTINCT FROM current_setting('app.actor_subject',true) THEN RAISE EXCEPTION 'invalid case command' USING ERRCODE='23514'; END IF;
 IF NEW.action IN ('withdraw','reply') THEN
  IF NEW.actor_subject<>target_subject THEN RAISE EXCEPTION 'principal authority required' USING ERRCODE='23514'; END IF;
 ELSE
  IF NEW.actor_subject=target_subject OR current_setting('app.privacy_officer',true) IS DISTINCT FROM 'true' OR NOT EXISTS(SELECT 1 FROM privacy_officers WHERE tenant_id=NEW.tenant_id AND subject=NEW.actor_subject AND status IN ('active','escalation_active') AND valid_until>clock_timestamp()) THEN RAISE EXCEPTION 'current officer authority required' USING ERRCODE='23514'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TABLE privacy_deadline_policies (
 tenant_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 version_label text NOT NULL CHECK(octet_length(version_label) BETWEEN 1 AND 64),
 acknowledgement_seconds integer NOT NULL CHECK(acknowledgement_seconds BETWEEN 60 AND 31536000),
 resolution_seconds integer NOT NULL CHECK(resolution_seconds BETWEEN acknowledgement_seconds AND 31536000),
 pause_while_waiting boolean NOT NULL,
 review_reference uuid NOT NULL CHECK(substring(review_reference::text,15,1)='7'),
 valid_until timestamptz NOT NULL,
 status text NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','active','retired')),
 version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 approved_by_subject text,
 approved_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,version_label),
 CHECK(valid_until>created_at),
 CHECK((status='draft' AND version=1 AND approved_at IS NULL AND approved_by_subject IS NULL) OR (status IN ('active','retired') AND approved_at IS NOT NULL AND approved_by_subject IS NOT NULL AND version>=2))
);
CREATE UNIQUE INDEX privacy_deadline_policy_current ON privacy_deadline_policies(tenant_id) WHERE status='active';
ALTER TABLE privacy_deadline_policies ENABLE ROW LEVEL SECURITY;
ALTER TABLE privacy_deadline_policies FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON privacy_deadline_policies USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid) WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.privacy_policy_write',true)='true');
CREATE FUNCTION privacy_deadline_policy_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'deadline policy history is retained' USING ERRCODE='23514'; END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.status<>'draft' OR NEW.version<>1 THEN RAISE EXCEPTION 'invalid policy proposal' USING ERRCODE='23514'; END IF;
 ELSE
  IF (NEW.tenant_id,NEW.id,NEW.version_label,NEW.acknowledgement_seconds,NEW.resolution_seconds,NEW.pause_while_waiting,NEW.review_reference,NEW.valid_until,NEW.created_at) IS DISTINCT FROM (OLD.tenant_id,OLD.id,OLD.version_label,OLD.acknowledgement_seconds,OLD.resolution_seconds,OLD.pause_while_waiting,OLD.review_reference,OLD.valid_until,OLD.created_at) OR NEW.version<>OLD.version+1 OR NOT ((OLD.status='draft' AND NEW.status='active') OR (OLD.status='active' AND NEW.status='retired')) THEN RAISE EXCEPTION 'invalid policy transition' USING ERRCODE='23514'; END IF;
  IF NEW.status='active' THEN
   IF NEW.valid_until<=clock_timestamp() OR NEW.approved_by_subject IS DISTINCT FROM current_setting('app.actor_subject',true) OR NOT EXISTS(SELECT 1 FROM privacy_officers p JOIN privacy_officers e ON e.tenant_id=p.tenant_id WHERE p.tenant_id=NEW.tenant_id AND p.role='primary' AND p.status='active' AND p.valid_until>clock_timestamp() AND e.role='escalation' AND e.status='escalation_active' AND e.valid_until>clock_timestamp() AND e.subject=NEW.approved_by_subject AND e.subject<>p.subject) THEN RAISE EXCEPTION 'independent accepted officer review required' USING ERRCODE='23514'; END IF;
  ELSIF (NEW.approved_by_subject,NEW.approved_at) IS DISTINCT FROM (OLD.approved_by_subject,OLD.approved_at) THEN RAISE EXCEPTION 'policy review is immutable' USING ERRCODE='23514'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER privacy_deadline_policy_guard BEFORE INSERT OR UPDATE OR DELETE ON privacy_deadline_policies FOR EACH ROW EXECUTE FUNCTION privacy_deadline_policy_guard();
CREATE TRIGGER privacy_deadline_policy_audit AFTER INSERT OR UPDATE ON privacy_deadline_policies FOR EACH ROW EXECUTE FUNCTION audit_record_change();

CREATE TABLE privacy_case_deadlines (
 tenant_id uuid NOT NULL,
 id uuid NOT NULL,
 policy_id uuid NOT NULL,
 version bigint NOT NULL CHECK(version>0),
 started_at timestamptz NOT NULL,
 acknowledge_by timestamptz NOT NULL,
 resolve_by timestamptz NOT NULL,
 acknowledged_at timestamptz,
 paused_at timestamptz,
 paused_microseconds bigint NOT NULL DEFAULT 0 CHECK(paused_microseconds>=0),
 terminal boolean NOT NULL DEFAULT false,
 worker_observed_at timestamptz,
 acknowledgement_escalated_at timestamptz,
 resolution_escalated_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,id),
 FOREIGN KEY(tenant_id,id) REFERENCES privacy_requests(tenant_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,policy_id) REFERENCES privacy_deadline_policies(tenant_id,id) ON DELETE RESTRICT,
 CHECK(acknowledge_by>=started_at AND resolve_by>=acknowledge_by),
 CHECK(NOT terminal OR paused_at IS NULL)
);
CREATE INDEX privacy_case_deadline_policy ON privacy_case_deadlines(tenant_id,policy_id);
ALTER TABLE privacy_case_deadlines ENABLE ROW LEVEL SECURITY;
ALTER TABLE privacy_case_deadlines FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON privacy_case_deadlines USING(hms_tenant_visible(tenant_id) AND EXISTS(SELECT 1 FROM privacy_requests r WHERE r.tenant_id=privacy_case_deadlines.tenant_id AND r.id=privacy_case_deadlines.id)) WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid);
CREATE TRIGGER privacy_case_deadline_audit AFTER INSERT OR UPDATE ON privacy_case_deadlines FOR EACH ROW EXECUTE FUNCTION audit_record_change();

CREATE FUNCTION privacy_deadline_clock() RETURNS timestamptz LANGUAGE sql VOLATILE AS $$ SELECT clock_timestamp() $$;

CREATE FUNCTION privacy_case_deadline_sync() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE policy privacy_deadline_policies%ROWTYPE; saved privacy_case_deadlines%ROWTYPE; at_time timestamptz:=privacy_deadline_clock(); event_id uuid:=gen_random_uuid(); pause_us bigint:=0;
BEGIN
 IF TG_OP='INSERT' THEN
  SELECT * INTO policy FROM privacy_deadline_policies WHERE tenant_id=NEW.tenant_id AND status='active' AND valid_until>at_time;
  IF NOT FOUND THEN RETURN NULL; END IF;
  INSERT INTO privacy_case_deadlines(tenant_id,id,policy_id,version,started_at,acknowledge_by,resolve_by,created_at,updated_at)
   VALUES(NEW.tenant_id,NEW.id,policy.id,NEW.version,at_time,at_time+make_interval(secs=>policy.acknowledgement_seconds),at_time+make_interval(secs=>policy.resolution_seconds),at_time,at_time);
 ELSE
  SELECT * INTO saved FROM privacy_case_deadlines WHERE tenant_id=NEW.tenant_id AND id=NEW.id FOR UPDATE;
  IF NOT FOUND THEN RETURN NULL; END IF;
  SELECT * INTO STRICT policy FROM privacy_deadline_policies WHERE tenant_id=NEW.tenant_id AND id=saved.policy_id;
  at_time:=greatest(at_time,saved.updated_at);
  IF OLD.status='received' AND NEW.status='under_review' THEN saved.acknowledged_at:=at_time; END IF;
  IF saved.paused_at IS NOT NULL AND NEW.status<>'awaiting_principal' THEN
   pause_us:=(extract(epoch FROM (at_time-saved.paused_at))*1000000)::bigint;
   saved.resolve_by:=saved.resolve_by+(pause_us * interval '1 microsecond');
   saved.paused_microseconds:=saved.paused_microseconds+pause_us;
   saved.paused_at:=NULL;
  END IF;
  IF policy.pause_while_waiting AND NEW.status='awaiting_principal' AND OLD.status<>'awaiting_principal' THEN saved.paused_at:=at_time; END IF;
  UPDATE privacy_case_deadlines SET version=NEW.version,acknowledged_at=saved.acknowledged_at,paused_at=saved.paused_at,resolve_by=saved.resolve_by,paused_microseconds=saved.paused_microseconds,terminal=NEW.status IN ('responded','withdrawn'),updated_at=at_time WHERE tenant_id=NEW.tenant_id AND id=NEW.id;
 END IF;
 INSERT INTO outbox_events(id,subject,tenant_id,payload)
  VALUES(event_id,'${PRIVACY_DEADLINE_CHANGED}',NEW.tenant_id,jsonb_build_object('event_id',event_id,'type','${PRIVACY_DEADLINE_CHANGED}','version',1,'tenant_id',NEW.tenant_id,'occurred_at',at_time,'data',jsonb_build_object('request_id',NEW.id,'version',NEW.version)));
 RETURN NULL;
END $$;
CREATE TRIGGER privacy_case_deadline_sync AFTER INSERT OR UPDATE ON privacy_requests FOR EACH ROW EXECUTE FUNCTION privacy_case_deadline_sync();

CREATE FUNCTION privacy_worker_allowed(t uuid,r uuid) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT current_user='helivanta_privacy_worker'
 AND t=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND r=NULLIF(current_setting('app.privacy_request',true),'')::uuid
 AND current_setting('app.actor_subject',true)='service:privacy-deadline-worker'
$$;
ALTER POLICY tenant_isolation ON privacy_case_deadlines USING(hms_tenant_visible(tenant_id) AND CASE WHEN current_user='helivanta_privacy_worker' THEN privacy_worker_allowed(tenant_id,id) ELSE EXISTS(SELECT 1 FROM privacy_requests r WHERE r.tenant_id=privacy_case_deadlines.tenant_id AND r.id=privacy_case_deadlines.id) END);
GRANT USAGE ON SCHEMA public TO helivanta_privacy_worker;
GRANT SELECT ON privacy_case_deadlines TO helivanta_privacy_worker;
GRANT SELECT(tenant_id,id) ON privacy_requests TO helivanta_privacy_worker;
GRANT UPDATE(worker_observed_at) ON privacy_case_deadlines TO helivanta_privacy_worker;
GRANT INSERT ON audit_events TO helivanta_privacy_worker;
CREATE POLICY privacy_worker_audit_guard ON audit_events AS RESTRICTIVE FOR INSERT TO helivanta_privacy_worker WITH CHECK(tenant_id IS NOT NULL AND privacy_worker_allowed(tenant_id,resource_id) AND actor_subject='service:privacy-deadline-worker' AND resource_type='privacy_case_deadlines' AND action='update');

CREATE TABLE privacy_deadline_escalations (
 tenant_id uuid NOT NULL,
 id uuid NOT NULL DEFAULT gen_random_uuid(),
 request_id uuid NOT NULL,
 kind text NOT NULL CHECK(kind IN ('acknowledgement','resolution')),
 occurred_at timestamptz NOT NULL,
 officer_id uuid,
 routing text NOT NULL CHECK(routing IN ('assigned','unconfigured')),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,id),
 UNIQUE(tenant_id,request_id,kind),
 FOREIGN KEY(tenant_id,request_id) REFERENCES privacy_case_deadlines(tenant_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,officer_id) REFERENCES privacy_officers(tenant_id,id) ON DELETE RESTRICT,
 CHECK((routing='assigned')=(officer_id IS NOT NULL))
);
CREATE INDEX privacy_deadline_escalation_officer ON privacy_deadline_escalations(tenant_id,officer_id);
ALTER TABLE privacy_deadline_escalations ENABLE ROW LEVEL SECURITY;
ALTER TABLE privacy_deadline_escalations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON privacy_deadline_escalations USING(hms_tenant_visible(tenant_id) AND CASE WHEN current_user='helivanta_privacy_worker' THEN privacy_worker_allowed(tenant_id,request_id) ELSE EXISTS(SELECT 1 FROM privacy_requests r WHERE r.tenant_id=privacy_deadline_escalations.tenant_id AND r.id=privacy_deadline_escalations.request_id) END) WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND privacy_worker_allowed(tenant_id,request_id));
GRANT SELECT ON privacy_deadline_escalations TO helivanta_privacy_worker;
GRANT INSERT(tenant_id,request_id,kind) ON privacy_deadline_escalations TO helivanta_privacy_worker;
GRANT SELECT(tenant_id,id,role,status,valid_until) ON privacy_officers TO helivanta_privacy_worker;
GRANT UPDATE(acknowledgement_escalated_at,resolution_escalated_at) ON privacy_case_deadlines TO helivanta_privacy_worker;

CREATE FUNCTION privacy_deadline_escalation_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE saved privacy_case_deadlines%ROWTYPE; at_time timestamptz:=privacy_deadline_clock();
BEGIN
 IF TG_OP<>'INSERT' OR NOT privacy_worker_allowed(NEW.tenant_id,NEW.request_id) THEN RAISE EXCEPTION 'deadline receipt requires restricted worker' USING ERRCODE='23514'; END IF;
 SELECT * INTO STRICT saved FROM privacy_case_deadlines WHERE tenant_id=NEW.tenant_id AND id=NEW.request_id FOR UPDATE;
 IF saved.terminal THEN RETURN NULL; END IF;
 IF NEW.kind='acknowledgement' THEN
  IF saved.acknowledged_at IS NOT NULL OR saved.acknowledgement_escalated_at IS NOT NULL OR at_time<saved.acknowledge_by THEN RETURN NULL; END IF;
 ELSIF NEW.kind='resolution' THEN
  IF saved.paused_at IS NOT NULL OR saved.resolution_escalated_at IS NOT NULL OR at_time<saved.resolve_by THEN RETURN NULL; END IF;
 ELSE RAISE EXCEPTION 'invalid deadline kind' USING ERRCODE='23514'; END IF;
 NEW.occurred_at:=at_time;
 SELECT id INTO NEW.officer_id FROM privacy_officers WHERE tenant_id=NEW.tenant_id AND role='escalation' AND status='escalation_active' AND valid_until>at_time;
 NEW.routing:=CASE WHEN NEW.officer_id IS NULL THEN 'unconfigured' ELSE 'assigned' END;
 RETURN NEW;
END $$;
CREATE TRIGGER privacy_deadline_escalation_guard BEFORE INSERT OR UPDATE OR DELETE ON privacy_deadline_escalations FOR EACH ROW EXECUTE FUNCTION privacy_deadline_escalation_guard();
CREATE FUNCTION privacy_deadline_escalation_apply() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.kind='acknowledgement' THEN
  UPDATE privacy_case_deadlines SET acknowledgement_escalated_at=NEW.occurred_at WHERE tenant_id=NEW.tenant_id AND id=NEW.request_id;
 ELSE
  UPDATE privacy_case_deadlines SET resolution_escalated_at=NEW.occurred_at WHERE tenant_id=NEW.tenant_id AND id=NEW.request_id;
 END IF;
 INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(NEW.tenant_id,current_setting('app.actor_subject',true),'deadline.'||NEW.kind,'privacy_deadline_escalations',NEW.request_id);
 RETURN NULL;
END $$;
CREATE TRIGGER privacy_deadline_escalation_apply AFTER INSERT ON privacy_deadline_escalations FOR EACH ROW EXECUTE FUNCTION privacy_deadline_escalation_apply();
ALTER POLICY privacy_worker_audit_guard ON audit_events WITH CHECK(tenant_id IS NOT NULL AND privacy_worker_allowed(tenant_id,resource_id) AND actor_subject='service:privacy-deadline-worker' AND ((resource_type='privacy_case_deadlines' AND action='update') OR (resource_type='privacy_deadline_escalations' AND action IN ('deadline.acknowledgement','deadline.resolution'))));
CREATE FUNCTION privacy_deadline_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'deadline history is retained' USING ERRCODE='23514'; END IF;
 IF current_user='helivanta_privacy_worker' THEN
  IF TG_OP<>'UPDATE' OR NOT privacy_worker_allowed(NEW.tenant_id,NEW.id)
   OR (to_jsonb(NEW)-ARRAY['worker_observed_at','acknowledgement_escalated_at','resolution_escalated_at']) IS DISTINCT FROM (to_jsonb(OLD)-ARRAY['worker_observed_at','acknowledgement_escalated_at','resolution_escalated_at']) THEN RAISE EXCEPTION 'invalid worker deadline mutation' USING ERRCODE='23514'; END IF;
  IF NEW.worker_observed_at IS DISTINCT FROM OLD.worker_observed_at AND (NEW.worker_observed_at IS NULL OR NEW.worker_observed_at>privacy_deadline_clock() OR NEW.worker_observed_at<OLD.worker_observed_at) THEN RAISE EXCEPTION 'invalid deadline observation' USING ERRCODE='23514'; END IF;
  IF NEW.acknowledgement_escalated_at IS DISTINCT FROM OLD.acknowledgement_escalated_at AND (OLD.acknowledgement_escalated_at IS NOT NULL OR NEW.acknowledgement_escalated_at IS NULL OR NOT EXISTS(SELECT 1 FROM privacy_deadline_escalations WHERE tenant_id=NEW.tenant_id AND request_id=NEW.id AND kind='acknowledgement' AND occurred_at=NEW.acknowledgement_escalated_at)) THEN RAISE EXCEPTION 'acknowledgement escalation receipt required' USING ERRCODE='23514'; END IF;
  IF NEW.resolution_escalated_at IS DISTINCT FROM OLD.resolution_escalated_at AND (OLD.resolution_escalated_at IS NOT NULL OR NEW.resolution_escalated_at IS NULL OR NOT EXISTS(SELECT 1 FROM privacy_deadline_escalations WHERE tenant_id=NEW.tenant_id AND request_id=NEW.id AND kind='resolution' AND occurred_at=NEW.resolution_escalated_at)) THEN RAISE EXCEPTION 'resolution escalation receipt required' USING ERRCODE='23514'; END IF;
 ELSIF pg_trigger_depth()<2 THEN RAISE EXCEPTION 'deadline changes require attributable case transition' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;

CREATE TRIGGER privacy_deadline_guard BEFORE INSERT OR UPDATE OR DELETE ON privacy_case_deadlines FOR EACH ROW EXECUTE FUNCTION privacy_deadline_guard();
