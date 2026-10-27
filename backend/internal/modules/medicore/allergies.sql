SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

-- Molecules the drug master has published (#1373 A4): global reference
-- data, so no tenant_id (tenantdb lintAllowlist). Only the
-- medicore-molecule-published consumer adds a row. The bus names every
-- consumer transaction's actor 'system:<consumer>', and no request path can
-- take a 'system:' actor, because authn subjects never carry that prefix.
CREATE TABLE medicore_drug_molecules (
 id uuid PRIMARY KEY,
 inn_name text NOT NULL CHECK (length(inn_name) BETWEEN 1 AND 200),
 atc_code text NOT NULL CHECK (atc_code='' OR atc_code ~ '^[A-Z][0-9]{2}[A-Z]{2}[0-9]{2}$'),
 created_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE medicore_drug_molecules ENABLE ROW LEVEL SECURITY;
ALTER TABLE medicore_drug_molecules FORCE ROW LEVEL SECURITY;
REVOKE ALL ON medicore_drug_molecules FROM PUBLIC, hms_app;
GRANT SELECT, INSERT ON medicore_drug_molecules TO hms_app;
CREATE POLICY medicore_molecules_read ON medicore_drug_molecules FOR SELECT TO hms_app USING (true);
CREATE POLICY medicore_molecules_project ON medicore_drug_molecules FOR INSERT TO hms_app
 WITH CHECK (current_setting('app.actor_subject',true)='system:medicore-molecule-published');

-- One function for every clinical-record table: insert-only (IN-CL-12).
CREATE FUNCTION medicore_clinical_record_retained() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION 'clinical records are corrected by a new entry, never changed or removed' USING ERRCODE='23514';
END $$;
CREATE TRIGGER medicore_drug_molecules_retained BEFORE UPDATE OR DELETE ON medicore_drug_molecules
 FOR EACH ROW EXECUTE FUNCTION medicore_clinical_record_retained();

-- Allergy entries (#1373 A3, A5, A6). Each change is a new row; the entry is
-- (id), its current state the highest version.
CREATE TABLE medicore_allergy_entries (
 tenant_id uuid NOT NULL,
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 version integer NOT NULL CHECK (version >= 1),
 hospital_id uuid NOT NULL,
 patient_id uuid NOT NULL,
 status text NOT NULL CHECK (status IN ('active','resolved','entered_in_error')),
 substance_kind text NOT NULL CHECK (substance_kind IN ('drug_molecule','drug_unlisted','non_drug')),
 molecule_id uuid REFERENCES medicore_drug_molecules(id),
 substance_label text NOT NULL CHECK (length(btrim(substance_label)) BETWEEN 1 AND 200),
 non_drug_category text CHECK (non_drug_category IN ('food','latex','environmental','contrast_media','other')),
 reaction text NOT NULL CHECK (reaction IN ('rash','urticaria','angioedema','anaphylaxis','bronchospasm','gastrointestinal','other')),
 reaction_note text CHECK (reaction_note IS NULL OR length(btrim(reaction_note)) BETWEEN 1 AND 500),
 severity text NOT NULL CHECK (severity IN ('mild','moderate','severe','life_threatening','unknown')),
 source text NOT NULL CHECK (source IN ('patient_reported','clinician_observed','prior_record')),
 onset_date date,
 encounter_id uuid,
 reason text CHECK (reason IS NULL OR length(btrim(reason)) BETWEEN 1 AND 500),
 intent_sha256 text NOT NULL CHECK (intent_sha256 ~ '^[0-9a-f]{64}$'),
 recorded_by text NOT NULL CHECK (length(recorded_by) BETWEEN 1 AND 512),
 recorded_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,id,version),
 FOREIGN KEY (tenant_id,patient_id,hospital_id) REFERENCES medicore_patients(tenant_id,patient_id,hospital_id),
 CHECK ((substance_kind='drug_molecule')=(molecule_id IS NOT NULL)),
 CHECK ((substance_kind='non_drug')=(non_drug_category IS NOT NULL)),
 CHECK ((status='active')=(reason IS NULL)),
 CHECK (version>1 OR status='active')
);
ALTER TABLE medicore_allergy_entries ENABLE ROW LEVEL SECURITY;
ALTER TABLE medicore_allergy_entries FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON medicore_allergy_entries
 USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND recorded_by=NULLIF(current_setting('app.actor_subject',true),''));
CREATE INDEX medicore_allergy_patient ON medicore_allergy_entries (tenant_id,hospital_id,patient_id,id,version DESC);
CREATE TRIGGER medicore_allergy_entries_retained BEFORE UPDATE OR DELETE ON medicore_allergy_entries
 FOR EACH ROW EXECUTE FUNCTION medicore_clinical_record_retained();
CREATE TRIGGER medicore_allergy_entries_audit AFTER INSERT ON medicore_allergy_entries
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();

-- A new version continues its entry: same substance, next number, and only
-- from active (#1373 A6). The encounter, if named, is at the same hospital
-- and of the same patient.
CREATE FUNCTION medicore_allergy_entry_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE prev medicore_allergy_entries;
BEGIN
 IF NEW.encounter_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM medicore_visits v WHERE v.tenant_id=NEW.tenant_id
    AND v.id=NEW.encounter_id AND v.hospital_id=NEW.hospital_id AND v.patient_id=NEW.patient_id) THEN
  RAISE EXCEPTION 'the encounter is not this patient''s at this hospital' USING ERRCODE='23514';
 END IF;
 IF NEW.version=1 THEN
  IF EXISTS (SELECT 1 FROM medicore_allergy_entries e WHERE e.tenant_id=NEW.tenant_id AND e.id=NEW.id) THEN
   RAISE EXCEPTION 'allergy entry already exists' USING ERRCODE='23505';
  END IF;
  RETURN NEW;
 END IF;
 SELECT * INTO prev FROM medicore_allergy_entries e WHERE e.tenant_id=NEW.tenant_id AND e.id=NEW.id ORDER BY e.version DESC LIMIT 1 FOR UPDATE;
 IF prev.id IS NULL OR prev.version<>NEW.version-1 OR prev.status<>'active'
    OR (NEW.hospital_id,NEW.patient_id,NEW.substance_kind,NEW.molecule_id,NEW.substance_label,NEW.non_drug_category,NEW.reaction,NEW.reaction_note,NEW.severity,NEW.source,NEW.onset_date,NEW.encounter_id)
    IS DISTINCT FROM (prev.hospital_id,prev.patient_id,prev.substance_kind,prev.molecule_id,prev.substance_label,prev.non_drug_category,prev.reaction,prev.reaction_note,prev.severity,prev.source,prev.onset_date,prev.encounter_id) THEN
  RAISE EXCEPTION 'an allergy correction continues its active entry unchanged' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER medicore_allergy_entry_guard BEFORE INSERT ON medicore_allergy_entries
 FOR EACH ROW EXECUTE FUNCTION medicore_allergy_entry_guard();

-- "Asked, no known allergy" statements (#1373 A2). Never stored as a
-- default: no row means unknown. A later active entry outranks it.
CREATE TABLE medicore_no_known_allergy (
 tenant_id uuid NOT NULL,
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 hospital_id uuid NOT NULL,
 patient_id uuid NOT NULL,
 encounter_id uuid,
 intent_sha256 text NOT NULL CHECK (intent_sha256 ~ '^[0-9a-f]{64}$'),
 recorded_by text NOT NULL CHECK (length(recorded_by) BETWEEN 1 AND 512),
 recorded_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,id),
 FOREIGN KEY (tenant_id,patient_id,hospital_id) REFERENCES medicore_patients(tenant_id,patient_id,hospital_id)
);
ALTER TABLE medicore_no_known_allergy ENABLE ROW LEVEL SECURITY;
ALTER TABLE medicore_no_known_allergy FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON medicore_no_known_allergy
 USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND recorded_by=NULLIF(current_setting('app.actor_subject',true),''));
