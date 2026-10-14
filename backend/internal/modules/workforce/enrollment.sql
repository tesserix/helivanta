SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';
ALTER TABLE workforce_staff ADD COLUMN version bigint NOT NULL DEFAULT 1 CHECK (version > 0);
CREATE FUNCTION workforce_staff_version() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW IS DISTINCT FROM OLD THEN NEW.version := OLD.version + 1; END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER workforce_staff_version BEFORE UPDATE ON workforce_staff
  FOR EACH ROW EXECUTE FUNCTION workforce_staff_version();
CREATE FUNCTION workforce_enrollment_allowed(row_tenant uuid, row_hospital uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
  SELECT COALESCE(current_setting('app.workforce_enrollment', true) = 'authorized', false)
    AND workforce_staff_allowed(row_tenant, row_hospital, true)
$$;

CREATE TABLE workforce_enrollment_profile (
  tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, staff_id uuid NOT NULL,
  "given_name" text NOT NULL DEFAULT '' CHECK (length("given_name") <= 100),
  "family_name" text NOT NULL DEFAULT '' CHECK (length("family_name") <= 100),
  "preferred_name" text NOT NULL DEFAULT '' CHECK (length("preferred_name") <= 100),
  "date_of_birth" date,
  "gender" text NOT NULL DEFAULT '' CHECK (length("gender") <= 40),
  "work_email" text NOT NULL DEFAULT '' CHECK (length("work_email") <= 254),
  "personal_email" text NOT NULL DEFAULT '' CHECK (length("personal_email") <= 254),
  "mobile_phone" text NOT NULL DEFAULT '' CHECK (length("mobile_phone") <= 25),
  "work_phone" text NOT NULL DEFAULT '' CHECK (length("work_phone") <= 25),
  "preferred_language" text NOT NULL DEFAULT '' CHECK (length("preferred_language") <= 80),
  "blood_group" text NOT NULL DEFAULT '' CHECK (length("blood_group") <= 10),
  "engagement_type" text NOT NULL DEFAULT 'employee' CHECK (length("engagement_type") <= 30),
  "department" text NOT NULL DEFAULT '' CHECK (length("department") <= 100),
  "designation" text NOT NULL DEFAULT '' CHECK (length("designation") <= 100),
  "grade" text NOT NULL DEFAULT '' CHECK (length("grade") <= 80),
  "supervisor_id" uuid,
  "start_date" date,
  "end_date" date,
  "probation_end_date" date,
  "confirmation_date" date,
  "exit_reason" text NOT NULL DEFAULT '' CHECK (length("exit_reason") <= 500),
  CHECK (gender IN ('', 'female', 'male', 'other', 'prefer_not_to_say')),
  CHECK (blood_group IN ('', 'A+', 'A-', 'B+', 'B-', 'AB+', 'AB-', 'O+', 'O-')),
  CHECK (engagement_type IN ('employee', 'consultant', 'contractor', 'locum', 'trainee', 'volunteer')),
  CHECK (date_of_birth IS NULL OR date_of_birth <= CURRENT_DATE),
  CHECK (end_date IS NULL OR (start_date IS NOT NULL AND end_date >= start_date)),
  CHECK (probation_end_date IS NULL OR (start_date IS NOT NULL AND probation_end_date >= start_date)),
  CHECK (confirmation_date IS NULL OR (start_date IS NOT NULL AND confirmation_date >= start_date)),
  CHECK (supervisor_id IS NULL OR supervisor_id <> staff_id),
  PRIMARY KEY (tenant_id, hospital_id, staff_id),
  FOREIGN KEY (tenant_id, hospital_id, staff_id) REFERENCES workforce_staff(tenant_id, hospital_id, id) ON DELETE RESTRICT,
  FOREIGN KEY (tenant_id, hospital_id, supervisor_id) REFERENCES workforce_staff(tenant_id, hospital_id, id) ON DELETE RESTRICT
);
ALTER TABLE workforce_enrollment_profile ENABLE ROW LEVEL SECURITY;
ALTER TABLE workforce_enrollment_profile FORCE ROW LEVEL SECURITY;
CREATE POLICY enrollment_isolation ON workforce_enrollment_profile
  USING (hms_tenant_visible(tenant_id) AND workforce_enrollment_allowed(tenant_id, hospital_id))
  WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
    AND workforce_enrollment_allowed(tenant_id, hospital_id));
CREATE TRIGGER workforce_enrollment_profile_no_truncate BEFORE TRUNCATE ON workforce_enrollment_profile
  FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal();

