SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- The leave-year boundary is immutable: every posted balance is anchored to it.
CREATE TABLE workforce_leave_settings (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL,
 year_start_month smallint NOT NULL CHECK (year_start_month BETWEEN 1 AND 12),
 reviewed_by_subject text NOT NULL CHECK (length(reviewed_by_subject) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id),
 FOREIGN KEY(tenant_id,hospital_id) REFERENCES workforce_hospitals(tenant_id,id) ON DELETE RESTRICT
);

ALTER TABLE workforce_leave_config_revisions ADD CONSTRAINT workforce_leave_revision_owner UNIQUE(tenant_id,hospital_id,id,config_id,kind);

CREATE TABLE workforce_staff_leave_assignments (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, staff_id uuid NOT NULL,
 structure_revision_id uuid NOT NULL, structure_kind text GENERATED ALWAYS AS ('structure') STORED,
 active boolean NOT NULL,
 effective_on date NOT NULL CHECK (effective_on BETWEEN DATE '2000-01-01' AND DATE '2199-12-31'),
 reviewed_by_subject text NOT NULL CHECK (length(reviewed_by_subject) BETWEEN 1 AND 200),
 version bigint NOT NULL CHECK (version > 0), revision_id uuid NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id,staff_id),
 FOREIGN KEY(tenant_id,hospital_id,staff_id) REFERENCES workforce_staff(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,structure_revision_id,structure_kind) REFERENCES workforce_leave_config_revisions(tenant_id,hospital_id,id,kind) ON DELETE RESTRICT
);
CREATE INDEX workforce_staff_leave_assignment_page ON workforce_staff_leave_assignments(tenant_id,hospital_id,created_at DESC,staff_id DESC);
CREATE TABLE workforce_staff_leave_assignment_revisions (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL, staff_id uuid NOT NULL,
 structure_revision_id uuid NOT NULL, structure_kind text GENERATED ALWAYS AS ('structure') STORED,
 active boolean NOT NULL, effective_on date NOT NULL CHECK (effective_on BETWEEN DATE '2000-01-01' AND DATE '2199-12-31'),
 reviewed_by_subject text NOT NULL, version bigint NOT NULL CHECK (version > 0), created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id,id), UNIQUE(tenant_id,hospital_id,staff_id,version,id), UNIQUE(tenant_id,hospital_id,staff_id,version),
 FOREIGN KEY(tenant_id,hospital_id,staff_id) REFERENCES workforce_staff_leave_assignments(tenant_id,hospital_id,staff_id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,structure_revision_id,structure_kind) REFERENCES workforce_leave_config_revisions(tenant_id,hospital_id,id,kind) ON DELETE RESTRICT
);
CREATE INDEX workforce_staff_leave_assignment_history ON workforce_staff_leave_assignment_revisions(tenant_id,hospital_id,staff_id,created_at DESC,id DESC);
ALTER TABLE workforce_staff_leave_assignments ADD CONSTRAINT workforce_staff_leave_assignment_current
 FOREIGN KEY(tenant_id,hospital_id,staff_id,version,revision_id) REFERENCES workforce_staff_leave_assignment_revisions(tenant_id,hospital_id,staff_id,version,id) DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE workforce_leave_years (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL, leave_year smallint NOT NULL CHECK (leave_year BETWEEN 2000 AND 2199),
 start_on date NOT NULL CHECK (extract(day FROM start_on)=1 AND extract(year FROM start_on)=leave_year),
 end_on date NOT NULL, posted_months smallint NOT NULL CHECK (posted_months BETWEEN 0 AND 12),
 closed boolean NOT NULL, closed_by_subject text, closed_at timestamptz,
 version bigint NOT NULL CHECK (version > 0),
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 CHECK (end_on = (start_on + interval '1 year' - interval '1 day')::date),
 CHECK (NOT closed OR posted_months = 12),
 CHECK (closed = (closed_by_subject IS NOT NULL)), CHECK (closed = (closed_at IS NOT NULL)),
 PRIMARY KEY(tenant_id,hospital_id,leave_year), UNIQUE(tenant_id,hospital_id,id),
 FOREIGN KEY(tenant_id,hospital_id) REFERENCES workforce_leave_settings(tenant_id,hospital_id) ON DELETE RESTRICT
);

