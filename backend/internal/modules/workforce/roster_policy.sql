SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE workforce_roster_settings (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL,
 timezone text NOT NULL CHECK (length(timezone) BETWEEN 1 AND 64),
 week_start smallint NOT NULL CHECK (week_start BETWEEN 0 AND 6),
 auto_population text NOT NULL CHECK (auto_population IN ('disabled','fixed_shift','all_assigned')),
 effective_on date NOT NULL CHECK (effective_on BETWEEN DATE '0001-01-01' AND DATE '9999-12-31'),
 reviewed_by_subject text NOT NULL CHECK (length(reviewed_by_subject) BETWEEN 1 AND 200),
 version bigint NOT NULL CHECK (version > 0), revision_id uuid NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id),
 FOREIGN KEY(tenant_id,hospital_id) REFERENCES workforce_hospitals(tenant_id,id) ON DELETE RESTRICT
);
CREATE TABLE workforce_roster_setting_revisions (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL,
 timezone text NOT NULL, week_start smallint NOT NULL CHECK (week_start BETWEEN 0 AND 6),
 auto_population text NOT NULL CHECK (auto_population IN ('disabled','fixed_shift','all_assigned')),
 effective_on date NOT NULL, reviewed_by_subject text NOT NULL,
 version bigint NOT NULL CHECK (version > 0), created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id,id), UNIQUE(tenant_id,hospital_id,version,id), UNIQUE(tenant_id,hospital_id,version),
 FOREIGN KEY(tenant_id,hospital_id) REFERENCES workforce_roster_settings(tenant_id,hospital_id) ON DELETE RESTRICT
);
CREATE INDEX workforce_roster_setting_history ON workforce_roster_setting_revisions(tenant_id,hospital_id,created_at DESC,id DESC);
ALTER TABLE workforce_roster_settings ADD CONSTRAINT workforce_roster_setting_current
 FOREIGN KEY(tenant_id,hospital_id,version,revision_id) REFERENCES workforce_roster_setting_revisions(tenant_id,hospital_id,version,id) DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE workforce_staff_roster_assignments (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, staff_id uuid NOT NULL,
 weekly_template_revision_id uuid NOT NULL, template_kind text GENERATED ALWAYS AS ('weekly_template') STORED,
 fixed boolean NOT NULL, active boolean NOT NULL,
 effective_on date NOT NULL CHECK (effective_on BETWEEN DATE '0001-01-01' AND DATE '9999-12-31'),
 reviewed_by_subject text NOT NULL CHECK (length(reviewed_by_subject) BETWEEN 1 AND 200),
 version bigint NOT NULL CHECK (version > 0), revision_id uuid NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id,staff_id),
 FOREIGN KEY(tenant_id,hospital_id,staff_id) REFERENCES workforce_staff(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,weekly_template_revision_id,template_kind) REFERENCES workforce_roster_config_revisions(tenant_id,hospital_id,id,kind) ON DELETE RESTRICT
);
CREATE INDEX workforce_staff_roster_assignment_page ON workforce_staff_roster_assignments(tenant_id,hospital_id,created_at DESC,staff_id DESC);
CREATE INDEX workforce_staff_roster_assignment_template ON workforce_staff_roster_assignments(tenant_id,hospital_id,weekly_template_revision_id);
CREATE TABLE workforce_staff_roster_assignment_revisions (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL, staff_id uuid NOT NULL,
 weekly_template_revision_id uuid NOT NULL, template_kind text GENERATED ALWAYS AS ('weekly_template') STORED,
 fixed boolean NOT NULL, active boolean NOT NULL, effective_on date NOT NULL, reviewed_by_subject text NOT NULL,
 version bigint NOT NULL CHECK (version > 0), created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id,id), UNIQUE(tenant_id,hospital_id,staff_id,version,id), UNIQUE(tenant_id,hospital_id,staff_id,version),
 FOREIGN KEY(tenant_id,hospital_id,staff_id) REFERENCES workforce_staff_roster_assignments(tenant_id,hospital_id,staff_id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,weekly_template_revision_id,template_kind) REFERENCES workforce_roster_config_revisions(tenant_id,hospital_id,id,kind) ON DELETE RESTRICT
);
CREATE INDEX workforce_staff_roster_assignment_history ON workforce_staff_roster_assignment_revisions(tenant_id,hospital_id,staff_id,created_at DESC,id DESC);
ALTER TABLE workforce_staff_roster_assignments ADD CONSTRAINT workforce_staff_roster_assignment_current
 FOREIGN KEY(tenant_id,hospital_id,staff_id,version,revision_id) REFERENCES workforce_staff_roster_assignment_revisions(tenant_id,hospital_id,staff_id,version,id) DEFERRABLE INITIALLY DEFERRED;

-- pg_timezone_names also lists posix/, right/ and abbreviation aliases; only canonical-style area names are admitted.
CREATE FUNCTION workforce_roster_timezone_valid() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.timezone !~ '^(UTC|[A-Z][A-Za-z_]+(/[A-Za-z0-9_+-]+){1,2})$' OR NOT EXISTS(SELECT 1 FROM pg_timezone_names WHERE name=NEW.timezone) THEN
  RAISE EXCEPTION 'roster timezone must be a supported IANA zone' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_roster_settings_timezone BEFORE INSERT OR UPDATE ON workforce_roster_settings FOR EACH ROW EXECUTE FUNCTION workforce_roster_timezone_valid();
CREATE TRIGGER workforce_roster_setting_revisions_timezone BEFORE INSERT ON workforce_roster_setting_revisions FOR EACH ROW EXECUTE FUNCTION workforce_roster_timezone_valid();

CREATE FUNCTION workforce_roster_policy_advances() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.tenant_id,NEW.hospital_id) IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id) OR NEW.version<>OLD.version+1
  OR (TG_TABLE_NAME='workforce_staff_roster_assignments' AND to_jsonb(NEW)->>'staff_id' IS DISTINCT FROM to_jsonb(OLD)->>'staff_id') THEN
  RAISE EXCEPTION 'roster policy identity is immutable and versions must advance' USING ERRCODE='23514';
 END IF;
 IF NEW.effective_on<OLD.effective_on THEN
  RAISE EXCEPTION 'roster policy cannot become effective before its current revision' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_roster_settings_advance BEFORE UPDATE ON workforce_roster_settings FOR EACH ROW EXECUTE FUNCTION workforce_roster_policy_advances();
CREATE TRIGGER workforce_staff_roster_assignments_advance BEFORE UPDATE ON workforce_staff_roster_assignments FOR EACH ROW EXECUTE FUNCTION workforce_roster_policy_advances();

DO $$DECLARE tbl text;BEGIN
 FOREACH tbl IN ARRAY ARRAY['workforce_roster_settings','workforce_roster_setting_revisions','workforce_staff_roster_assignments','workforce_staff_roster_assignment_revisions'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',tbl);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',tbl);
  EXECUTE format('CREATE POLICY hospital_isolation ON %I USING (hms_tenant_visible(tenant_id) AND workforce_staff_allowed(tenant_id,hospital_id,false)) WITH CHECK(tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND workforce_staff_allowed(tenant_id,hospital_id,true) AND current_setting(''app.workforce_roster_policy'',true)=''authorized'')',tbl);
  IF tbl LIKE '%revisions' THEN
   EXECUTE format('CREATE TRIGGER workforce_roster_policy_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION workforce_audit_immutable()',tbl);
  ELSE
   EXECUTE format('CREATE TRIGGER workforce_roster_policy_no_delete BEFORE DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal()',tbl);
  END IF;
 END LOOP;
END $$;