CREATE TABLE workforce_enrollment_addresses (
  tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, staff_id uuid NOT NULL,
  id uuid NOT NULL CHECK (id <> '00000000-0000-0000-0000-000000000000'::uuid),
  "use" text NOT NULL DEFAULT '' CHECK (length("use") <= 20),
  "line1" text NOT NULL DEFAULT '' CHECK (length("line1") <= 150),
  "line2" text NOT NULL DEFAULT '' CHECK (length("line2") <= 150),
  "city" text NOT NULL DEFAULT '' CHECK (length("city") <= 100),
  "region" text NOT NULL DEFAULT '' CHECK (length("region") <= 100),
  "postal_code" text NOT NULL DEFAULT '' CHECK (length("postal_code") <= 30),
  "country" text NOT NULL DEFAULT '' CHECK (length("country") <= 2),
  CHECK ("use" IN ('current','permanent')),
  CHECK (length(btrim(line1)) > 0),
  CHECK (country ~ '^[A-Z]{2}$'),
  PRIMARY KEY (tenant_id, hospital_id, staff_id, id),
  FOREIGN KEY (tenant_id, hospital_id, staff_id) REFERENCES workforce_staff(tenant_id, hospital_id, id) ON DELETE RESTRICT,
  UNIQUE (tenant_id, hospital_id, staff_id, "use")
);
ALTER TABLE workforce_enrollment_addresses ENABLE ROW LEVEL SECURITY;
ALTER TABLE workforce_enrollment_addresses FORCE ROW LEVEL SECURITY;
CREATE POLICY enrollment_isolation ON workforce_enrollment_addresses
  USING (hms_tenant_visible(tenant_id) AND workforce_enrollment_allowed(tenant_id, hospital_id))
  WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
    AND workforce_enrollment_allowed(tenant_id, hospital_id));
CREATE TRIGGER workforce_enrollment_addresses_no_truncate BEFORE TRUNCATE ON workforce_enrollment_addresses
  FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal();

CREATE TABLE workforce_enrollment_emergency_contacts (
  tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, staff_id uuid NOT NULL,
  id uuid NOT NULL CHECK (id <> '00000000-0000-0000-0000-000000000000'::uuid),
  "name" text NOT NULL DEFAULT '' CHECK (length("name") <= 150),
  "relationship" text NOT NULL DEFAULT '' CHECK (length("relationship") <= 60),
  "phone" text NOT NULL DEFAULT '' CHECK (length("phone") <= 25),
  "email" text NOT NULL DEFAULT '' CHECK (length("email") <= 254),
  "priority" integer NOT NULL DEFAULT 0,
  CHECK (length(btrim(name)) > 0),
  CHECK (length(btrim(relationship)) > 0),
  CHECK (phone ~ '^\+[1-9][0-9]{6,14}$'),
  CHECK (priority BETWEEN 1 AND 5),
  PRIMARY KEY (tenant_id, hospital_id, staff_id, id),
  FOREIGN KEY (tenant_id, hospital_id, staff_id) REFERENCES workforce_staff(tenant_id, hospital_id, id) ON DELETE RESTRICT,
  UNIQUE (tenant_id, hospital_id, staff_id, priority)
);
ALTER TABLE workforce_enrollment_emergency_contacts ENABLE ROW LEVEL SECURITY;
ALTER TABLE workforce_enrollment_emergency_contacts FORCE ROW LEVEL SECURITY;
CREATE POLICY enrollment_isolation ON workforce_enrollment_emergency_contacts
  USING (hms_tenant_visible(tenant_id) AND workforce_enrollment_allowed(tenant_id, hospital_id))
  WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
    AND workforce_enrollment_allowed(tenant_id, hospital_id));
CREATE TRIGGER workforce_enrollment_emergency_contacts_no_truncate BEFORE TRUNCATE ON workforce_enrollment_emergency_contacts
  FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal();

