SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE TABLE emergency_receiving_facilities(
 tenant_id uuid NOT NULL,id uuid NOT NULL CHECK(get_byte(uuid_send(id),6)/16=7),
 created_at timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,id)
);
CREATE INDEX ON emergency_receiving_facilities(tenant_id,created_at DESC,id DESC);
ALTER TABLE emergency_receiving_facilities ENABLE ROW LEVEL SECURITY;
ALTER TABLE emergency_receiving_facilities FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON emergency_receiving_facilities
 USING(hms_tenant_visible(tenant_id) AND current_setting('app.emergency_admin',true)='true')
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.emergency_admin',true)='true');
CREATE TRIGGER emergency_receiving_facility_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON emergency_receiving_facilities FOR EACH STATEMENT EXECUTE FUNCTION emergency_append_only();

CREATE TABLE emergency_receiving_facility_revisions(
 tenant_id uuid NOT NULL,facility_id uuid NOT NULL,version bigint NOT NULL CHECK(version>0),
 name text NOT NULL CHECK(length(btrim(name)) BETWEEN 1 AND 150),enabled boolean NOT NULL,
 policy_reference text NOT NULL CHECK(policy_reference~'^[A-Za-z0-9._/-]{1,120}$'),expires_at timestamptz NOT NULL,
 verified_by text NOT NULL CHECK(length(verified_by) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,facility_id,version),FOREIGN KEY(tenant_id,facility_id) REFERENCES emergency_receiving_facilities(tenant_id,id) ON DELETE RESTRICT
);
ALTER TABLE emergency_receiving_facility_revisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE emergency_receiving_facility_revisions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON emergency_receiving_facility_revisions
 USING(hms_tenant_visible(tenant_id) AND current_setting('app.emergency_admin',true)='true')
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.emergency_admin',true)='true' AND verified_by=current_setting('app.emergency_subject',true));
CREATE TRIGGER emergency_receiving_revision_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON emergency_receiving_facility_revisions FOR EACH STATEMENT EXECUTE FUNCTION emergency_append_only();
CREATE FUNCTION emergency_receiving_revision_valid() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE previous bigint;
BEGIN
 PERFORM 1 FROM emergency_receiving_facilities WHERE tenant_id=NEW.tenant_id AND id=NEW.facility_id FOR UPDATE;
 SELECT COALESCE(max(version),0) INTO previous FROM emergency_receiving_facility_revisions WHERE tenant_id=NEW.tenant_id AND facility_id=NEW.facility_id;
 IF NEW.version<>previous+1 OR NEW.created_at<>now() OR (NEW.enabled AND (NEW.expires_at<=clock_timestamp() OR NEW.expires_at>clock_timestamp()+interval '1 year')) THEN
  RAISE EXCEPTION 'receiving revision changed' USING ERRCODE='40001';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER emergency_receiving_revision_valid BEFORE INSERT ON emergency_receiving_facility_revisions FOR EACH ROW EXECUTE FUNCTION emergency_receiving_revision_valid();
CREATE VIEW emergency_receiving_facility_history WITH(security_invoker=true) AS
 SELECT f.tenant_id,f.id,f.created_at,r.version,r.name,r.enabled,r.policy_reference,r.expires_at,r.verified_by,r.created_at AS updated_at
 FROM emergency_receiving_facilities f JOIN emergency_receiving_facility_revisions r ON r.tenant_id=f.tenant_id AND r.facility_id=f.id;

CREATE TABLE emergency_receiving_access(
 tenant_id uuid NOT NULL,facility_id uuid NOT NULL,subject text NOT NULL CHECK(length(btrim(subject)) BETWEEN 1 AND 200),
 version bigint NOT NULL CHECK(version>0),level text NOT NULL CHECK(level IN('receiver','manager')),enabled boolean NOT NULL,
 policy_reference text NOT NULL CHECK(policy_reference~'^[A-Za-z0-9._/-]{1,120}$'),expires_at timestamptz NOT NULL,
 verified_by text NOT NULL CHECK(length(verified_by) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,facility_id,subject,version),
 FOREIGN KEY(tenant_id,facility_id) REFERENCES emergency_receiving_facilities(tenant_id,id) ON DELETE RESTRICT,
 CHECK(NOT enabled OR subject<>verified_by)
);
CREATE INDEX ON emergency_receiving_access(tenant_id,subject,facility_id,version DESC);
ALTER TABLE emergency_receiving_access ENABLE ROW LEVEL SECURITY;
ALTER TABLE emergency_receiving_access FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON emergency_receiving_access
 USING(hms_tenant_visible(tenant_id) AND (current_setting('app.emergency_admin',true)='true' OR subject=current_setting('app.emergency_subject',true)))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.emergency_admin',true)='true' AND verified_by=current_setting('app.emergency_subject',true));
CREATE TRIGGER emergency_receiving_access_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON emergency_receiving_access FOR EACH STATEMENT EXECUTE FUNCTION emergency_append_only();
CREATE FUNCTION emergency_receiving_access_valid() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE previous bigint;
BEGIN
 PERFORM 1 FROM emergency_receiving_facilities WHERE tenant_id=NEW.tenant_id AND id=NEW.facility_id FOR UPDATE;
 SELECT COALESCE(max(version),0) INTO previous FROM emergency_receiving_access WHERE tenant_id=NEW.tenant_id AND facility_id=NEW.facility_id AND subject=NEW.subject;
 IF NEW.version<>previous+1 OR NEW.created_at<>now() OR (NEW.enabled AND (NEW.expires_at<=emergency_dispatch_clock() OR NEW.expires_at>emergency_dispatch_clock()+interval '1 year')) THEN
  RAISE EXCEPTION 'receiving authority changed' USING ERRCODE='40001';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER emergency_receiving_access_valid BEFORE INSERT ON emergency_receiving_access FOR EACH ROW EXECUTE FUNCTION emergency_receiving_access_valid();
CREATE FUNCTION emergency_receiving_allowed(t uuid,f uuid,ability text) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT t=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND EXISTS(
 SELECT 1 FROM emergency_receiving_access a WHERE a.tenant_id=t AND a.facility_id=f
 AND a.subject=current_setting('app.emergency_subject',true) AND a.enabled AND a.expires_at>emergency_dispatch_clock()
 AND (ability IN('read','accept') OR (ability='manage' AND a.level='manager'))
 AND NOT EXISTS(SELECT 1 FROM emergency_receiving_access newer WHERE newer.tenant_id=a.tenant_id AND newer.facility_id=a.facility_id AND newer.subject=a.subject AND newer.version>a.version))
$$;
ALTER POLICY tenant_isolation ON emergency_receiving_facilities USING(hms_tenant_visible(tenant_id) AND (current_setting('app.emergency_admin',true)='true' OR emergency_receiving_allowed(tenant_id,id,'read')));
ALTER POLICY tenant_isolation ON emergency_receiving_facility_revisions USING(hms_tenant_visible(tenant_id) AND (current_setting('app.emergency_admin',true)='true' OR emergency_receiving_allowed(tenant_id,facility_id,'read')));

