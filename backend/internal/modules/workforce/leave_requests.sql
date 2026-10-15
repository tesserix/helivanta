-- Leave requests (#1037 slice 3): HR files on a staff member's behalf, a different principal decides, approval consumes the ledger.

CREATE TABLE workforce_leave_requests (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL, staff_id uuid NOT NULL,
 leave_type_id uuid NOT NULL, leave_type_revision_id uuid NOT NULL, type_kind text GENERATED ALWAYS AS ('leave_type') STORED,
 assignment_revision_id uuid NOT NULL, leave_year smallint NOT NULL,
 unit text NOT NULL CHECK (unit IN ('full_day','half_day','short_leave')),
 start_on date NOT NULL, end_on date NOT NULL,
 session text CHECK (session IN ('first_half','second_half')),
 minutes smallint CHECK (minutes BETWEEN 1 AND 480),
 half_days integer NOT NULL CHECK (half_days BETWEEN 0 AND 732),
 reason text NOT NULL DEFAULT '' CHECK (length(reason) <= 500),
 status text NOT NULL CHECK (status IN ('pending','approved','rejected','cancelled')),
 requested_by_subject text NOT NULL CHECK (length(requested_by_subject) BETWEEN 1 AND 200),
 decided_by_subject text CHECK (length(decided_by_subject) BETWEEN 1 AND 200), decided_at timestamptz,
 version bigint NOT NULL CHECK (version > 0),
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(), created_tx bigint NOT NULL DEFAULT txid_current(),
 -- Half-day slots since 2000-01-01: a full day or short leave holds both halves, a half day holds one.
 slots int4range GENERATED ALWAYS AS (int4range((start_on - DATE '2000-01-01') * 2 + CASE WHEN session = 'second_half' THEN 1 ELSE 0 END,
  (end_on - DATE '2000-01-01') * 2 + CASE WHEN session = 'first_half' THEN 1 ELSE 2 END)) STORED,
 CHECK (start_on BETWEEN DATE '2000-01-01' AND DATE '2199-12-31' AND end_on BETWEEN start_on AND start_on + 365),
 CHECK (unit = 'full_day' OR start_on = end_on),
 CHECK ((unit = 'half_day') = (session IS NOT NULL)),
 CHECK ((unit = 'short_leave') = (minutes IS NOT NULL)),
 CHECK (unit <> 'short_leave' OR half_days = 0),
 CHECK (unit <> 'half_day' OR half_days IN (0,1)),
 CHECK ((decided_by_subject IS NULL) = (decided_at IS NULL)),
 CHECK (status NOT IN ('approved','rejected') OR decided_by_subject IS NOT NULL),
 CONSTRAINT workforce_leave_request_separation CHECK (decided_by_subject IS DISTINCT FROM requested_by_subject),
 PRIMARY KEY(tenant_id,hospital_id,id),
 CONSTRAINT workforce_leave_request_overlap EXCLUDE USING gist (tenant_id WITH =, hospital_id WITH =, staff_id WITH =, slots WITH &&) WHERE (status IN ('pending','approved')),
 FOREIGN KEY(tenant_id,hospital_id,staff_id) REFERENCES workforce_staff(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,leave_type_revision_id,leave_type_id,type_kind) REFERENCES workforce_leave_config_revisions(tenant_id,hospital_id,id,config_id,kind) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,assignment_revision_id) REFERENCES workforce_staff_leave_assignment_revisions(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,leave_year) REFERENCES workforce_leave_years(tenant_id,hospital_id,leave_year) ON DELETE RESTRICT
);
CREATE INDEX workforce_leave_requests_page ON workforce_leave_requests(tenant_id,hospital_id,created_at DESC,id DESC);
CREATE INDEX workforce_leave_requests_staff ON workforce_leave_requests(tenant_id,hospital_id,staff_id,leave_year,status);

