-- Attendance corrections (#1038 slice 3): separately approved requests that add, void or retime punches through the append-only punch tables.
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Whether a hospital date is inside a policy's back-dating window as seen at an instant; a missing policy is never inside.
CREATE FUNCTION workforce_attendance_in_window(t uuid, h uuid, policy uuid, day date, tz text, at timestamptz) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE((SELECT day >= (at AT TIME ZONE tz)::date - backdate_days FROM workforce_attendance_setting_revisions WHERE tenant_id=t AND hospital_id=h AND id=policy), false)
$$;

CREATE TABLE workforce_attendance_corrections (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL, staff_id uuid NOT NULL,
 kind text NOT NULL CHECK (kind IN ('add','void','replace')),
 target_punch_id uuid,
 direction text CHECK (direction IN ('in','out')),
 punched_at timestamptz,
 -- The corrected punch's date and pins, or the target's for a void.
 local_date date NOT NULL CHECK (local_date BETWEEN DATE '2000-01-01' AND DATE '2199-12-31'),
 timezone text NOT NULL, settings_revision_id uuid NOT NULL, policy_revision_id uuid NOT NULL,
 reason text NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 500),
 status text NOT NULL CHECK (status IN ('pending','approved','rejected','cancelled')),
 requested_by_subject text NOT NULL CHECK (length(requested_by_subject) BETWEEN 1 AND 200),
 decided_by_subject text CHECK (length(decided_by_subject) BETWEEN 1 AND 200), decided_at timestamptz,
 version bigint NOT NULL CHECK (version > 0),
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 CHECK ((kind = 'add') = (target_punch_id IS NULL)),
 CHECK ((kind = 'void') = (direction IS NULL)),
 CHECK ((direction IS NULL) = (punched_at IS NULL)),
 CHECK (punched_at IS NULL OR date_trunc('minute', punched_at) = punched_at),
 CHECK (punched_at IS NULL OR (punched_at AT TIME ZONE timezone)::date = local_date),
 CHECK ((decided_by_subject IS NULL) = (decided_at IS NULL)),
 CHECK ((status IN ('approved','rejected')) = (decided_by_subject IS NOT NULL)),
 CONSTRAINT workforce_attendance_correction_separation CHECK (decided_by_subject IS DISTINCT FROM requested_by_subject),
 PRIMARY KEY(tenant_id,hospital_id,id),
 FOREIGN KEY(tenant_id,hospital_id,staff_id) REFERENCES workforce_staff(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,target_punch_id) REFERENCES workforce_attendance_punches(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,settings_revision_id) REFERENCES workforce_roster_setting_revisions(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,policy_revision_id) REFERENCES workforce_attendance_setting_revisions(tenant_id,hospital_id,id) ON DELETE RESTRICT
);
CREATE INDEX workforce_attendance_corrections_page ON workforce_attendance_corrections(tenant_id,hospital_id,created_at DESC,id DESC);
CREATE INDEX workforce_attendance_corrections_staff ON workforce_attendance_corrections(tenant_id,hospital_id,staff_id,local_date);
CREATE UNIQUE INDEX workforce_attendance_correction_target ON workforce_attendance_corrections(tenant_id,hospital_id,target_punch_id)
 WHERE status IN ('pending','approved') AND target_punch_id IS NOT NULL;

CREATE TABLE workforce_attendance_correction_events (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL, correction_id uuid NOT NULL,
 version bigint NOT NULL CHECK (version > 0),
 status text NOT NULL CHECK (status IN ('pending','approved','rejected','cancelled')),
 note text NOT NULL DEFAULT '' CHECK (length(note) <= 500),
 actor_subject text NOT NULL CHECK (length(actor_subject) BETWEEN 1 AND 200), created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id,id), UNIQUE(tenant_id,hospital_id,correction_id,version),
 FOREIGN KEY(tenant_id,hospital_id,correction_id) REFERENCES workforce_attendance_corrections(tenant_id,hospital_id,id) ON DELETE RESTRICT
);
CREATE INDEX workforce_attendance_correction_events_page ON workforce_attendance_correction_events(tenant_id,hospital_id,correction_id,created_at DESC,id DESC);