CREATE TABLE emergency_receiving_links(
 tenant_id uuid NOT NULL,base_id uuid NOT NULL,id uuid NOT NULL CHECK(get_byte(uuid_send(id),6)/16=7),
 destination_tenant_id uuid NOT NULL,facility_id uuid NOT NULL,
 policy_reference text NOT NULL CHECK(policy_reference~'^[A-Za-z0-9._/-]{1,120}$'),valid_until timestamptz NOT NULL,
 requested_by text NOT NULL CHECK(length(requested_by) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,base_id,id),UNIQUE(tenant_id,id),
 FOREIGN KEY(tenant_id,base_id) REFERENCES emergency_bases(tenant_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(destination_tenant_id,facility_id) REFERENCES emergency_receiving_facilities(tenant_id,id) ON DELETE RESTRICT
);
CREATE INDEX ON emergency_receiving_links(destination_tenant_id,facility_id,created_at DESC,id DESC);
CREATE INDEX ON emergency_receiving_links(tenant_id,base_id,created_at DESC,id DESC);
ALTER TABLE emergency_receiving_links ENABLE ROW LEVEL SECURITY;
ALTER TABLE emergency_receiving_links FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON emergency_receiving_links
 USING((hms_tenant_visible(tenant_id) AND emergency_allowed(tenant_id,base_id,'read'))
 OR(hms_tenant_visible(destination_tenant_id) AND emergency_receiving_allowed(destination_tenant_id,facility_id,'read')))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND emergency_allowed(tenant_id,base_id,'close') AND requested_by=current_setting('app.emergency_subject',true));
CREATE TRIGGER emergency_receiving_link_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON emergency_receiving_links FOR EACH STATEMENT EXECUTE FUNCTION emergency_append_only();
CREATE FUNCTION emergency_receiving_link_valid()RETURNS trigger LANGUAGE plpgsql AS $$BEGIN
 IF NEW.valid_until<=emergency_dispatch_clock() OR NEW.valid_until>emergency_dispatch_clock()+interval '1 year' OR NEW.created_at<>now() THEN
 RAISE EXCEPTION 'invalid receiving agreement window' USING ERRCODE='40001';END IF;RETURN NEW;
END $$;
CREATE TRIGGER emergency_receiving_link_valid BEFORE INSERT ON emergency_receiving_links FOR EACH ROW EXECUTE FUNCTION emergency_receiving_link_valid();
CREATE TABLE emergency_receiving_link_commands(
 tenant_id uuid NOT NULL,base_id uuid NOT NULL,link_id uuid NOT NULL,id uuid NOT NULL CHECK(get_byte(uuid_send(id),6)/16=7),
 expected_version bigint NOT NULL CHECK(expected_version>0),operation text NOT NULL CHECK(operation IN('approve','reject','revoke')),
 actor_tenant_id uuid NOT NULL,actor_subject text NOT NULL,
 evidence_reference text NOT NULL CHECK(evidence_reference~'^[A-Za-z0-9._/-]{1,120}$'),
 result_status text NOT NULL CHECK(result_status IN('approved','rejected','revoked')),result_version bigint NOT NULL,
 receiver_policy_reference text,destination_name text,
 created_at timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,base_id,id),UNIQUE(tenant_id,base_id,link_id,result_version),
 FOREIGN KEY(tenant_id,base_id,link_id) REFERENCES emergency_receiving_links(tenant_id,base_id,id) ON DELETE RESTRICT
);
ALTER TABLE emergency_receiving_link_commands ENABLE ROW LEVEL SECURITY;
ALTER TABLE emergency_receiving_link_commands FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON emergency_receiving_link_commands
 USING(EXISTS(SELECT 1 FROM emergency_receiving_links l WHERE l.tenant_id=emergency_receiving_link_commands.tenant_id AND l.base_id=emergency_receiving_link_commands.base_id AND l.id=emergency_receiving_link_commands.link_id AND (hms_tenant_visible(l.tenant_id) OR hms_tenant_visible(l.destination_tenant_id))))
 WITH CHECK(actor_tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND actor_subject=current_setting('app.emergency_subject',true)
 AND EXISTS(SELECT 1 FROM emergency_receiving_links l WHERE l.tenant_id=emergency_receiving_link_commands.tenant_id AND l.base_id=emergency_receiving_link_commands.base_id AND l.id=emergency_receiving_link_commands.link_id));
CREATE TRIGGER emergency_receiving_link_command_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON emergency_receiving_link_commands FOR EACH STATEMENT EXECUTE FUNCTION emergency_append_only();
CREATE FUNCTION emergency_receiving_link_command_valid() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE saved emergency_receiving_links%ROWTYPE;previous emergency_receiving_link_commands%ROWTYPE;facility emergency_receiving_facility_revisions%ROWTYPE;
BEGIN
 SELECT * INTO saved FROM emergency_receiving_links WHERE tenant_id=NEW.tenant_id AND base_id=NEW.base_id AND id=NEW.link_id FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'receiving link unavailable' USING ERRCODE='42501';END IF;
 SELECT * INTO previous FROM emergency_receiving_link_commands WHERE tenant_id=NEW.tenant_id AND base_id=NEW.base_id AND link_id=NEW.link_id ORDER BY result_version DESC LIMIT 1;
 IF NEW.expected_version<>COALESCE(previous.result_version,1) THEN RAISE EXCEPTION 'receiving agreement version changed' USING ERRCODE='40001';END IF;
 IF NEW.operation IN('approve','reject') THEN
  IF NEW.actor_tenant_id<>saved.destination_tenant_id OR NOT COALESCE(emergency_receiving_allowed(saved.destination_tenant_id,saved.facility_id,'manage'),false) THEN RAISE EXCEPTION 'receiving authority required' USING ERRCODE='42501';END IF;
  IF previous.id IS NOT NULL OR saved.valid_until<=emergency_dispatch_clock() THEN RAISE EXCEPTION 'receiving proposal no longer active' USING ERRCODE='40001';END IF;
  IF NEW.operation='approve' THEN
   IF NEW.actor_subject=saved.requested_by THEN RAISE EXCEPTION 'independent approval required' USING ERRCODE='42501';END IF;
   SELECT * INTO facility FROM emergency_receiving_facility_revisions WHERE tenant_id=saved.destination_tenant_id AND facility_id=saved.facility_id ORDER BY version DESC LIMIT 1;
   IF NOT FOUND OR NOT facility.enabled OR facility.expires_at<=emergency_dispatch_clock() THEN RAISE EXCEPTION 'receiving facility inactive' USING ERRCODE='40001';END IF;
   NEW.result_status='approved';NEW.receiver_policy_reference=facility.policy_reference;NEW.destination_name=facility.name;
  ELSE NEW.result_status='rejected';END IF;
 ELSIF NEW.operation='revoke' THEN
  IF NOT COALESCE((NEW.actor_tenant_id=saved.tenant_id AND emergency_allowed(saved.tenant_id,saved.base_id,'close')) OR (NEW.actor_tenant_id=saved.destination_tenant_id AND emergency_receiving_allowed(saved.destination_tenant_id,saved.facility_id,'manage')),false) THEN RAISE EXCEPTION 'receiving revocation authority required' USING ERRCODE='42501';END IF;
  IF previous.result_status IN('rejected','revoked') THEN RAISE EXCEPTION 'receiving link already ended' USING ERRCODE='40001';END IF;
  NEW.result_status='revoked';NEW.receiver_policy_reference=previous.receiver_policy_reference;NEW.destination_name=previous.destination_name;
 END IF;
 NEW.result_version=NEW.expected_version+1;NEW.created_at=now();NEW.updated_at=now();RETURN NEW;
