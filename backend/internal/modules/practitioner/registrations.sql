SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE TABLE practitioner_registrations (
 tenant_id uuid NOT NULL,
 practitioner_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7' AND substring(id::text,20,1) IN ('8','9','a','b')),
 council_system text NOT NULL CHECK(council_system ~ '^[a-z][a-z0-9.-]{0,63}$'),
 registration_number text NOT NULL CHECK(octet_length(registration_number) BETWEEN 1 AND 100 AND length(btrim(registration_number))>0),
 version bigint NOT NULL DEFAULT 1 CHECK(version BETWEEN 1 AND 9007199254740991),
 created_by text NOT NULL CHECK(length(created_by) BETWEEN 1 AND 512),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,id),
 UNIQUE(tenant_id,practitioner_id,id),
 UNIQUE(tenant_id,practitioner_id,council_system,registration_number),
 FOREIGN KEY(tenant_id,practitioner_id) REFERENCES practitioner_records(tenant_id,id)
);
CREATE INDEX practitioner_registration_page ON practitioner_registrations(tenant_id,practitioner_id,created_at DESC,id DESC);
ALTER TABLE practitioner_registrations ENABLE ROW LEVEL SECURITY;
ALTER TABLE practitioner_registrations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON practitioner_registrations
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.practitioner_access',true)='true')
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.practitioner_manage',true)='true');
CREATE TABLE practitioner_registration_revisions (
 tenant_id uuid NOT NULL,
 practitioner_id uuid NOT NULL,
 registration_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7' AND substring(id::text,20,1) IN ('8','9','a','b')),
 expected_version bigint NOT NULL CHECK(expected_version BETWEEN 0 AND 9007199254740990),
 version bigint NOT NULL CHECK(version=expected_version+1),
 issued_on date,
 expires_on date,
 verification_state text NOT NULL DEFAULT 'unverified' CHECK(verification_state='unverified'),
 created_by text NOT NULL CHECK(length(created_by) BETWEEN 1 AND 512),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,id),
 UNIQUE(tenant_id,registration_id,version),
 FOREIGN KEY(tenant_id,practitioner_id,registration_id) REFERENCES practitioner_registrations(tenant_id,practitioner_id,id),
 CHECK(issued_on IS NULL OR issued_on BETWEEN DATE '0001-01-01' AND DATE '9999-12-31'),
 CHECK(expires_on IS NULL OR expires_on BETWEEN DATE '0001-01-01' AND DATE '9999-12-31'),
 CHECK(issued_on IS NULL OR expires_on IS NULL OR expires_on>=issued_on)
);
CREATE INDEX practitioner_registration_revision_page ON practitioner_registration_revisions(tenant_id,practitioner_id,registration_id,created_at DESC,id DESC);
ALTER TABLE practitioner_registration_revisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE practitioner_registration_revisions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON practitioner_registration_revisions
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.practitioner_access',true)='true')
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.practitioner_manage',true)='true' AND created_by=NULLIF(current_setting('app.actor_subject',true),''));
CREATE FUNCTION practitioner_registration_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'registration history is retained' USING ERRCODE='23514'; END IF;
 IF current_setting('app.practitioner_manage',true) IS DISTINCT FROM 'true' THEN RAISE EXCEPTION 'registration management authority required' USING ERRCODE='23514'; END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.version<>1 OR NEW.created_by IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'') THEN RAISE EXCEPTION 'invalid registration intake' USING ERRCODE='23514'; END IF;
 ELSE
  IF (NEW.tenant_id,NEW.practitioner_id,NEW.id,NEW.council_system,NEW.registration_number,NEW.created_by,NEW.created_at) IS DISTINCT FROM (OLD.tenant_id,OLD.practitioner_id,OLD.id,OLD.council_system,OLD.registration_number,OLD.created_by,OLD.created_at) OR NEW.version<>OLD.version+1 OR NOT EXISTS(SELECT 1 FROM practitioner_registration_revisions v WHERE v.tenant_id=NEW.tenant_id AND v.registration_id=NEW.id AND v.version=NEW.version AND v.expected_version=OLD.version AND v.created_by=NULLIF(current_setting('app.actor_subject',true),'')) THEN RAISE EXCEPTION 'registration advance requires attributable revision' USING ERRCODE='23514'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER practitioner_registration_guard BEFORE INSERT OR UPDATE OR DELETE ON practitioner_registrations FOR EACH ROW EXECUTE FUNCTION practitioner_registration_guard();
CREATE FUNCTION practitioner_registration_revision_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE head_version bigint;
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'registration revision is immutable' USING ERRCODE='23514'; END IF;
 IF NEW.created_by IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'') OR current_setting('app.practitioner_manage',true) IS DISTINCT FROM 'true' THEN RAISE EXCEPTION 'registration management authority required' USING ERRCODE='23514'; END IF;
 SELECT version INTO head_version FROM practitioner_registrations WHERE tenant_id=NEW.tenant_id AND practitioner_id=NEW.practitioner_id AND id=NEW.registration_id FOR UPDATE;
 IF NOT FOUND OR NOT ((NEW.version=1 AND head_version=1 AND NEW.expected_version=0 AND NEW.id=NEW.registration_id) OR (NEW.version>1 AND head_version=NEW.expected_version)) THEN RAISE EXCEPTION 'stale registration revision' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER practitioner_registration_revision_guard BEFORE INSERT OR UPDATE OR DELETE ON practitioner_registration_revisions FOR EACH ROW EXECUTE FUNCTION practitioner_registration_revision_guard();
CREATE FUNCTION practitioner_registration_commit_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM practitioner_registration_revisions v WHERE v.tenant_id=NEW.tenant_id AND v.registration_id=NEW.id AND v.version=(SELECT r.version FROM practitioner_registrations r WHERE r.tenant_id=NEW.tenant_id AND r.id=NEW.id)) THEN RAISE EXCEPTION 'registration requires committed revision' USING ERRCODE='23514'; END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER practitioner_registration_committed AFTER INSERT OR UPDATE ON practitioner_registrations DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION practitioner_registration_commit_guard();
CREATE FUNCTION practitioner_registration_revision_commit_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM practitioner_registrations r WHERE r.tenant_id=NEW.tenant_id AND r.practitioner_id=NEW.practitioner_id AND r.id=NEW.registration_id AND r.version>=NEW.version) THEN RAISE EXCEPTION 'registration revision requires advanced head' USING ERRCODE='23514'; END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER practitioner_registration_revision_committed AFTER INSERT ON practitioner_registration_revisions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION practitioner_registration_revision_commit_guard();
CREATE TRIGGER practitioner_registration_audit AFTER INSERT OR UPDATE ON practitioner_registrations FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER practitioner_registration_revision_audit AFTER INSERT ON practitioner_registration_revisions FOR EACH ROW EXECUTE FUNCTION audit_record_change();
