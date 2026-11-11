SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- Every hospital name workforce announced for the tenant (#1637), newest
-- version only. Kept for all hospitals; disclosed only through a verified
-- affiliation (fillHospitalNames), never listed on its own.
CREATE TABLE pharmacy_hospital_names (
 tenant_id uuid NOT NULL,
 hospital_id uuid NOT NULL,
 name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 150),
 name_version bigint NOT NULL CHECK (name_version >= 1),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY (tenant_id,hospital_id)
);
ALTER TABLE pharmacy_hospital_names ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_hospital_names FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_hospital_names USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid) WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid);