END $$;
CREATE TRIGGER emergency_receiving_link_command_valid BEFORE INSERT ON emergency_receiving_link_commands FOR EACH ROW EXECUTE FUNCTION emergency_receiving_link_command_valid();
CREATE VIEW emergency_receiving_link_status WITH(security_invoker=true)AS
 SELECT l.*,COALESCE(c.result_status,'proposed') AS status,COALESCE(c.result_version,1) AS version,c.receiver_policy_reference,c.destination_name
 FROM emergency_receiving_links l LEFT JOIN LATERAL(SELECT result_status,result_version,receiver_policy_reference,destination_name FROM emergency_receiving_link_commands WHERE tenant_id=l.tenant_id AND base_id=l.base_id AND link_id=l.id ORDER BY result_version DESC LIMIT 1)c ON true;

CREATE FUNCTION emergency_receiving_owner_allowed(t uuid,f uuid)RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT current_user='hms_owner' AND NULLIF(current_setting('app.emergency_receiving_request',true),'') IS NOT NULL
 AND t=NULLIF(current_setting('app.emergency_receiving_tenant',true),'')::uuid
 AND f=NULLIF(current_setting('app.emergency_receiving_facility',true),'')::uuid
 AND hms_tenant_visible(NULLIF(current_setting('app.tenant_id',true),'')::uuid)
$$;
ALTER POLICY tenant_isolation ON emergency_receiving_facilities USING((hms_tenant_visible(tenant_id) AND (current_setting('app.emergency_admin',true)='true' OR emergency_receiving_allowed(tenant_id,id,'read'))) OR emergency_receiving_owner_allowed(tenant_id,id));
ALTER POLICY tenant_isolation ON emergency_receiving_facility_revisions USING((hms_tenant_visible(tenant_id) AND (current_setting('app.emergency_admin',true)='true' OR emergency_receiving_allowed(tenant_id,facility_id,'read'))) OR emergency_receiving_owner_allowed(tenant_id,facility_id));
ALTER POLICY tenant_isolation ON emergency_receiving_access USING((hms_tenant_visible(tenant_id) AND (current_setting('app.emergency_admin',true)='true' OR subject=current_setting('app.emergency_subject',true))) OR(emergency_receiving_owner_allowed(tenant_id,facility_id) AND subject=current_setting('app.emergency_receiving_actor',true)));
CREATE FUNCTION emergency_receiving_link_active(t uuid,b uuid,link uuid)RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,public AS $$
DECLARE saved emergency_receiving_link_status%ROWTYPE;facility emergency_receiving_facility_revisions%ROWTYPE;pt text;pf text;pr text;answer boolean;
BEGIN
 IF t IS DISTINCT FROM NULLIF(current_setting('app.tenant_id',true),'')::uuid OR NOT COALESCE(emergency_allowed(t,b,'intake'),false) THEN RETURN false;END IF;
 SELECT * INTO saved FROM emergency_receiving_link_status WHERE tenant_id=t AND base_id=b AND id=link;
 IF NOT FOUND OR saved.status<>'approved' OR saved.valid_until<=emergency_dispatch_clock() THEN RETURN false;END IF;
 pt=current_setting('app.emergency_receiving_tenant',true);pf=current_setting('app.emergency_receiving_facility',true);pr=current_setting('app.emergency_receiving_request',true);
 PERFORM set_config('app.emergency_receiving_tenant',saved.destination_tenant_id::text,true),set_config('app.emergency_receiving_facility',saved.facility_id::text,true),set_config('app.emergency_receiving_request',saved.id::text,true);
 PERFORM 1 FROM emergency_receiving_facilities WHERE tenant_id=saved.destination_tenant_id AND id=saved.facility_id FOR SHARE;
 SELECT * INTO facility FROM emergency_receiving_facility_revisions WHERE tenant_id=saved.destination_tenant_id AND facility_id=saved.facility_id ORDER BY version DESC LIMIT 1;
 answer=FOUND AND facility.enabled AND facility.expires_at>emergency_dispatch_clock() AND facility.policy_reference=saved.receiver_policy_reference;
 PERFORM set_config('app.emergency_receiving_tenant',COALESCE(pt,''),true),set_config('app.emergency_receiving_facility',COALESCE(pf,''),true),set_config('app.emergency_receiving_request',COALESCE(pr,''),true);
 RETURN COALESCE(answer,false);
