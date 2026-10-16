-- Attendance timesheets (#1039 slice 3): finalised period snapshots, numbered recalculations and database-enforced period locks.
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE workforce_attendance_periods (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL,
 period_from date NOT NULL CHECK (period_from BETWEEN DATE '2000-01-01' AND DATE '2199-12-31'),
 period_to date NOT NULL CHECK (period_to BETWEEN DATE '2000-01-01' AND DATE '2199-12-31'),
 status text NOT NULL CHECK (status IN ('finalised','reopened')),
 version bigint NOT NULL CHECK (version > 0),
 timesheet_id uuid NOT NULL, timesheet_number int NOT NULL CHECK (timesheet_number > 0),
 created_by_subject text NOT NULL CHECK (length(created_by_subject) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 CHECK (period_to >= period_from AND period_to - period_from < 31),
 PRIMARY KEY(tenant_id,hospital_id,id),
 FOREIGN KEY(tenant_id,hospital_id) REFERENCES workforce_hospitals(tenant_id,id) ON DELETE RESTRICT
);
CREATE INDEX workforce_attendance_periods_page ON workforce_attendance_periods(tenant_id,hospital_id,created_at DESC,id DESC);

-- One row per covered day; the primary key is what makes periods unable to overlap.
CREATE TABLE workforce_attendance_period_days (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, day date NOT NULL, period_id uuid NOT NULL,
 PRIMARY KEY(tenant_id,hospital_id,day),
 FOREIGN KEY(tenant_id,hospital_id,period_id) REFERENCES workforce_attendance_periods(tenant_id,hospital_id,id) ON DELETE RESTRICT
);
CREATE INDEX workforce_attendance_period_days_period ON workforce_attendance_period_days(tenant_id,hospital_id,period_id);

CREATE TABLE workforce_attendance_timesheets (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL, period_id uuid NOT NULL,
 number int NOT NULL CHECK (number > 0),
 people int NOT NULL CHECK (people >= 0), days int NOT NULL CHECK (days >= 0), shifts int NOT NULL CHECK (shifts >= 0),
 payable_minutes bigint NOT NULL CHECK (payable_minutes >= 0), approved_overtime_minutes bigint NOT NULL CHECK (approved_overtime_minutes >= 0),
 unpaid_half_days int NOT NULL CHECK (unpaid_half_days >= 0),
 statuses jsonb NOT NULL CHECK (jsonb_typeof(statuses) = 'array'), exceptions jsonb NOT NULL CHECK (jsonb_typeof(exceptions) = 'array'),
 policy_revision_ids uuid[] NOT NULL,
 note text NOT NULL DEFAULT '' CHECK (length(note) <= 500),
 finalised_by_subject text NOT NULL CHECK (length(finalised_by_subject) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id,id), UNIQUE(tenant_id,hospital_id,period_id,number), UNIQUE(tenant_id,hospital_id,period_id,number,id),
 FOREIGN KEY(tenant_id,hospital_id,period_id) REFERENCES workforce_attendance_periods(tenant_id,hospital_id,id) ON DELETE RESTRICT
);
CREATE INDEX workforce_attendance_timesheets_page ON workforce_attendance_timesheets(tenant_id,hospital_id,period_id,created_at DESC,id DESC);
ALTER TABLE workforce_attendance_periods ADD CONSTRAINT workforce_attendance_period_current
 FOREIGN KEY(tenant_id,hospital_id,id,timesheet_number,timesheet_id) REFERENCES workforce_attendance_timesheets(tenant_id,hospital_id,period_id,number,id) DEFERRABLE INITIALLY DEFERRED;

CREATE TABLE workforce_attendance_timesheet_people (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, timesheet_id uuid NOT NULL, staff_id uuid NOT NULL,
 employee_code text NOT NULL, display_name text NOT NULL,
 -- The staff record's creation time, so pages follow the staff list order.
 sort_at timestamptz NOT NULL,
 days int NOT NULL CHECK (days >= 0), shifts int NOT NULL CHECK (shifts >= 0),
 payable_minutes int NOT NULL CHECK (payable_minutes >= 0), approved_overtime_minutes int NOT NULL CHECK (approved_overtime_minutes >= 0),
 unpaid_half_days int NOT NULL CHECK (unpaid_half_days >= 0),
 statuses jsonb NOT NULL CHECK (jsonb_typeof(statuses) = 'array'), exceptions jsonb NOT NULL CHECK (jsonb_typeof(exceptions) = 'array'),
 PRIMARY KEY(tenant_id,hospital_id,timesheet_id,staff_id),
 FOREIGN KEY(tenant_id,hospital_id,timesheet_id) REFERENCES workforce_attendance_timesheets(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,staff_id) REFERENCES workforce_staff(tenant_id,hospital_id,id) ON DELETE RESTRICT
);
CREATE INDEX workforce_attendance_timesheet_people_page ON workforce_attendance_timesheet_people(tenant_id,hospital_id,timesheet_id,sort_at DESC,staff_id DESC);

CREATE FUNCTION workforce_attendance_detail_unpaid(detail jsonb) RETURNS int LANGUAGE sql IMMUTABLE AS $$
 SELECT COALESCE(sum((s->>'unpaid_half_days')::int),0)::int FROM jsonb_array_elements(detail->'shifts') s
$$;

CREATE TABLE workforce_attendance_timesheet_days (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, timesheet_id uuid NOT NULL, staff_id uuid NOT NULL, day date NOT NULL,
 change text NOT NULL CHECK (change IN ('added','changed','unchanged','removed')),
 shifts int NOT NULL CHECK (shifts >= 0), payable_minutes int NOT NULL CHECK (payable_minutes >= 0),
 approved_overtime_minutes int NOT NULL CHECK (approved_overtime_minutes >= 0), unpaid_half_days int NOT NULL CHECK (unpaid_half_days >= 0),
 -- The derived day exactly as finalised; a removed day keeps no detail and counts nothing.
 detail jsonb,
 CHECK ((change = 'removed') = (detail IS NULL)),
 CHECK (change <> 'removed' OR (shifts, payable_minutes, approved_overtime_minutes, unpaid_half_days) = (0, 0, 0, 0)),
 CHECK (detail IS NULL OR ((detail->>'staff_id')::uuid = staff_id AND detail->>'date' = day::text AND jsonb_array_length(detail->'shifts') = shifts
  AND (detail->>'payable_minutes')::int = payable_minutes AND (detail->>'approved_overtime_minutes')::int = approved_overtime_minutes
  AND workforce_attendance_detail_unpaid(detail) = unpaid_half_days)),
 PRIMARY KEY(tenant_id,hospital_id,timesheet_id,staff_id,day),
 FOREIGN KEY(tenant_id,hospital_id,timesheet_id,staff_id) REFERENCES workforce_attendance_timesheet_people(tenant_id,hospital_id,timesheet_id,staff_id) ON DELETE RESTRICT
);
CREATE INDEX workforce_attendance_timesheet_days_page ON workforce_attendance_timesheet_days(tenant_id,hospital_id,timesheet_id,day DESC,staff_id DESC);

CREATE TABLE workforce_attendance_period_events (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL, period_id uuid NOT NULL,
 version bigint NOT NULL CHECK (version > 0),
 status text NOT NULL CHECK (status IN ('finalised','reopened')),
 timesheet_id uuid NOT NULL,
 note text NOT NULL DEFAULT '' CHECK (length(note) <= 500),
 actor_subject text NOT NULL CHECK (length(actor_subject) BETWEEN 1 AND 200), created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id,id), UNIQUE(tenant_id,hospital_id,period_id,version),
 FOREIGN KEY(tenant_id,hospital_id,period_id) REFERENCES workforce_attendance_periods(tenant_id,hospital_id,id) ON DELETE RESTRICT
);
CREATE INDEX workforce_attendance_period_events_page ON workforce_attendance_period_events(tenant_id,hospital_id,period_id,created_at DESC,id DESC);

-- A period is born finalised with timesheet 1; reopening keeps the timesheet, finalising again moves to the next one.
CREATE FUNCTION workforce_attendance_period_opens() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.status <> 'finalised' OR NEW.version <> 1 OR NEW.timesheet_number <> 1 THEN
  RAISE EXCEPTION 'periods are created finalised at version 1 with timesheet 1' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_attendance_period_opens BEFORE INSERT ON workforce_attendance_periods FOR EACH ROW EXECUTE FUNCTION workforce_attendance_period_opens();

CREATE FUNCTION workforce_attendance_period_advances() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.tenant_id,NEW.hospital_id,NEW.id,NEW.period_from,NEW.period_to,NEW.created_by_subject,NEW.created_at)
   IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.id,OLD.period_from,OLD.period_to,OLD.created_by_subject,OLD.created_at)
  OR NEW.version <> OLD.version + 1
  OR NOT ((OLD.status = 'finalised' AND NEW.status = 'reopened' AND (NEW.timesheet_id,NEW.timesheet_number) = (OLD.timesheet_id,OLD.timesheet_number))
   OR (NEW.status = 'finalised' AND NEW.timesheet_number = OLD.timesheet_number + 1)) THEN
  RAISE EXCEPTION 'attendance periods keep their range and move one version at a time' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_attendance_period_advances BEFORE UPDATE ON workforce_attendance_periods FOR EACH ROW EXECUTE FUNCTION workforce_attendance_period_advances();

