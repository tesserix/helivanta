-- Overtime (#1038 slice 2): a versioned hospital limit policy and separately approved requests; only approved minutes are payable.
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE workforce_overtime_settings (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL,
 daily_limit_minutes integer NOT NULL CHECK (daily_limit_minutes BETWEEN 1 AND 1440),
 weekly_limit_minutes integer NOT NULL CHECK (weekly_limit_minutes BETWEEN 1 AND 10080),
 monthly_limit_minutes integer NOT NULL CHECK (monthly_limit_minutes BETWEEN 1 AND 44640),
 filing_window_days smallint NOT NULL CHECK (filing_window_days BETWEEN 0 AND 92),
 effective_on date NOT NULL CHECK (effective_on BETWEEN DATE '2000-01-01' AND DATE '2199-12-31'),
 reviewed_by_subject text NOT NULL CHECK (length(reviewed_by_subject) BETWEEN 1 AND 200),
 version bigint NOT NULL CHECK (version > 0), revision_id uuid NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 CHECK (daily_limit_minutes <= weekly_limit_minutes AND weekly_limit_minutes <= monthly_limit_minutes),
 PRIMARY KEY(tenant_id,hospital_id),
 FOREIGN KEY(tenant_id,hospital_id) REFERENCES workforce_hospitals(tenant_id,id) ON DELETE RESTRICT
);
CREATE TABLE workforce_overtime_setting_revisions (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL,
 daily_limit_minutes integer NOT NULL CHECK (daily_limit_minutes BETWEEN 1 AND 1440),
 weekly_limit_minutes integer NOT NULL CHECK (weekly_limit_minutes BETWEEN 1 AND 10080),
 monthly_limit_minutes integer NOT NULL CHECK (monthly_limit_minutes BETWEEN 1 AND 44640),
 filing_window_days smallint NOT NULL CHECK (filing_window_days BETWEEN 0 AND 92),
 effective_on date NOT NULL CHECK (effective_on BETWEEN DATE '2000-01-01' AND DATE '2199-12-31'),
 reviewed_by_subject text NOT NULL CHECK (length(reviewed_by_subject) BETWEEN 1 AND 200),
 version bigint NOT NULL CHECK (version > 0), created_at timestamptz NOT NULL DEFAULT now(),
 CHECK (daily_limit_minutes <= weekly_limit_minutes AND weekly_limit_minutes <= monthly_limit_minutes),
 PRIMARY KEY(tenant_id,hospital_id,id), UNIQUE(tenant_id,hospital_id,version,id), UNIQUE(tenant_id,hospital_id,version),
 FOREIGN KEY(tenant_id,hospital_id) REFERENCES workforce_overtime_settings(tenant_id,hospital_id) ON DELETE RESTRICT
);
CREATE INDEX workforce_overtime_setting_history ON workforce_overtime_setting_revisions(tenant_id,hospital_id,created_at DESC,id DESC);
ALTER TABLE workforce_overtime_settings ADD CONSTRAINT workforce_overtime_setting_current
 FOREIGN KEY(tenant_id,hospital_id,version,revision_id) REFERENCES workforce_overtime_setting_revisions(tenant_id,hospital_id,version,id) DEFERRABLE INITIALLY DEFERRED;
CREATE TRIGGER workforce_overtime_settings_advance BEFORE UPDATE ON workforce_overtime_settings FOR EACH ROW EXECUTE FUNCTION workforce_roster_policy_advances();

-- The revision governing a date: latest effective on or before it, ties to the higher version.
CREATE FUNCTION workforce_overtime_policy_on(t uuid, h uuid, day date) RETURNS uuid LANGUAGE sql STABLE AS $$
 SELECT id FROM workforce_overtime_setting_revisions WHERE tenant_id=t AND hospital_id=h AND effective_on<=day ORDER BY effective_on DESC, version DESC LIMIT 1
$$;
CREATE FUNCTION workforce_roster_settings_on(t uuid, h uuid, day date) RETURNS uuid LANGUAGE sql STABLE AS $$
 SELECT id FROM workforce_roster_setting_revisions WHERE tenant_id=t AND hospital_id=h AND effective_on<=day ORDER BY effective_on DESC, version DESC LIMIT 1
$$;

CREATE TABLE workforce_overtime_requests (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL, staff_id uuid NOT NULL,
 work_date date NOT NULL CHECK (work_date BETWEEN DATE '2000-01-01' AND DATE '2199-12-31'),
 start_time text NOT NULL CHECK (start_time ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$'),
 end_time text NOT NULL CHECK (end_time ~ '^([01][0-9]|2[0-3]):[0-5][0-9]$'),
 settings_revision_id uuid NOT NULL, timezone text NOT NULL,
 starts_at timestamptz NOT NULL, ends_at timestamptz NOT NULL,
 requested_minutes integer GENERATED ALWAYS AS ((extract(epoch FROM ends_at - starts_at) / 60)::integer) STORED,
 approved_minutes integer, policy_revision_id uuid,
 reason text NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 500),
 status text NOT NULL CHECK (status IN ('pending','approved','rejected','cancelled','revoked')),
 requested_by_subject text NOT NULL CHECK (length(requested_by_subject) BETWEEN 1 AND 200),
 decided_by_subject text CHECK (length(decided_by_subject) BETWEEN 1 AND 200), decided_at timestamptz,
 revoked_by_subject text CHECK (length(revoked_by_subject) BETWEEN 1 AND 200),
 version bigint NOT NULL CHECK (version > 0),
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 -- Instants are the hospital wall-clock times on the work date; an end at or before the start is the next day.
 CHECK ((starts_at AT TIME ZONE timezone) = work_date + start_time::time),
 CHECK ((ends_at AT TIME ZONE timezone) = work_date + (CASE WHEN end_time <= start_time THEN 1 ELSE 0 END) + end_time::time),
 CHECK (ends_at - starts_at BETWEEN interval '15 minutes' AND interval '24 hours'),
 CHECK ((status IN ('approved','revoked')) = (approved_minutes IS NOT NULL)),
 CHECK (approved_minutes IS NULL OR approved_minutes BETWEEN 1 AND requested_minutes),
 CHECK ((status IN ('approved','revoked')) = (policy_revision_id IS NOT NULL)),
 CHECK ((decided_by_subject IS NULL) = (decided_at IS NULL)),
 CHECK ((status IN ('approved','rejected','revoked')) = (decided_by_subject IS NOT NULL)),
 CHECK ((status = 'revoked') = (revoked_by_subject IS NOT NULL)),
 CONSTRAINT workforce_overtime_separation CHECK (decided_by_subject IS DISTINCT FROM requested_by_subject AND revoked_by_subject IS DISTINCT FROM requested_by_subject),
 CONSTRAINT workforce_overtime_overlap EXCLUDE USING gist (tenant_id WITH =, hospital_id WITH =, staff_id WITH =, tstzrange(starts_at,ends_at,'[)') WITH &&) WHERE (status IN ('pending','approved')),
 PRIMARY KEY(tenant_id,hospital_id,id),
 FOREIGN KEY(tenant_id,hospital_id,staff_id) REFERENCES workforce_staff(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,settings_revision_id) REFERENCES workforce_roster_setting_revisions(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,policy_revision_id) REFERENCES workforce_overtime_setting_revisions(tenant_id,hospital_id,id) ON DELETE RESTRICT
);
CREATE INDEX workforce_overtime_requests_page ON workforce_overtime_requests(tenant_id,hospital_id,created_at DESC,id DESC);
CREATE INDEX workforce_overtime_requests_staff ON workforce_overtime_requests(tenant_id,hospital_id,staff_id,work_date);
CREATE INDEX workforce_overtime_requests_month ON workforce_overtime_requests(tenant_id,hospital_id,work_date) WHERE status IN ('pending','approved');

CREATE TABLE workforce_overtime_events (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL, request_id uuid NOT NULL,
 version bigint NOT NULL CHECK (version > 0),
 status text NOT NULL CHECK (status IN ('pending','approved','rejected','cancelled','revoked')),
 approved_minutes integer,
 note text NOT NULL DEFAULT '' CHECK (length(note) <= 500),
 actor_subject text NOT NULL CHECK (length(actor_subject) BETWEEN 1 AND 200), created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id,id), UNIQUE(tenant_id,hospital_id,request_id,version),
 FOREIGN KEY(tenant_id,hospital_id,request_id) REFERENCES workforce_overtime_requests(tenant_id,hospital_id,id) ON DELETE RESTRICT
);
CREATE INDEX workforce_overtime_events_page ON workforce_overtime_events(tenant_id,hospital_id,request_id,created_at DESC,id DESC);