END $$;
REVOKE ALL ON FUNCTION emergency_receiving_link_active(uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION emergency_receiving_link_active(uuid,uuid,uuid) TO hms_app;

CREATE TABLE emergency_receiving_requests(
 tenant_id uuid NOT NULL,base_id uuid NOT NULL,id uuid NOT NULL CHECK(get_byte(uuid_send(id),6)/16=7),dispatch_id uuid NOT NULL,dispatch_version bigint NOT NULL CHECK(dispatch_version>0),
 link_id uuid NOT NULL,destination_tenant_id uuid NOT NULL,facility_id uuid NOT NULL,previous_request_id uuid,sequence bigint NOT NULL CHECK(sequence>0),
 required_class text NOT NULL CHECK(required_class IN('patient_transport','bls','als')),
 response_seconds int NOT NULL CHECK(response_seconds BETWEEN 15 AND 300),response_deadline timestamptz NOT NULL,
 evidence_reference text NOT NULL CHECK(evidence_reference~'^[A-Za-z0-9._/-]{1,120}$'),requested_by text NOT NULL,
 driver_subject text NOT NULL,clinician_subject text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,base_id,id),UNIQUE(tenant_id,id),UNIQUE(tenant_id,base_id,dispatch_id,sequence),
 FOREIGN KEY(tenant_id,base_id,dispatch_id) REFERENCES emergency_dispatches(tenant_id,base_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,base_id,link_id) REFERENCES emergency_receiving_links(tenant_id,base_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,base_id,previous_request_id) REFERENCES emergency_receiving_requests(tenant_id,base_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(destination_tenant_id,facility_id) REFERENCES emergency_receiving_facilities(tenant_id,id) ON DELETE RESTRICT
);
CREATE INDEX ON emergency_receiving_requests(destination_tenant_id,facility_id,created_at DESC,id DESC);
CREATE INDEX ON emergency_receiving_requests(tenant_id,base_id,dispatch_id,sequence DESC);
ALTER TABLE emergency_receiving_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE emergency_receiving_requests FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON emergency_receiving_requests
 USING((hms_tenant_visible(tenant_id) AND CASE WHEN current_user='helivanta_emergency_worker' THEN emergency_worker_allowed(tenant_id,base_id,dispatch_id) ELSE
 (emergency_allowed(tenant_id,base_id,'read') OR current_setting('app.emergency_subject',true) IN(driver_subject,clinician_subject)
 OR(current_setting('app.emergency_subject',true)='service:emergency-receiving-notifications' AND id=NULLIF(current_setting('app.emergency_notification',true),'')::uuid))END)
 OR(hms_tenant_visible(destination_tenant_id) AND CASE WHEN current_user='helivanta_emergency_worker' THEN false ELSE emergency_receiving_allowed(destination_tenant_id,facility_id,'read')END))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND emergency_allowed(tenant_id,base_id,'intake') AND requested_by=current_setting('app.emergency_subject',true));
CREATE TRIGGER emergency_receiving_request_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON emergency_receiving_requests FOR EACH STATEMENT EXECUTE FUNCTION emergency_append_only();

CREATE TABLE emergency_receiving_commands(
 tenant_id uuid NOT NULL,base_id uuid NOT NULL,request_id uuid NOT NULL,id uuid NOT NULL CHECK(get_byte(uuid_send(id),6)/16=7),
 expected_version bigint NOT NULL CHECK(expected_version>0),operation text NOT NULL CHECK(operation IN('accept','reject','revoke','handover_received')),
 valid_until timestamptz,evidence_reference text NOT NULL CHECK(evidence_reference~'^[A-Za-z0-9._/-]{1,120}$'),
 actor_tenant_id uuid NOT NULL,actor_subject text NOT NULL,grant_version bigint NOT NULL,
 result_status text NOT NULL CHECK(result_status IN('accepted','rejected','revoked','handover_received')),result_version bigint NOT NULL,
 result_valid_until timestamptz,destination_name text,
 created_at timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,base_id,id),UNIQUE(tenant_id,base_id,request_id,result_version),
 FOREIGN KEY(tenant_id,base_id,request_id) REFERENCES emergency_receiving_requests(tenant_id,base_id,id) ON DELETE RESTRICT
);
ALTER TABLE emergency_receiving_commands ENABLE ROW LEVEL SECURITY;
ALTER TABLE emergency_receiving_commands FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON emergency_receiving_commands
 USING(EXISTS(SELECT 1 FROM emergency_receiving_requests r WHERE r.tenant_id=emergency_receiving_commands.tenant_id AND r.base_id=emergency_receiving_commands.base_id AND r.id=emergency_receiving_commands.request_id AND(hms_tenant_visible(r.tenant_id) OR hms_tenant_visible(r.destination_tenant_id))))
 WITH CHECK(actor_tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND actor_subject=current_setting('app.emergency_subject',true)
 AND EXISTS(SELECT 1 FROM emergency_receiving_requests r WHERE r.tenant_id=emergency_receiving_commands.tenant_id AND r.base_id=emergency_receiving_commands.base_id AND r.id=emergency_receiving_commands.request_id AND r.destination_tenant_id=actor_tenant_id AND emergency_receiving_allowed(r.destination_tenant_id,r.facility_id,'accept')));
CREATE TRIGGER emergency_receiving_command_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON emergency_receiving_commands FOR EACH STATEMENT EXECUTE FUNCTION emergency_append_only();
CREATE VIEW emergency_receiving_request_status WITH(security_invoker=true)AS
 SELECT r.*,COALESCE(c.result_status,'pending') AS status,COALESCE(c.result_version,1) AS version,c.result_valid_until AS acceptance_valid_until,c.destination_name,
 EXISTS(SELECT 1 FROM emergency_receiving_requests newer WHERE newer.tenant_id=r.tenant_id AND newer.base_id=r.base_id AND newer.dispatch_id=r.dispatch_id AND newer.sequence>r.sequence)AS superseded
 FROM emergency_receiving_requests r LEFT JOIN LATERAL(SELECT result_status,result_version,result_valid_until,destination_name FROM emergency_receiving_commands WHERE tenant_id=r.tenant_id AND base_id=r.base_id AND request_id=r.id ORDER BY result_version DESC LIMIT 1)c ON true;
CREATE FUNCTION emergency_receiving_request_valid() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE trip emergency_dispatches%ROWTYPE;link emergency_receiving_links%ROWTYPE;previous emergency_receiving_request_status%ROWTYPE;
BEGIN
 SELECT * INTO trip FROM emergency_dispatches WHERE tenant_id=NEW.tenant_id AND base_id=NEW.base_id AND id=NEW.dispatch_id FOR UPDATE;
 IF NOT FOUND OR trip.version<>NEW.dispatch_version OR trip.state NOT IN('on_scene','transporting','arrived') THEN RAISE EXCEPTION 'trip cannot request a destination' USING ERRCODE='40001';END IF;
 SELECT * INTO previous FROM emergency_receiving_request_status WHERE tenant_id=NEW.tenant_id AND base_id=NEW.base_id AND dispatch_id=NEW.dispatch_id ORDER BY sequence DESC LIMIT 1;
 IF previous.id IS DISTINCT FROM NEW.previous_request_id OR previous.status='handover_received' THEN RAISE EXCEPTION 'destination superseded' USING ERRCODE='40001';END IF;
 IF NOT emergency_receiving_link_active(NEW.tenant_id,NEW.base_id,NEW.link_id) THEN RAISE EXCEPTION 'receiving agreement inactive' USING ERRCODE='40001';END IF;
 SELECT * INTO link FROM emergency_receiving_links WHERE tenant_id=NEW.tenant_id AND base_id=NEW.base_id AND id=NEW.link_id FOR SHARE;
 NEW.destination_tenant_id=link.destination_tenant_id;NEW.facility_id=link.facility_id;
 SELECT required_class INTO NEW.required_class FROM emergency_transport_assessments WHERE tenant_id=trip.tenant_id AND base_id=trip.base_id AND id=trip.assessment_id;
 NEW.sequence=COALESCE(previous.sequence,0)+1;NEW.driver_subject=trip.driver_subject;NEW.clinician_subject=trip.clinician_subject;
 NEW.response_deadline=emergency_dispatch_clock()+NEW.response_seconds*interval '1 second';NEW.created_at=now();NEW.updated_at=now();RETURN NEW;
