-- Attendance (#1039 slice 1): a versioned policy and append-only manual punches; derived attendance is computed on read.
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE workforce_attendance_settings (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL,
 early_arrival_minutes smallint NOT NULL CHECK (early_arrival_minutes BETWEEN 0 AND 240),
 late_departure_minutes smallint NOT NULL CHECK (late_departure_minutes BETWEEN 0 AND 720),
 early_departure_grace_minutes smallint NOT NULL CHECK (early_departure_grace_minutes BETWEEN 0 AND 240),
 short_duty_minutes smallint NOT NULL CHECK (short_duty_minutes BETWEEN 0 AND 720),
 duplicate_window_minutes smallint NOT NULL CHECK (duplicate_window_minutes BETWEEN 1 AND 60),
 backdate_days smallint NOT NULL CHECK (backdate_days BETWEEN 0 AND 92),
 effective_on date NOT NULL CHECK (effective_on BETWEEN DATE '2000-01-01' AND DATE '2199-12-31'),
 reviewed_by_subject text NOT NULL CHECK (length(reviewed_by_subject) BETWEEN 1 AND 200),
 version bigint NOT NULL CHECK (version > 0), revision_id uuid NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id),
 FOREIGN KEY(tenant_id,hospital_id) REFERENCES workforce_hospitals(tenant_id,id) ON DELETE RESTRICT
);
CREATE TABLE workforce_attendance_setting_revisions (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL,
 early_arrival_minutes smallint NOT NULL CHECK (early_arrival_minutes BETWEEN 0 AND 240),
 late_departure_minutes smallint NOT NULL CHECK (late_departure_minutes BETWEEN 0 AND 720),
 early_departure_grace_minutes smallint NOT NULL CHECK (early_departure_grace_minutes BETWEEN 0 AND 240),
 short_duty_minutes smallint NOT NULL CHECK (short_duty_minutes BETWEEN 0 AND 720),
 duplicate_window_minutes smallint NOT NULL CHECK (duplicate_window_minutes BETWEEN 1 AND 60),
 backdate_days smallint NOT NULL CHECK (backdate_days BETWEEN 0 AND 92),
 effective_on date NOT NULL CHECK (effective_on BETWEEN DATE '2000-01-01' AND DATE '2199-12-31'),
 reviewed_by_subject text NOT NULL CHECK (length(reviewed_by_subject) BETWEEN 1 AND 200),
 version bigint NOT NULL CHECK (version > 0), created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id,id), UNIQUE(tenant_id,hospital_id,version,id), UNIQUE(tenant_id,hospital_id,version),
 FOREIGN KEY(tenant_id,hospital_id) REFERENCES workforce_attendance_settings(tenant_id,hospital_id) ON DELETE RESTRICT
);
CREATE INDEX workforce_attendance_setting_history ON workforce_attendance_setting_revisions(tenant_id,hospital_id,created_at DESC,id DESC);
ALTER TABLE workforce_attendance_settings ADD CONSTRAINT workforce_attendance_setting_current
 FOREIGN KEY(tenant_id,hospital_id,version,revision_id) REFERENCES workforce_attendance_setting_revisions(tenant_id,hospital_id,version,id) DEFERRABLE INITIALLY DEFERRED;
CREATE TRIGGER workforce_attendance_settings_advance BEFORE UPDATE ON workforce_attendance_settings FOR EACH ROW EXECUTE FUNCTION workforce_roster_policy_advances();

CREATE FUNCTION workforce_attendance_policy_on(t uuid, h uuid, day date) RETURNS uuid LANGUAGE sql STABLE AS $$
 SELECT id FROM workforce_attendance_setting_revisions WHERE tenant_id=t AND hospital_id=h AND effective_on<=day ORDER BY effective_on DESC, version DESC LIMIT 1
$$;

