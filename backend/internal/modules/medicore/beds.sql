SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE EXTENSION IF NOT EXISTS btree_gist;

-- A hospital's wards, rooms and beds (#106 slice 1). Identity never changes.
CREATE FUNCTION medicore_hospital_allowed(row_tenant uuid,row_hospital uuid) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT row_tenant=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND EXISTS (SELECT 1 FROM medicore_hospital_access a WHERE a.tenant_id=row_tenant AND a.hospital_id=row_hospital
  AND a.subject=NULLIF(current_setting('app.medicore_subject',true),''))
$$;

CREATE TABLE medicore_wards (
 tenant_id uuid NOT NULL,
 hospital_id uuid NOT NULL,
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 code text NOT NULL CHECK (code ~ '^[A-Z0-9][A-Z0-9-]{0,19}$'),
 name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 100 AND name=btrim(name)),
 created_by text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY (tenant_id,id),
 UNIQUE (tenant_id,hospital_id,id),
 UNIQUE (tenant_id,hospital_id,code)
);
CREATE TABLE medicore_rooms (
 tenant_id uuid NOT NULL,
 hospital_id uuid NOT NULL,
 ward_id uuid NOT NULL,
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 code text NOT NULL CHECK (code ~ '^[A-Z0-9][A-Z0-9-]{0,19}$'),
 created_by text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY (tenant_id,id),
 UNIQUE (tenant_id,hospital_id,id),
 UNIQUE (tenant_id,hospital_id,ward_id,id),
 UNIQUE (tenant_id,hospital_id,code),
 FOREIGN KEY (tenant_id,hospital_id,ward_id) REFERENCES medicore_wards (tenant_id,hospital_id,id)
);
CREATE TABLE medicore_beds (
 tenant_id uuid NOT NULL,
 hospital_id uuid NOT NULL,
 ward_id uuid NOT NULL,
 room_id uuid NOT NULL,
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 code text NOT NULL CHECK (code ~ '^[A-Z0-9][A-Z0-9-]{0,19}$'),
 bed_class text NOT NULL CHECK (bed_class IN ('general','semi_private','private','icu','hdu','isolation','neonatal','day_care')),
 state text NOT NULL DEFAULT 'available' CHECK (state IN ('available','occupied','cleaning','out_of_service')),
 version integer NOT NULL DEFAULT 1 CHECK (version >= 1),
 created_by text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY (tenant_id,id),
 UNIQUE (tenant_id,hospital_id,id),
 UNIQUE (tenant_id,hospital_id,code),
 FOREIGN KEY (tenant_id,hospital_id,ward_id,room_id) REFERENCES medicore_rooms (tenant_id,hospital_id,ward_id,id)
);
CREATE INDEX medicore_beds_page ON medicore_beds (tenant_id,hospital_id,created_at DESC,id DESC);

-- Every bed state change, append-only. Only legal moves; the bed row moves
-- with exactly one transition for exactly the next version.
CREATE TABLE medicore_bed_transitions (
 tenant_id uuid NOT NULL,
 hospital_id uuid NOT NULL,
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 bed_id uuid NOT NULL,
 version integer NOT NULL CHECK (version > 1),
 from_state text NOT NULL,
 to_state text NOT NULL,
 cause text NOT NULL CHECK (cause IN ('admission','transfer_in','transfer_out','release','ready','out_of_service','return_to_service')),
 reason text CHECK (reason IS NULL OR (reason=btrim(reason) AND length(reason) BETWEEN 1 AND 500)),
 actor_subject text NOT NULL,
 occurred_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY (tenant_id,id),
 UNIQUE (tenant_id,bed_id,version),
 FOREIGN KEY (tenant_id,hospital_id,bed_id) REFERENCES medicore_beds (tenant_id,hospital_id,id),
 CONSTRAINT medicore_bed_transition_legal CHECK (
  (from_state,to_state,cause) IN (('available','occupied','admission'),('available','occupied','transfer_in'),
   ('occupied','cleaning','transfer_out'),('occupied','cleaning','release'),('cleaning','available','ready'),
   ('available','out_of_service','out_of_service'),('cleaning','out_of_service','out_of_service'),
   ('out_of_service','cleaning','return_to_service'))),
 CONSTRAINT medicore_bed_transition_reasoned CHECK ((cause IN ('out_of_service','return_to_service')) = (reason IS NOT NULL))
);