END $$;
CREATE TRIGGER emergency_receiving_request_valid BEFORE INSERT ON emergency_receiving_requests FOR EACH ROW EXECUTE FUNCTION emergency_receiving_request_valid();

DO $$ DECLARE previous text; BEGIN
 SELECT pg_get_expr(polqual,polrelid) INTO previous FROM pg_policy WHERE polrelid='emergency_dispatches'::regclass AND polname='tenant_isolation';
 EXECUTE format('ALTER POLICY tenant_isolation ON emergency_dispatches USING((%s) OR (current_user=''hms_owner'' AND EXISTS(SELECT 1 FROM emergency_receiving_requests r WHERE r.tenant_id=emergency_dispatches.tenant_id AND r.base_id=emergency_dispatches.base_id AND r.dispatch_id=emergency_dispatches.id AND r.id=NULLIF(current_setting(''app.emergency_receiving_arrival'',true),'''')::uuid AND hms_tenant_visible(r.destination_tenant_id) AND emergency_receiving_allowed(r.destination_tenant_id,r.facility_id,''accept''))))',previous);
END $$;
CREATE FUNCTION emergency_receiving_arrived(t uuid,b uuid,trip uuid,request uuid)RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,public AS $$
DECLARE saved emergency_receiving_requests%ROWTYPE;previous text;answer boolean;
BEGIN
 SELECT * INTO saved FROM emergency_receiving_requests WHERE tenant_id=t AND base_id=b AND dispatch_id=trip AND id=request;
 IF NOT FOUND OR saved.destination_tenant_id IS DISTINCT FROM NULLIF(current_setting('app.tenant_id',true),'')::uuid OR NOT COALESCE(emergency_receiving_allowed(saved.destination_tenant_id,saved.facility_id,'accept'),false) THEN RETURN false;END IF;
 previous=current_setting('app.emergency_receiving_arrival',true);PERFORM set_config('app.emergency_receiving_arrival',request::text,true);
 SELECT EXISTS(SELECT 1 FROM emergency_dispatches WHERE tenant_id=t AND base_id=b AND id=trip AND state='arrived') INTO answer;
 PERFORM set_config('app.emergency_receiving_arrival',COALESCE(previous,''),true);RETURN answer;
END $$;
REVOKE ALL ON FUNCTION emergency_receiving_arrived(uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION emergency_receiving_arrived(uuid,uuid,uuid,uuid) TO hms_app;
CREATE FUNCTION emergency_receiving_command_valid() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE saved emergency_receiving_request_status%ROWTYPE;facility emergency_receiving_facility_revisions%ROWTYPE;link emergency_receiving_link_status%ROWTYPE;delegation emergency_receiving_access%ROWTYPE;
BEGIN
 PERFORM 1 FROM emergency_receiving_requests WHERE tenant_id=NEW.tenant_id AND base_id=NEW.base_id AND id=NEW.request_id FOR UPDATE;
 SELECT * INTO saved FROM emergency_receiving_request_status WHERE tenant_id=NEW.tenant_id AND base_id=NEW.base_id AND id=NEW.request_id;
 IF NOT FOUND OR saved.version<>NEW.expected_version OR saved.superseded THEN RAISE EXCEPTION 'receiving request changed' USING ERRCODE='40001';END IF;
 IF NEW.actor_tenant_id<>saved.destination_tenant_id OR NOT COALESCE(emergency_receiving_allowed(saved.destination_tenant_id,saved.facility_id,'accept'),false) THEN RAISE EXCEPTION 'receiving authority required' USING ERRCODE='42501';END IF;
 IF NEW.actor_subject IN(saved.driver_subject,saved.clinician_subject,saved.requested_by) THEN RAISE EXCEPTION 'independent receiving subject required' USING ERRCODE='42501';END IF;
 SELECT * INTO delegation FROM emergency_receiving_access WHERE tenant_id=saved.destination_tenant_id AND facility_id=saved.facility_id AND subject=NEW.actor_subject ORDER BY version DESC LIMIT 1;
 NEW.grant_version=delegation.version;
 IF NEW.operation='accept' THEN
  SELECT * INTO link FROM emergency_receiving_link_status WHERE tenant_id=saved.tenant_id AND base_id=saved.base_id AND id=saved.link_id;
  PERFORM 1 FROM emergency_receiving_facilities WHERE tenant_id=saved.destination_tenant_id AND id=saved.facility_id FOR SHARE;
  SELECT * INTO facility FROM emergency_receiving_facility_revisions WHERE tenant_id=saved.destination_tenant_id AND facility_id=saved.facility_id ORDER BY version DESC LIMIT 1;
  IF saved.status<>'pending' OR saved.response_deadline<=emergency_dispatch_clock() OR link.status IS DISTINCT FROM 'approved' OR link.valid_until<=emergency_dispatch_clock()
  OR NOT facility.enabled OR facility.expires_at<=emergency_dispatch_clock() OR facility.policy_reference IS DISTINCT FROM link.receiver_policy_reference
  OR NEW.valid_until IS NULL OR NEW.valid_until<=emergency_dispatch_clock() OR NEW.valid_until>emergency_dispatch_clock()+interval '2 hours'
  OR NEW.valid_until>LEAST(link.valid_until,facility.expires_at,delegation.expires_at) THEN RAISE EXCEPTION 'acceptance eligibility changed' USING ERRCODE='40001';END IF;
  NEW.result_status='accepted';NEW.result_valid_until=NEW.valid_until;NEW.destination_name=facility.name;
 ELSIF NEW.operation='reject' AND saved.status='pending' AND NEW.valid_until IS NULL THEN NEW.result_status='rejected';
 ELSIF NEW.operation='revoke' AND saved.status='accepted' AND NEW.valid_until IS NULL THEN NEW.result_status='revoked';
 ELSIF NEW.operation='handover_received' AND saved.status='accepted' AND NEW.valid_until IS NULL AND emergency_receiving_arrived(saved.tenant_id,saved.base_id,saved.dispatch_id,saved.id) THEN NEW.result_status='handover_received';
 ELSE RAISE EXCEPTION 'receiving transition changed' USING ERRCODE='40001';END IF;
 NEW.result_valid_until=COALESCE(NEW.result_valid_until,saved.acceptance_valid_until);NEW.destination_name=COALESCE(NEW.destination_name,saved.destination_name);
 NEW.result_version=NEW.expected_version+1;NEW.created_at=now();NEW.updated_at=now();RETURN NEW;
END $$;
CREATE TRIGGER emergency_receiving_command_valid BEFORE INSERT ON emergency_receiving_commands FOR EACH ROW EXECUTE FUNCTION emergency_receiving_command_valid();

GRANT SELECT(tenant_id,base_id,dispatch_id,id,destination_tenant_id,facility_id)ON emergency_receiving_requests TO helivanta_emergency_worker;

DO $$ DECLARE previous text;BEGIN
 SELECT pg_get_expr(polqual,polrelid) INTO previous FROM pg_policy WHERE polrelid='emergency_receiving_links'::regclass AND polname='tenant_isolation';
 EXECUTE format('ALTER POLICY tenant_isolation ON emergency_receiving_links USING((%s) OR(current_user=''hms_owner'' AND EXISTS(SELECT 1 FROM emergency_receiving_requests r WHERE r.tenant_id=emergency_receiving_links.tenant_id AND r.base_id=emergency_receiving_links.base_id AND r.link_id=emergency_receiving_links.id AND r.id=NULLIF(current_setting(''app.emergency_receiving_request'',true),'''')::uuid AND hms_tenant_visible(r.tenant_id))))',previous);
