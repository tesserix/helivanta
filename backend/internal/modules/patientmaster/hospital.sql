SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE patients ADD COLUMN hospital_id uuid;
-- Existing rows stay quarantined; all new writes require verified scope.
ALTER TABLE patients ADD CONSTRAINT patients_hospital_required CHECK (hospital_id IS NOT NULL) NOT VALID;
CREATE INDEX patients_hospital_page ON patients (tenant_id, hospital_id, created_at DESC, id DESC) WHERE hospital_id IS NOT NULL;
CREATE INDEX patients_hospital_name ON patients (tenant_id, hospital_id, normalized_name text_pattern_ops) WHERE hospital_id IS NOT NULL;
CREATE INDEX patients_hospital_phone ON patients (tenant_id, hospital_id, mobile) WHERE hospital_id IS NOT NULL;
CREATE INDEX patients_hospital_match ON patients (tenant_id, hospital_id, phonetic_key) WHERE hospital_id IS NOT NULL;

CREATE TABLE patient_hospital_access (
  id uuid NOT NULL DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  hospital_id uuid NOT NULL CHECK (hospital_id <> '00000000-0000-0000-0000-000000000000'::uuid),
  subject text NOT NULL CHECK (length(btrim(subject)) BETWEEN 1 AND 200),
  level text NOT NULL CHECK (level IN ('reader', 'registrar')),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id, hospital_id, subject)
);
ALTER TABLE patient_hospital_access ENABLE ROW LEVEL SECURITY;
ALTER TABLE patient_hospital_access FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON patient_hospital_access
  USING (hms_tenant_visible(tenant_id) AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
  WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

CREATE FUNCTION patient_hospital_allowed(row_tenant uuid, row_hospital uuid, writing boolean)
RETURNS boolean LANGUAGE sql STABLE AS $$
  SELECT row_tenant = NULLIF(current_setting('app.tenant_id', true), '')::uuid
    AND row_hospital = NULLIF(current_setting('app.patient_hospital', true), '')::uuid
    AND EXISTS (SELECT 1 FROM patient_hospital_access a
      WHERE a.tenant_id = row_tenant AND a.hospital_id = row_hospital
        AND a.subject = NULLIF(current_setting('app.patient_subject', true), '')
        AND (NOT writing OR a.level = 'registrar'))
$$;
ALTER POLICY tenant_isolation ON patients
  USING (hms_tenant_visible(tenant_id) AND patient_hospital_allowed(tenant_id, hospital_id, false))
  WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid AND patient_hospital_allowed(tenant_id, hospital_id, true));

ALTER POLICY tenant_isolation ON patient_consents
  USING (hms_tenant_visible(tenant_id) AND EXISTS (SELECT 1 FROM patients p WHERE p.tenant_id = patient_consents.tenant_id AND p.id = patient_consents.patient_id))
  WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid AND EXISTS (SELECT 1 FROM patients p WHERE p.tenant_id = patient_consents.tenant_id AND p.id = patient_consents.patient_id AND patient_hospital_allowed(p.tenant_id, p.hospital_id, true)));
ALTER POLICY tenant_isolation ON patient_identifiers
  USING (hms_tenant_visible(tenant_id) AND EXISTS (SELECT 1 FROM patients p WHERE p.tenant_id = patient_identifiers.tenant_id AND p.id = patient_identifiers.patient_id))
  WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid AND EXISTS (SELECT 1 FROM patients p WHERE p.tenant_id = patient_identifiers.tenant_id AND p.id = patient_identifiers.patient_id AND patient_hospital_allowed(p.tenant_id, p.hospital_id, true)));
ALTER POLICY tenant_isolation ON patient_duplicate_overrides
  USING (hms_tenant_visible(tenant_id) AND EXISTS (SELECT 1 FROM patients p WHERE p.tenant_id = patient_duplicate_overrides.tenant_id AND p.id = created_patient_id)
     AND EXISTS (SELECT 1 FROM patients p WHERE p.tenant_id = patient_duplicate_overrides.tenant_id AND p.id = matched_patient_id))
  WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid AND EXISTS (SELECT 1 FROM patients p WHERE p.tenant_id = patient_duplicate_overrides.tenant_id AND p.id = created_patient_id AND patient_hospital_allowed(p.tenant_id, p.hospital_id, true))
     AND EXISTS (SELECT 1 FROM patients p WHERE p.tenant_id = patient_duplicate_overrides.tenant_id AND p.id = matched_patient_id));

CREATE TABLE patient_access_audit (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id uuid NOT NULL,
  hospital_id uuid NOT NULL,
  actor_subject text NOT NULL CHECK (length(btrim(actor_subject)) BETWEEN 1 AND 200),
  action text NOT NULL CHECK (action IN ('read', 'list', 'match', 'register', 'grant', 'revoke')),
  resource_ids jsonb NOT NULL DEFAULT '[]'::jsonb CHECK (jsonb_typeof(resource_ids) = 'array'),
  target_subject text NOT NULL DEFAULT '',
  access_level text NOT NULL DEFAULT '',
  created_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE patient_access_audit ENABLE ROW LEVEL SECURITY;
ALTER TABLE patient_access_audit FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON patient_access_audit
  USING (hms_tenant_visible(tenant_id) AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
  WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);
CREATE INDEX ON patient_access_audit (tenant_id, hospital_id, created_at DESC, id DESC);
CREATE FUNCTION patient_audit_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION 'patient access audit is append only' USING ERRCODE = '23514';
END $$;
CREATE TRIGGER patient_audit_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON patient_access_audit
 FOR EACH STATEMENT EXECUTE FUNCTION patient_audit_immutable();

CREATE TABLE patient_registration_requests (
 id uuid NOT NULL,
 tenant_id uuid NOT NULL,
 hospital_id uuid NOT NULL,
 fingerprint text NOT NULL,
 response jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id, hospital_id, id)
);
ALTER TABLE patient_registration_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE patient_registration_requests FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON patient_registration_requests
 USING (hms_tenant_visible(tenant_id) AND patient_hospital_allowed(tenant_id, hospital_id, false))
 WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid AND patient_hospital_allowed(tenant_id, hospital_id, true));