CREATE TABLE workforce_leave_request_days (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, request_id uuid NOT NULL, day date NOT NULL,
 half_days smallint NOT NULL CHECK (half_days BETWEEN 0 AND 2), holiday boolean NOT NULL,
 calendar_revision_id uuid NOT NULL, calendar_kind text GENERATED ALWAYS AS ('holiday_calendar') STORED,
 CHECK (NOT holiday OR half_days = 0),
 PRIMARY KEY(tenant_id,hospital_id,request_id,day),
 FOREIGN KEY(tenant_id,hospital_id,request_id) REFERENCES workforce_leave_requests(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,calendar_revision_id,calendar_kind) REFERENCES workforce_leave_config_revisions(tenant_id,hospital_id,id,kind) ON DELETE RESTRICT
);

CREATE TABLE workforce_leave_request_events (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL, request_id uuid NOT NULL,
 version bigint NOT NULL CHECK (version > 0),
 status text NOT NULL CHECK (status IN ('pending','approved','rejected','cancelled')),
 note text NOT NULL DEFAULT '' CHECK (length(note) <= 500),
 actor_subject text NOT NULL CHECK (length(actor_subject) BETWEEN 1 AND 200), created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id,id), UNIQUE(tenant_id,hospital_id,request_id,version),
 FOREIGN KEY(tenant_id,hospital_id,request_id) REFERENCES workforce_leave_requests(tenant_id,hospital_id,id) ON DELETE RESTRICT
);
CREATE INDEX workforce_leave_request_events_page ON workforce_leave_request_events(tenant_id,hospital_id,request_id,created_at DESC,id DESC);

-- New requests start pending at version 1 inside an open year that contains all their dates.
CREATE FUNCTION workforce_leave_request_opens() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.status <> 'pending' OR NEW.version <> 1 OR NEW.decided_by_subject IS NOT NULL THEN
  RAISE EXCEPTION 'leave requests are filed pending at version 1' USING ERRCODE='23514';
 END IF;
 IF NOT EXISTS(SELECT 1 FROM workforce_leave_years y WHERE y.tenant_id=NEW.tenant_id AND y.hospital_id=NEW.hospital_id AND y.leave_year=NEW.leave_year
   AND NOT y.closed AND NEW.start_on >= y.start_on AND NEW.end_on <= y.end_on) THEN
  RAISE EXCEPTION 'leave requests must fall inside one open leave year' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_leave_request_opens BEFORE INSERT ON workforce_leave_requests FOR EACH ROW EXECUTE FUNCTION workforce_leave_request_opens();

-- Only status, decision and version move, one step at a time along the allowed transitions, while the year is open.
CREATE FUNCTION workforce_leave_request_advances() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.tenant_id,NEW.hospital_id,NEW.id,NEW.staff_id,NEW.leave_type_id,NEW.leave_type_revision_id,NEW.assignment_revision_id,NEW.leave_year,NEW.unit,NEW.start_on,NEW.end_on,NEW.session,NEW.minutes,NEW.half_days,NEW.reason,NEW.requested_by_subject,NEW.created_at,NEW.created_tx)
   IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.id,OLD.staff_id,OLD.leave_type_id,OLD.leave_type_revision_id,OLD.assignment_revision_id,OLD.leave_year,OLD.unit,OLD.start_on,OLD.end_on,OLD.session,OLD.minutes,OLD.half_days,OLD.reason,OLD.requested_by_subject,OLD.created_at,OLD.created_tx)
  OR NEW.version <> OLD.version + 1
  OR NOT ((OLD.status = 'pending' AND NEW.status IN ('approved','rejected','cancelled')) OR (OLD.status = 'approved' AND NEW.status = 'cancelled'))
  OR (OLD.status = 'approved' AND NEW.decided_by_subject IS DISTINCT FROM OLD.decided_by_subject) THEN
  RAISE EXCEPTION 'leave requests advance one allowed transition at a time' USING ERRCODE='23514';
 END IF;
 IF EXISTS(SELECT 1 FROM workforce_leave_years y WHERE y.tenant_id=NEW.tenant_id AND y.hospital_id=NEW.hospital_id AND y.leave_year=NEW.leave_year AND y.closed) THEN
  RAISE EXCEPTION 'leave requests in a closed year cannot change' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_leave_request_advances BEFORE UPDATE ON workforce_leave_requests FOR EACH ROW EXECUTE FUNCTION workforce_leave_request_advances();