-- Deferred to commit: the period covers exactly its days, and every version has the event recording it.
CREATE FUNCTION workforce_attendance_period_complete() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (SELECT count(*) FROM workforce_attendance_period_days d WHERE (d.tenant_id,d.hospital_id,d.period_id)=(NEW.tenant_id,NEW.hospital_id,NEW.id)
     AND d.day BETWEEN NEW.period_from AND NEW.period_to) <> NEW.period_to - NEW.period_from + 1
  OR EXISTS(SELECT 1 FROM workforce_attendance_period_days d WHERE (d.tenant_id,d.hospital_id,d.period_id)=(NEW.tenant_id,NEW.hospital_id,NEW.id)
     AND d.day NOT BETWEEN NEW.period_from AND NEW.period_to)
  OR NOT EXISTS(SELECT 1 FROM workforce_attendance_periods p JOIN workforce_attendance_period_events e
     ON (e.tenant_id,e.hospital_id,e.period_id,e.version,e.status,e.timesheet_id)=(p.tenant_id,p.hospital_id,p.id,p.version,p.status,p.timesheet_id)
     WHERE (p.tenant_id,p.hospital_id,p.id)=(NEW.tenant_id,NEW.hospital_id,NEW.id)) THEN
  RAISE EXCEPTION 'a period covers exactly its days and records an event for every version' USING ERRCODE='23514';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER workforce_attendance_period_complete AFTER INSERT OR UPDATE ON workforce_attendance_periods DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION workforce_attendance_period_complete();