CREATE INDEX medicore_no_known_allergy_patient ON medicore_no_known_allergy (tenant_id,hospital_id,patient_id,recorded_at DESC);
CREATE TRIGGER medicore_no_known_allergy_retained BEFORE UPDATE OR DELETE ON medicore_no_known_allergy
 FOR EACH ROW EXECUTE FUNCTION medicore_clinical_record_retained();
CREATE TRIGGER medicore_no_known_allergy_audit AFTER INSERT ON medicore_no_known_allergy
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();
-- Refused while any allergy is active: it would contradict the record.
CREATE FUNCTION medicore_no_known_allergy_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.encounter_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM medicore_visits v WHERE v.tenant_id=NEW.tenant_id
    AND v.id=NEW.encounter_id AND v.hospital_id=NEW.hospital_id AND v.patient_id=NEW.patient_id) THEN
  RAISE EXCEPTION 'the encounter is not this patient''s at this hospital' USING ERRCODE='23514';
 END IF;
 IF EXISTS (SELECT 1 FROM medicore_allergy_entries e WHERE e.tenant_id=NEW.tenant_id AND e.hospital_id=NEW.hospital_id
    AND e.patient_id=NEW.patient_id AND e.status='active' AND NOT EXISTS (SELECT 1 FROM medicore_allergy_entries n
     WHERE n.tenant_id=e.tenant_id AND n.id=e.id AND n.version>e.version)) THEN
  RAISE EXCEPTION 'the patient has an active allergy' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER medicore_no_known_allergy_guard BEFORE INSERT ON medicore_no_known_allergy
 FOR EACH ROW EXECUTE FUNCTION medicore_no_known_allergy_guard();

