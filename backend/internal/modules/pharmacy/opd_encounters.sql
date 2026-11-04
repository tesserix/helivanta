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

-- A registered patient's counter sale may cite the OPD encounter it serves.
-- Never an admission's or a walk-in's: an admission is its own link.
ALTER TABLE pharmacy_sales
 ADD COLUMN encounter_id uuid,
 ADD CONSTRAINT pharmacy_sale_encounter_patient_sale CHECK (encounter_id IS NULL OR (patient_id IS NOT NULL AND admission_id IS NULL));

-- The cited encounter must be this patient's, at this patient's hospital,
-- and a visit that happened (#987 slice 2 decision 2). Checked here so no
-- code path can post a sale against another's visit. FOR SHARE orders the
-- sale wholly before or after a status change being applied.
CREATE FUNCTION pharmacy_sale_encounter_citable() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
 e record;
BEGIN
 IF NEW.encounter_id IS NULL THEN RETURN NEW; END IF;
 SELECT hospital_id,patient_id,status INTO e FROM pharmacy_opd_encounters
  WHERE tenant_id=NEW.tenant_id AND encounter_id=NEW.encounter_id FOR SHARE;
 IF NOT FOUND OR e.patient_id IS NULL THEN
  RAISE EXCEPTION 'the encounter is not known to the pharmacy' USING ERRCODE='23514', CONSTRAINT='pharmacy_sale_encounter_unknown';
 END IF;
 IF e.patient_id IS DISTINCT FROM NEW.patient_id OR e.hospital_id IS DISTINCT FROM NEW.patient_hospital_id THEN
  RAISE EXCEPTION 'the encounter is not this patient''s at this hospital' USING ERRCODE='23514', CONSTRAINT='pharmacy_sale_encounter_other';
 END IF;
 IF e.status NOT IN ('arrived','in_consultation','closed') THEN
  RAISE EXCEPTION 'the encounter was cancelled or the patient left without being seen' USING ERRCODE='23514', CONSTRAINT='pharmacy_sale_encounter_status';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_sale_encounter_citable BEFORE INSERT ON pharmacy_sales FOR EACH ROW EXECUTE FUNCTION pharmacy_sale_encounter_citable();
