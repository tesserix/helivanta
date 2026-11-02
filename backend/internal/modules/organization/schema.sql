SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE TABLE organization_hospital_requests (
 tenant_id uuid NOT NULL, id uuid NOT NULL CHECK(substring(id::text,15,1)='7' AND substring(id::text,20,1) IN ('8','9','a','b')),
 hospital_id uuid NOT NULL CHECK(hospital_id<>'00000000-0000-0000-0000-000000000000'::uuid),
 reason text NOT NULL CHECK(length(btrim(reason)) BETWEEN 1 AND 500),
 actor_subject text NOT NULL CHECK(length(actor_subject) BETWEEN 1 AND 200),
 status text NOT NULL CHECK(status IN ('pending','verified','rejected')),
 observed_exists boolean,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,hospital_id,id),
 CHECK((status='pending' AND observed_exists IS NULL) OR (status='verified' AND observed_exists IS TRUE) OR (status='rejected' AND observed_exists IS FALSE))
);
CREATE TABLE organization_hospital_registrations (
 tenant_id uuid NOT NULL, id uuid NOT NULL CHECK(id<>'00000000-0000-0000-0000-000000000000'::uuid),
 request_id uuid NOT NULL, status text NOT NULL CHECK(status IN ('pending','verified','rejected')),
 version bigint NOT NULL CHECK(version BETWEEN 1 AND 9007199254740991),
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,id),
 FOREIGN KEY(tenant_id,id,request_id) REFERENCES organization_hospital_requests(tenant_id,hospital_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED
);
ALTER TABLE organization_hospital_requests ADD CONSTRAINT organization_request_hospital
 FOREIGN KEY(tenant_id,hospital_id) REFERENCES organization_hospital_registrations(tenant_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED;
CREATE INDEX organization_hospital_registration_page ON organization_hospital_registrations(tenant_id,created_at DESC,id DESC);
CREATE INDEX organization_hospital_request_history ON organization_hospital_requests(tenant_id,hospital_id,created_at DESC,id DESC);

ALTER TABLE organization_hospital_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE organization_hospital_requests FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON organization_hospital_requests
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.organization_write',true)='authorized');
ALTER TABLE organization_hospital_registrations ENABLE ROW LEVEL SECURITY;
ALTER TABLE organization_hospital_registrations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON organization_hospital_registrations
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.organization_write',true)='authorized');

CREATE FUNCTION organization_prevent_removal() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'organization evidence must be retained' USING ERRCODE='23514'; END $$;
CREATE TRIGGER organization_request_retained BEFORE DELETE OR TRUNCATE ON organization_hospital_requests
 FOR EACH STATEMENT EXECUTE FUNCTION organization_prevent_removal();
CREATE TRIGGER organization_registration_retained BEFORE DELETE OR TRUNCATE ON organization_hospital_registrations
 FOR EACH STATEMENT EXECUTE FUNCTION organization_prevent_removal();
CREATE FUNCTION organization_request_update_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.tenant_id,NEW.id,NEW.hospital_id,NEW.reason,NEW.actor_subject,NEW.created_at) IS DISTINCT FROM
    (OLD.tenant_id,OLD.id,OLD.hospital_id,OLD.reason,OLD.actor_subject,OLD.created_at)
    OR OLD.status<>'pending' OR NEW.status NOT IN ('verified','rejected')
    OR current_setting('app.organization_observation',true) IS DISTINCT FROM 'authorized' THEN
  RAISE EXCEPTION 'organization request intent is immutable' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER organization_request_update_guard BEFORE UPDATE ON organization_hospital_requests
 FOR EACH ROW EXECUTE FUNCTION organization_request_update_guard();
CREATE FUNCTION organization_registration_update_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.tenant_id,NEW.id,NEW.created_at) IS DISTINCT FROM (OLD.tenant_id,OLD.id,OLD.created_at)
    OR NEW.version<>OLD.version+1
    OR (NEW.request_id=OLD.request_id AND (OLD.status<>'pending' OR NEW.status NOT IN ('verified','rejected') OR current_setting('app.organization_observation',true) IS DISTINCT FROM 'authorized'))
    OR (NEW.request_id<>OLD.request_id AND (OLD.status<>'rejected' OR NEW.status<>'pending')) THEN
  RAISE EXCEPTION 'organization registration transition is invalid' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER organization_registration_update_guard BEFORE UPDATE ON organization_hospital_registrations
 FOR EACH ROW EXECUTE FUNCTION organization_registration_update_guard();
CREATE FUNCTION organization_registration_consistent() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM organization_hospital_registrations h JOIN organization_hospital_requests r
   ON (r.tenant_id,r.hospital_id,r.id)=(h.tenant_id,h.id,h.request_id)
   WHERE h.tenant_id=NEW.tenant_id AND h.id=NEW.id AND h.status<>r.status) THEN
  RAISE EXCEPTION 'organization registration and request must agree' USING ERRCODE='23514';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER organization_registration_consistent AFTER INSERT OR UPDATE ON organization_hospital_registrations
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION organization_registration_consistent();