CREATE TABLE workforce_enrollment_credentials (
  tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, staff_id uuid NOT NULL,
  id uuid NOT NULL CHECK (id <> '00000000-0000-0000-0000-000000000000'::uuid),
  "type" text NOT NULL DEFAULT '' CHECK (length("type") <= 80),
  "number" text NOT NULL DEFAULT '' CHECK (length("number") <= 100),
  "issuer" text NOT NULL DEFAULT '' CHECK (length("issuer") <= 150),
  "country" text NOT NULL DEFAULT '' CHECK (length("country") <= 2),
  "specialty" text NOT NULL DEFAULT '' CHECK (length("specialty") <= 100),
  "valid_from" date,
  "valid_until" date,
  CHECK (length(btrim(type)) > 0),
  CHECK (length(btrim(number)) > 0),
  CHECK (length(btrim(issuer)) > 0),
  CHECK (country = '' OR country ~ '^[A-Z]{2}$'),
  CHECK (valid_until IS NULL OR valid_from IS NULL OR valid_until >= valid_from),
  PRIMARY KEY (tenant_id, hospital_id, staff_id, id),
  FOREIGN KEY (tenant_id, hospital_id, staff_id) REFERENCES workforce_staff(tenant_id, hospital_id, id) ON DELETE RESTRICT,
  UNIQUE (tenant_id, hospital_id, staff_id, type, number, issuer)
);
ALTER TABLE workforce_enrollment_credentials ENABLE ROW LEVEL SECURITY;
ALTER TABLE workforce_enrollment_credentials FORCE ROW LEVEL SECURITY;
CREATE POLICY enrollment_isolation ON workforce_enrollment_credentials
  USING (hms_tenant_visible(tenant_id) AND workforce_enrollment_allowed(tenant_id, hospital_id))
  WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
    AND workforce_enrollment_allowed(tenant_id, hospital_id));
CREATE TRIGGER workforce_enrollment_credentials_no_truncate BEFORE TRUNCATE ON workforce_enrollment_credentials
  FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal();

CREATE TABLE workforce_enrollment_qualifications (
  tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, staff_id uuid NOT NULL,
  id uuid NOT NULL CHECK (id <> '00000000-0000-0000-0000-000000000000'::uuid),
  "qualification" text NOT NULL DEFAULT '' CHECK (length("qualification") <= 150),
  "institution" text NOT NULL DEFAULT '' CHECK (length("institution") <= 150),
  "country" text NOT NULL DEFAULT '' CHECK (length("country") <= 2),
  "completion_year" integer NOT NULL DEFAULT 0,
  CHECK (length(btrim(qualification)) > 0),
  CHECK (length(btrim(institution)) > 0),
  CHECK (country = '' OR country ~ '^[A-Z]{2}$'),
  CHECK (completion_year = 0 OR completion_year BETWEEN 1900 AND 2200),
  PRIMARY KEY (tenant_id, hospital_id, staff_id, id),
  FOREIGN KEY (tenant_id, hospital_id, staff_id) REFERENCES workforce_staff(tenant_id, hospital_id, id) ON DELETE RESTRICT
);
ALTER TABLE workforce_enrollment_qualifications ENABLE ROW LEVEL SECURITY;
ALTER TABLE workforce_enrollment_qualifications FORCE ROW LEVEL SECURITY;
CREATE POLICY enrollment_isolation ON workforce_enrollment_qualifications
  USING (hms_tenant_visible(tenant_id) AND workforce_enrollment_allowed(tenant_id, hospital_id))
  WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
    AND workforce_enrollment_allowed(tenant_id, hospital_id));
CREATE TRIGGER workforce_enrollment_qualifications_no_truncate BEFORE TRUNCATE ON workforce_enrollment_qualifications
  FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal();

CREATE TABLE workforce_enrollment_experiences (
  tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, staff_id uuid NOT NULL,
  id uuid NOT NULL CHECK (id <> '00000000-0000-0000-0000-000000000000'::uuid),
  "employer" text NOT NULL DEFAULT '' CHECK (length("employer") <= 150),
  "role" text NOT NULL DEFAULT '' CHECK (length("role") <= 100),
  "start_date" date,
  "end_date" date,
  CHECK (length(btrim(employer)) > 0),
  CHECK (length(btrim(role)) > 0),
  CHECK (start_date IS NOT NULL),
  CHECK (end_date IS NULL OR end_date >= start_date),
  PRIMARY KEY (tenant_id, hospital_id, staff_id, id),
  FOREIGN KEY (tenant_id, hospital_id, staff_id) REFERENCES workforce_staff(tenant_id, hospital_id, id) ON DELETE RESTRICT
);
ALTER TABLE workforce_enrollment_experiences ENABLE ROW LEVEL SECURITY;
ALTER TABLE workforce_enrollment_experiences FORCE ROW LEVEL SECURITY;
CREATE POLICY enrollment_isolation ON workforce_enrollment_experiences
  USING (hms_tenant_visible(tenant_id) AND workforce_enrollment_allowed(tenant_id, hospital_id))
  WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
    AND workforce_enrollment_allowed(tenant_id, hospital_id));
CREATE TRIGGER workforce_enrollment_experiences_no_truncate BEFORE TRUNCATE ON workforce_enrollment_experiences
  FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal();