-- Deferred to commit: totals equal their people, people equal their days, counts reconcile and days sit inside the period.
CREATE FUNCTION workforce_attendance_timesheet_reconciles() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE t workforce_attendance_timesheets%ROWTYPE;
BEGIN
 SELECT * INTO t FROM workforce_attendance_timesheets WHERE (tenant_id,hospital_id,id)=(NEW.tenant_id,NEW.hospital_id,NEW.id);
 IF (t.number > 1 AND NOT EXISTS(SELECT 1 FROM workforce_attendance_timesheets x WHERE (x.tenant_id,x.hospital_id,x.period_id,x.number)=(t.tenant_id,t.hospital_id,t.period_id,t.number-1)))
  OR EXISTS(SELECT 1 FROM (SELECT count(*) AS people,COALESCE(sum(days),0) AS days,COALESCE(sum(shifts),0) AS shifts,COALESCE(sum(payable_minutes),0) AS payable,
    COALESCE(sum(approved_overtime_minutes),0) AS overtime,COALESCE(sum(unpaid_half_days),0) AS unpaid
   FROM workforce_attendance_timesheet_people p WHERE (p.tenant_id,p.hospital_id,p.timesheet_id)=(t.tenant_id,t.hospital_id,t.id)) s
   WHERE (t.people,t.days,t.shifts,t.payable_minutes,t.approved_overtime_minutes,t.unpaid_half_days) IS DISTINCT FROM (s.people,s.days,s.shifts,s.payable,s.overtime,s.unpaid))
  OR EXISTS(SELECT 1 FROM workforce_attendance_timesheet_people p CROSS JOIN LATERAL (SELECT count(*) FILTER (WHERE d.change <> 'removed') AS days,COALESCE(sum(d.shifts),0) AS shifts,
    COALESCE(sum(d.payable_minutes),0) AS payable,COALESCE(sum(d.approved_overtime_minutes),0) AS overtime,COALESCE(sum(d.unpaid_half_days),0) AS unpaid
    FROM workforce_attendance_timesheet_days d WHERE (d.tenant_id,d.hospital_id,d.timesheet_id,d.staff_id)=(p.tenant_id,p.hospital_id,p.timesheet_id,p.staff_id)) s
   WHERE (p.tenant_id,p.hospital_id,p.timesheet_id)=(t.tenant_id,t.hospital_id,t.id)
   AND (p.days,p.shifts,p.payable_minutes,p.approved_overtime_minutes,p.unpaid_half_days) IS DISTINCT FROM (s.days,s.shifts,s.payable,s.overtime,s.unpaid))
  OR EXISTS(SELECT 1 FROM workforce_attendance_timesheet_days d JOIN workforce_attendance_periods pr ON (pr.tenant_id,pr.hospital_id,pr.id)=(t.tenant_id,t.hospital_id,t.period_id)
   WHERE (d.tenant_id,d.hospital_id,d.timesheet_id)=(t.tenant_id,t.hospital_id,t.id) AND d.day NOT BETWEEN pr.period_from AND pr.period_to)
  -- Hospital counts equal the people's, and each person's status counts equal their finalised shifts.
  OR EXISTS(SELECT 1 FROM (SELECT 'statuses' AS kind, e FROM jsonb_array_elements(t.statuses) e UNION ALL SELECT 'exceptions', e FROM jsonb_array_elements(t.exceptions) e) c
   WHERE (c.e->>'count')::int <> (SELECT COALESCE(sum((x->>'count')::int),0) FROM workforce_attendance_timesheet_people p,
    jsonb_array_elements(CASE c.kind WHEN 'statuses' THEN p.statuses ELSE p.exceptions END) x
    WHERE (p.tenant_id,p.hospital_id,p.timesheet_id)=(t.tenant_id,t.hospital_id,t.id) AND x->>'code' = c.e->>'code'))
  OR EXISTS(SELECT 1 FROM workforce_attendance_timesheet_people p, jsonb_array_elements(p.statuses) e
   WHERE (p.tenant_id,p.hospital_id,p.timesheet_id)=(t.tenant_id,t.hospital_id,t.id)
   AND (e->>'count')::int <> (SELECT count(*) FROM workforce_attendance_timesheet_days d, jsonb_array_elements(d.detail->'shifts') s
    WHERE (d.tenant_id,d.hospital_id,d.timesheet_id,d.staff_id)=(p.tenant_id,p.hospital_id,p.timesheet_id,p.staff_id) AND d.detail IS NOT NULL AND s->>'status' = e->>'code')) THEN
  RAISE EXCEPTION 'timesheet totals must reconcile to their people and days' USING ERRCODE='23514', CONSTRAINT='workforce_attendance_timesheet_reconciles';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER workforce_attendance_timesheet_reconciles AFTER INSERT ON workforce_attendance_timesheets DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION workforce_attendance_timesheet_reconciles();