ALTER TABLE workforce_attendance_punches ADD COLUMN correction_id uuid,
 DROP CONSTRAINT workforce_attendance_punches_source_check,
 ADD CONSTRAINT workforce_attendance_punches_source_check CHECK (source IN ('manual','correction')),
 ADD CONSTRAINT workforce_attendance_punch_correction CHECK ((source = 'correction') = (correction_id IS NOT NULL)),
 ADD CONSTRAINT workforce_attendance_punch_correction_once UNIQUE(tenant_id,hospital_id,correction_id),
 ADD CONSTRAINT workforce_attendance_punch_correction_fk FOREIGN KEY(tenant_id,hospital_id,correction_id) REFERENCES workforce_attendance_corrections(tenant_id,hospital_id,id) ON DELETE RESTRICT;
ALTER TABLE workforce_attendance_punch_voids ADD COLUMN correction_id uuid,
 ADD CONSTRAINT workforce_attendance_void_correction_once UNIQUE(tenant_id,hospital_id,correction_id),
 ADD CONSTRAINT workforce_attendance_void_correction_fk FOREIGN KEY(tenant_id,hospital_id,correction_id) REFERENCES workforce_attendance_corrections(tenant_id,hospital_id,id) ON DELETE RESTRICT;

-- Filed pending at version 1; the target is a live punch of the same person, and every affected date pins its own revisions and is inside its window.
CREATE FUNCTION workforce_attendance_correction_opens() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE target workforce_attendance_punches%ROWTYPE;
BEGIN
 IF NEW.status <> 'pending' OR NEW.version <> 1 OR NEW.decided_by_subject IS NOT NULL THEN
  RAISE EXCEPTION 'corrections are filed pending at version 1' USING ERRCODE='23514';
 END IF;
 IF NEW.target_punch_id IS NOT NULL THEN
  SELECT * INTO target FROM workforce_attendance_punches WHERE tenant_id=NEW.tenant_id AND hospital_id=NEW.hospital_id AND id=NEW.target_punch_id;
  IF target.staff_id IS DISTINCT FROM NEW.staff_id
    OR EXISTS(SELECT 1 FROM workforce_attendance_punch_voids v WHERE v.tenant_id=NEW.tenant_id AND v.hospital_id=NEW.hospital_id AND v.punch_id=NEW.target_punch_id)
    OR NOT workforce_attendance_in_window(target.tenant_id,target.hospital_id,target.policy_revision_id,target.local_date,target.timezone,clock_timestamp()) THEN
   RAISE EXCEPTION 'a correction targets a live punch of the same person inside its back-dating window' USING ERRCODE='23514';
  END IF;
 END IF;
 IF NEW.kind = 'void' THEN
  IF (NEW.local_date,NEW.timezone,NEW.settings_revision_id,NEW.policy_revision_id) IS DISTINCT FROM (target.local_date,target.timezone,target.settings_revision_id,target.policy_revision_id) THEN
   RAISE EXCEPTION 'a void correction carries its target punch''s date and revisions' USING ERRCODE='23514';
  END IF;
 ELSIF NEW.settings_revision_id IS DISTINCT FROM workforce_roster_settings_on(NEW.tenant_id,NEW.hospital_id,NEW.local_date)
   OR NEW.timezone IS DISTINCT FROM (SELECT timezone FROM workforce_roster_setting_revisions WHERE tenant_id=NEW.tenant_id AND hospital_id=NEW.hospital_id AND id=NEW.settings_revision_id)
   OR NEW.policy_revision_id IS DISTINCT FROM workforce_attendance_policy_on(NEW.tenant_id,NEW.hospital_id,NEW.local_date)
   OR NEW.punched_at > clock_timestamp() + interval '5 minutes'
   OR NOT workforce_attendance_in_window(NEW.tenant_id,NEW.hospital_id,NEW.policy_revision_id,NEW.local_date,NEW.timezone,clock_timestamp()) THEN
  RAISE EXCEPTION 'a corrected punch pins the timezone and policy of its local date and falls inside the back-dating window' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_attendance_correction_opens BEFORE INSERT ON workforce_attendance_corrections FOR EACH ROW EXECUTE FUNCTION workforce_attendance_correction_opens();

