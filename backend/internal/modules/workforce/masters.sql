SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE workforce_hr_masters (
  tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL,
  kind text NOT NULL CHECK (kind IN ('department','designation','grade','qualification','document_category','exit_reason','relationship','language','contract_type')),
  code text NOT NULL CHECK (code ~ '^[A-Z0-9_-]{1,40}$'),
  name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 80),
  description text NOT NULL DEFAULT '' CHECK (length(description) <= 500),
  active boolean NOT NULL,
  version bigint NOT NULL CHECK (version > 0),
  revision_id uuid NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id,hospital_id,id),
  UNIQUE (tenant_id,hospital_id,kind,code),
  UNIQUE (tenant_id,hospital_id,id,kind,code),
  FOREIGN KEY (tenant_id,hospital_id) REFERENCES workforce_hospitals(tenant_id,id) ON DELETE RESTRICT
);
CREATE UNIQUE INDEX workforce_hr_master_name_unique ON workforce_hr_masters (tenant_id,hospital_id,kind,lower(name));
CREATE INDEX workforce_hr_masters_page ON workforce_hr_masters (tenant_id,hospital_id,kind,created_at DESC,id DESC);

CREATE TABLE workforce_hr_master_revisions (
  tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL,
  master_id uuid NOT NULL, version bigint NOT NULL CHECK (version > 0),
  kind text NOT NULL, code text NOT NULL, name text NOT NULL, description text NOT NULL, active boolean NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (tenant_id,hospital_id,id),
  UNIQUE (tenant_id,hospital_id,master_id,version,id),
  UNIQUE (tenant_id,hospital_id,master_id,version),
  UNIQUE (tenant_id,hospital_id,id,kind),
  FOREIGN KEY (tenant_id,hospital_id,master_id,kind,code) REFERENCES workforce_hr_masters(tenant_id,hospital_id,id,kind,code) ON DELETE RESTRICT
);
CREATE INDEX workforce_hr_master_history_page ON workforce_hr_master_revisions (tenant_id,hospital_id,master_id,created_at DESC,id DESC);
ALTER TABLE workforce_hr_masters ADD CONSTRAINT workforce_hr_master_current_revision
  FOREIGN KEY (tenant_id,hospital_id,id,version,revision_id)
  REFERENCES workforce_hr_master_revisions(tenant_id,hospital_id,master_id,version,id)
  DEFERRABLE INITIALLY DEFERRED;

ALTER TABLE workforce_hr_masters ENABLE ROW LEVEL SECURITY;
ALTER TABLE workforce_hr_masters FORCE ROW LEVEL SECURITY;
CREATE POLICY hospital_isolation ON workforce_hr_masters
  USING (hms_tenant_visible(tenant_id) AND workforce_staff_allowed(tenant_id,hospital_id,false))
  WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id',true),'')::uuid
    AND workforce_staff_allowed(tenant_id,hospital_id,true)
    AND current_setting('app.workforce_master',true) = 'authorized');
ALTER TABLE workforce_hr_master_revisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE workforce_hr_master_revisions FORCE ROW LEVEL SECURITY;
CREATE POLICY hospital_isolation ON workforce_hr_master_revisions
  USING (hms_tenant_visible(tenant_id) AND workforce_staff_allowed(tenant_id,hospital_id,false))
  WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id',true),'')::uuid
    AND workforce_staff_allowed(tenant_id,hospital_id,true)
    AND current_setting('app.workforce_master',true) = 'authorized');
CREATE TRIGGER workforce_hr_revision_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON workforce_hr_master_revisions
  FOR EACH STATEMENT EXECUTE FUNCTION workforce_audit_immutable();
CREATE TRIGGER workforce_hr_master_no_delete BEFORE DELETE OR TRUNCATE ON workforce_hr_masters
  FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal();
CREATE FUNCTION workforce_hr_master_stable_identity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF (NEW.tenant_id,NEW.hospital_id,NEW.id,NEW.kind,NEW.code) IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.id,OLD.kind,OLD.code)
    OR NEW.version <> OLD.version + 1 THEN
    RAISE EXCEPTION 'master identity is immutable and versions must advance' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER workforce_hr_master_stable_identity BEFORE UPDATE ON workforce_hr_masters
  FOR EACH ROW EXECUTE FUNCTION workforce_hr_master_stable_identity();

ALTER TABLE workforce_enrollment_profile
  ADD COLUMN department_revision_id uuid,
  ADD COLUMN designation_revision_id uuid,
  ADD COLUMN grade_revision_id uuid,
  ADD COLUMN department_kind text GENERATED ALWAYS AS ('department'::text) STORED,
  ADD COLUMN designation_kind text GENERATED ALWAYS AS ('designation'::text) STORED,
  ADD COLUMN grade_kind text GENERATED ALWAYS AS ('grade'::text) STORED,
  ADD FOREIGN KEY (tenant_id,hospital_id,department_revision_id,department_kind) REFERENCES workforce_hr_master_revisions(tenant_id,hospital_id,id,kind) ON DELETE RESTRICT,
  ADD FOREIGN KEY (tenant_id,hospital_id,designation_revision_id,designation_kind) REFERENCES workforce_hr_master_revisions(tenant_id,hospital_id,id,kind) ON DELETE RESTRICT,
  ADD FOREIGN KEY (tenant_id,hospital_id,grade_revision_id,grade_kind) REFERENCES workforce_hr_master_revisions(tenant_id,hospital_id,id,kind) ON DELETE RESTRICT;

CREATE FUNCTION workforce_hr_reference_write_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF (OLD.department_revision_id IS NOT NULL OR OLD.designation_revision_id IS NOT NULL OR OLD.grade_revision_id IS NOT NULL)
    AND current_setting('app.workforce_hr_references',true) IS DISTINCT FROM 'supported' THEN
    RAISE EXCEPTION 'use a revision-aware enrollment writer' USING ERRCODE='23514';
  END IF;
  RETURN OLD;
END $$;
CREATE TRIGGER workforce_hr_reference_write_guard BEFORE DELETE ON workforce_enrollment_profile
  FOR EACH ROW EXECUTE FUNCTION workforce_hr_reference_write_guard();
