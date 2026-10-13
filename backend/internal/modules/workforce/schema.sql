CREATE TABLE workforce_hospitals (
  id uuid NOT NULL,
  tenant_id uuid NOT NULL,
  name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 150),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, id)
);
ALTER TABLE workforce_hospitals ENABLE ROW LEVEL SECURITY;
ALTER TABLE workforce_hospitals FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workforce_hospitals
  USING (hms_tenant_visible(tenant_id) AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
  WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

CREATE TABLE workforce_hospital_access (
  id uuid NOT NULL DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  hospital_id uuid NOT NULL,
  subject text NOT NULL CHECK (length(btrim(subject)) BETWEEN 1 AND 200),
  level text NOT NULL CHECK (level IN ('reader', 'manager')),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, hospital_id, subject),
  FOREIGN KEY (tenant_id, hospital_id) REFERENCES workforce_hospitals(tenant_id, id) ON DELETE RESTRICT
);
ALTER TABLE workforce_hospital_access ENABLE ROW LEVEL SECURITY;
ALTER TABLE workforce_hospital_access FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workforce_hospital_access
  USING (hms_tenant_visible(tenant_id) AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
  WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

CREATE FUNCTION workforce_staff_allowed(row_tenant uuid, row_hospital uuid, writing boolean)
RETURNS boolean LANGUAGE sql STABLE AS $$
  SELECT row_tenant = NULLIF(current_setting('app.tenant_id', true), '')::uuid
     AND row_hospital = NULLIF(current_setting('app.workforce_hospital', true), '')::uuid
     AND EXISTS (
       SELECT 1 FROM workforce_hospital_access a
       WHERE a.tenant_id = row_tenant AND a.hospital_id = row_hospital
         AND a.subject = NULLIF(current_setting('app.workforce_subject', true), '')
         AND (NOT writing OR a.level = 'manager')
     )
$$;

CREATE TABLE workforce_staff (
  id uuid NOT NULL,
  tenant_id uuid NOT NULL,
  hospital_id uuid NOT NULL,
  employee_code text NOT NULL CHECK (employee_code ~ '^[A-Za-z0-9_-]{1,40}$'),
  display_name text NOT NULL CHECK (length(btrim(display_name)) BETWEEN 1 AND 150),
  job_category text NOT NULL CHECK (job_category IN ('doctor', 'nurse', 'pharmacist', 'lab_technician', 'receptionist', 'administration', 'other')),
  employment_status text NOT NULL CHECK (employment_status IN ('active', 'inactive')),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, hospital_id, id),
  UNIQUE (tenant_id, hospital_id, employee_code),
  FOREIGN KEY (tenant_id, hospital_id) REFERENCES workforce_hospitals(tenant_id, id) ON DELETE RESTRICT
);
CREATE INDEX ON workforce_staff (tenant_id, hospital_id, created_at DESC, id DESC);
ALTER TABLE workforce_staff ENABLE ROW LEVEL SECURITY;
ALTER TABLE workforce_staff FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workforce_staff
  USING (hms_tenant_visible(tenant_id) AND workforce_staff_allowed(tenant_id, hospital_id, false))
  WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
    AND workforce_staff_allowed(tenant_id, hospital_id, true));

CREATE FUNCTION workforce_prevent_removal() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'destructive workforce operation is not supported' USING ERRCODE = '23514';
END $$;
CREATE TRIGGER workforce_staff_no_delete BEFORE DELETE OR TRUNCATE ON workforce_staff
  FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal();
CREATE TRIGGER workforce_hospital_no_truncate BEFORE TRUNCATE ON workforce_hospitals
  FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal();
CREATE TRIGGER workforce_access_no_truncate BEFORE TRUNCATE ON workforce_hospital_access
  FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal();

CREATE TABLE workforce_audit (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  hospital_id uuid NOT NULL,
  actor_subject text NOT NULL CHECK (length(actor_subject) > 0),
  action text NOT NULL,
  resource_id uuid NOT NULL,
  changes jsonb NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ON workforce_audit (tenant_id, hospital_id, created_at DESC, id DESC);
ALTER TABLE workforce_audit ENABLE ROW LEVEL SECURITY;
ALTER TABLE workforce_audit FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workforce_audit
  USING (hms_tenant_visible(tenant_id) AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
  WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

CREATE FUNCTION workforce_record_audit() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
  record_data jsonb := CASE WHEN TG_OP = 'DELETE' THEN to_jsonb(OLD) ELSE to_jsonb(NEW) END;
BEGIN
  INSERT INTO workforce_audit (tenant_id, hospital_id, actor_subject, action, resource_id, changes)
  VALUES (
    (record_data->>'tenant_id')::uuid,
    COALESCE((record_data->>'hospital_id')::uuid, (record_data->>'id')::uuid),
    NULLIF(current_setting('app.workforce_subject', true), ''),
    TG_TABLE_NAME || '.' || lower(TG_OP),
    (record_data->>'id')::uuid,
    jsonb_build_object('old_level', to_jsonb(OLD)->'level', 'new_level', to_jsonb(NEW)->'level',
      'old_status', to_jsonb(OLD)->'employment_status', 'new_status', to_jsonb(NEW)->'employment_status',
      'target_subject', record_data->'subject')
  );
  RETURN NULL;
END $$;
CREATE TRIGGER workforce_hospital_audit AFTER INSERT OR UPDATE OR DELETE ON workforce_hospitals
  FOR EACH ROW EXECUTE FUNCTION workforce_record_audit();
CREATE TRIGGER workforce_access_audit AFTER INSERT OR UPDATE OR DELETE ON workforce_hospital_access
  FOR EACH ROW EXECUTE FUNCTION workforce_record_audit();
CREATE TRIGGER workforce_staff_audit AFTER INSERT OR UPDATE OR DELETE ON workforce_staff
  FOR EACH ROW EXECUTE FUNCTION workforce_record_audit();

CREATE FUNCTION workforce_audit_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'workforce audit is append only' USING ERRCODE = '23514';
END $$;
CREATE TRIGGER workforce_audit_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON workforce_audit
  FOR EACH STATEMENT EXECUTE FUNCTION workforce_audit_immutable();