-- Filed pending at version 1, in the timezone of the roster settings in force on the work date.
CREATE FUNCTION workforce_overtime_opens() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.status <> 'pending' OR NEW.version <> 1 OR NEW.decided_by_subject IS NOT NULL
   OR NEW.settings_revision_id IS DISTINCT FROM workforce_roster_settings_on(NEW.tenant_id,NEW.hospital_id,NEW.work_date)
   OR NEW.timezone IS DISTINCT FROM (SELECT timezone FROM workforce_roster_setting_revisions WHERE tenant_id=NEW.tenant_id AND hospital_id=NEW.hospital_id AND id=NEW.settings_revision_id) THEN
  RAISE EXCEPTION 'overtime is filed pending at version 1 in the hospital timezone in force on the work date' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_overtime_opens BEFORE INSERT ON workforce_overtime_requests FOR EACH ROW EXECUTE FUNCTION workforce_overtime_opens();

-- Filing facts never change; status moves one allowed step, approval pins the policy in force, revocation keeps the decision.
CREATE FUNCTION workforce_overtime_advances() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.tenant_id,NEW.hospital_id,NEW.id,NEW.staff_id,NEW.work_date,NEW.start_time,NEW.end_time,NEW.settings_revision_id,NEW.timezone,NEW.starts_at,NEW.ends_at,NEW.reason,NEW.requested_by_subject,NEW.created_at)
   IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.id,OLD.staff_id,OLD.work_date,OLD.start_time,OLD.end_time,OLD.settings_revision_id,OLD.timezone,OLD.starts_at,OLD.ends_at,OLD.reason,OLD.requested_by_subject,OLD.created_at)
  OR NEW.version <> OLD.version + 1
  OR NOT ((OLD.status = 'pending' AND NEW.status IN ('approved','rejected','cancelled')) OR (OLD.status = 'approved' AND NEW.status = 'revoked'))
  OR (NEW.status = 'approved' AND NEW.policy_revision_id IS DISTINCT FROM workforce_overtime_policy_on(NEW.tenant_id,NEW.hospital_id,NEW.work_date))
  OR (OLD.status = 'approved' AND (NEW.decided_by_subject,NEW.decided_at,NEW.approved_minutes,NEW.policy_revision_id)
      IS DISTINCT FROM (OLD.decided_by_subject,OLD.decided_at,OLD.approved_minutes,OLD.policy_revision_id)) THEN
  RAISE EXCEPTION 'overtime requests advance one allowed transition at a time' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_overtime_advances BEFORE UPDATE ON workforce_overtime_requests FOR EACH ROW EXECUTE FUNCTION workforce_overtime_advances();

