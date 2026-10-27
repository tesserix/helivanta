SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

-- Patients registered at a hospital, as patientmaster announced them (#80 E2).
-- No name or contact data: the pair is all an encounter needs to prove its
-- patient belongs to its hospital.
CREATE TABLE medicore_patients (
 tenant_id uuid NOT NULL,
 patient_id uuid NOT NULL,
 hospital_id uuid NOT NULL CHECK(hospital_id<>'00000000-0000-0000-0000-000000000000'::uuid),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,patient_id),
 UNIQUE(tenant_id,patient_id,hospital_id)
);
ALTER TABLE medicore_patients ENABLE ROW LEVEL SECURITY;
ALTER TABLE medicore_patients FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON medicore_patients
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid);

-- Who may see and change a hospital's encounters (#80 E3).
CREATE TABLE medicore_hospital_access (
 id uuid NOT NULL DEFAULT gen_random_uuid(),
 tenant_id uuid NOT NULL,
 hospital_id uuid NOT NULL CHECK(hospital_id<>'00000000-0000-0000-0000-000000000000'::uuid),
 subject text NOT NULL CHECK(length(btrim(subject)) BETWEEN 1 AND 200),
 created_by text NOT NULL CHECK(length(created_by) BETWEEN 1 AND 512),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id,subject)
);
ALTER TABLE medicore_hospital_access ENABLE ROW LEVEL SECURITY;
ALTER TABLE medicore_hospital_access FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON medicore_hospital_access
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND created_by=NULLIF(current_setting('app.actor_subject',true),''));
CREATE TRIGGER medicore_hospital_access_audit AFTER INSERT OR UPDATE OR DELETE ON medicore_hospital_access
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();

-- The visit becomes the OPD encounter (#80 E1). A row with a hospital is an
-- encounter; a row without one is a legacy visit, which keeps its name and
-- its 'open' status and can never hold a prescription.
ALTER TABLE medicore_visits
 ALTER COLUMN patient_name DROP NOT NULL,
 ADD COLUMN hospital_id uuid,
 ADD COLUMN consult_mode text CHECK(consult_mode IN ('in_person','teleconsultation')),
 ADD COLUMN visit_type text CHECK(visit_type IN ('first','follow_up')),
 ADD COLUMN version integer NOT NULL DEFAULT 1,
 ADD COLUMN closed_at timestamptz,
 ADD COLUMN created_by text,
 ADD COLUMN intent_sha256 text CHECK(intent_sha256 IS NULL OR intent_sha256 ~ '^[0-9a-f]{64}$'),
 ADD CONSTRAINT medicore_visits_status CHECK(status IN ('open','arrived','in_consultation','closed','left_without_being_seen','cancelled')) NOT VALID,
 ADD CONSTRAINT medicore_visits_kind CHECK(
  (hospital_id IS NULL AND patient_name IS NOT NULL AND status='open')
  OR (hospital_id IS NOT NULL AND patient_name IS NULL AND patient_id IS NOT NULL AND consult_mode IS NOT NULL
      AND visit_type IS NOT NULL AND created_by IS NOT NULL AND intent_sha256 IS NOT NULL AND department='OPD'
      AND status<>'open' AND (closed_at IS NOT NULL)=(status IN ('closed','left_without_being_seen','cancelled')))) NOT VALID,
 -- MATCH SIMPLE skips legacy rows (no hospital); an encounter's patient must be
 -- registered at the encounter's hospital, by construction.
 ADD CONSTRAINT medicore_visits_patient FOREIGN KEY(tenant_id,patient_id,hospital_id)
  REFERENCES medicore_patients(tenant_id,patient_id,hospital_id) NOT VALID;
-- Added NOT VALID, then validated: validation scans existing visits under a
-- lock that does not block reads or writes of the table.
ALTER TABLE medicore_visits VALIDATE CONSTRAINT medicore_visits_status;
ALTER TABLE medicore_visits VALIDATE CONSTRAINT medicore_visits_kind;
ALTER TABLE medicore_visits VALIDATE CONSTRAINT medicore_visits_patient;
CREATE INDEX medicore_encounters_page ON medicore_visits(tenant_id,hospital_id,created_at DESC,id DESC) WHERE hospital_id IS NOT NULL;

-- Every status change, append-only (#80 E5).
CREATE TABLE medicore_encounter_transitions (
 tenant_id uuid NOT NULL,
 id uuid NOT NULL DEFAULT gen_random_uuid(),
 encounter_id uuid NOT NULL,
 version integer NOT NULL CHECK(version>1),
 from_status text NOT NULL,
 to_status text NOT NULL,
 reason text CHECK(reason IS NULL OR length(btrim(reason)) BETWEEN 1 AND 500),
 actor_subject text NOT NULL CHECK(length(actor_subject) BETWEEN 1 AND 512),
 occurred_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,id),
 UNIQUE(tenant_id,encounter_id,version)
);
ALTER TABLE medicore_encounter_transitions ENABLE ROW LEVEL SECURITY;
ALTER TABLE medicore_encounter_transitions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON medicore_encounter_transitions
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND actor_subject=NULLIF(current_setting('app.actor_subject',true),''));
CREATE FUNCTION medicore_encounter_transition_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'encounter history is retained' USING ERRCODE='23514'; END IF;
 IF NOT ((NEW.from_status='arrived' AND NEW.to_status IN ('in_consultation','left_without_being_seen','cancelled'))
      OR (NEW.from_status='in_consultation' AND NEW.to_status='closed')) THEN
  RAISE EXCEPTION 'illegal encounter transition' USING ERRCODE='23514';
 END IF;
 IF NEW.to_status IN ('left_without_being_seen','cancelled') AND NEW.reason IS NULL THEN
  RAISE EXCEPTION 'this transition needs a reason' USING ERRCODE='23514';
 END IF;
 -- History only for a move the encounter is making now: it must be at
 -- from_status and the version just before this one.
 IF NOT EXISTS(SELECT 1 FROM medicore_visits v WHERE v.tenant_id=NEW.tenant_id AND v.id=NEW.encounter_id
    AND v.hospital_id IS NOT NULL AND v.status=NEW.from_status AND v.version=NEW.version-1 FOR UPDATE) THEN
  RAISE EXCEPTION 'transition does not match the encounter' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER medicore_encounter_transition_guard BEFORE INSERT OR UPDATE OR DELETE ON medicore_encounter_transitions
 FOR EACH ROW EXECUTE FUNCTION medicore_encounter_transition_guard();
