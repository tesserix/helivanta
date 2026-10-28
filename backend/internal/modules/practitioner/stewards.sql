SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE FUNCTION practitioner_registry_now() RETURNS timestamptz LANGUAGE sql VOLATILE AS $$ SELECT clock_timestamp() $$;
CREATE TABLE practitioner_steward_appointments (
 tenant_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7' AND substring(id::text,20,1) IN ('8','9','a','b')),
 subject text NOT NULL CHECK(octet_length(subject) BETWEEN 1 AND 512),
 nominated_by text NOT NULL CHECK(octet_length(nominated_by) BETWEEN 1 AND 512 AND nominated_by<>subject),
 review_reference uuid NOT NULL CHECK(substring(review_reference::text,15,1)='7' AND substring(review_reference::text,20,1) IN ('8','9','a','b')),
 valid_until timestamptz NOT NULL,
 status text NOT NULL DEFAULT 'nominated' CHECK(status IN ('nominated','active','withdrawn','revoked','expired')),
 version bigint NOT NULL DEFAULT 1 CHECK(version BETWEEN 1 AND 3),
 accepted_at timestamptz,
 ended_at timestamptz,
 ended_by text,
 end_reference uuid CHECK(end_reference IS NULL OR (substring(end_reference::text,15,1)='7' AND substring(end_reference::text,20,1) IN ('8','9','a','b'))),
 created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,id),
 CHECK(valid_until>created_at AND valid_until<='9999-12-31T23:59:59.999999Z'),
 CHECK((status='nominated' AND version=1 AND accepted_at IS NULL AND ended_at IS NULL AND ended_by IS NULL AND end_reference IS NULL)
 OR (status='active' AND version=2 AND accepted_at IS NOT NULL AND ended_at IS NULL AND ended_by IS NULL AND end_reference IS NULL)
 OR (status IN ('withdrawn','revoked') AND version IN (2,3) AND ((version=2 AND accepted_at IS NULL) OR (version=3 AND accepted_at IS NOT NULL)) AND ended_at IS NOT NULL AND ended_by IS NOT NULL AND end_reference IS NOT NULL)
 OR (status='expired' AND version=3 AND accepted_at IS NOT NULL AND ended_at IS NOT NULL AND ended_by IS NOT NULL AND end_reference IS NULL))
);
CREATE UNIQUE INDEX practitioner_steward_one_active ON practitioner_steward_appointments(tenant_id,subject) WHERE status='active';
CREATE INDEX practitioner_steward_page ON practitioner_steward_appointments(tenant_id,created_at DESC,id DESC);
CREATE INDEX practitioner_steward_own_page ON practitioner_steward_appointments(tenant_id,subject,created_at DESC,id DESC);
ALTER TABLE practitioner_steward_appointments ENABLE ROW LEVEL SECURITY;
ALTER TABLE practitioner_steward_appointments FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON practitioner_steward_appointments
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND (current_setting('app.practitioner_steward_manage',true)='true' OR subject=NULLIF(current_setting('app.actor_subject',true),'')))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND (current_setting('app.practitioner_steward_manage',true)='true' OR subject=NULLIF(current_setting('app.actor_subject',true),'')));
CREATE TABLE practitioner_steward_events (
 tenant_id uuid NOT NULL,
 appointment_id uuid NOT NULL,
 id uuid NOT NULL DEFAULT gen_random_uuid(),
 version bigint NOT NULL,
 status text NOT NULL,
 actor_subject text NOT NULL,
 review_reference uuid NOT NULL,
 created_at timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,id),
 UNIQUE(tenant_id,appointment_id,version),
 FOREIGN KEY(tenant_id,appointment_id) REFERENCES practitioner_steward_appointments(tenant_id,id)
);
CREATE INDEX practitioner_steward_history_page ON practitioner_steward_events(tenant_id,appointment_id,created_at DESC,id DESC);
ALTER TABLE practitioner_steward_events ENABLE ROW LEVEL SECURITY;
ALTER TABLE practitioner_steward_events FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON practitioner_steward_events
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND EXISTS(SELECT 1 FROM practitioner_steward_appointments a WHERE a.tenant_id=practitioner_steward_events.tenant_id AND a.id=practitioner_steward_events.appointment_id))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND EXISTS(SELECT 1 FROM practitioner_steward_appointments a WHERE a.tenant_id=practitioner_steward_events.tenant_id AND a.id=practitioner_steward_events.appointment_id));
CREATE FUNCTION practitioner_steward_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE actor text:=NULLIF(current_setting('app.actor_subject',true),''); at_time timestamptz:=practitioner_registry_now(); managed boolean:=current_setting('app.practitioner_steward_manage',true)='true';
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'steward appointments are retained' USING ERRCODE='23514'; END IF;
 IF actor IS NULL THEN RAISE EXCEPTION 'steward actor required' USING ERRCODE='23514'; END IF;
 IF TG_OP='INSERT' THEN
  IF managed IS DISTINCT FROM true OR NEW.nominated_by<>actor OR NEW.subject=actor OR NEW.status<>'nominated' OR NEW.version<>1 OR NEW.valid_until<=at_time OR NEW.accepted_at IS NOT NULL OR NEW.ended_at IS NOT NULL OR NEW.ended_by IS NOT NULL OR NEW.end_reference IS NOT NULL THEN RAISE EXCEPTION 'invalid steward nomination' USING ERRCODE='23514',CONSTRAINT='practitioner_steward_nomination'; END IF;
  NEW.created_at:=at_time; NEW.updated_at:=at_time;
  RETURN NEW;
 END IF;
 IF (NEW.tenant_id,NEW.id,NEW.subject,NEW.nominated_by,NEW.review_reference,NEW.valid_until,NEW.created_at) IS DISTINCT FROM (OLD.tenant_id,OLD.id,OLD.subject,OLD.nominated_by,OLD.review_reference,OLD.valid_until,OLD.created_at) OR NEW.version<>OLD.version+1 THEN RAISE EXCEPTION 'immutable steward nomination or stale version' USING ERRCODE='23514'; END IF;
 IF OLD.status='nominated' AND NEW.status='active' THEN
  IF actor<>OLD.subject OR OLD.valid_until<=at_time THEN RAISE EXCEPTION 'steward acceptance requires current nominee' USING ERRCODE='23514',CONSTRAINT='practitioner_steward_acceptance'; END IF;
  NEW.accepted_at:=at_time; NEW.ended_at:=NULL; NEW.ended_by:=NULL; NEW.end_reference:=NULL;
 ELSIF OLD.status IN ('nominated','active') AND NEW.status IN ('withdrawn','revoked') THEN
  IF (NEW.status='withdrawn' AND actor<>OLD.subject) OR (NEW.status='revoked' AND managed IS DISTINCT FROM true) OR NEW.end_reference IS NULL THEN RAISE EXCEPTION 'steward termination authority required' USING ERRCODE='23514'; END IF;
  NEW.accepted_at:=OLD.accepted_at; NEW.ended_at:=at_time; NEW.ended_by:=actor;
 ELSIF OLD.status='active' AND NEW.status='expired' THEN
  IF OLD.valid_until>at_time OR (actor<>OLD.subject AND managed IS DISTINCT FROM true) THEN RAISE EXCEPTION 'steward validity has not elapsed' USING ERRCODE='23514'; END IF;
  NEW.accepted_at:=OLD.accepted_at; NEW.ended_at:=at_time; NEW.ended_by:=actor; NEW.end_reference:=NULL;
 ELSE RAISE EXCEPTION 'illegal steward transition' USING ERRCODE='23514';
 END IF;
 NEW.updated_at:=at_time;
 RETURN NEW;