-- The punch report reads one hospital's punches by local date.
CREATE INDEX workforce_attendance_punches_local_date ON workforce_attendance_punches(tenant_id,hospital_id,local_date);

-- Whether a finalised period covers the day; writers hold the manager grant, so RLS never hides a covering period from them.
CREATE FUNCTION workforce_attendance_day_locked(t uuid, h uuid, d date) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT EXISTS(SELECT 1 FROM workforce_attendance_period_days pd JOIN workforce_attendance_periods p ON (p.tenant_id,p.hospital_id,p.id)=(pd.tenant_id,pd.hospital_id,pd.period_id)
  WHERE pd.tenant_id=t AND pd.hospital_id=h AND pd.day=d AND p.status='finalised')
$$;

CREATE FUNCTION workforce_attendance_period_lock() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE locked boolean;
BEGIN
 -- Separate branches, because each table's NEW has different columns.
 IF TG_TABLE_NAME = 'workforce_attendance_punches' THEN
  locked := workforce_attendance_day_locked(NEW.tenant_id,NEW.hospital_id,NEW.local_date);
 ELSIF TG_TABLE_NAME = 'workforce_attendance_punch_voids' THEN
  locked := EXISTS(SELECT 1 FROM workforce_attendance_punches p WHERE (p.tenant_id,p.hospital_id,p.id)=(NEW.tenant_id,NEW.hospital_id,NEW.punch_id)
   AND workforce_attendance_day_locked(p.tenant_id,p.hospital_id,p.local_date));
 ELSIF TG_TABLE_NAME = 'workforce_attendance_corrections' THEN
  locked := workforce_attendance_day_locked(NEW.tenant_id,NEW.hospital_id,NEW.local_date)
   OR EXISTS(SELECT 1 FROM workforce_attendance_punches p WHERE (p.tenant_id,p.hospital_id,p.id)=(NEW.tenant_id,NEW.hospital_id,NEW.target_punch_id)
   AND workforce_attendance_day_locked(p.tenant_id,p.hospital_id,p.local_date));
 ELSE
  locked := EXISTS(SELECT 1 FROM workforce_attendance_period_days pd JOIN workforce_attendance_periods p ON (p.tenant_id,p.hospital_id,p.id)=(pd.tenant_id,pd.hospital_id,pd.period_id)
   WHERE pd.tenant_id=NEW.tenant_id AND pd.hospital_id=NEW.hospital_id AND pd.day >= NEW.effective_on AND p.status='finalised');
 END IF;
 IF locked THEN
  RAISE EXCEPTION 'the date is inside a finalised attendance period' USING ERRCODE='23514', CONSTRAINT='workforce_attendance_period_locked';
 END IF;
 RETURN NEW;