CREATE FUNCTION workforce_leave_request_day_creation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM workforce_leave_requests r WHERE r.tenant_id=NEW.tenant_id AND r.hospital_id=NEW.hospital_id AND r.id=NEW.request_id
   AND NEW.day BETWEEN r.start_on AND r.end_on) THEN
  RAISE EXCEPTION 'leave request days must fall inside the request dates' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_leave_request_day_creation BEFORE INSERT ON workforce_leave_request_days FOR EACH ROW EXECUTE FUNCTION workforce_leave_request_day_creation();

-- Each event records the head state it describes, so history and head cannot disagree.
CREATE FUNCTION workforce_leave_request_event_matches() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM workforce_leave_requests r WHERE r.tenant_id=NEW.tenant_id AND r.hospital_id=NEW.hospital_id AND r.id=NEW.request_id
   AND r.version=NEW.version AND r.status=NEW.status) THEN
  RAISE EXCEPTION 'leave request events must match the request state they record' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_leave_request_event_matches BEFORE INSERT ON workforce_leave_request_events FOR EACH ROW EXECUTE FUNCTION workforce_leave_request_event_matches();

-- Deferred so the request, its days and its event are checked together at commit.
CREATE FUNCTION workforce_leave_request_complete() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE r workforce_leave_requests%ROWTYPE;
BEGIN
 SELECT * INTO r FROM workforce_leave_requests WHERE tenant_id=NEW.tenant_id AND hospital_id=NEW.hospital_id AND id=NEW.id;
 IF (SELECT count(*) FROM workforce_leave_request_days d WHERE d.tenant_id=r.tenant_id AND d.hospital_id=r.hospital_id AND d.request_id=r.id) <> r.end_on - r.start_on + 1
  OR (SELECT coalesce(sum(d.half_days),0) FROM workforce_leave_request_days d WHERE d.tenant_id=r.tenant_id AND d.hospital_id=r.hospital_id AND d.request_id=r.id) <> r.half_days THEN
  RAISE EXCEPTION 'leave request days must cover every date and sum to the counted half-days' USING ERRCODE='23514';
 END IF;
 IF NOT EXISTS(SELECT 1 FROM workforce_leave_request_events e WHERE e.tenant_id=r.tenant_id AND e.hospital_id=r.hospital_id AND e.request_id=r.id AND e.version=r.version AND e.status=r.status) THEN
  RAISE EXCEPTION 'every leave request version needs its event' USING ERRCODE='23514';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER workforce_leave_request_complete AFTER INSERT OR UPDATE ON workforce_leave_requests DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION workforce_leave_request_complete();

-- A year cannot close over undecided requests; their consumption would land after year end.
CREATE FUNCTION workforce_leave_year_settled() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.closed AND NOT OLD.closed AND EXISTS(SELECT 1 FROM workforce_leave_requests r WHERE r.tenant_id=NEW.tenant_id AND r.hospital_id=NEW.hospital_id AND r.leave_year=NEW.leave_year AND r.status='pending') THEN
  RAISE EXCEPTION 'leave years with pending requests cannot close' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_leave_year_settled BEFORE UPDATE ON workforce_leave_years FOR EACH ROW EXECUTE FUNCTION workforce_leave_year_settled();

-- Consumption and reversal entries belong to a request; the 0011 checks are rebuilt to admit them.
ALTER TABLE workforce_leave_ledger ADD COLUMN request_id uuid,
 ADD FOREIGN KEY(tenant_id,hospital_id,request_id) REFERENCES workforce_leave_requests(tenant_id,hospital_id,id) ON DELETE RESTRICT;
