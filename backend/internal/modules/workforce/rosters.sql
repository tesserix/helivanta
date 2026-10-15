SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Trusted extension (PG13+): the database owner creates it without superuser, in prod and the harness alike.
CREATE EXTENSION IF NOT EXISTS btree_gist;

ALTER TABLE workforce_roster_setting_revisions ADD CONSTRAINT workforce_roster_setting_pin UNIQUE(tenant_id,hospital_id,id,timezone,week_start,auto_population);

CREATE TABLE workforce_rosters (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL,
 name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 80),
 start_date date NOT NULL, end_date date NOT NULL,
 status text NOT NULL CHECK (status IN ('draft','frozen','discarded')),
 settings_revision_id uuid NOT NULL, timezone text NOT NULL,
 week_start smallint NOT NULL CHECK (week_start BETWEEN 0 AND 6),
 auto_population text NOT NULL CHECK (auto_population IN ('disabled','fixed_shift','all_assigned')),
 reviewed_by_subject text NOT NULL CHECK (length(reviewed_by_subject) BETWEEN 1 AND 200),
 version bigint NOT NULL CHECK (version > 0), revision_id uuid NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 CHECK (start_date >= DATE '1900-01-01' AND end_date <= DATE '9999-12-31'),
 CHECK (end_date - start_date + 1 BETWEEN 7 AND 42 AND (end_date - start_date + 1) % 7 = 0),
 CHECK (extract(isodow FROM start_date)::int - 1 = week_start),
 PRIMARY KEY(tenant_id,hospital_id,id),
 FOREIGN KEY(tenant_id,hospital_id) REFERENCES workforce_hospitals(tenant_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,settings_revision_id,timezone,week_start,auto_population)
  REFERENCES workforce_roster_setting_revisions(tenant_id,hospital_id,id,timezone,week_start,auto_population) ON DELETE RESTRICT,
 CONSTRAINT workforce_roster_period_overlap EXCLUDE USING gist (tenant_id WITH =, hospital_id WITH =, daterange(start_date,end_date,'[]') WITH &&) WHERE (status <> 'discarded')
);
CREATE INDEX workforce_rosters_page ON workforce_rosters(tenant_id,hospital_id,created_at DESC,id DESC);