CREATE TABLE medicore_admissions (
 tenant_id uuid NOT NULL,
 hospital_id uuid NOT NULL,
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 patient_id uuid NOT NULL,
 intent text NOT NULL CHECK (intent IN ('elective','emergency')),
 status text NOT NULL DEFAULT 'admitted' CHECK (status IN ('admitted','released')),
 intent_sha256 text NOT NULL CHECK (intent_sha256 ~ '^[0-9a-f]{64}$'),
 admitted_by text NOT NULL,
 admitted_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 release_reason text CHECK (release_reason IN ('discharged','transferred_out','died','left_against_advice','other')),
 release_note text CHECK (release_note IS NULL OR (release_note=btrim(release_note) AND length(release_note) BETWEEN 1 AND 500)),
 released_by text,
 released_at timestamptz,
 PRIMARY KEY (tenant_id,id),
 UNIQUE (tenant_id,hospital_id,id),
 FOREIGN KEY (tenant_id,patient_id,hospital_id) REFERENCES medicore_patients (tenant_id,patient_id,hospital_id),
 CONSTRAINT medicore_admission_release CHECK (
  (status='admitted' AND release_reason IS NULL AND release_note IS NULL AND released_by IS NULL AND released_at IS NULL)
  OR (status='released' AND release_reason IS NOT NULL AND released_by IS NOT NULL AND released_at IS NOT NULL
      AND (release_reason<>'other' OR release_note IS NOT NULL)))
);
-- One stay at a time per patient per hospital.
CREATE UNIQUE INDEX medicore_admission_active ON medicore_admissions (tenant_id,hospital_id,patient_id) WHERE status='admitted';
CREATE INDEX medicore_admissions_page ON medicore_admissions (tenant_id,hospital_id,admitted_at DESC,id DESC);

CREATE TABLE medicore_bed_occupancies (
 tenant_id uuid NOT NULL,
 hospital_id uuid NOT NULL,
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 bed_id uuid NOT NULL,
 admission_id uuid NOT NULL,
 started_at timestamptz NOT NULL,
 started_by text NOT NULL,
 ended_at timestamptz,
 ended_by text,
 ended_by_cause text CHECK (ended_by_cause IN ('transfer','release')),
 PRIMARY KEY (tenant_id,id),
 FOREIGN KEY (tenant_id,hospital_id,bed_id) REFERENCES medicore_beds (tenant_id,hospital_id,id),
 FOREIGN KEY (tenant_id,hospital_id,admission_id) REFERENCES medicore_admissions (tenant_id,hospital_id,id),
 CONSTRAINT medicore_occupancy_end CHECK ((ended_at IS NULL) = (ended_by IS NULL) AND (ended_at IS NULL) = (ended_by_cause IS NULL)
  AND (ended_at IS NULL OR ended_at >= started_at)),
 -- No two stays in one bed overlap, and no patient is in two beds at once.
 CONSTRAINT medicore_occupancy_bed_overlap EXCLUDE USING gist (tenant_id WITH =, bed_id WITH =, tstzrange(started_at,ended_at,'[)') WITH &&),
 CONSTRAINT medicore_occupancy_admission_overlap EXCLUDE USING gist (tenant_id WITH =, admission_id WITH =, tstzrange(started_at,ended_at,'[)') WITH &&)
);
CREATE UNIQUE INDEX medicore_occupancy_open_bed ON medicore_bed_occupancies (tenant_id,bed_id) WHERE ended_at IS NULL;
CREATE UNIQUE INDEX medicore_occupancy_open_admission ON medicore_bed_occupancies (tenant_id,admission_id) WHERE ended_at IS NULL;

-- Idempotent transfer commands: one row per transfer id.
CREATE TABLE medicore_admission_transfers (
 tenant_id uuid NOT NULL,
 hospital_id uuid NOT NULL,
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 admission_id uuid NOT NULL,
 from_occupancy_id uuid NOT NULL,
 to_occupancy_id uuid NOT NULL,
 intent_sha256 text NOT NULL CHECK (intent_sha256 ~ '^[0-9a-f]{64}$'),
 actor_subject text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY (tenant_id,id),
 FOREIGN KEY (tenant_id,hospital_id,admission_id) REFERENCES medicore_admissions (tenant_id,hospital_id,id)
);