ALTER TABLE workforce_leave_ledger DROP CONSTRAINT workforce_leave_ledger_kind_check, DROP CONSTRAINT workforce_leave_ledger_check, DROP CONSTRAINT workforce_leave_ledger_check1,
 ADD CONSTRAINT workforce_leave_ledger_kind CHECK (kind IN ('allocation','accrual','carry_in','carry_out','expiry','correction','consumption','reversal')),
 ADD CONSTRAINT workforce_leave_ledger_sign CHECK ((kind IN ('allocation','accrual','carry_in','reversal') AND half_days > 0) OR (kind IN ('carry_out','expiry','consumption') AND half_days < 0) OR (kind = 'correction' AND half_days <> 0)),
 ADD CONSTRAINT workforce_leave_ledger_run CHECK ((kind IN ('correction','consumption','reversal')) = (run_id IS NULL)),
 ADD CONSTRAINT workforce_leave_ledger_request CHECK ((kind IN ('consumption','reversal')) = (request_id IS NOT NULL));
CREATE UNIQUE INDEX workforce_leave_ledger_request_entry ON workforce_leave_ledger(tenant_id,hospital_id,request_id,kind) WHERE request_id IS NOT NULL;

-- Consumption mirrors an approved request exactly; a reversal only undoes a cancelled approval.
CREATE FUNCTION workforce_leave_request_entry_valid() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.request_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM workforce_leave_requests r WHERE r.tenant_id=NEW.tenant_id AND r.hospital_id=NEW.hospital_id AND r.id=NEW.request_id
   AND r.staff_id=NEW.staff_id AND r.leave_type_id=NEW.leave_type_id AND r.leave_type_revision_id=NEW.leave_type_revision_id AND r.leave_year=NEW.leave_year AND r.start_on=NEW.effective_on
   AND ((NEW.kind='consumption' AND r.status='approved' AND NEW.half_days=-r.half_days)
    OR (NEW.kind='reversal' AND r.status='cancelled' AND r.decided_by_subject IS NOT NULL AND NEW.half_days=r.half_days
     AND EXISTS(SELECT 1 FROM workforce_leave_ledger c WHERE c.tenant_id=NEW.tenant_id AND c.hospital_id=NEW.hospital_id AND c.request_id=NEW.request_id AND c.kind='consumption')))) THEN
  RAISE EXCEPTION 'request ledger entries must mirror the request decision' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_leave_request_entry_valid BEFORE INSERT ON workforce_leave_ledger FOR EACH ROW EXECUTE FUNCTION workforce_leave_request_entry_valid();

-- Population records staff-days it skipped for approved full-day leave.
ALTER TABLE workforce_roster_revisions ADD COLUMN skipped_leave integer NOT NULL DEFAULT 0 CHECK (skipped_leave >= 0);

DO $$DECLARE tbl text;BEGIN
 FOREACH tbl IN ARRAY ARRAY['workforce_leave_requests','workforce_leave_request_days','workforce_leave_request_events'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',tbl);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',tbl);
  EXECUTE format('CREATE POLICY hospital_isolation ON %I USING (hms_tenant_visible(tenant_id) AND workforce_staff_allowed(tenant_id,hospital_id,false)) WITH CHECK(tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND workforce_staff_allowed(tenant_id,hospital_id,true) AND current_setting(''app.workforce_leave_ledger'',true)=''authorized'')',tbl);
  IF tbl = 'workforce_leave_requests' THEN
   EXECUTE format('CREATE TRIGGER workforce_leave_no_delete BEFORE DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal()',tbl);
  ELSE
   EXECUTE format('CREATE TRIGGER workforce_leave_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION workforce_audit_immutable()',tbl);
  END IF;
 END LOOP;
END $$;