CREATE TABLE workforce_roster_revisions (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL, roster_id uuid NOT NULL,
 action text NOT NULL CHECK (action IN ('created','changed','populated','frozen','unfrozen','discarded')),
 mode text CHECK (mode IN ('roster','employee','additional')),
 status text NOT NULL CHECK (status IN ('draft','frozen','discarded')),
 added integer NOT NULL CHECK (added BETWEEN 0 AND 20000), removed integer NOT NULL CHECK (removed >= 0),
 skipped_existing integer NOT NULL CHECK (skipped_existing >= 0),
 skipped_unassigned integer NOT NULL CHECK (skipped_unassigned >= 0),
 skipped_conflicts integer NOT NULL CHECK (skipped_conflicts >= 0),
 reason text NOT NULL CHECK (length(reason) <= 500),
 reviewed_by_subject text NOT NULL CHECK (length(reviewed_by_subject) BETWEEN 1 AND 200),
 version bigint NOT NULL CHECK (version > 0), created_at timestamptz NOT NULL DEFAULT now(),
 CHECK ((action = 'changed') = (mode IS NOT NULL)),
 CHECK (action NOT IN ('unfrozen','discarded') OR length(btrim(reason)) > 0),
 PRIMARY KEY(tenant_id,hospital_id,id), UNIQUE(tenant_id,hospital_id,roster_id,version,id), UNIQUE(tenant_id,hospital_id,roster_id,version),
 FOREIGN KEY(tenant_id,hospital_id,roster_id) REFERENCES workforce_rosters(tenant_id,hospital_id,id) ON DELETE RESTRICT
);
CREATE INDEX workforce_roster_history ON workforce_roster_revisions(tenant_id,hospital_id,roster_id,created_at DESC,id DESC);
ALTER TABLE workforce_rosters ADD CONSTRAINT workforce_roster_current
 FOREIGN KEY(tenant_id,hospital_id,id,version,revision_id) REFERENCES workforce_roster_revisions(tenant_id,hospital_id,roster_id,version,id) DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE workforce_roster_allocations (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL, roster_id uuid NOT NULL,
 staff_id uuid NOT NULL, roster_date date NOT NULL,
 shift_revision_id uuid NOT NULL, area_revision_id uuid NOT NULL, area_kind text GENERATED ALWAYS AS ('area') STORED,
 starts_at timestamptz NOT NULL, ends_at timestamptz NOT NULL, additional boolean NOT NULL,
 added_version bigint NOT NULL, added_revision_id uuid NOT NULL,
 removed_version bigint, removed_revision_id uuid,
 created_at timestamptz NOT NULL DEFAULT now(),
 CHECK (ends_at > starts_at AND ends_at - starts_at <= interval '26 hours'),
 CHECK ((removed_version IS NULL) = (removed_revision_id IS NULL) AND (removed_version IS NULL OR removed_version > added_version)),
 PRIMARY KEY(tenant_id,hospital_id,id),
 FOREIGN KEY(tenant_id,hospital_id,roster_id) REFERENCES workforce_rosters(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,staff_id) REFERENCES workforce_staff(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,shift_revision_id) REFERENCES workforce_shift_revisions(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,area_revision_id,area_kind) REFERENCES workforce_roster_config_revisions(tenant_id,hospital_id,id,kind) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,roster_id,added_version,added_revision_id) REFERENCES workforce_roster_revisions(tenant_id,hospital_id,roster_id,version,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,roster_id,removed_version,removed_revision_id) REFERENCES workforce_roster_revisions(tenant_id,hospital_id,roster_id,version,id) ON DELETE RESTRICT,
 CONSTRAINT workforce_roster_allocation_overlap EXCLUDE USING gist (tenant_id WITH =, hospital_id WITH =, staff_id WITH =, tstzrange(starts_at,ends_at,'[)') WITH &&) WHERE (removed_version IS NULL)
);
CREATE UNIQUE INDEX workforce_roster_allocation_primary ON workforce_roster_allocations(tenant_id,hospital_id,staff_id,roster_date) WHERE removed_version IS NULL AND NOT additional;
CREATE INDEX workforce_roster_allocations_page ON workforce_roster_allocations(tenant_id,hospital_id,roster_id,created_at DESC,id DESC);
CREATE INDEX workforce_roster_allocations_staff ON workforce_roster_allocations(tenant_id,hospital_id,staff_id,roster_date);
CREATE INDEX workforce_roster_allocations_shift ON workforce_roster_allocations(tenant_id,hospital_id,shift_revision_id);
CREATE INDEX workforce_roster_allocations_area ON workforce_roster_allocations(tenant_id,hospital_id,area_revision_id);

-- Roster pins the settings revision in force on its first day and never re-pins.
CREATE FUNCTION workforce_roster_head_valid() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP = 'INSERT' THEN
  IF NEW.status <> 'draft' OR NEW.version <> 1 OR NEW.settings_revision_id IS DISTINCT FROM (
    SELECT r.id FROM workforce_roster_setting_revisions r WHERE r.tenant_id=NEW.tenant_id AND r.hospital_id=NEW.hospital_id AND r.effective_on<=NEW.start_date
    ORDER BY r.effective_on DESC, r.version DESC LIMIT 1) THEN
   RAISE EXCEPTION 'roster must start as a draft pinned to the settings in force on its first day' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;
 IF (NEW.tenant_id,NEW.hospital_id,NEW.id,NEW.name,NEW.start_date,NEW.end_date,NEW.settings_revision_id,NEW.timezone,NEW.week_start,NEW.auto_population)
   IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.id,OLD.name,OLD.start_date,OLD.end_date,OLD.settings_revision_id,OLD.timezone,OLD.week_start,OLD.auto_population)
   OR NEW.version <> OLD.version + 1 OR OLD.status = 'discarded'
   OR (NEW.status <> OLD.status AND (OLD.status, NEW.status) NOT IN (('draft','frozen'),('frozen','draft'),('draft','discarded')))
   OR (OLD.status = 'frozen' AND NEW.status = 'frozen') THEN
  RAISE EXCEPTION 'roster identity is immutable, versions must advance and status must follow draft/frozen/discarded' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_roster_head_valid BEFORE INSERT OR UPDATE ON workforce_rosters FOR EACH ROW EXECUTE FUNCTION workforce_roster_head_valid();

