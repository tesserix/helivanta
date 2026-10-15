SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE workforce_leave_configs (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL,
 kind text NOT NULL CHECK (kind IN ('leave_type','holiday_calendar','structure')),
 code text NOT NULL CHECK (code ~ '^[A-Z0-9_-]{1,40}$'),
 name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 80),
 description text NOT NULL CHECK (length(description) <= 500), active boolean NOT NULL,
 calendar_year smallint CHECK (calendar_year BETWEEN 2000 AND 2200),
 effective_on date NOT NULL CHECK (effective_on BETWEEN DATE '0001-01-01' AND DATE '9999-12-31'),
 reviewed_by_subject text NOT NULL CHECK (length(reviewed_by_subject) BETWEEN 1 AND 200),
 version bigint NOT NULL CHECK (version > 0), revision_id uuid NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 CHECK ((kind='holiday_calendar') = (calendar_year IS NOT NULL)),
 CHECK (calendar_year IS NULL OR effective_on <= make_date(calendar_year,1,1)),
 PRIMARY KEY(tenant_id,hospital_id,id), UNIQUE(tenant_id,hospital_id,id,kind,code), UNIQUE(tenant_id,hospital_id,kind,code),
 FOREIGN KEY(tenant_id,hospital_id) REFERENCES workforce_hospitals(tenant_id,id) ON DELETE RESTRICT
);
CREATE UNIQUE INDEX workforce_leave_config_name_unique ON workforce_leave_configs(tenant_id,hospital_id,kind,lower(name));
CREATE UNIQUE INDEX workforce_leave_calendar_year_unique ON workforce_leave_configs(tenant_id,hospital_id,calendar_year) WHERE kind='holiday_calendar';
CREATE INDEX workforce_leave_configs_page ON workforce_leave_configs(tenant_id,hospital_id,kind,created_at DESC,id DESC);

