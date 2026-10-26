SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE TABLE practitioner_registration_policies (
 tenant_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7' AND substring(id::text,20,1) IN ('8','9','a','b')),
 source_system text NOT NULL CHECK(source_system ~ '^[a-z][a-z0-9.-]{0,62}$'),
 normalization text NOT NULL CHECK(normalization IN ('exact','ascii_upper')),
 expiry_time_zone text NOT NULL CHECK(octet_length(expiry_time_zone) BETWEEN 1 AND 128),
 version bigint NOT NULL DEFAULT 1 CHECK(version BETWEEN 1 AND 9007199254740991),
 created_by text NOT NULL CHECK(octet_length(created_by) BETWEEN 1 AND 512),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,id),
 CONSTRAINT registration_policy_source_unique UNIQUE(tenant_id,source_system)
);
CREATE INDEX practitioner_registration_policy_page ON practitioner_registration_policies(tenant_id,created_at DESC,id DESC);
ALTER TABLE practitioner_registration_policies ENABLE ROW LEVEL SECURITY;
ALTER TABLE practitioner_registration_policies FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON practitioner_registration_policies
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.practitioner_access',true)='true')
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.practitioner_policy_manage',true)='true');
CREATE TABLE practitioner_registration_policy_revisions (
 tenant_id uuid NOT NULL,
 policy_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7' AND substring(id::text,20,1) IN ('8','9','a','b')),
 expected_version bigint NOT NULL CHECK(expected_version BETWEEN 0 AND 9007199254740990),
 version bigint NOT NULL CHECK(version=expected_version+1),
 max_source_age_seconds bigint NOT NULL CHECK(max_source_age_seconds BETWEEN 1 AND 31536000),
 date_rule text NOT NULL CHECK(date_rule IN ('date_required','allow_undated_with_attestation')),
 valid_until timestamptz NOT NULL CHECK(valid_until BETWEEN '0001-01-01T00:00:00Z' AND '9999-12-31T23:59:59.999999Z'),
 review_reference uuid NOT NULL CHECK(substring(review_reference::text,15,1)='7' AND substring(review_reference::text,20,1) IN ('8','9','a','b')),
 appointment_id uuid NOT NULL,
 appointment_version bigint NOT NULL CHECK(appointment_version=2),
 created_by text NOT NULL CHECK(octet_length(created_by) BETWEEN 1 AND 512),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,policy_id,version),
 FOREIGN KEY(tenant_id,policy_id) REFERENCES practitioner_registration_policies(tenant_id,id),
 FOREIGN KEY(tenant_id,appointment_id,appointment_version) REFERENCES practitioner_steward_events(tenant_id,appointment_id,version)
);
CREATE INDEX practitioner_registration_policy_revision_page ON practitioner_registration_policy_revisions(tenant_id,policy_id,created_at DESC,id DESC);
ALTER TABLE practitioner_registration_policy_revisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE practitioner_registration_policy_revisions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON practitioner_registration_policy_revisions
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.practitioner_access',true)='true')
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.practitioner_policy_manage',true)='true' AND created_by=NULLIF(current_setting('app.actor_subject',true),''));
CREATE FUNCTION practitioner_registration_policy_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'registration policy is retained' USING ERRCODE='23514'; END IF;
 IF current_setting('app.practitioner_policy_manage',true) IS DISTINCT FROM 'true' THEN RAISE EXCEPTION 'policy management authority required' USING ERRCODE='42501'; END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.version<>1 OR NEW.created_by IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'') THEN RAISE EXCEPTION 'invalid policy intake' USING ERRCODE='23514'; END IF;
  IF NEW.expiry_time_zone='Local' OR NOT EXISTS(SELECT 1 FROM pg_timezone_names WHERE name=NEW.expiry_time_zone) THEN RAISE EXCEPTION 'unknown issuer calendar' USING ERRCODE='23514',CONSTRAINT='registration_policy_input'; END IF;
  NEW.created_at:=practitioner_registry_now();NEW.updated_at:=NEW.created_at;RETURN NEW;
 END IF;
 IF (NEW.tenant_id,NEW.id,NEW.source_system,NEW.normalization,NEW.expiry_time_zone,NEW.created_by,NEW.created_at) IS DISTINCT FROM (OLD.tenant_id,OLD.id,OLD.source_system,OLD.normalization,OLD.expiry_time_zone,OLD.created_by,OLD.created_at) OR NEW.version<>OLD.version+1 THEN RAISE EXCEPTION 'policy identity is immutable' USING ERRCODE='23514'; END IF;
 IF NOT EXISTS(SELECT 1 FROM practitioner_registration_policy_revisions WHERE tenant_id=NEW.tenant_id AND policy_id=NEW.id AND expected_version=OLD.version AND version=NEW.version AND created_by=NULLIF(current_setting('app.actor_subject',true),'')) THEN RAISE EXCEPTION 'policy advance requires attributed edition' USING ERRCODE='23514'; END IF;
 NEW.updated_at:=practitioner_registry_now();RETURN NEW;