END $$;
-- Named to sort before each table's validity trigger, so a locked date reports the lock.
CREATE TRIGGER workforce_attendance_punch_locked BEFORE INSERT ON workforce_attendance_punches FOR EACH ROW EXECUTE FUNCTION workforce_attendance_period_lock();
CREATE TRIGGER workforce_attendance_void_locked BEFORE INSERT ON workforce_attendance_punch_voids FOR EACH ROW EXECUTE FUNCTION workforce_attendance_period_lock();
CREATE TRIGGER workforce_attendance_correction_locked BEFORE INSERT ON workforce_attendance_corrections FOR EACH ROW EXECUTE FUNCTION workforce_attendance_period_lock();
CREATE TRIGGER workforce_attendance_setting_locked BEFORE INSERT ON workforce_attendance_setting_revisions FOR EACH ROW EXECUTE FUNCTION workforce_attendance_period_lock();

DO $$DECLARE tbl text;BEGIN
 FOREACH tbl IN ARRAY ARRAY['workforce_attendance_periods','workforce_attendance_period_days','workforce_attendance_timesheets','workforce_attendance_timesheet_people','workforce_attendance_timesheet_days','workforce_attendance_period_events'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',tbl);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',tbl);
  EXECUTE format('CREATE POLICY hospital_isolation ON %I USING (hms_tenant_visible(tenant_id) AND workforce_staff_allowed(tenant_id,hospital_id,false)) WITH CHECK(tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND workforce_staff_allowed(tenant_id,hospital_id,true) AND current_setting(''app.workforce_attendance'',true)=''authorized'')',tbl);
  IF tbl = 'workforce_attendance_periods' THEN
   EXECUTE format('CREATE TRIGGER workforce_attendance_period_no_delete BEFORE DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal()',tbl);
  ELSE
   EXECUTE format('CREATE TRIGGER workforce_attendance_timesheet_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION workforce_audit_immutable()',tbl);
  END IF;
 END LOOP;
END $$;