CREATE TABLE workforce_leave_config_revisions (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL, config_id uuid NOT NULL,
 kind text NOT NULL CHECK (kind IN ('leave_type','holiday_calendar','structure')), code text NOT NULL, name text NOT NULL, description text NOT NULL, active boolean NOT NULL,
 calendar_year smallint CHECK (calendar_year BETWEEN 2000 AND 2200),
 effective_on date NOT NULL, reviewed_by_subject text NOT NULL,
 version bigint NOT NULL CHECK (version > 0), created_at timestamptz NOT NULL DEFAULT now(), created_tx bigint NOT NULL DEFAULT txid_current(),
 CHECK ((kind='holiday_calendar') = (calendar_year IS NOT NULL)),
 CHECK (calendar_year IS NULL OR effective_on <= make_date(calendar_year,1,1)),
 PRIMARY KEY(tenant_id,hospital_id,id), UNIQUE(tenant_id,hospital_id,id,kind),
 UNIQUE(tenant_id,hospital_id,config_id,version,id), UNIQUE(tenant_id,hospital_id,config_id,version),
 FOREIGN KEY(tenant_id,hospital_id,config_id,kind,code) REFERENCES workforce_leave_configs(tenant_id,hospital_id,id,kind,code) ON DELETE RESTRICT
);
CREATE INDEX workforce_leave_config_history ON workforce_leave_config_revisions(tenant_id,hospital_id,config_id,created_at DESC,id DESC);
ALTER TABLE workforce_leave_configs ADD CONSTRAINT workforce_leave_config_current
 FOREIGN KEY(tenant_id,hospital_id,id,version,revision_id) REFERENCES workforce_leave_config_revisions(tenant_id,hospital_id,config_id,version,id) DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE workforce_leave_type_rules (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, revision_id uuid NOT NULL,
 kind text GENERATED ALWAYS AS ('leave_type') STORED,
 paid boolean NOT NULL, balance_tracked boolean NOT NULL, half_day_allowed boolean NOT NULL,
 short_leave_minutes smallint NOT NULL CHECK (short_leave_minutes BETWEEN 0 AND 480 AND short_leave_minutes % 15 = 0),
 clubbable boolean NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id,revision_id),
 FOREIGN KEY(tenant_id,hospital_id,revision_id,kind) REFERENCES workforce_leave_config_revisions(tenant_id,hospital_id,id,kind) ON DELETE RESTRICT
);
CREATE TABLE workforce_leave_holidays (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, revision_id uuid NOT NULL,
 kind text GENERATED ALWAYS AS ('holiday_calendar') STORED,
 holiday_date date NOT NULL, name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 80),
 category text NOT NULL CHECK (category IN ('regular','government','optional')),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id,revision_id,holiday_date),
 FOREIGN KEY(tenant_id,hospital_id,revision_id,kind) REFERENCES workforce_leave_config_revisions(tenant_id,hospital_id,id,kind) ON DELETE RESTRICT
);
CREATE TABLE workforce_leave_structure_entries (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, revision_id uuid NOT NULL,
 kind text GENERATED ALWAYS AS ('structure') STORED,
 position smallint NOT NULL CHECK (position BETWEEN 0 AND 29),
 leave_type_revision_id uuid NOT NULL, type_kind text GENERATED ALWAYS AS ('leave_type') STORED,
 annual_half_days smallint NOT NULL CHECK (annual_half_days BETWEEN 0 AND 730),
 accrual text NOT NULL CHECK (accrual IN ('upfront','monthly')),
 carry_forward_half_days smallint NOT NULL CHECK (carry_forward_half_days BETWEEN 0 AND 730),
 proration text NOT NULL CHECK (proration IN ('none','join_month')),
 probation_eligible boolean NOT NULL, created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id,revision_id,position), UNIQUE(tenant_id,hospital_id,revision_id,leave_type_revision_id),
 FOREIGN KEY(tenant_id,hospital_id,revision_id,kind) REFERENCES workforce_leave_config_revisions(tenant_id,hospital_id,id,kind) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,leave_type_revision_id,type_kind) REFERENCES workforce_leave_config_revisions(tenant_id,hospital_id,id,kind) ON DELETE RESTRICT
);
CREATE INDEX workforce_leave_structure_type ON workforce_leave_structure_entries(tenant_id,hospital_id,leave_type_revision_id);

CREATE FUNCTION workforce_leave_child_creation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM workforce_leave_config_revisions r WHERE r.tenant_id=NEW.tenant_id AND r.hospital_id=NEW.hospital_id AND r.id=NEW.revision_id AND r.created_tx=txid_current()) THEN
  RAISE EXCEPTION 'leave children must be inserted in their revision creating transaction' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;