CREATE FUNCTION workforce_overtime_event_matches() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM workforce_overtime_requests r WHERE r.tenant_id=NEW.tenant_id AND r.hospital_id=NEW.hospital_id AND r.id=NEW.request_id
   AND r.version=NEW.version AND r.status=NEW.status AND r.approved_minutes IS NOT DISTINCT FROM NEW.approved_minutes) THEN
  RAISE EXCEPTION 'overtime events must match the request state they record' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_overtime_event_matches BEFORE INSERT ON workforce_overtime_events FOR EACH ROW EXECUTE FUNCTION workforce_overtime_event_matches();

-- Deferred so the request and the event recording its current version are checked together at commit.
CREATE FUNCTION workforce_overtime_complete() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM workforce_overtime_requests r JOIN workforce_overtime_events v ON (v.tenant_id,v.hospital_id,v.request_id,v.version,v.status)=(r.tenant_id,r.hospital_id,r.id,r.version,r.status)
   WHERE r.tenant_id=NEW.tenant_id AND r.hospital_id=NEW.hospital_id AND r.id=NEW.id) THEN
  RAISE EXCEPTION 'every overtime request version needs its event' USING ERRCODE='23514';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER workforce_overtime_complete AFTER INSERT OR UPDATE ON workforce_overtime_requests DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION workforce_overtime_complete();

DO $$DECLARE tbl text;BEGIN
 FOREACH tbl IN ARRAY ARRAY['workforce_overtime_settings','workforce_overtime_setting_revisions','workforce_overtime_requests','workforce_overtime_events'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',tbl);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',tbl);
  EXECUTE format('CREATE POLICY hospital_isolation ON %I USING (hms_tenant_visible(tenant_id) AND workforce_staff_allowed(tenant_id,hospital_id,false)) WITH CHECK(tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND workforce_staff_allowed(tenant_id,hospital_id,true) AND current_setting(''app.workforce_overtime'',true)=''authorized'')',tbl);
  IF tbl IN ('workforce_overtime_setting_revisions','workforce_overtime_events') THEN
   EXECUTE format('CREATE TRIGGER workforce_overtime_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION workforce_audit_immutable()',tbl);
  ELSE
   EXECUTE format('CREATE TRIGGER workforce_overtime_no_delete BEFORE DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal()',tbl);
  END IF;
 END LOOP;
END $$;