-- Filing facts never change, and a request leaves pending exactly once.
CREATE FUNCTION workforce_attendance_correction_advances() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.tenant_id,NEW.hospital_id,NEW.id,NEW.staff_id,NEW.kind,NEW.target_punch_id,NEW.direction,NEW.punched_at,NEW.local_date,NEW.timezone,NEW.settings_revision_id,NEW.policy_revision_id,NEW.reason,NEW.requested_by_subject,NEW.created_at)
   IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.id,OLD.staff_id,OLD.kind,OLD.target_punch_id,OLD.direction,OLD.punched_at,OLD.local_date,OLD.timezone,OLD.settings_revision_id,OLD.policy_revision_id,OLD.reason,OLD.requested_by_subject,OLD.created_at)
  OR NEW.version <> OLD.version + 1
  OR NOT (OLD.status = 'pending' AND NEW.status IN ('approved','rejected','cancelled')) THEN
  RAISE EXCEPTION 'attendance corrections leave pending once and keep their filing facts' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_attendance_correction_advances BEFORE UPDATE ON workforce_attendance_corrections FOR EACH ROW EXECUTE FUNCTION workforce_attendance_correction_advances();

CREATE FUNCTION workforce_attendance_correction_event_matches() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM workforce_attendance_corrections r WHERE r.tenant_id=NEW.tenant_id AND r.hospital_id=NEW.hospital_id AND r.id=NEW.correction_id
   AND r.version=NEW.version AND r.status=NEW.status) THEN
  RAISE EXCEPTION 'correction events must match the request state they record' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_attendance_correction_event_matches BEFORE INSERT ON workforce_attendance_correction_events FOR EACH ROW EXECUTE FUNCTION workforce_attendance_correction_event_matches();

-- Deferred to commit: every version has its event, and an approved request has exactly the punch and void its kind implies.
CREATE FUNCTION workforce_attendance_correction_complete() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM workforce_attendance_corrections r
   JOIN workforce_attendance_correction_events v ON (v.tenant_id,v.hospital_id,v.correction_id,v.version,v.status)=(r.tenant_id,r.hospital_id,r.id,r.version,r.status)
   WHERE r.tenant_id=NEW.tenant_id AND r.hospital_id=NEW.hospital_id AND r.id=NEW.id
   AND (r.status <> 'approved' OR (
    (r.kind = 'void') <> EXISTS(SELECT 1 FROM workforce_attendance_punches p WHERE (p.tenant_id,p.hospital_id,p.correction_id)=(r.tenant_id,r.hospital_id,r.id))
    AND (r.kind = 'add') <> EXISTS(SELECT 1 FROM workforce_attendance_punch_voids x WHERE (x.tenant_id,x.hospital_id,x.correction_id)=(r.tenant_id,r.hospital_id,r.id))))) THEN
  RAISE EXCEPTION 'every correction version needs its event and an approved correction its punch changes' USING ERRCODE='23514';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER workforce_attendance_correction_complete AFTER INSERT OR UPDATE ON workforce_attendance_corrections DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION workforce_attendance_correction_complete();

