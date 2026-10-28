SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE TABLE practitioner_specialties (
 tenant_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7' AND substring(id::text,20,1) IN ('8','9','a','b')),
 code text NOT NULL CHECK(code ~ '^[a-z][a-z0-9.-]{0,63}$'),
 display text NOT NULL CHECK(octet_length(display) BETWEEN 1 AND 150 AND display=btrim(display)),
 created_by text NOT NULL CHECK(octet_length(created_by) BETWEEN 1 AND 512),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,code)
);
CREATE INDEX practitioner_specialty_page ON practitioner_specialties(tenant_id,created_at DESC,id DESC);
ALTER TABLE practitioner_specialties ENABLE ROW LEVEL SECURITY;
ALTER TABLE practitioner_specialties FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON practitioner_specialties
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.practitioner_access',true)='true')
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.practitioner_specialty_manage',true)='true' AND created_by=NULLIF(current_setting('app.actor_subject',true),''));
CREATE FUNCTION practitioner_specialty_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'specialty catalogue entry is immutable' USING ERRCODE='23514'; END IF;
 IF current_setting('app.practitioner_specialty_manage',true) IS DISTINCT FROM 'true' OR NEW.created_by IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'') THEN RAISE EXCEPTION 'specialty management authority required' USING ERRCODE='42501'; END IF;
 NEW.created_at:=clock_timestamp(); RETURN NEW;
END $$;
CREATE TRIGGER practitioner_specialty_guard BEFORE INSERT OR UPDATE OR DELETE ON practitioner_specialties FOR EACH ROW EXECUTE FUNCTION practitioner_specialty_guard();
CREATE TRIGGER practitioner_specialty_audit AFTER INSERT ON practitioner_specialties FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TABLE practitioner_qualifications (
 tenant_id uuid NOT NULL,
 practitioner_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7' AND substring(id::text,20,1) IN ('8','9','a','b')),
 version bigint NOT NULL DEFAULT 1 CHECK(version BETWEEN 1 AND 9007199254740991),
 created_by text NOT NULL CHECK(octet_length(created_by) BETWEEN 1 AND 512),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,practitioner_id,id),
 FOREIGN KEY(tenant_id,practitioner_id) REFERENCES practitioner_records(tenant_id,id)
);
CREATE INDEX practitioner_qualification_page ON practitioner_qualifications(tenant_id,practitioner_id,created_at DESC,id DESC);
ALTER TABLE practitioner_qualifications ENABLE ROW LEVEL SECURITY;
ALTER TABLE practitioner_qualifications FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON practitioner_qualifications
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.practitioner_access',true)='true')
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.practitioner_manage',true)='true');
CREATE TABLE practitioner_qualification_revisions (
 tenant_id uuid NOT NULL,
 practitioner_id uuid NOT NULL,
 qualification_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7' AND substring(id::text,20,1) IN ('8','9','a','b')),
 expected_version bigint NOT NULL CHECK(expected_version BETWEEN 0 AND 9007199254740990),
 version bigint NOT NULL CHECK(version=expected_version+1),
 degree text NOT NULL CHECK(octet_length(degree) BETWEEN 1 AND 150 AND degree=btrim(degree)),
 awarding_body text NOT NULL CHECK(octet_length(awarding_body) BETWEEN 1 AND 150 AND awarding_body=btrim(awarding_body)),
 completion_year bigint CHECK(completion_year BETWEEN 1 AND 9999),
 specialty_id uuid,
 verification_state text NOT NULL DEFAULT 'unverified' CHECK(verification_state='unverified'),
 created_by text NOT NULL CHECK(octet_length(created_by) BETWEEN 1 AND 512),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,qualification_id,version),
 FOREIGN KEY(tenant_id,practitioner_id,qualification_id) REFERENCES practitioner_qualifications(tenant_id,practitioner_id,id),
 FOREIGN KEY(tenant_id,specialty_id) REFERENCES practitioner_specialties(tenant_id,id)
);
CREATE INDEX practitioner_qualification_revision_page ON practitioner_qualification_revisions(tenant_id,qualification_id,created_at DESC,id DESC);
ALTER TABLE practitioner_qualification_revisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE practitioner_qualification_revisions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON practitioner_qualification_revisions
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.practitioner_access',true)='true')
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.practitioner_manage',true)='true' AND created_by=NULLIF(current_setting('app.actor_subject',true),''));
CREATE FUNCTION practitioner_qualification_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'qualification history is retained' USING ERRCODE='23514'; END IF;
 IF current_setting('app.practitioner_manage',true) IS DISTINCT FROM 'true' THEN RAISE EXCEPTION 'qualification management authority required' USING ERRCODE='23514'; END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.version<>1 OR NEW.created_by IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'') THEN RAISE EXCEPTION 'invalid qualification intake' USING ERRCODE='23514'; END IF;
 ELSE
  IF (NEW.tenant_id,NEW.practitioner_id,NEW.id,NEW.created_by,NEW.created_at) IS DISTINCT FROM (OLD.tenant_id,OLD.practitioner_id,OLD.id,OLD.created_by,OLD.created_at) OR NEW.version<>OLD.version+1 OR NOT EXISTS(SELECT 1 FROM practitioner_qualification_revisions v WHERE v.tenant_id=NEW.tenant_id AND v.qualification_id=NEW.id AND v.version=NEW.version AND v.expected_version=OLD.version AND v.created_by=NULLIF(current_setting('app.actor_subject',true),'')) THEN RAISE EXCEPTION 'qualification advance requires attributable revision' USING ERRCODE='23514'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER practitioner_qualification_guard BEFORE INSERT OR UPDATE OR DELETE ON practitioner_qualifications FOR EACH ROW EXECUTE FUNCTION practitioner_qualification_guard();