DO $$ DECLARE t text; BEGIN
 FOREACH t IN ARRAY ARRAY['medicore_wards','medicore_rooms','medicore_beds','medicore_bed_transitions','medicore_admissions','medicore_bed_occupancies','medicore_admission_transfers'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
  EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (hms_tenant_visible(tenant_id) AND medicore_hospital_allowed(tenant_id,hospital_id)) WITH CHECK (medicore_hospital_allowed(tenant_id,hospital_id))', t);
  EXECUTE format('CREATE TRIGGER %I AFTER INSERT OR UPDATE ON %I FOR EACH ROW EXECUTE FUNCTION audit_record_change()', t||'_audit', t);
 END LOOP;
END $$;

-- Identity never changes; history is never removed.
CREATE FUNCTION medicore_bed_history_retained() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION 'bed and admission history is retained' USING ERRCODE='23514';
END $$;
CREATE TRIGGER medicore_wards_fixed BEFORE UPDATE OR DELETE OR TRUNCATE ON medicore_wards FOR EACH STATEMENT EXECUTE FUNCTION medicore_bed_history_retained();
CREATE TRIGGER medicore_rooms_fixed BEFORE UPDATE OR DELETE OR TRUNCATE ON medicore_rooms FOR EACH STATEMENT EXECUTE FUNCTION medicore_bed_history_retained();
CREATE TRIGGER medicore_bed_transitions_fixed BEFORE UPDATE OR DELETE OR TRUNCATE ON medicore_bed_transitions FOR EACH STATEMENT EXECUTE FUNCTION medicore_bed_history_retained();
CREATE TRIGGER medicore_admission_transfers_fixed BEFORE UPDATE OR DELETE OR TRUNCATE ON medicore_admission_transfers FOR EACH STATEMENT EXECUTE FUNCTION medicore_bed_history_retained();
CREATE TRIGGER medicore_beds_kept BEFORE DELETE OR TRUNCATE ON medicore_beds FOR EACH STATEMENT EXECUTE FUNCTION medicore_bed_history_retained();
CREATE TRIGGER medicore_admissions_kept BEFORE DELETE OR TRUNCATE ON medicore_admissions FOR EACH STATEMENT EXECUTE FUNCTION medicore_bed_history_retained();
CREATE TRIGGER medicore_occupancies_kept BEFORE DELETE OR TRUNCATE ON medicore_bed_occupancies FOR EACH STATEMENT EXECUTE FUNCTION medicore_bed_history_retained();

-- A bed's identity is fixed; its state moves one version at a time, with
-- the transition for exactly that move already written.
CREATE FUNCTION medicore_bed_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.state<>'available' OR NEW.version<>1 THEN RAISE EXCEPTION 'a bed starts available' USING ERRCODE='23514'; END IF;
  RETURN NEW;
 END IF;
 IF (NEW.tenant_id,NEW.hospital_id,NEW.ward_id,NEW.room_id,NEW.id,NEW.code,NEW.bed_class,NEW.created_by,NEW.created_at)
  IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.ward_id,OLD.room_id,OLD.id,OLD.code,OLD.bed_class,OLD.created_by,OLD.created_at)
  OR NEW.version<>OLD.version+1
  OR NOT EXISTS (SELECT 1 FROM medicore_bed_transitions t WHERE t.tenant_id=NEW.tenant_id AND t.bed_id=NEW.id
   AND t.version=NEW.version AND t.from_state=OLD.state AND t.to_state=NEW.state) THEN
  RAISE EXCEPTION 'a bed changes only through a legal transition' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER medicore_bed_guard BEFORE INSERT OR UPDATE ON medicore_beds FOR EACH ROW EXECUTE FUNCTION medicore_bed_guard();

