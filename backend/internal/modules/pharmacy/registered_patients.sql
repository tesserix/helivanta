SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- Patients registered at the tenant's hospitals, as patientmaster announced
-- them (#1416). Ids only. Every hospital is kept: authority is checked when
-- an id is used on a sale (a verified affiliation, B-2c), never on arrival.
CREATE TABLE pharmacy_registered_patients (
 tenant_id uuid NOT NULL,
 patient_id uuid NOT NULL,
 hospital_id uuid NOT NULL CHECK (hospital_id <> '00000000-0000-0000-0000-000000000000'::uuid),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY (tenant_id,patient_id),
 UNIQUE (tenant_id,patient_id,hospital_id)
);
ALTER TABLE pharmacy_registered_patients ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_registered_patients FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_registered_patients USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid) WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid);
-- A registration's hospital never changes; a different one under the same id is refused.
CREATE FUNCTION pharmacy_registered_patients_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION 'pharmacy registered patients are only added' USING ERRCODE='23514';
END $$;
CREATE TRIGGER pharmacy_registered_patients_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_registered_patients FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_registered_patients_immutable();