CREATE FUNCTION practitioner_qualification_revision_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE head_version bigint;
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'qualification revision is immutable' USING ERRCODE='23514'; END IF;
 IF NEW.completion_year>extract(year FROM clock_timestamp() AT TIME ZONE 'UTC') THEN RAISE EXCEPTION 'qualification year is in the future' USING ERRCODE='23514',CONSTRAINT='practitioner_qualification_year'; END IF;
 IF NEW.created_by IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'') OR current_setting('app.practitioner_manage',true) IS DISTINCT FROM 'true' THEN RAISE EXCEPTION 'qualification management authority required' USING ERRCODE='23514'; END IF;
 SELECT version INTO head_version FROM practitioner_qualifications WHERE tenant_id=NEW.tenant_id AND practitioner_id=NEW.practitioner_id AND id=NEW.qualification_id FOR UPDATE;
 IF NOT FOUND OR NOT ((NEW.version=1 AND head_version=1 AND NEW.expected_version=0 AND NEW.id=NEW.qualification_id) OR (NEW.version>1 AND head_version=NEW.expected_version)) THEN RAISE EXCEPTION 'stale qualification revision' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER practitioner_qualification_revision_guard BEFORE INSERT OR UPDATE OR DELETE ON practitioner_qualification_revisions FOR EACH ROW EXECUTE FUNCTION practitioner_qualification_revision_guard();
CREATE FUNCTION practitioner_qualification_commit_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM practitioner_qualification_revisions v WHERE v.tenant_id=NEW.tenant_id AND v.qualification_id=NEW.id AND v.version=(SELECT r.version FROM practitioner_qualifications r WHERE r.tenant_id=NEW.tenant_id AND r.id=NEW.id)) THEN RAISE EXCEPTION 'qualification requires committed revision' USING ERRCODE='23514'; END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER practitioner_qualification_committed AFTER INSERT OR UPDATE ON practitioner_qualifications DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION practitioner_qualification_commit_guard();
CREATE FUNCTION practitioner_qualification_revision_commit_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM practitioner_qualifications r WHERE r.tenant_id=NEW.tenant_id AND r.practitioner_id=NEW.practitioner_id AND r.id=NEW.qualification_id AND r.version>=NEW.version) THEN RAISE EXCEPTION 'qualification revision requires advanced head' USING ERRCODE='23514'; END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER practitioner_qualification_revision_committed AFTER INSERT ON practitioner_qualification_revisions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION practitioner_qualification_revision_commit_guard();
CREATE TRIGGER practitioner_qualification_audit AFTER INSERT OR UPDATE ON practitioner_qualifications FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER practitioner_qualification_revision_audit AFTER INSERT ON practitioner_qualification_revisions FOR EACH ROW EXECUTE FUNCTION audit_record_change();