-- Manual punches keep slice 1's rule; a correction punch must match its approved request and is windowed from the filing time.
CREATE OR REPLACE FUNCTION workforce_attendance_punch_valid() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE backdate smallint; filed timestamptz := clock_timestamp();
BEGIN
 IF NEW.correction_id IS NOT NULL THEN
  SELECT c.created_at INTO filed FROM workforce_attendance_corrections c WHERE c.tenant_id=NEW.tenant_id AND c.hospital_id=NEW.hospital_id AND c.id=NEW.correction_id
   AND c.status='approved' AND c.kind IN ('add','replace') AND c.staff_id=NEW.staff_id AND c.direction=NEW.direction AND c.punched_at=NEW.punched_at;
  IF filed IS NULL THEN
   RAISE EXCEPTION 'a correction punch matches its approved add or replace request' USING ERRCODE='23514';
  END IF;
 END IF;
 SELECT backdate_days INTO backdate FROM workforce_attendance_setting_revisions WHERE tenant_id=NEW.tenant_id AND hospital_id=NEW.hospital_id AND id=NEW.policy_revision_id;
 IF NEW.settings_revision_id IS DISTINCT FROM workforce_roster_settings_on(NEW.tenant_id,NEW.hospital_id,NEW.local_date)
   OR NEW.timezone IS DISTINCT FROM (SELECT timezone FROM workforce_roster_setting_revisions WHERE tenant_id=NEW.tenant_id AND hospital_id=NEW.hospital_id AND id=NEW.settings_revision_id)
   OR NEW.policy_revision_id IS DISTINCT FROM workforce_attendance_policy_on(NEW.tenant_id,NEW.hospital_id,NEW.local_date)
   OR NEW.punched_at > clock_timestamp() + interval '5 minutes'
   OR backdate IS NULL OR NEW.local_date < (filed AT TIME ZONE NEW.timezone)::date - backdate THEN
  RAISE EXCEPTION 'punches pin the timezone and policy of their local date and fall inside the back-dating window' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION workforce_attendance_void_valid() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE filed timestamptz := clock_timestamp();
BEGIN
 IF NEW.correction_id IS NOT NULL THEN
  SELECT c.created_at INTO filed FROM workforce_attendance_corrections c WHERE c.tenant_id=NEW.tenant_id AND c.hospital_id=NEW.hospital_id AND c.id=NEW.correction_id
   AND c.status='approved' AND c.kind IN ('void','replace') AND c.target_punch_id=NEW.punch_id;
  IF filed IS NULL THEN
   RAISE EXCEPTION 'a correction void matches its approved void or replace request' USING ERRCODE='23514';
  END IF;
 END IF;
 IF NOT EXISTS(SELECT 1 FROM workforce_attendance_punches p WHERE p.tenant_id=NEW.tenant_id AND p.hospital_id=NEW.hospital_id AND p.id=NEW.punch_id
   AND workforce_attendance_in_window(p.tenant_id,p.hospital_id,p.policy_revision_id,p.local_date,p.timezone,filed)) THEN
  RAISE EXCEPTION 'punches are voided inside their back-dating window' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;

DO $$DECLARE tbl text;BEGIN
 FOREACH tbl IN ARRAY ARRAY['workforce_attendance_corrections','workforce_attendance_correction_events'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',tbl);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',tbl);
  EXECUTE format('CREATE POLICY hospital_isolation ON %I USING (hms_tenant_visible(tenant_id) AND workforce_staff_allowed(tenant_id,hospital_id,false)) WITH CHECK(tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND workforce_staff_allowed(tenant_id,hospital_id,true) AND current_setting(''app.workforce_attendance'',true)=''authorized'')',tbl);
  IF tbl = 'workforce_attendance_corrections' THEN
   EXECUTE format('CREATE TRIGGER workforce_attendance_correction_no_delete BEFORE DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal()',tbl);
  ELSE
   EXECUTE format('CREATE TRIGGER workforce_attendance_correction_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION workforce_audit_immutable()',tbl);
  END IF;
 END LOOP;
END $$;