CREATE TABLE workforce_attendance_punches (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL, staff_id uuid NOT NULL,
 direction text NOT NULL CHECK (direction IN ('in','out')),
 punched_at timestamptz NOT NULL,
 local_date date NOT NULL CHECK (local_date BETWEEN DATE '2000-01-01' AND DATE '2199-12-31'),
 timezone text NOT NULL, settings_revision_id uuid NOT NULL, policy_revision_id uuid NOT NULL,
 -- Only manual entry exists today; device and import sources arrive with their ingestion paths.
 source text NOT NULL CHECK (source IN ('manual')),
 reason text NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 500),
 recorded_by_subject text NOT NULL CHECK (length(recorded_by_subject) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT now(),
 CHECK (date_trunc('minute', punched_at) = punched_at),
 CHECK ((punched_at AT TIME ZONE timezone)::date = local_date),
 PRIMARY KEY(tenant_id,hospital_id,id),
 FOREIGN KEY(tenant_id,hospital_id,staff_id) REFERENCES workforce_staff(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,settings_revision_id) REFERENCES workforce_roster_setting_revisions(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,policy_revision_id) REFERENCES workforce_attendance_setting_revisions(tenant_id,hospital_id,id) ON DELETE RESTRICT
);
CREATE INDEX workforce_attendance_punches_staff ON workforce_attendance_punches(tenant_id,hospital_id,staff_id,punched_at);
CREATE INDEX workforce_attendance_punches_day ON workforce_attendance_punches(tenant_id,hospital_id,punched_at);

CREATE TABLE workforce_attendance_punch_voids (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, punch_id uuid NOT NULL,
 reason text NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 500),
 voided_by_subject text NOT NULL CHECK (length(voided_by_subject) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id,punch_id),
 FOREIGN KEY(tenant_id,hospital_id,punch_id) REFERENCES workforce_attendance_punches(tenant_id,hospital_id,id) ON DELETE RESTRICT
);

-- A punch pins the roster timezone and attendance policy of its local date, is not in the future, and is inside the back-dating window.
CREATE FUNCTION workforce_attendance_punch_valid() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE backdate smallint;
BEGIN
 SELECT backdate_days INTO backdate FROM workforce_attendance_setting_revisions WHERE tenant_id=NEW.tenant_id AND hospital_id=NEW.hospital_id AND id=NEW.policy_revision_id;
 IF NEW.settings_revision_id IS DISTINCT FROM workforce_roster_settings_on(NEW.tenant_id,NEW.hospital_id,NEW.local_date)
   OR NEW.timezone IS DISTINCT FROM (SELECT timezone FROM workforce_roster_setting_revisions WHERE tenant_id=NEW.tenant_id AND hospital_id=NEW.hospital_id AND id=NEW.settings_revision_id)
   OR NEW.policy_revision_id IS DISTINCT FROM workforce_attendance_policy_on(NEW.tenant_id,NEW.hospital_id,NEW.local_date)
   OR NEW.punched_at > clock_timestamp() + interval '5 minutes'
   OR NEW.local_date < (clock_timestamp() AT TIME ZONE NEW.timezone)::date - backdate THEN
  RAISE EXCEPTION 'punches pin the timezone and policy of their local date and fall inside the back-dating window' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_attendance_punch_valid BEFORE INSERT ON workforce_attendance_punches FOR EACH ROW EXECUTE FUNCTION workforce_attendance_punch_valid();

-- A void rewrites past attendance, so it is held to the back-dating window of the punch's own policy.
CREATE FUNCTION workforce_attendance_void_valid() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM workforce_attendance_punches p JOIN workforce_attendance_setting_revisions r ON (r.tenant_id,r.hospital_id,r.id)=(p.tenant_id,p.hospital_id,p.policy_revision_id)
   WHERE p.tenant_id=NEW.tenant_id AND p.hospital_id=NEW.hospital_id AND p.id=NEW.punch_id AND p.local_date >= (clock_timestamp() AT TIME ZONE p.timezone)::date - r.backdate_days) THEN
  RAISE EXCEPTION 'punches are voided inside their back-dating window' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_attendance_void_valid BEFORE INSERT ON workforce_attendance_punch_voids FOR EACH ROW EXECUTE FUNCTION workforce_attendance_void_valid();

DO $$DECLARE tbl text;BEGIN
 FOREACH tbl IN ARRAY ARRAY['workforce_attendance_settings','workforce_attendance_setting_revisions','workforce_attendance_punches','workforce_attendance_punch_voids'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',tbl);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',tbl);
  EXECUTE format('CREATE POLICY hospital_isolation ON %I USING (hms_tenant_visible(tenant_id) AND workforce_staff_allowed(tenant_id,hospital_id,false)) WITH CHECK(tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND workforce_staff_allowed(tenant_id,hospital_id,true) AND current_setting(''app.workforce_attendance'',true)=''authorized'')',tbl);
  IF tbl = 'workforce_attendance_settings' THEN
   EXECUTE format('CREATE TRIGGER workforce_attendance_no_delete BEFORE DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal()',tbl);
  ELSE
   EXECUTE format('CREATE TRIGGER workforce_attendance_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION workforce_audit_immutable()',tbl);
  END IF;
 END LOOP;
END $$;