END $$;
CREATE TRIGGER practitioner_steward_guard BEFORE INSERT OR UPDATE OR DELETE ON practitioner_steward_appointments FOR EACH ROW EXECUTE FUNCTION practitioner_steward_guard();
CREATE FUNCTION practitioner_steward_record_event() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO practitioner_steward_events(tenant_id,appointment_id,version,status,actor_subject,review_reference,created_at)
 VALUES(NEW.tenant_id,NEW.id,NEW.version,NEW.status,NULLIF(current_setting('app.actor_subject',true),''),COALESCE(NEW.end_reference,NEW.review_reference),NEW.updated_at);
 RETURN NULL;
END $$;
CREATE FUNCTION practitioner_steward_event_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP<>'INSERT' OR pg_trigger_depth()<2 THEN RAISE EXCEPTION 'steward history is immutable' USING ERRCODE='23514'; END IF;
 IF NEW.actor_subject IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'') OR NOT EXISTS(SELECT 1 FROM practitioner_steward_appointments a WHERE a.tenant_id=NEW.tenant_id AND a.id=NEW.appointment_id AND a.version=NEW.version AND a.status=NEW.status AND a.updated_at=NEW.created_at AND COALESCE(a.end_reference,a.review_reference)=NEW.review_reference) THEN RAISE EXCEPTION 'steward history does not match transition' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER practitioner_steward_event_guard BEFORE INSERT OR UPDATE OR DELETE ON practitioner_steward_events FOR EACH ROW EXECUTE FUNCTION practitioner_steward_event_guard();
CREATE TRIGGER practitioner_steward_audit AFTER INSERT OR UPDATE ON practitioner_steward_appointments FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER practitioner_steward_history AFTER INSERT OR UPDATE ON practitioner_steward_appointments FOR EACH ROW EXECUTE FUNCTION practitioner_steward_record_event();