END $$;
CREATE FUNCTION emergency_receiving_ready(t uuid,b uuid,trip uuid,handover boolean,expected_request uuid)RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,public AS $$
DECLARE assignment emergency_dispatches%ROWTYPE;saved emergency_receiving_request_status%ROWTYPE;receipt emergency_receiving_commands%ROWTYPE;link emergency_receiving_link_status%ROWTYPE;facility emergency_receiving_facility_revisions%ROWTYPE;delegation emergency_receiving_access%ROWTYPE;
 pt text;pf text;pr text;pa text;answer boolean=false;
BEGIN
 IF t IS DISTINCT FROM NULLIF(current_setting('app.tenant_id',true),'')::uuid THEN RETURN false;END IF;
 SELECT * INTO assignment FROM emergency_dispatches WHERE tenant_id=t AND base_id=b AND id=trip FOR SHARE;
 IF NOT FOUND OR NOT COALESCE(emergency_allowed(t,b,'read') OR current_setting('app.emergency_subject',true) IN(assignment.driver_subject,assignment.clinician_subject),false) THEN RETURN false;END IF;
 SELECT * INTO saved FROM emergency_receiving_request_status WHERE tenant_id=t AND base_id=b AND dispatch_id=trip ORDER BY sequence DESC LIMIT 1;
 IF NOT FOUND OR saved.id IS DISTINCT FROM expected_request THEN RETURN false;END IF;
 PERFORM 1 FROM emergency_receiving_requests WHERE tenant_id=t AND base_id=b AND id=saved.id FOR SHARE;
 SELECT * INTO receipt FROM emergency_receiving_commands WHERE tenant_id=t AND base_id=b AND request_id=saved.id ORDER BY result_version DESC LIMIT 1;
 IF NOT FOUND THEN RETURN false;END IF;
 IF handover THEN RETURN receipt.result_status='handover_received' AND receipt.actor_subject NOT IN(assignment.driver_subject,assignment.clinician_subject);END IF;
 IF receipt.result_status<>'accepted' OR receipt.result_valid_until<=emergency_dispatch_clock() THEN RETURN false;END IF;
 pt=current_setting('app.emergency_receiving_tenant',true);pf=current_setting('app.emergency_receiving_facility',true);pr=current_setting('app.emergency_receiving_request',true);pa=current_setting('app.emergency_receiving_actor',true);
 PERFORM set_config('app.emergency_receiving_tenant',saved.destination_tenant_id::text,true),set_config('app.emergency_receiving_facility',saved.facility_id::text,true),set_config('app.emergency_receiving_request',saved.id::text,true),set_config('app.emergency_receiving_actor',receipt.actor_subject,true);
 PERFORM 1 FROM emergency_receiving_links WHERE tenant_id=t AND base_id=b AND id=saved.link_id FOR SHARE;
 SELECT * INTO link FROM emergency_receiving_link_status WHERE tenant_id=t AND base_id=b AND id=saved.link_id;
 PERFORM 1 FROM emergency_receiving_facilities WHERE tenant_id=saved.destination_tenant_id AND id=saved.facility_id FOR SHARE;
 SELECT * INTO facility FROM emergency_receiving_facility_revisions WHERE tenant_id=saved.destination_tenant_id AND facility_id=saved.facility_id ORDER BY version DESC LIMIT 1;
 SELECT * INTO delegation FROM emergency_receiving_access WHERE tenant_id=saved.destination_tenant_id AND facility_id=saved.facility_id AND subject=receipt.actor_subject ORDER BY version DESC LIMIT 1;
 answer=link.status='approved' AND link.valid_until>emergency_dispatch_clock() AND facility.enabled AND facility.expires_at>emergency_dispatch_clock() AND facility.policy_reference=link.receiver_policy_reference
 AND delegation.enabled AND delegation.expires_at>emergency_dispatch_clock() AND delegation.version=receipt.grant_version;
 PERFORM set_config('app.emergency_receiving_tenant',COALESCE(pt,''),true),set_config('app.emergency_receiving_facility',COALESCE(pf,''),true),set_config('app.emergency_receiving_request',COALESCE(pr,''),true),set_config('app.emergency_receiving_actor',COALESCE(pa,''),true);
 RETURN COALESCE(answer,false);
END $$;
REVOKE ALL ON FUNCTION emergency_receiving_ready(uuid,uuid,uuid,boolean,uuid)FROM PUBLIC;
GRANT EXECUTE ON FUNCTION emergency_receiving_ready(uuid,uuid,uuid,boolean,uuid)TO hms_app;

ALTER TABLE emergency_dispatch_commands ADD COLUMN receiving_request_id uuid;
ALTER TABLE emergency_dispatch_commands ADD CONSTRAINT emergency_dispatch_receiving_request_fk FOREIGN KEY(tenant_id,base_id,receiving_request_id)REFERENCES emergency_receiving_requests(tenant_id,base_id,id)ON DELETE RESTRICT;
ALTER TABLE emergency_dispatch_commands ADD CONSTRAINT emergency_dispatch_receiving_request_check CHECK((operation IN('transporting','arrived','handover_complete'))=(receiving_request_id IS NOT NULL));
ALTER TABLE emergency_dispatch_commands DROP CONSTRAINT emergency_dispatch_commands_operation_check;
ALTER TABLE emergency_dispatch_commands ADD CONSTRAINT emergency_dispatch_commands_operation_check CHECK(operation IN('request_cancel','reconcile_cancelled','acknowledge','en_route','on_scene','transporting','arrived','handover_complete','clear','offer','acknowledgement_timeout'));
CREATE FUNCTION emergency_case_completed_trip_guard()RETURNS trigger LANGUAGE plpgsql AS $$BEGIN
 IF EXISTS(SELECT 1 FROM emergency_dispatches d WHERE d.tenant_id=NEW.tenant_id AND d.base_id=NEW.base_id AND d.case_id=NEW.case_id AND d.state='cleared') THEN RAISE EXCEPTION 'case journey already completed' USING ERRCODE='40001';END IF;RETURN NEW;
