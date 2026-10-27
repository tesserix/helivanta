SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE workforce_enrollment_profile
  ADD COLUMN preferred_language_revision_id uuid,
  ADD COLUMN exit_reason_revision_id uuid,
  ADD COLUMN contract_type text NOT NULL DEFAULT '' CHECK (length(contract_type) <= 80),
  ADD COLUMN contract_type_revision_id uuid,
  ADD COLUMN preferred_language_kind text NOT NULL DEFAULT 'language' CHECK (preferred_language_kind = 'language'),
  ADD COLUMN exit_reason_kind text NOT NULL DEFAULT 'exit_reason' CHECK (exit_reason_kind = 'exit_reason'),
  ADD COLUMN contract_type_kind text NOT NULL DEFAULT 'contract_type' CHECK (contract_type_kind = 'contract_type'),
  ADD CONSTRAINT workforce_profile_contract_type_reference_required CHECK (contract_type = '' OR contract_type_revision_id IS NOT NULL),
  ADD CONSTRAINT workforce_profile_language_reference FOREIGN KEY (tenant_id,hospital_id,preferred_language_revision_id,preferred_language_kind)
    REFERENCES workforce_hr_master_revisions(tenant_id,hospital_id,id,kind) ON DELETE RESTRICT NOT VALID,
  ADD CONSTRAINT workforce_profile_exit_reason_reference FOREIGN KEY (tenant_id,hospital_id,exit_reason_revision_id,exit_reason_kind)
    REFERENCES workforce_hr_master_revisions(tenant_id,hospital_id,id,kind) ON DELETE RESTRICT NOT VALID,
  ADD CONSTRAINT workforce_profile_contract_type_reference FOREIGN KEY (tenant_id,hospital_id,contract_type_revision_id,contract_type_kind)
    REFERENCES workforce_hr_master_revisions(tenant_id,hospital_id,id,kind) ON DELETE RESTRICT NOT VALID;

ALTER TABLE workforce_enrollment_profile VALIDATE CONSTRAINT workforce_profile_language_reference;
ALTER TABLE workforce_enrollment_profile VALIDATE CONSTRAINT workforce_profile_exit_reason_reference;
ALTER TABLE workforce_enrollment_profile VALIDATE CONSTRAINT workforce_profile_contract_type_reference;

CREATE OR REPLACE FUNCTION workforce_hr_reference_write_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF (OLD.department_revision_id IS NOT NULL OR OLD.designation_revision_id IS NOT NULL OR OLD.grade_revision_id IS NOT NULL)
     AND current_setting('app.workforce_hr_references',true) IS DISTINCT FROM 'supported' THEN
    RAISE EXCEPTION 'use a revision-aware enrollment writer' USING ERRCODE='23514';
  END IF;
  IF (OLD.preferred_language_revision_id IS NOT NULL OR OLD.exit_reason_revision_id IS NOT NULL OR OLD.contract_type_revision_id IS NOT NULL)
     AND current_setting('app.workforce_profile_master_references',true) IS DISTINCT FROM 'supported' THEN
    RAISE EXCEPTION 'use a profile-master-aware enrollment writer'
      USING ERRCODE='23514', CONSTRAINT='workforce_profile_master_writer';
  END IF;
  RETURN OLD;
END $$;