CREATE TRIGGER medicore_encounter_transition_audit AFTER INSERT ON medicore_encounter_transitions
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();

-- An encounter's identity never changes, and its status moves only with an
-- attributable transition row for exactly that version. A legacy visit
-- keeps its old behaviour.
CREATE FUNCTION medicore_encounter_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN
  IF OLD.hospital_id IS NOT NULL THEN RAISE EXCEPTION 'encounter history is retained' USING ERRCODE='23514'; END IF;
  RETURN OLD;
 END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.hospital_id IS NOT NULL AND (NEW.status<>'arrived' OR NEW.version<>1
     OR NEW.created_by IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'')) THEN
   RAISE EXCEPTION 'an encounter opens as arrived, by its actor' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;
 IF OLD.hospital_id IS NULL AND NEW.hospital_id IS NULL THEN RETURN NEW; END IF;
 IF (NEW.tenant_id,NEW.id,NEW.hospital_id,NEW.patient_id,NEW.consult_mode,NEW.visit_type,NEW.department,NEW.created_by,NEW.intent_sha256,NEW.created_at)
    IS DISTINCT FROM (OLD.tenant_id,OLD.id,OLD.hospital_id,OLD.patient_id,OLD.consult_mode,OLD.visit_type,OLD.department,OLD.created_by,OLD.intent_sha256,OLD.created_at)
    OR NEW.version<>OLD.version+1
    OR NOT EXISTS(SELECT 1 FROM medicore_encounter_transitions t WHERE t.tenant_id=NEW.tenant_id AND t.encounter_id=NEW.id
      AND t.version=NEW.version AND t.from_status=OLD.status AND t.to_status=NEW.status
      AND t.actor_subject=NULLIF(current_setting('app.actor_subject',true),'')) THEN
  RAISE EXCEPTION 'encounter changes only by an attributable transition' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER medicore_encounter_guard BEFORE INSERT OR UPDATE OR DELETE ON medicore_visits
 FOR EACH ROW EXECUTE FUNCTION medicore_encounter_guard();