-- Deferred so a revision and its children are checked together at commit.
CREATE FUNCTION workforce_leave_aggregate_valid() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.calendar_year IS DISTINCT FROM (SELECT c.calendar_year FROM workforce_leave_configs c WHERE c.tenant_id=NEW.tenant_id AND c.hospital_id=NEW.hospital_id AND c.id=NEW.config_id) THEN
  RAISE EXCEPTION 'leave revision calendar year must match its configuration' USING ERRCODE='23514';
 END IF;
 IF NEW.kind='leave_type' AND NOT EXISTS(SELECT 1 FROM workforce_leave_type_rules t WHERE t.tenant_id=NEW.tenant_id AND t.hospital_id=NEW.hospital_id AND t.revision_id=NEW.id) THEN
  RAISE EXCEPTION 'leave type requires rules' USING ERRCODE='23514';
 END IF;
 IF NEW.kind='holiday_calendar' AND EXISTS(SELECT 1 FROM workforce_leave_holidays h WHERE h.tenant_id=NEW.tenant_id AND h.hospital_id=NEW.hospital_id AND h.revision_id=NEW.id AND extract(year FROM h.holiday_date)<>NEW.calendar_year) THEN
  RAISE EXCEPTION 'holidays must fall inside the calendar year' USING ERRCODE='23514';
 END IF;
 IF NEW.kind='structure' AND (
  NOT EXISTS(SELECT 1 FROM workforce_leave_structure_entries e WHERE e.tenant_id=NEW.tenant_id AND e.hospital_id=NEW.hospital_id AND e.revision_id=NEW.id)
  OR EXISTS(SELECT 1 FROM workforce_leave_structure_entries e JOIN workforce_leave_config_revisions t ON (t.tenant_id,t.hospital_id,t.id)=(e.tenant_id,e.hospital_id,e.leave_type_revision_id)
   JOIN workforce_leave_type_rules r ON (r.tenant_id,r.hospital_id,r.revision_id)=(t.tenant_id,t.hospital_id,t.id)
   WHERE e.tenant_id=NEW.tenant_id AND e.hospital_id=NEW.hospital_id AND e.revision_id=NEW.id
    AND (t.effective_on>NEW.effective_on OR (NOT r.balance_tracked AND (e.annual_half_days<>0 OR e.carry_forward_half_days<>0))))
  OR EXISTS(SELECT 1 FROM workforce_leave_structure_entries e JOIN workforce_leave_config_revisions t ON (t.tenant_id,t.hospital_id,t.id)=(e.tenant_id,e.hospital_id,e.leave_type_revision_id)
   WHERE e.tenant_id=NEW.tenant_id AND e.hospital_id=NEW.hospital_id AND e.revision_id=NEW.id GROUP BY t.config_id HAVING count(*)>1)
 ) THEN
  RAISE EXCEPTION 'leave structure has no entries, duplicate types, later types or untracked allowances' USING ERRCODE='23514';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER workforce_leave_aggregate_valid AFTER INSERT ON workforce_leave_config_revisions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION workforce_leave_aggregate_valid();

CREATE FUNCTION workforce_leave_stable_identity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.tenant_id,NEW.hospital_id,NEW.id,NEW.kind,NEW.code,NEW.calendar_year) IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.id,OLD.kind,OLD.code,OLD.calendar_year) OR NEW.version<>OLD.version+1 THEN
  RAISE EXCEPTION 'leave configuration identity is immutable and versions must advance' USING ERRCODE='23514';
 END IF;
 IF NEW.effective_on<OLD.effective_on THEN
  RAISE EXCEPTION 'leave configuration cannot become effective before its current revision' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_leave_stable_identity BEFORE UPDATE ON workforce_leave_configs FOR EACH ROW EXECUTE FUNCTION workforce_leave_stable_identity();
CREATE TRIGGER workforce_leave_no_delete BEFORE DELETE OR TRUNCATE ON workforce_leave_configs FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal();

DO $$DECLARE tbl text;BEGIN
 FOREACH tbl IN ARRAY ARRAY['workforce_leave_configs','workforce_leave_config_revisions','workforce_leave_type_rules','workforce_leave_holidays','workforce_leave_structure_entries'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',tbl);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',tbl);
  EXECUTE format('CREATE POLICY hospital_isolation ON %I USING (hms_tenant_visible(tenant_id) AND workforce_staff_allowed(tenant_id,hospital_id,false)) WITH CHECK(tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND workforce_staff_allowed(tenant_id,hospital_id,true) AND current_setting(''app.workforce_leave'',true)=''authorized'')',tbl);
  IF tbl<>'workforce_leave_configs' THEN
   EXECUTE format('CREATE TRIGGER workforce_leave_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION workforce_audit_immutable()',tbl);
  END IF;
  IF tbl IN ('workforce_leave_type_rules','workforce_leave_holidays','workforce_leave_structure_entries') THEN
   EXECUTE format('CREATE TRIGGER workforce_leave_child_creation BEFORE INSERT ON %I FOR EACH ROW EXECUTE FUNCTION workforce_leave_child_creation()',tbl);
  END IF;
 END LOOP;
END $$;