-- Weight observations (#1373 A7), in kilograms, insert-only; an error is
-- marked by a later row naming it.
CREATE TABLE medicore_weight_observations (
 tenant_id uuid NOT NULL,
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 hospital_id uuid NOT NULL,
 patient_id uuid NOT NULL,
 weight_kg numeric(6,3) NOT NULL CHECK (weight_kg BETWEEN 0.2 AND 500),
 measured_at timestamptz NOT NULL,
 method text NOT NULL CHECK (method IN ('measured','estimated','reported')),
 encounter_id uuid,
 intent_sha256 text NOT NULL CHECK (intent_sha256 ~ '^[0-9a-f]{64}$'),
 recorded_by text NOT NULL CHECK (length(recorded_by) BETWEEN 1 AND 512),
 recorded_at timestamptz NOT NULL DEFAULT now(),
 CHECK (measured_at <= recorded_at + interval '5 minutes'),
 PRIMARY KEY (tenant_id,id),
 FOREIGN KEY (tenant_id,patient_id,hospital_id) REFERENCES medicore_patients(tenant_id,patient_id,hospital_id)
);
ALTER TABLE medicore_weight_observations ENABLE ROW LEVEL SECURITY;
ALTER TABLE medicore_weight_observations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON medicore_weight_observations
 USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND recorded_by=NULLIF(current_setting('app.actor_subject',true),''));
CREATE INDEX medicore_weight_patient ON medicore_weight_observations (tenant_id,hospital_id,patient_id,measured_at DESC,id DESC);
CREATE TRIGGER medicore_weight_observations_retained BEFORE UPDATE OR DELETE ON medicore_weight_observations
 FOR EACH ROW EXECUTE FUNCTION medicore_clinical_record_retained();
CREATE TRIGGER medicore_weight_observations_audit AFTER INSERT ON medicore_weight_observations
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE FUNCTION medicore_weight_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.encounter_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM medicore_visits v WHERE v.tenant_id=NEW.tenant_id
    AND v.id=NEW.encounter_id AND v.hospital_id=NEW.hospital_id AND v.patient_id=NEW.patient_id) THEN
  RAISE EXCEPTION 'the encounter is not this patient''s at this hospital' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER medicore_weight_guard BEFORE INSERT ON medicore_weight_observations
 FOR EACH ROW EXECUTE FUNCTION medicore_weight_guard();

CREATE TABLE medicore_weight_errors (
 tenant_id uuid NOT NULL,
 id uuid NOT NULL DEFAULT gen_random_uuid(),
 observation_id uuid NOT NULL,
 reason text NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 500),
 recorded_by text NOT NULL CHECK (length(recorded_by) BETWEEN 1 AND 512),
 recorded_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,observation_id),
 UNIQUE (tenant_id,id),
 FOREIGN KEY (tenant_id,observation_id) REFERENCES medicore_weight_observations(tenant_id,id)
);
ALTER TABLE medicore_weight_errors ENABLE ROW LEVEL SECURITY;
ALTER TABLE medicore_weight_errors FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON medicore_weight_errors
 USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND recorded_by=NULLIF(current_setting('app.actor_subject',true),''));
CREATE TRIGGER medicore_weight_errors_retained BEFORE UPDATE OR DELETE ON medicore_weight_errors
 FOR EACH ROW EXECUTE FUNCTION medicore_clinical_record_retained();
CREATE TRIGGER medicore_weight_errors_audit AFTER INSERT ON medicore_weight_errors
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();

-- Sensitive reads (#1373 A9): one row per read of a patient's allergies or
-- weights, naming who read what.
CREATE TABLE medicore_clinical_read_audit (
 tenant_id uuid NOT NULL,
 id uuid NOT NULL DEFAULT gen_random_uuid(),
 hospital_id uuid NOT NULL,
 patient_id uuid NOT NULL,
 record text NOT NULL CHECK (record IN ('allergies','allergy_history','weights')),
 actor_subject text NOT NULL CHECK (length(actor_subject) BETWEEN 1 AND 512),
 read_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,id)
);
ALTER TABLE medicore_clinical_read_audit ENABLE ROW LEVEL SECURITY;
ALTER TABLE medicore_clinical_read_audit FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON medicore_clinical_read_audit
 USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND actor_subject=NULLIF(current_setting('app.actor_subject',true),''));
CREATE INDEX medicore_clinical_read_audit_patient ON medicore_clinical_read_audit (tenant_id,hospital_id,patient_id,read_at DESC);
CREATE TRIGGER medicore_clinical_read_audit_retained BEFORE UPDATE OR DELETE ON medicore_clinical_read_audit
 FOR EACH ROW EXECUTE FUNCTION medicore_clinical_record_retained();