END $$;
CREATE TRIGGER emergency_case_completed_trip_guard BEFORE INSERT ON emergency_dispatches FOR EACH ROW EXECUTE FUNCTION emergency_case_completed_trip_guard();

CREATE OR REPLACE FUNCTION emergency_dispatch_command_valid() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE saved emergency_dispatches%ROWTYPE;
BEGIN
 PERFORM 1 FROM emergency_bases WHERE tenant_id=NEW.tenant_id AND id=NEW.base_id FOR NO KEY UPDATE;
 SELECT * INTO saved FROM emergency_dispatches WHERE tenant_id=NEW.tenant_id AND base_id=NEW.base_id AND id=NEW.dispatch_id FOR UPDATE;
 IF NOT FOUND OR saved.version<>NEW.expected_version THEN
  RAISE EXCEPTION 'dispatch version changed' USING ERRCODE='40001';
 END IF;
 IF NEW.actor_subject IS DISTINCT FROM NULLIF(current_setting('app.emergency_subject',true),'') THEN
  RAISE EXCEPTION 'dispatch actor mismatch' USING ERRCODE='42501';
 END IF;
 NEW.result_driver_acknowledged=saved.driver_acknowledged; NEW.result_clinician_acknowledged=saved.clinician_acknowledged;
 IF NEW.operation='acknowledge' AND saved.state='offered' AND saved.acknowledgement_deadline>emergency_dispatch_clock()
 AND NEW.actor_subject IN(saved.driver_subject,saved.clinician_subject) THEN
  IF NEW.actor_subject=saved.driver_subject AND NOT saved.driver_acknowledged THEN NEW.result_driver_acknowledged=true;
  ELSIF NEW.actor_subject=saved.clinician_subject AND NOT saved.clinician_acknowledged THEN NEW.result_clinician_acknowledged=true;
  ELSE RAISE EXCEPTION 'crew acknowledgement already recorded' USING ERRCODE='40001'; END IF;
  IF NOT emergency_dispatch_offer_eligible(NEW.tenant_id,NEW.base_id,NEW.dispatch_id) THEN RAISE EXCEPTION 'dispatch eligibility changed' USING ERRCODE='40001'; END IF;
  INSERT INTO emergency_crew_commitments(tenant_id,base_id,dispatch_id,subject) VALUES(NEW.tenant_id,NEW.base_id,NEW.dispatch_id,NEW.actor_subject);
  IF NEW.result_driver_acknowledged AND NEW.result_clinician_acknowledged THEN NEW.result_state='acknowledged'; ELSE NEW.result_state='offered'; END IF;
 ELSIF NEW.operation='en_route' AND saved.state='acknowledged' AND NEW.actor_subject IN(saved.driver_subject,saved.clinician_subject) THEN
  IF NOT emergency_dispatch_offer_eligible(NEW.tenant_id,NEW.base_id,NEW.dispatch_id) THEN RAISE EXCEPTION 'dispatch eligibility changed' USING ERRCODE='40001'; END IF;
  NEW.result_state='en_route';
 ELSIF NEW.operation='on_scene' AND saved.state='en_route' AND NEW.actor_subject IN(saved.driver_subject,saved.clinician_subject) THEN NEW.result_state='on_scene';
 ELSIF NEW.operation='transporting' AND saved.state IN('on_scene','arrived') AND NEW.actor_subject IN(saved.driver_subject,saved.clinician_subject) THEN
  IF NOT emergency_dispatch_offer_eligible(NEW.tenant_id,NEW.base_id,NEW.dispatch_id) THEN RAISE EXCEPTION 'clinical or fleet eligibility changed' USING ERRCODE='40001';END IF;
  IF NOT emergency_receiving_ready(NEW.tenant_id,NEW.base_id,NEW.dispatch_id,false,NEW.receiving_request_id) THEN RAISE EXCEPTION 'live receiving acceptance required' USING ERRCODE='40001';END IF;
  NEW.result_state='transporting';
 ELSIF NEW.operation='arrived' AND saved.state='transporting' AND NEW.actor_subject IN(saved.driver_subject,saved.clinician_subject) THEN
  IF NOT emergency_receiving_ready(NEW.tenant_id,NEW.base_id,NEW.dispatch_id,false,NEW.receiving_request_id) THEN RAISE EXCEPTION 'live receiving acceptance required' USING ERRCODE='40001';END IF;
  NEW.result_state='arrived';
 ELSIF NEW.operation='handover_complete' AND saved.state='arrived' AND NEW.actor_subject IN(saved.driver_subject,saved.clinician_subject) THEN
  IF NOT emergency_receiving_ready(NEW.tenant_id,NEW.base_id,NEW.dispatch_id,true,NEW.receiving_request_id) THEN RAISE EXCEPTION 'independent receiving handover required' USING ERRCODE='40001';END IF;
  NEW.result_state='handover_complete';
 ELSIF NEW.operation='clear' AND saved.state='handover_complete' AND emergency_allowed(NEW.tenant_id,NEW.base_id,'close') THEN
  IF NOT EXISTS(SELECT 1 FROM emergency_readiness r JOIN emergency_vehicles v ON v.tenant_id=r.tenant_id AND v.base_id=r.base_id AND v.id=r.vehicle_id
  WHERE r.tenant_id=saved.tenant_id AND r.base_id=saved.base_id AND r.vehicle_id=saved.vehicle_id AND r.duty_id=saved.duty_id AND r.vehicle_version=v.version AND r.created_at>saved.updated_at AND r.valid_until>emergency_dispatch_clock()
  AND NOT EXISTS(SELECT 1 FROM emergency_readiness newer WHERE newer.tenant_id=r.tenant_id AND newer.base_id=r.base_id AND newer.vehicle_id=r.vehicle_id AND(newer.created_at,newer.id)>(r.created_at,r.id))) THEN
   RAISE EXCEPTION 'post-handover readiness observation required' USING ERRCODE='40001';END IF;
  NEW.result_state='cleared';
 ELSIF NEW.operation='offer' AND emergency_worker_allowed(NEW.tenant_id,NEW.base_id,NEW.dispatch_id) AND saved.state='workflow_pending' THEN
  IF emergency_dispatch_offer_eligible(NEW.tenant_id,NEW.base_id,NEW.dispatch_id) THEN
   NEW.result_state='offered'; NEW.result_acknowledgement_deadline=emergency_dispatch_clock()+saved.acknowledgement_seconds*interval '1 second';
  ELSE NEW.result_state='reconciliation_required'; END IF;
 ELSIF NEW.operation='acknowledgement_timeout' AND emergency_worker_allowed(NEW.tenant_id,NEW.base_id,NEW.dispatch_id)
 AND saved.state='offered' AND saved.acknowledgement_deadline<=emergency_dispatch_clock() THEN
  NEW.result_state='reconciliation_required';
 ELSIF NEW.operation='request_cancel' AND emergency_allowed(NEW.tenant_id,NEW.base_id,'intake')
 AND saved.state NOT IN('transporting','arrived','handover_complete','cleared','cancelled','cancel_requested') THEN
  NEW.result_state='cancel_requested';
 ELSIF NEW.operation='reconcile_cancelled' AND emergency_allowed(NEW.tenant_id,NEW.base_id,'close')
 AND saved.state IN('cancel_requested','reconciliation_required') THEN
  NEW.result_state='cancelled';
 ELSIF NEW.operation IN('acknowledge','en_route','on_scene','transporting','arrived','handover_complete') AND NEW.actor_subject IN(saved.driver_subject,saved.clinician_subject) THEN
  RAISE EXCEPTION 'crew command state changed' USING ERRCODE='40001';
 ELSIF NEW.operation IN('request_cancel','clear') AND emergency_allowed(NEW.tenant_id,NEW.base_id,'intake') THEN RAISE EXCEPTION 'dispatch transition changed' USING ERRCODE='40001';
 ELSE
  RAISE EXCEPTION 'dispatch command not permitted' USING ERRCODE='42501';
 END IF;
 NEW.result_acknowledgement_deadline=COALESCE(NEW.result_acknowledgement_deadline,saved.acknowledgement_deadline);

 NEW.result_version=saved.version+1;
 NEW.created_at=now(); NEW.updated_at=now();
 RETURN NEW;