-- An occupancy is opened in an occupied bed and closed once; an admission is
-- released once, and only with no bed left open.
CREATE FUNCTION medicore_occupancy_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.ended_at IS NOT NULL THEN RAISE EXCEPTION 'an occupancy opens before it ends' USING ERRCODE='23514'; END IF;
  -- Locked, so a release cannot slip between this check and commit.
  PERFORM 1 FROM medicore_admissions a WHERE a.tenant_id=NEW.tenant_id AND a.id=NEW.admission_id AND a.status='admitted' FOR SHARE;
  IF NOT FOUND THEN RAISE EXCEPTION 'only an admitted patient takes a bed' USING ERRCODE='23514'; END IF;
  RETURN NEW;
 END IF;
 IF (NEW.tenant_id,NEW.hospital_id,NEW.id,NEW.bed_id,NEW.admission_id,NEW.started_at,NEW.started_by)
  IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.id,OLD.bed_id,OLD.admission_id,OLD.started_at,OLD.started_by)
  OR OLD.ended_at IS NOT NULL THEN
  RAISE EXCEPTION 'an occupancy is closed once and otherwise fixed' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER medicore_occupancy_guard BEFORE INSERT OR UPDATE ON medicore_bed_occupancies FOR EACH ROW EXECUTE FUNCTION medicore_occupancy_guard();

CREATE FUNCTION medicore_admission_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.status<>'admitted' THEN RAISE EXCEPTION 'an admission starts admitted' USING ERRCODE='23514'; END IF;
  RETURN NEW;
 END IF;
 IF (NEW.tenant_id,NEW.hospital_id,NEW.id,NEW.patient_id,NEW.intent,NEW.intent_sha256,NEW.admitted_by,NEW.admitted_at)
  IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.id,OLD.patient_id,OLD.intent,OLD.intent_sha256,OLD.admitted_by,OLD.admitted_at)
  OR OLD.status<>'admitted' OR NEW.status<>'released'
  OR EXISTS (SELECT 1 FROM medicore_bed_occupancies o WHERE o.tenant_id=NEW.tenant_id AND o.admission_id=NEW.id AND o.ended_at IS NULL) THEN
  RAISE EXCEPTION 'an admission is released once, out of its bed' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER medicore_admission_guard BEFORE INSERT OR UPDATE ON medicore_admissions FOR EACH ROW EXECUTE FUNCTION medicore_admission_guard();

-- An open occupancy and an occupied bed are the same fact, and a recorded
-- transition is one the bed actually made: both checked at commit, whatever
-- order the statements ran in or whichever table a statement wrote. A bed
-- that cannot be seen fails the check rather than passing it.
CREATE FUNCTION medicore_bed_consistent(row_tenant uuid, row_bed uuid) RETURNS void LANGUAGE plpgsql AS $$
DECLARE b record;
BEGIN
 SELECT state INTO b FROM medicore_beds WHERE tenant_id=row_tenant AND id=row_bed;
 IF NOT FOUND THEN RAISE EXCEPTION 'bed consistency cannot be checked' USING ERRCODE='23514'; END IF;
 IF (b.state='occupied') <> EXISTS (SELECT 1 FROM medicore_bed_occupancies o WHERE o.tenant_id=row_tenant AND o.bed_id=row_bed AND o.ended_at IS NULL) THEN
  RAISE EXCEPTION 'a bed is occupied exactly while it has an open occupancy' USING ERRCODE='23514';
 END IF;
END $$;
CREATE FUNCTION medicore_bed_row_consistent() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM medicore_bed_consistent(NEW.tenant_id, NEW.id);
 RETURN NULL;
END $$;
CREATE FUNCTION medicore_occupancy_row_consistent() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM medicore_bed_consistent(NEW.tenant_id, NEW.bed_id);
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER medicore_bed_occupancy_consistent AFTER UPDATE ON medicore_beds DEFERRABLE INITIALLY DEFERRED
 FOR EACH ROW EXECUTE FUNCTION medicore_bed_row_consistent();
CREATE CONSTRAINT TRIGGER medicore_occupancy_bed_consistent AFTER INSERT OR UPDATE ON medicore_bed_occupancies DEFERRABLE INITIALLY DEFERRED
 FOR EACH ROW EXECUTE FUNCTION medicore_occupancy_row_consistent();
-- The bed guard admits a version step only with its transition; this is the
-- converse: a transition only with its version step.
CREATE FUNCTION medicore_bed_transition_applied() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM 1 FROM medicore_beds b WHERE b.tenant_id=NEW.tenant_id AND b.id=NEW.bed_id AND b.version>=NEW.version;
 IF NOT FOUND THEN RAISE EXCEPTION 'a bed transition was recorded but not applied' USING ERRCODE='23514'; END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER medicore_bed_transition_applied AFTER INSERT ON medicore_bed_transitions DEFERRABLE INITIALLY DEFERRED
 FOR EACH ROW EXECUTE FUNCTION medicore_bed_transition_applied();
