SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE patient_reported_blood_groups (
  tenant_id uuid NOT NULL,
  patient_id uuid NOT NULL,
  abo text NOT NULL CHECK (abo IN ('A', 'B', 'AB', 'O', 'unknown')),
  rh text NOT NULL CHECK (rh IN ('positive', 'negative', 'unknown')),
  source text NOT NULL CHECK (source IN ('patient', 'guardian', 'document')),
  recorded_by_subject text NOT NULL CHECK (length(btrim(recorded_by_subject)) BETWEEN 1 AND 200),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, patient_id),
  FOREIGN KEY (tenant_id, patient_id) REFERENCES patients (tenant_id, id) ON DELETE RESTRICT
);
ALTER TABLE patient_reported_blood_groups ENABLE ROW LEVEL SECURITY;
ALTER TABLE patient_reported_blood_groups FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON patient_reported_blood_groups
  USING (hms_tenant_visible(tenant_id) AND EXISTS (
    SELECT 1 FROM patients p
    WHERE p.tenant_id = patient_reported_blood_groups.tenant_id
      AND p.id = patient_reported_blood_groups.patient_id
  ))
  WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid AND EXISTS (
    SELECT 1 FROM patients p
    WHERE p.tenant_id = patient_reported_blood_groups.tenant_id
      AND p.id = patient_reported_blood_groups.patient_id
      AND patient_hospital_allowed(p.tenant_id, p.hospital_id, true)
  ));

CREATE FUNCTION patient_reported_blood_group_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  RAISE EXCEPTION 'reported blood group is append only' USING ERRCODE = '23514';
END $$;
CREATE TRIGGER patient_reported_blood_group_immutable
  BEFORE UPDATE OR DELETE OR TRUNCATE ON patient_reported_blood_groups
  FOR EACH STATEMENT EXECUTE FUNCTION patient_reported_blood_group_immutable();
