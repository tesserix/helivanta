-- Reporting-line leave routing (#1536): a versioned policy, a route pinned at filing, scoped reviewers limited to open steps.
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE workforce_leave_routing_revisions (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL,
 version integer NOT NULL CHECK (version > 0),
 mode text NOT NULL CHECK (mode IN ('hospital','reporting_line')),
 escalation_hours smallint CHECK (escalation_hours BETWEEN 1 AND 720),
 source_reference text NOT NULL CHECK (length(source_reference) BETWEEN 1 AND 200 AND source_reference = btrim(source_reference)),
 reviewed_by_subject text NOT NULL CHECK (length(reviewed_by_subject) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT now(),
 CHECK ((mode = 'reporting_line') = (escalation_hours IS NOT NULL)),
 PRIMARY KEY (tenant_id,hospital_id,version)
);

CREATE FUNCTION workforce_leave_routing_sequenced() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.version <> COALESCE((SELECT max(r.version) FROM workforce_leave_routing_revisions r WHERE r.tenant_id=NEW.tenant_id AND r.hospital_id=NEW.hospital_id),0) + 1 THEN
  RAISE EXCEPTION 'leave routing revisions advance one version at a time' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_leave_routing_sequenced BEFORE INSERT ON workforce_leave_routing_revisions FOR EACH ROW EXECUTE FUNCTION workforce_leave_routing_sequenced();

ALTER TABLE workforce_leave_requests
 ADD COLUMN routing_version integer,
 ADD COLUMN routed boolean NOT NULL DEFAULT false,
 ADD CONSTRAINT workforce_leave_request_routing CHECK (NOT routed OR routing_version IS NOT NULL),
 ADD CONSTRAINT workforce_leave_request_routing_revision FOREIGN KEY (tenant_id,hospital_id,routing_version)
  REFERENCES workforce_leave_routing_revisions(tenant_id,hospital_id,version) ON DELETE RESTRICT;

-- The policy in force at filing decides the route; callers cannot choose it.
CREATE FUNCTION workforce_leave_request_routing_pinned() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE current_version integer; current_mode text;
BEGIN
 SELECT r.version,r.mode INTO current_version,current_mode FROM workforce_leave_routing_revisions r
  WHERE r.tenant_id=NEW.tenant_id AND r.hospital_id=NEW.hospital_id ORDER BY r.version DESC LIMIT 1;
 NEW.routing_version := current_version;
 NEW.routed := COALESCE(current_mode = 'reporting_line', false);
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_leave_request_routing_pinned BEFORE INSERT ON workforce_leave_requests FOR EACH ROW EXECUTE FUNCTION workforce_leave_request_routing_pinned();

CREATE FUNCTION workforce_leave_request_routing_fixed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.routing_version,NEW.routed) IS DISTINCT FROM (OLD.routing_version,OLD.routed) THEN
  RAISE EXCEPTION 'leave request routing is pinned at filing' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_leave_request_routing_fixed BEFORE UPDATE ON workforce_leave_requests FOR EACH ROW EXECUTE FUNCTION workforce_leave_request_routing_fixed();

CREATE TABLE workforce_leave_route_steps (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, request_id uuid NOT NULL,
 level smallint NOT NULL CHECK (level BETWEEN 1 AND 8),
 staff_id uuid NOT NULL, opens_at timestamptz NOT NULL,
 PRIMARY KEY (tenant_id,hospital_id,request_id,level),
 UNIQUE (tenant_id,hospital_id,request_id,staff_id),
 FOREIGN KEY (tenant_id,hospital_id,request_id) REFERENCES workforce_leave_requests(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 FOREIGN KEY (tenant_id,hospital_id,staff_id) REFERENCES workforce_staff(tenant_id,hospital_id,id) ON DELETE RESTRICT
);
CREATE INDEX workforce_leave_route_steps_reviewer ON workforce_leave_route_steps(tenant_id,hospital_id,staff_id,request_id);

-- Steps exist only as the filing transaction's own pin, timed from the request and its policy.
CREATE FUNCTION workforce_leave_route_step_pinned() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF current_setting('app.workforce_leave_route_pin',true) IS DISTINCT FROM NEW.request_id::text OR NOT EXISTS(
   SELECT 1 FROM workforce_leave_requests r
   JOIN workforce_leave_routing_revisions v ON (v.tenant_id,v.hospital_id,v.version)=(r.tenant_id,r.hospital_id,r.routing_version)
   WHERE (r.tenant_id,r.hospital_id,r.id)=(NEW.tenant_id,NEW.hospital_id,NEW.request_id)
     AND r.routed AND r.created_tx=txid_current() AND r.staff_id<>NEW.staff_id
     AND NEW.opens_at = r.created_at + (NEW.level - 1) * make_interval(hours => v.escalation_hours::integer)) THEN
  RAISE EXCEPTION 'leave route steps are pinned when the request is filed' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_leave_route_step_pinned BEFORE INSERT ON workforce_leave_route_steps FOR EACH ROW EXECUTE FUNCTION workforce_leave_route_step_pinned();

-- Walks the supervision chain upward, skipping ancestors without an active, reviewed login. The chain is acyclic; depth is bounded anyway.
CREATE FUNCTION workforce_pin_leave_route() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
 enrollment text := current_setting('app.workforce_enrollment',true);
 links text := current_setting('app.workforce_login_link_access',true);
 pin text := current_setting('app.workforce_leave_route_pin',true);
 step_hours integer;
BEGIN
 SELECT v.escalation_hours INTO STRICT step_hours FROM workforce_leave_routing_revisions v
  WHERE (v.tenant_id,v.hospital_id,v.version)=(NEW.tenant_id,NEW.hospital_id,NEW.routing_version);
 PERFORM set_config('app.workforce_enrollment','authorized',true),set_config('app.workforce_login_link_access','authorized',true),set_config('app.workforce_leave_route_pin',NEW.id::text,true);
 INSERT INTO workforce_leave_route_steps(tenant_id,hospital_id,request_id,level,staff_id,opens_at)
 WITH RECURSIVE chain(depth,staff_id) AS (
  SELECT 1,parent.supervisor_id FROM (VALUES (NEW.staff_id)) seed(staff_id)
  JOIN LATERAL (SELECT p.supervisor_id FROM workforce_enrollment_profile p
   WHERE p.tenant_id=NEW.tenant_id AND p.hospital_id=NEW.hospital_id AND p.staff_id=seed.staff_id LIMIT 1) parent ON parent.supervisor_id IS NOT NULL
  UNION ALL
  SELECT c.depth+1,parent.supervisor_id FROM chain c
  JOIN LATERAL (SELECT p.supervisor_id FROM workforce_enrollment_profile p
   WHERE p.tenant_id=NEW.tenant_id AND p.hospital_id=NEW.hospital_id AND p.staff_id=c.staff_id LIMIT 1) parent ON parent.supervisor_id IS NOT NULL
  WHERE c.depth < 32
 ), eligible AS (
  SELECT c.staff_id,row_number() OVER (ORDER BY c.depth) AS level FROM chain c
  WHERE c.staff_id<>NEW.staff_id
   AND EXISTS(SELECT 1 FROM workforce_staff s WHERE (s.tenant_id,s.hospital_id,s.id)=(NEW.tenant_id,NEW.hospital_id,c.staff_id) AND s.employment_status='active')
   AND EXISTS(SELECT 1 FROM workforce_login_links l WHERE (l.tenant_id,l.hospital_id,l.staff_id)=(NEW.tenant_id,NEW.hospital_id,c.staff_id) AND l.status='active')
 )
 SELECT NEW.tenant_id,NEW.hospital_id,NEW.id,e.level,e.staff_id,NEW.created_at + (e.level - 1) * make_interval(hours => step_hours)
 FROM eligible e WHERE e.level <= 8;
 PERFORM set_config('app.workforce_enrollment',COALESCE(enrollment,''),true),set_config('app.workforce_login_link_access',COALESCE(links,''),true),set_config('app.workforce_leave_route_pin',COALESCE(pin,''),true);
 RETURN NULL;
END $$;
CREATE TRIGGER workforce_pin_leave_route AFTER INSERT ON workforce_leave_requests FOR EACH ROW WHEN (NEW.routed) EXECUTE FUNCTION workforce_pin_leave_route();

CREATE FUNCTION workforce_leave_review_session() RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(current_setting('app.workforce_leave_review',true)='authorized',false)
     OR COALESCE(current_setting('app.workforce_leave_review_read',true)='authorized',false)
$$;

CREATE FUNCTION workforce_leave_route_reviewer(row_tenant uuid,row_hospital uuid,row_staff uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT hms_tenant_visible(row_tenant)
  AND row_tenant=NULLIF(current_setting('app.tenant_id',true),'')::uuid
  AND row_hospital=NULLIF(current_setting('app.workforce_hospital',true),'')::uuid
  AND workforce_leave_review_session()
  AND row_staff=NULLIF(current_setting('app.workforce_leave_review_actor',true),'')::uuid
$$;

DO $$DECLARE tbl text;BEGIN
 FOREACH tbl IN ARRAY ARRAY['workforce_leave_routing_revisions','workforce_leave_route_steps'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',tbl);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',tbl);
  EXECUTE format('CREATE TRIGGER workforce_leave_routing_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION workforce_audit_immutable()',tbl);
 END LOOP;
END $$;
CREATE POLICY hospital_isolation ON workforce_leave_routing_revisions
 USING (hms_tenant_visible(tenant_id) AND workforce_staff_allowed(tenant_id,hospital_id,false))
 WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND workforce_staff_allowed(tenant_id,hospital_id,true)
  AND current_setting('app.workforce_leave_routing',true)='authorized');
CREATE POLICY hospital_isolation ON workforce_leave_route_steps
 USING ((hms_tenant_visible(tenant_id) AND workforce_staff_allowed(tenant_id,hospital_id,false)) OR workforce_leave_route_reviewer(tenant_id,hospital_id,staff_id))
 WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND workforce_staff_allowed(tenant_id,hospital_id,true)
  AND current_setting('app.workforce_leave_route_pin',true)=request_id::text);

-- A scoped review session sees a routed request only through an open step held by its own staff member.
CREATE POLICY leave_route_review_guard ON workforce_leave_requests AS RESTRICTIVE
 USING (NOT routed OR NOT workforce_leave_review_session() OR EXISTS(
  SELECT 1 FROM workforce_leave_route_steps s
  WHERE (s.tenant_id,s.hospital_id,s.request_id)=(workforce_leave_requests.tenant_id,workforce_leave_requests.hospital_id,workforce_leave_requests.id)
   AND s.staff_id=NULLIF(current_setting('app.workforce_leave_review_actor',true),'')::uuid
   AND s.opens_at<=now()));