-- Step 1-12 posts that month of the leave year; step 13 is the year-end close.
CREATE TABLE workforce_leave_runs (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL, leave_year smallint NOT NULL,
 step smallint NOT NULL CHECK (step BETWEEN 1 AND 13), entries integer NOT NULL CHECK (entries >= 0),
 actor_subject text NOT NULL CHECK (length(actor_subject) BETWEEN 1 AND 200), created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id,id), UNIQUE(tenant_id,hospital_id,leave_year,step),
 FOREIGN KEY(tenant_id,hospital_id,leave_year) REFERENCES workforce_leave_years(tenant_id,hospital_id,leave_year) ON DELETE RESTRICT
);

CREATE TABLE workforce_leave_ledger (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL, staff_id uuid NOT NULL,
 leave_type_id uuid NOT NULL, leave_type_revision_id uuid NOT NULL, type_kind text GENERATED ALWAYS AS ('leave_type') STORED,
 leave_year smallint NOT NULL,
 kind text NOT NULL CHECK (kind IN ('allocation','accrual','carry_in','carry_out','expiry','correction')),
 half_days integer NOT NULL CHECK (half_days BETWEEN -1460 AND 1460),
 effective_on date NOT NULL, idempotency_key text NOT NULL CHECK (length(idempotency_key) BETWEEN 1 AND 200),
 assignment_revision_id uuid, run_id uuid,
 reason text NOT NULL DEFAULT '' CHECK (length(reason) <= 500),
 actor_subject text NOT NULL CHECK (length(actor_subject) BETWEEN 1 AND 200), created_at timestamptz NOT NULL DEFAULT now(),
 CHECK ((kind IN ('allocation','accrual','carry_in') AND half_days > 0) OR (kind IN ('carry_out','expiry') AND half_days < 0) OR (kind = 'correction' AND half_days <> 0)),
 CHECK ((kind = 'correction') = (run_id IS NULL)),
 CHECK ((kind = 'correction') = (length(btrim(reason)) >= 10)),
 CHECK ((kind IN ('allocation','accrual')) = (assignment_revision_id IS NOT NULL)),
 PRIMARY KEY(tenant_id,hospital_id,id), UNIQUE(tenant_id,hospital_id,idempotency_key),
 FOREIGN KEY(tenant_id,hospital_id,staff_id) REFERENCES workforce_staff(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,leave_type_revision_id,leave_type_id,type_kind) REFERENCES workforce_leave_config_revisions(tenant_id,hospital_id,id,config_id,kind) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,leave_year) REFERENCES workforce_leave_years(tenant_id,hospital_id,leave_year) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,run_id) REFERENCES workforce_leave_runs(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,assignment_revision_id) REFERENCES workforce_staff_leave_assignment_revisions(tenant_id,hospital_id,id) ON DELETE RESTRICT
);
CREATE INDEX workforce_leave_ledger_balance ON workforce_leave_ledger(tenant_id,hospital_id,leave_year,staff_id,leave_type_id,effective_on);
CREATE INDEX workforce_leave_ledger_page ON workforce_leave_ledger(tenant_id,hospital_id,staff_id,leave_year,created_at DESC,id DESC);

-- Entries land only in an open year, inside its dates, against a balance-tracked type revision.
CREATE FUNCTION workforce_leave_entry_valid() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM workforce_leave_years y WHERE y.tenant_id=NEW.tenant_id AND y.hospital_id=NEW.hospital_id AND y.leave_year=NEW.leave_year
   AND NOT y.closed AND NEW.effective_on BETWEEN y.start_on AND y.end_on) THEN
  RAISE EXCEPTION 'leave entries must fall inside an open leave year' USING ERRCODE='23514';
 END IF;
 IF NOT EXISTS(SELECT 1 FROM workforce_leave_type_rules r WHERE r.tenant_id=NEW.tenant_id AND r.hospital_id=NEW.hospital_id AND r.revision_id=NEW.leave_type_revision_id AND r.balance_tracked) THEN
  RAISE EXCEPTION 'leave entries require a balance-tracked leave type' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_leave_entry_valid BEFORE INSERT ON workforce_leave_ledger FOR EACH ROW EXECUTE FUNCTION workforce_leave_entry_valid();