END $$;

CREATE TABLE emergency_receiving_timeouts(
 tenant_id uuid NOT NULL,base_id uuid NOT NULL,dispatch_id uuid NOT NULL,request_id uuid NOT NULL,
 kind text NOT NULL CHECK(kind IN('response','acceptance')),deadline timestamptz NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,base_id,request_id,kind),
 FOREIGN KEY(tenant_id,base_id,request_id)REFERENCES emergency_receiving_requests(tenant_id,base_id,id)ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,base_id,dispatch_id)REFERENCES emergency_dispatches(tenant_id,base_id,id)ON DELETE RESTRICT
);
ALTER TABLE emergency_receiving_timeouts ENABLE ROW LEVEL SECURITY;
ALTER TABLE emergency_receiving_timeouts FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON emergency_receiving_timeouts
 USING(hms_tenant_visible(tenant_id) AND EXISTS(SELECT 1 FROM emergency_receiving_requests r WHERE r.tenant_id=emergency_receiving_timeouts.tenant_id AND r.base_id=emergency_receiving_timeouts.base_id AND r.id=emergency_receiving_timeouts.request_id))
 WITH CHECK(current_user='helivanta_emergency_worker' AND emergency_worker_allowed(tenant_id,base_id,dispatch_id));
CREATE TRIGGER emergency_receiving_timeout_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON emergency_receiving_timeouts FOR EACH STATEMENT EXECUTE FUNCTION emergency_append_only();
CREATE FUNCTION emergency_receiving_timeout_valid()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE saved record;expected timestamptz;
BEGIN
 SELECT * INTO saved FROM emergency_receiving_workflow_state WHERE tenant_id=NEW.tenant_id AND base_id=NEW.base_id AND dispatch_id=NEW.dispatch_id AND id=NEW.request_id;
 IF NOT FOUND OR saved.superseded THEN RAISE EXCEPTION 'receiving deadline changed' USING ERRCODE='40001';END IF;
 IF NEW.kind=saved.kind THEN expected=saved.deadline;END IF;
 IF expected IS NULL OR expected IS DISTINCT FROM NEW.deadline OR expected>emergency_dispatch_clock() OR NEW.created_at<>now() THEN RAISE EXCEPTION 'receiving deadline changed' USING ERRCODE='40001';END IF;RETURN NEW;
END $$;
CREATE TRIGGER emergency_receiving_timeout_valid BEFORE INSERT ON emergency_receiving_timeouts FOR EACH ROW EXECUTE FUNCTION emergency_receiving_timeout_valid();
GRANT SELECT(tenant_id,base_id,id,dispatch_id,sequence,response_deadline)ON emergency_receiving_requests TO helivanta_emergency_worker;
GRANT SELECT(tenant_id,base_id,request_id,result_status,result_version,result_valid_until,destination_name)ON emergency_receiving_commands TO helivanta_emergency_worker;
GRANT SELECT ON emergency_receiving_timeouts TO helivanta_emergency_worker;
GRANT INSERT(tenant_id,base_id,dispatch_id,request_id,kind,deadline)ON emergency_receiving_timeouts TO helivanta_emergency_worker;
ALTER POLICY emergency_worker_audit_guard ON emergency_audit
 WITH CHECK(emergency_worker_allowed(tenant_id,base_id,resource_id) AND actor_subject='service:emergency-dispatch-worker' AND target_subject IS NULL AND action IN('dispatch.offer','dispatch.acknowledgement_timeout','dispatch.receiving_timeout'));


CREATE VIEW emergency_receiving_workflow_state WITH(security_invoker=true)AS
 SELECT r.tenant_id,r.base_id,r.dispatch_id,r.id,r.sequence,COALESCE(c.result_status,'pending')AS status,
 CASE WHEN c.result_status IS NULL THEN r.response_deadline WHEN c.result_status='accepted' THEN c.result_valid_until END AS deadline,
 CASE WHEN c.result_status IS NULL THEN 'response' WHEN c.result_status='accepted' THEN 'acceptance' ELSE '' END AS kind,
 EXISTS(SELECT 1 FROM emergency_receiving_requests newer WHERE newer.tenant_id=r.tenant_id AND newer.base_id=r.base_id AND newer.dispatch_id=r.dispatch_id AND newer.sequence>r.sequence)AS superseded
 FROM emergency_receiving_requests r LEFT JOIN LATERAL(SELECT result_status,result_valid_until FROM emergency_receiving_commands WHERE tenant_id=r.tenant_id AND base_id=r.base_id AND request_id=r.id ORDER BY result_version DESC LIMIT 1)c ON true;
GRANT SELECT ON emergency_receiving_workflow_state TO helivanta_emergency_worker;

CREATE VIEW emergency_receiving_facility_status WITH(security_invoker=true)AS
 SELECT f.*,(SELECT CASE WHEN a.enabled AND a.expires_at>emergency_dispatch_clock() THEN a.level END FROM emergency_receiving_access a WHERE a.tenant_id=f.tenant_id AND a.facility_id=f.id AND a.subject=current_setting('app.emergency_subject',true)ORDER BY a.version DESC LIMIT 1)AS access_level
 FROM emergency_receiving_facility_history f WHERE NOT EXISTS(SELECT 1 FROM emergency_receiving_facility_revisions newer WHERE newer.tenant_id=f.tenant_id AND newer.facility_id=f.id AND newer.version>f.version);
