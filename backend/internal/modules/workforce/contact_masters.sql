SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE workforce_enrollment_emergency_contacts
  ADD COLUMN relationship_revision_id uuid,
  ADD COLUMN relationship_kind text NOT NULL DEFAULT 'relationship' CHECK (relationship_kind = 'relationship'),
  ADD CONSTRAINT workforce_contact_master_reference FOREIGN KEY (tenant_id,hospital_id,relationship_revision_id,relationship_kind)
    REFERENCES workforce_hr_master_revisions(tenant_id,hospital_id,id,kind) ON DELETE RESTRICT NOT VALID;

ALTER TABLE workforce_enrollment_emergency_contacts VALIDATE CONSTRAINT workforce_contact_master_reference;

CREATE FUNCTION workforce_contact_master_write_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF OLD.relationship_revision_id IS NOT NULL
     AND current_setting('app.workforce_contact_master_references',true) IS DISTINCT FROM 'supported' THEN
    RAISE EXCEPTION 'use a contact-revision-aware enrollment writer'
      USING ERRCODE='23514', CONSTRAINT='workforce_contact_master_writer';
  END IF;
  IF TG_OP = 'DELETE' THEN RETURN OLD; END IF;
  RETURN NEW;
END;
$$;

CREATE TRIGGER workforce_contact_master_writer BEFORE UPDATE OR DELETE ON workforce_enrollment_emergency_contacts
  FOR EACH ROW EXECUTE FUNCTION workforce_contact_master_write_guard();
