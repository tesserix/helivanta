SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE TABLE practitioner_specialty_retirements (
 tenant_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7' AND substring(id::text,20,1) IN ('8','9','a','b')),
 specialty_id uuid NOT NULL,
 review_reference uuid NOT NULL CHECK(substring(review_reference::text,15,1)='7' AND substring(review_reference::text,20,1) IN ('8','9','a','b')),
 replacement_id uuid CHECK(replacement_id<>specialty_id),
 created_by text NOT NULL CHECK(octet_length(created_by) BETWEEN 1 AND 512),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,specialty_id),
 FOREIGN KEY(tenant_id,specialty_id) REFERENCES practitioner_specialties(tenant_id,id),
 FOREIGN KEY(tenant_id,replacement_id) REFERENCES practitioner_specialties(tenant_id,id)
);
ALTER TABLE practitioner_specialty_retirements ENABLE ROW LEVEL SECURITY;
ALTER TABLE practitioner_specialty_retirements FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON practitioner_specialty_retirements
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.practitioner_access',true)='true')
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.practitioner_specialty_manage',true)='true' AND created_by=NULLIF(current_setting('app.actor_subject',true),''));
CREATE FUNCTION practitioner_specialty_retirement_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'specialty retirement receipt is immutable' USING ERRCODE='23514'; END IF;
 IF current_setting('app.practitioner_specialty_manage',true) IS DISTINCT FROM 'true' OR NEW.created_by IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'') THEN RAISE EXCEPTION 'specialty retirement authority required' USING ERRCODE='42501'; END IF;
 PERFORM id FROM practitioner_specialties WHERE tenant_id=NEW.tenant_id AND (id=NEW.specialty_id OR id=NEW.replacement_id) ORDER BY id FOR UPDATE;
 IF NOT EXISTS(SELECT 1 FROM practitioner_specialties WHERE tenant_id=NEW.tenant_id AND id=NEW.specialty_id) OR EXISTS(SELECT 1 FROM practitioner_specialty_retirements WHERE tenant_id=NEW.tenant_id AND specialty_id=NEW.specialty_id) OR (NEW.replacement_id IS NOT NULL AND (NEW.replacement_id=NEW.specialty_id OR NOT EXISTS(SELECT 1 FROM practitioner_specialties WHERE tenant_id=NEW.tenant_id AND id=NEW.replacement_id) OR EXISTS(SELECT 1 FROM practitioner_specialty_retirements WHERE tenant_id=NEW.tenant_id AND specialty_id=NEW.replacement_id))) THEN RAISE EXCEPTION 'specialty or replacement is unavailable' USING ERRCODE='23514',CONSTRAINT='practitioner_specialty_retirement'; END IF;
 NEW.created_at:=clock_timestamp(); RETURN NEW;
END $$;
CREATE TRIGGER practitioner_specialty_retirement_guard BEFORE INSERT OR UPDATE OR DELETE ON practitioner_specialty_retirements FOR EACH ROW EXECUTE FUNCTION practitioner_specialty_retirement_guard();
CREATE TRIGGER practitioner_specialty_retirement_audit AFTER INSERT ON practitioner_specialty_retirements FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE FUNCTION practitioner_qualification_specialty_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.specialty_id IS NULL THEN RETURN NEW; END IF;
 PERFORM id FROM practitioner_specialties WHERE tenant_id=NEW.tenant_id AND id=NEW.specialty_id FOR SHARE;
 IF NOT FOUND OR EXISTS(SELECT 1 FROM practitioner_specialty_retirements WHERE tenant_id=NEW.tenant_id AND specialty_id=NEW.specialty_id) THEN RAISE EXCEPTION 'qualification specialty is retired or unavailable' USING ERRCODE='23514',CONSTRAINT='practitioner_specialty_retirement'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER practitioner_qualification_specialty_guard BEFORE INSERT ON practitioner_qualification_revisions FOR EACH ROW EXECUTE FUNCTION practitioner_qualification_specialty_guard();
