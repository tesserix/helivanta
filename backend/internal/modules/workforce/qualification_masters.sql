SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE workforce_enrollment_qualifications
  ADD COLUMN qualification_revision_id uuid,
  ADD COLUMN qualification_kind text NOT NULL DEFAULT 'qualification' CHECK (qualification_kind = 'qualification'),
  ADD CONSTRAINT workforce_qualification_master_reference FOREIGN KEY (tenant_id,hospital_id,qualification_revision_id,qualification_kind)
    REFERENCES workforce_hr_master_revisions(tenant_id,hospital_id,id,kind) ON DELETE RESTRICT NOT VALID;

ALTER TABLE workforce_enrollment_qualifications VALIDATE CONSTRAINT workforce_qualification_master_reference;

CREATE FUNCTION workforce_qualification_master_write_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.qualification_revision_id IS NOT NULL
     AND current_setting('app.workforce_qualification_master_references',true) IS DISTINCT FROM 'supported' THEN
    RAISE EXCEPTION 'use a qualification-revision-aware enrollment writer'
      USING ERRCODE='23514', CONSTRAINT='workforce_qualification_master_writer';
  END IF;
  IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER workforce_qualification_master_writer BEFORE UPDATE OR DELETE ON workforce_enrollment_qualifications
  FOR EACH ROW EXECUTE FUNCTION workforce_qualification_master_write_guard();