-- Last day already credited by any posting run; assignment history up to it is frozen.
CREATE FUNCTION workforce_leave_posted_through(row_tenant uuid, row_hospital uuid) RETURNS date LANGUAGE sql STABLE AS $$
 SELECT max((y.start_on + make_interval(months => y.posted_months) - interval '1 day')::date)
 FROM workforce_leave_years y WHERE y.tenant_id=row_tenant AND y.hospital_id=row_hospital AND y.posted_months > 0
$$;

CREATE FUNCTION workforce_leave_assignment_open() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 -- A first assignment may start in posted months (the next run catches it up), never in a closed year.
 IF NEW.version > 1 AND NEW.effective_on <= workforce_leave_posted_through(NEW.tenant_id,NEW.hospital_id) THEN
  RAISE EXCEPTION 'leave assignment changes must start after the last posted month' USING ERRCODE='23514';
 END IF;
 IF EXISTS(SELECT 1 FROM workforce_leave_years y WHERE y.tenant_id=NEW.tenant_id AND y.hospital_id=NEW.hospital_id AND y.closed AND y.end_on >= NEW.effective_on) THEN
  RAISE EXCEPTION 'leave assignments cannot start in a closed leave year' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_leave_assignment_open BEFORE INSERT ON workforce_staff_leave_assignment_revisions FOR EACH ROW EXECUTE FUNCTION workforce_leave_assignment_open();

CREATE FUNCTION workforce_leave_assignment_advances() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.tenant_id,NEW.hospital_id,NEW.staff_id) IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.staff_id) OR NEW.version<>OLD.version+1 OR NEW.effective_on<OLD.effective_on THEN
  RAISE EXCEPTION 'leave assignment identity is immutable and revisions must advance' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_staff_leave_assignments_advance BEFORE UPDATE ON workforce_staff_leave_assignments FOR EACH ROW EXECUTE FUNCTION workforce_leave_assignment_advances();

-- A year only moves forward: one more posted month, or a close after all twelve.
CREATE FUNCTION workforce_leave_year_advances() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.tenant_id,NEW.hospital_id,NEW.leave_year,NEW.start_on,NEW.end_on) IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.leave_year,OLD.start_on,OLD.end_on)
  OR NEW.id<>OLD.id
  OR NEW.version<>OLD.version+1 OR OLD.closed
  OR NOT ((NEW.posted_months=OLD.posted_months+1 AND NOT NEW.closed) OR (NEW.posted_months=12 AND OLD.posted_months=12 AND NEW.closed)) THEN
  RAISE EXCEPTION 'leave years advance one posted month at a time and close once' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_leave_years_advance BEFORE UPDATE ON workforce_leave_years FOR EACH ROW EXECUTE FUNCTION workforce_leave_year_advances();

DO $$DECLARE tbl text;BEGIN
 FOREACH tbl IN ARRAY ARRAY['workforce_leave_settings','workforce_staff_leave_assignments','workforce_staff_leave_assignment_revisions','workforce_leave_years','workforce_leave_runs','workforce_leave_ledger'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',tbl);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',tbl);
  EXECUTE format('CREATE POLICY hospital_isolation ON %I USING (hms_tenant_visible(tenant_id) AND workforce_staff_allowed(tenant_id,hospital_id,false)) WITH CHECK(tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND workforce_staff_allowed(tenant_id,hospital_id,true) AND current_setting(%L,true)=''authorized'')',
   tbl, CASE WHEN tbl='workforce_leave_settings' THEN 'app.workforce_leave' ELSE 'app.workforce_leave_ledger' END);
  IF tbl IN ('workforce_staff_leave_assignments','workforce_leave_years') THEN
   EXECUTE format('CREATE TRIGGER workforce_leave_no_delete BEFORE DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal()',tbl);
  ELSE
   EXECUTE format('CREATE TRIGGER workforce_leave_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION workforce_audit_immutable()',tbl);
  END IF;
 END LOOP;
END $$;
