SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE TABLE practitioner_records (
 tenant_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7' AND substring(id::text,20,1) IN ('8','9','a','b')),
 given_name text NOT NULL CHECK(octet_length(given_name) BETWEEN 1 AND 150 AND length(btrim(given_name))>0),
 family_name text CHECK(family_name IS NULL OR (octet_length(family_name) BETWEEN 1 AND 150 AND length(btrim(family_name))>0)),
 professional_type text NOT NULL CHECK(professional_type IN ('physician','surgeon','dentist','nurse','pharmacist','pathologist','radiologist','lab_technician','physiotherapist','paramedic','biomedical_technician')),
 verification_state text NOT NULL DEFAULT 'unverified' CHECK(verification_state='unverified'),
 created_by text NOT NULL CHECK(length(created_by) BETWEEN 1 AND 512),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,id)
);
ALTER TABLE practitioner_records ENABLE ROW LEVEL SECURITY;
ALTER TABLE practitioner_records FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON practitioner_records
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.practitioner_access',true)='true')
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.practitioner_manage',true)='true' AND created_by=NULLIF(current_setting('app.actor_subject',true),''));
CREATE FUNCTION practitioner_record_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'practitioner identity intent is immutable' USING ERRCODE='23514'; END IF;
 IF NEW.created_by IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'') OR current_setting('app.practitioner_manage',true) IS DISTINCT FROM 'true' THEN RAISE EXCEPTION 'practitioner intake authority required' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER practitioner_record_guard BEFORE INSERT OR UPDATE OR DELETE ON practitioner_records FOR EACH ROW EXECUTE FUNCTION practitioner_record_guard();
CREATE TRIGGER practitioner_record_audit AFTER INSERT ON practitioner_records FOR EACH ROW EXECUTE FUNCTION audit_record_change();