-- Allocations change only in a draft at the version being written (or the discard itself); removal is a one-way tombstone.
CREATE FUNCTION workforce_roster_allocation_valid() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE r workforce_rosters%ROWTYPE;
BEGIN
 SELECT * INTO r FROM workforce_rosters WHERE tenant_id=NEW.tenant_id AND hospital_id=NEW.hospital_id AND id=NEW.roster_id;
 IF TG_OP = 'INSERT' THEN
  IF r.status IS DISTINCT FROM 'draft' OR NEW.added_version <> r.version OR NEW.removed_version IS NOT NULL
    OR NEW.roster_date NOT BETWEEN r.start_date AND r.end_date THEN
   RAISE EXCEPTION 'allocations can only be added to the current version of a draft roster within its period' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;
 IF OLD.removed_version IS NOT NULL OR NEW.removed_version IS DISTINCT FROM r.version OR r.status NOT IN ('draft','discarded')
   OR (NEW.tenant_id,NEW.hospital_id,NEW.id,NEW.roster_id,NEW.staff_id,NEW.roster_date,NEW.shift_revision_id,NEW.area_revision_id,NEW.starts_at,NEW.ends_at,NEW.additional,NEW.added_version,NEW.added_revision_id,NEW.created_at)
   IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.id,OLD.roster_id,OLD.staff_id,OLD.roster_date,OLD.shift_revision_id,OLD.area_revision_id,OLD.starts_at,OLD.ends_at,OLD.additional,OLD.added_version,OLD.added_revision_id,OLD.created_at) THEN
  RAISE EXCEPTION 'allocations are immutable except for a single removal in the current draft version' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_roster_allocation_valid BEFORE INSERT OR UPDATE ON workforce_roster_allocations FOR EACH ROW EXECUTE FUNCTION workforce_roster_allocation_valid();

-- An additional allocation needs a primary on the same staff-date; checked at commit so batches can be ordered freely.
CREATE FUNCTION workforce_roster_additional_valid() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM workforce_roster_allocations a WHERE a.tenant_id=NEW.tenant_id AND a.hospital_id=NEW.hospital_id AND a.staff_id=NEW.staff_id AND a.roster_date=NEW.roster_date
    AND a.removed_version IS NULL AND a.additional
    AND NOT EXISTS(SELECT 1 FROM workforce_roster_allocations p WHERE p.tenant_id=a.tenant_id AND p.hospital_id=a.hospital_id AND p.staff_id=a.staff_id AND p.roster_date=a.roster_date AND p.removed_version IS NULL AND NOT p.additional)) THEN
  RAISE EXCEPTION 'an additional allocation requires a primary allocation on the same date' USING ERRCODE='23514';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER workforce_roster_additional_valid AFTER INSERT OR UPDATE ON workforce_roster_allocations DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION workforce_roster_additional_valid();

DO $$DECLARE tbl text;BEGIN
 FOREACH tbl IN ARRAY ARRAY['workforce_rosters','workforce_roster_revisions','workforce_roster_allocations'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',tbl);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',tbl);
  EXECUTE format('CREATE POLICY hospital_isolation ON %I USING (hms_tenant_visible(tenant_id) AND workforce_staff_allowed(tenant_id,hospital_id,false)) WITH CHECK(tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND workforce_staff_allowed(tenant_id,hospital_id,true) AND current_setting(''app.workforce_rostering'',true)=''authorized'')',tbl);
  IF tbl = 'workforce_roster_revisions' THEN
   EXECUTE format('CREATE TRIGGER workforce_roster_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION workforce_audit_immutable()',tbl);
  ELSE
   EXECUTE format('CREATE TRIGGER workforce_roster_no_delete BEFORE DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal()',tbl);
  END IF;
 END LOOP;
END $$;