END $$;
CREATE FUNCTION practitioner_registration_policy_revision_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE head bigint; appointment practitioner_steward_appointments; at_time timestamptz;
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'policy edition is immutable' USING ERRCODE='23514'; END IF;
 IF current_setting('app.practitioner_policy_manage',true) IS DISTINCT FROM 'true' OR NEW.created_by IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'') THEN RAISE EXCEPTION 'policy management authority required' USING ERRCODE='42501'; END IF;
 SELECT version INTO head FROM practitioner_registration_policies WHERE tenant_id=NEW.tenant_id AND id=NEW.policy_id FOR UPDATE;
 IF NOT FOUND OR (NEW.expected_version=0 AND (head<>1 OR NEW.id<>NEW.policy_id)) OR (NEW.expected_version>0 AND head<>NEW.expected_version) THEN RAISE EXCEPTION 'policy edition is stale' USING ERRCODE='23514',CONSTRAINT='registration_policy_version'; END IF;
 SELECT * INTO appointment FROM practitioner_steward_appointments WHERE tenant_id=NEW.tenant_id AND id=NEW.appointment_id AND subject=NEW.created_by FOR UPDATE;
 at_time:=practitioner_registry_now();
 IF NOT FOUND OR appointment.status<>'active' OR appointment.version<>NEW.appointment_version OR appointment.valid_until<=at_time THEN RAISE EXCEPTION 'current accepted policy authority required' USING ERRCODE='23514',CONSTRAINT='registration_policy_authority'; END IF;
 IF NEW.valid_until<=at_time THEN RAISE EXCEPTION 'policy validity must be future bounded' USING ERRCODE='23514',CONSTRAINT='registration_policy_input'; END IF;
 NEW.created_at:=at_time;RETURN NEW;
END $$;
CREATE FUNCTION practitioner_registration_policy_commit_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM practitioner_registration_policy_revisions WHERE tenant_id=NEW.tenant_id AND policy_id=NEW.id AND version=NEW.version) THEN RAISE EXCEPTION 'policy head requires retained edition' USING ERRCODE='23514'; END IF;RETURN NULL;
END $$;
CREATE TRIGGER practitioner_registration_policy_guard BEFORE INSERT OR UPDATE OR DELETE ON practitioner_registration_policies FOR EACH ROW EXECUTE FUNCTION practitioner_registration_policy_guard();
CREATE CONSTRAINT TRIGGER practitioner_registration_policy_commit_guard AFTER INSERT OR UPDATE ON practitioner_registration_policies DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION practitioner_registration_policy_commit_guard();
CREATE TRIGGER practitioner_registration_policy_revision_guard BEFORE INSERT OR UPDATE OR DELETE ON practitioner_registration_policy_revisions FOR EACH ROW EXECUTE FUNCTION practitioner_registration_policy_revision_guard();
CREATE TRIGGER practitioner_registration_policy_audit AFTER INSERT OR UPDATE ON practitioner_registration_policies FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER practitioner_registration_policy_revision_audit AFTER INSERT ON practitioner_registration_policy_revisions FOR EACH ROW EXECUTE FUNCTION audit_record_change();
