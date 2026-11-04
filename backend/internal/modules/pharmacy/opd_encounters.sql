SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Pharmacy's copy of MediCore's OPD encounters (#987 slice 2), fed by
-- encounter_opened.v1 and encounter_status_changed.v1 as published. Either
-- may arrive first: an opening fills the patient, a status change the status
-- and its version, and only a newer version moves the status. No name.
CREATE TABLE pharmacy_opd_encounters (
 tenant_id uuid NOT NULL,
 encounter_id uuid NOT NULL,
 hospital_id uuid NOT NULL,
 patient_id uuid,
 status text NOT NULL CHECK (status IN ('arrived','in_consultation','closed','left_without_being_seen','cancelled')),
 version integer NOT NULL CHECK (version >= 1),
 PRIMARY KEY (tenant_id,encounter_id)
);
CREATE INDEX pharmacy_opd_encounter_patient ON pharmacy_opd_encounters (tenant_id,patient_id,hospital_id) WHERE patient_id IS NOT NULL;
ALTER TABLE pharmacy_opd_encounters ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_opd_encounters FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_opd_encounters USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid) WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid);

-- Identity is fixed, the patient is set once, and the status moves only
-- with a newer version: a consumer that got this wrong is refused here.
CREATE FUNCTION pharmacy_opd_encounter_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'encounter history is retained' USING ERRCODE='23514'; END IF;
 IF (NEW.tenant_id,NEW.encounter_id,NEW.hospital_id) IS DISTINCT FROM (OLD.tenant_id,OLD.encounter_id,OLD.hospital_id)
  OR (OLD.patient_id IS NOT NULL AND NEW.patient_id IS DISTINCT FROM OLD.patient_id)
  OR NEW.version < OLD.version
  OR (NEW.version = OLD.version AND NEW.status IS DISTINCT FROM OLD.status) THEN
  RAISE EXCEPTION 'an encounter''s identity is fixed and its status moves forward' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_opd_encounter_guard BEFORE UPDATE OR DELETE ON pharmacy_opd_encounters FOR EACH ROW EXECUTE FUNCTION pharmacy_opd_encounter_guard();
CREATE TRIGGER pharmacy_opd_encounter_no_truncate BEFORE TRUNCATE ON pharmacy_opd_encounters FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_registered_patients_immutable();
