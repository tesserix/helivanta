-- Duty exchanges (#1038 slice 1): approved swaps and covers are written into frozen rosters as `exchanged` revisions.
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Lets exchanges reference an allocation together with the person holding it.
ALTER TABLE workforce_roster_allocations ADD CONSTRAINT workforce_roster_allocation_holder UNIQUE(tenant_id,hospital_id,id,staff_id);

CREATE TABLE workforce_duty_exchanges (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL,
 kind text NOT NULL CHECK (kind IN ('swap','cover')),
 requester_staff_id uuid NOT NULL, counterpart_staff_id uuid NOT NULL,
 requester_allocation_id uuid NOT NULL, counterpart_allocation_id uuid,
 requester_replacement_id uuid, counterpart_replacement_id uuid,
 requester_restored_id uuid, counterpart_restored_id uuid,
 duty_from date NOT NULL, duty_to date NOT NULL,
 counterpart_consent boolean NOT NULL CHECK (counterpart_consent),
 reason text NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 500),
 status text NOT NULL CHECK (status IN ('pending','approved','rejected','cancelled','revoked')),
 requested_by_subject text NOT NULL CHECK (length(requested_by_subject) BETWEEN 1 AND 200),
 decided_by_subject text CHECK (length(decided_by_subject) BETWEEN 1 AND 200), decided_at timestamptz,
 revoked_by_subject text CHECK (length(revoked_by_subject) BETWEEN 1 AND 200),
 version bigint NOT NULL CHECK (version > 0),
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 CHECK (requester_staff_id <> counterpart_staff_id),
 CHECK ((kind = 'swap') = (counterpart_allocation_id IS NOT NULL)),
 CHECK (duty_from <= duty_to AND duty_to <= duty_from + 366),
 CHECK ((decided_by_subject IS NULL) = (decided_at IS NULL)),
 CHECK ((status IN ('approved','rejected','revoked')) = (decided_by_subject IS NOT NULL)),
 CHECK ((status = 'revoked') = (revoked_by_subject IS NOT NULL)),
 CHECK ((status IN ('approved','revoked')) = (requester_replacement_id IS NOT NULL)),
 CHECK ((status IN ('approved','revoked') AND kind = 'swap') = (counterpart_replacement_id IS NOT NULL)),
 CHECK ((status = 'revoked') = (requester_restored_id IS NOT NULL)),
 CHECK ((status = 'revoked' AND kind = 'swap') = (counterpart_restored_id IS NOT NULL)),
 CONSTRAINT workforce_duty_exchange_separation CHECK (decided_by_subject IS DISTINCT FROM requested_by_subject AND revoked_by_subject IS DISTINCT FROM requested_by_subject),
 PRIMARY KEY(tenant_id,hospital_id,id),
 FOREIGN KEY(tenant_id,hospital_id,requester_staff_id) REFERENCES workforce_staff(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,counterpart_staff_id) REFERENCES workforce_staff(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,requester_allocation_id,requester_staff_id) REFERENCES workforce_roster_allocations(tenant_id,hospital_id,id,staff_id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,counterpart_allocation_id,counterpart_staff_id) REFERENCES workforce_roster_allocations(tenant_id,hospital_id,id,staff_id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,requester_replacement_id,counterpart_staff_id) REFERENCES workforce_roster_allocations(tenant_id,hospital_id,id,staff_id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,counterpart_replacement_id,requester_staff_id) REFERENCES workforce_roster_allocations(tenant_id,hospital_id,id,staff_id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,requester_restored_id,requester_staff_id) REFERENCES workforce_roster_allocations(tenant_id,hospital_id,id,staff_id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,counterpart_restored_id,counterpart_staff_id) REFERENCES workforce_roster_allocations(tenant_id,hospital_id,id,staff_id) ON DELETE RESTRICT
);
CREATE INDEX workforce_duty_exchanges_page ON workforce_duty_exchanges(tenant_id,hospital_id,created_at DESC,id DESC);
CREATE INDEX workforce_duty_exchanges_requester ON workforce_duty_exchanges(tenant_id,hospital_id,requester_staff_id,duty_from);
CREATE INDEX workforce_duty_exchanges_counterpart ON workforce_duty_exchanges(tenant_id,hospital_id,counterpart_staff_id,duty_from);

-- An allocation is held by at most one pending or approved exchange.
CREATE TABLE workforce_duty_exchange_holds (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, exchange_id uuid NOT NULL, allocation_id uuid NOT NULL,
 role text NOT NULL CHECK (role IN ('requester','counterpart')), held boolean NOT NULL,
 PRIMARY KEY(tenant_id,hospital_id,exchange_id,role),
 FOREIGN KEY(tenant_id,hospital_id,exchange_id) REFERENCES workforce_duty_exchanges(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,allocation_id) REFERENCES workforce_roster_allocations(tenant_id,hospital_id,id) ON DELETE RESTRICT
);
CREATE UNIQUE INDEX workforce_duty_exchange_held ON workforce_duty_exchange_holds(tenant_id,hospital_id,allocation_id) WHERE held;

CREATE TABLE workforce_duty_exchange_events (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL, exchange_id uuid NOT NULL,
 version bigint NOT NULL CHECK (version > 0),
 status text NOT NULL CHECK (status IN ('pending','approved','rejected','cancelled','revoked')),
 note text NOT NULL DEFAULT '' CHECK (length(note) <= 500),
 actor_subject text NOT NULL CHECK (length(actor_subject) BETWEEN 1 AND 200), created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id,id), UNIQUE(tenant_id,hospital_id,exchange_id,version),
 FOREIGN KEY(tenant_id,hospital_id,exchange_id) REFERENCES workforce_duty_exchanges(tenant_id,hospital_id,id) ON DELETE RESTRICT
);
CREATE INDEX workforce_duty_exchange_events_page ON workforce_duty_exchange_events(tenant_id,hospital_id,exchange_id,created_at DESC,id DESC);

-- Roster revisions gain the `exchanged` action, always tied to the exchange it applies.
ALTER TABLE workforce_roster_revisions ADD COLUMN exchange_id uuid,
 ADD CONSTRAINT workforce_roster_revision_exchange FOREIGN KEY(tenant_id,hospital_id,exchange_id) REFERENCES workforce_duty_exchanges(tenant_id,hospital_id,id) ON DELETE RESTRICT;
ALTER TABLE workforce_roster_revisions DROP CONSTRAINT workforce_roster_revisions_action_check,
 ADD CONSTRAINT workforce_roster_revision_action CHECK (action IN ('created','changed','populated','frozen','unfrozen','discarded','exchanged')),
 ADD CONSTRAINT workforce_roster_revision_exchanged CHECK ((action = 'exchanged') = (exchange_id IS NOT NULL) AND (action <> 'exchanged' OR status = 'frozen'));
CREATE INDEX workforce_roster_revisions_exchange ON workforce_roster_revisions(tenant_id,hospital_id,exchange_id) WHERE exchange_id IS NOT NULL;

CREATE FUNCTION workforce_duty_exchange_opens() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.status <> 'pending' OR NEW.version <> 1 OR NEW.decided_by_subject IS NOT NULL THEN
  RAISE EXCEPTION 'duty exchanges are filed pending at version 1' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_duty_exchange_opens BEFORE INSERT ON workforce_duty_exchanges FOR EACH ROW EXECUTE FUNCTION workforce_duty_exchange_opens();

-- Filing facts never change; status moves one allowed step and each step sets only its own columns.
CREATE FUNCTION workforce_duty_exchange_advances() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.tenant_id,NEW.hospital_id,NEW.id,NEW.kind,NEW.requester_staff_id,NEW.counterpart_staff_id,NEW.requester_allocation_id,NEW.counterpart_allocation_id,NEW.duty_from,NEW.duty_to,NEW.counterpart_consent,NEW.reason,NEW.requested_by_subject,NEW.created_at)
   IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.id,OLD.kind,OLD.requester_staff_id,OLD.counterpart_staff_id,OLD.requester_allocation_id,OLD.counterpart_allocation_id,OLD.duty_from,OLD.duty_to,OLD.counterpart_consent,OLD.reason,OLD.requested_by_subject,OLD.created_at)
  OR NEW.version <> OLD.version + 1
  OR NOT ((OLD.status = 'pending' AND NEW.status IN ('approved','rejected','cancelled')) OR (OLD.status = 'approved' AND NEW.status = 'revoked'))
  OR (OLD.status = 'approved' AND (NEW.decided_by_subject,NEW.decided_at,NEW.requester_replacement_id,NEW.counterpart_replacement_id)
      IS DISTINCT FROM (OLD.decided_by_subject,OLD.decided_at,OLD.requester_replacement_id,OLD.counterpart_replacement_id)) THEN
  RAISE EXCEPTION 'duty exchanges advance one allowed transition at a time' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_duty_exchange_advances BEFORE UPDATE ON workforce_duty_exchanges FOR EACH ROW EXECUTE FUNCTION workforce_duty_exchange_advances();

-- Holds are released exactly when their exchange stops being live.
CREATE FUNCTION workforce_duty_exchange_release() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.status IN ('rejected','cancelled','revoked') THEN
  UPDATE workforce_duty_exchange_holds SET held=false WHERE tenant_id=NEW.tenant_id AND hospital_id=NEW.hospital_id AND exchange_id=NEW.id AND held;
 END IF;
 RETURN NULL;
END $$;
CREATE TRIGGER workforce_duty_exchange_release AFTER UPDATE ON workforce_duty_exchanges FOR EACH ROW EXECUTE FUNCTION workforce_duty_exchange_release();

CREATE FUNCTION workforce_duty_exchange_hold_valid() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE e workforce_duty_exchanges%ROWTYPE;
BEGIN
 SELECT * INTO e FROM workforce_duty_exchanges WHERE tenant_id=NEW.tenant_id AND hospital_id=NEW.hospital_id AND id=NEW.exchange_id;
 IF TG_OP = 'INSERT' THEN
  IF NOT NEW.held OR e.status IS DISTINCT FROM 'pending'
    OR NEW.allocation_id IS DISTINCT FROM (CASE NEW.role WHEN 'requester' THEN e.requester_allocation_id ELSE e.counterpart_allocation_id END) THEN
   RAISE EXCEPTION 'exchange holds are the filed allocations of a pending exchange' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;
 IF (NEW.tenant_id,NEW.hospital_id,NEW.exchange_id,NEW.allocation_id,NEW.role) IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.exchange_id,OLD.allocation_id,OLD.role)
   OR NOT OLD.held OR NEW.held OR e.status NOT IN ('rejected','cancelled','revoked') THEN
  RAISE EXCEPTION 'exchange holds are only released when the exchange ends' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_duty_exchange_hold_valid BEFORE INSERT OR UPDATE ON workforce_duty_exchange_holds FOR EACH ROW EXECUTE FUNCTION workforce_duty_exchange_hold_valid();

CREATE FUNCTION workforce_duty_exchange_event_matches() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM workforce_duty_exchanges e WHERE e.tenant_id=NEW.tenant_id AND e.hospital_id=NEW.hospital_id AND e.id=NEW.exchange_id
   AND e.version=NEW.version AND e.status=NEW.status) THEN
  RAISE EXCEPTION 'duty exchange events must match the exchange state they record' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_duty_exchange_event_matches BEFORE INSERT ON workforce_duty_exchange_events FOR EACH ROW EXECUTE FUNCTION workforce_duty_exchange_event_matches();

-- True when b copies a's duty for another person, a was removed and b added by the same exchanged revision.
CREATE FUNCTION workforce_duty_handover(t uuid, h uuid, a_id uuid, b_id uuid, exchange uuid) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT EXISTS(SELECT 1 FROM workforce_roster_allocations a JOIN workforce_roster_allocations b ON (b.tenant_id,b.hospital_id)=(a.tenant_id,a.hospital_id)
  JOIN workforce_roster_revisions r ON (r.tenant_id,r.hospital_id,r.id)=(b.tenant_id,b.hospital_id,b.added_revision_id)
  WHERE a.tenant_id=t AND a.hospital_id=h AND a.id=a_id AND b.id=b_id AND a.removed_revision_id=b.added_revision_id
   AND r.action='exchanged' AND r.exchange_id=exchange AND b.removed_version IS NULL AND NOT b.additional
   AND (a.roster_id,a.roster_date,a.shift_revision_id,a.area_revision_id,a.starts_at,a.ends_at)=(b.roster_id,b.roster_date,b.shift_revision_id,b.area_revision_id,b.starts_at,b.ends_at))
$$;

-- Deferred so the head, its holds, its event and any roster handover are checked together at commit.
CREATE FUNCTION workforce_duty_exchange_complete() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE e workforce_duty_exchanges%ROWTYPE;
BEGIN
 SELECT * INTO e FROM workforce_duty_exchanges WHERE tenant_id=NEW.tenant_id AND hospital_id=NEW.hospital_id AND id=NEW.id;
 IF (SELECT count(*) FROM workforce_duty_exchange_holds x WHERE x.tenant_id=e.tenant_id AND x.hospital_id=e.hospital_id AND x.exchange_id=e.id) <> (CASE e.kind WHEN 'swap' THEN 2 ELSE 1 END) THEN
  RAISE EXCEPTION 'every filed allocation of a duty exchange needs its hold' USING ERRCODE='23514';
 END IF;
 IF NOT EXISTS(SELECT 1 FROM workforce_duty_exchange_events v WHERE v.tenant_id=e.tenant_id AND v.hospital_id=e.hospital_id AND v.exchange_id=e.id AND v.version=e.version AND v.status=e.status) THEN
  RAISE EXCEPTION 'every duty exchange version needs its event' USING ERRCODE='23514';
 END IF;
 IF TG_OP = 'UPDATE' AND OLD.status = 'pending' AND e.status = 'approved' AND NOT (
   workforce_duty_handover(e.tenant_id,e.hospital_id,e.requester_allocation_id,e.requester_replacement_id,e.id)
   AND (e.kind = 'cover' OR workforce_duty_handover(e.tenant_id,e.hospital_id,e.counterpart_allocation_id,e.counterpart_replacement_id,e.id))) THEN
  RAISE EXCEPTION 'approved exchanges must hand over the filed duty unchanged' USING ERRCODE='23514';
 END IF;
 IF TG_OP = 'UPDATE' AND OLD.status = 'approved' AND e.status = 'revoked' AND NOT (
   workforce_duty_handover(e.tenant_id,e.hospital_id,e.requester_replacement_id,e.requester_restored_id,e.id)
   AND (e.kind = 'cover' OR workforce_duty_handover(e.tenant_id,e.hospital_id,e.counterpart_replacement_id,e.counterpart_restored_id,e.id))) THEN
  RAISE EXCEPTION 'revoked exchanges must restore the original duty unchanged' USING ERRCODE='23514';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER workforce_duty_exchange_complete AFTER INSERT OR UPDATE ON workforce_duty_exchanges DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION workforce_duty_exchange_complete();

-- A frozen roster advances only through an exchanged revision, and cannot reopen over pending exchanges.
CREATE OR REPLACE FUNCTION workforce_roster_head_valid() RETURNS trigger LANGUAGE plpgsql AS $$
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
   OR (OLD.status = 'frozen' AND NEW.status = 'frozen' AND NOT EXISTS(SELECT 1 FROM workforce_roster_revisions v
     WHERE v.tenant_id=NEW.tenant_id AND v.hospital_id=NEW.hospital_id AND v.id=NEW.revision_id AND v.roster_id=NEW.id AND v.action='exchanged')) THEN
  RAISE EXCEPTION 'roster identity is immutable, versions must advance and status must follow draft/frozen/discarded' USING ERRCODE='23514';
 END IF;
 IF NEW.status <> 'frozen' AND EXISTS(SELECT 1 FROM workforce_duty_exchange_holds x JOIN workforce_duty_exchanges e ON (e.tenant_id,e.hospital_id,e.id)=(x.tenant_id,x.hospital_id,x.exchange_id)
   JOIN workforce_roster_allocations a ON (a.tenant_id,a.hospital_id,a.id)=(x.tenant_id,x.hospital_id,x.allocation_id)
   WHERE x.tenant_id=NEW.tenant_id AND x.hospital_id=NEW.hospital_id AND a.roster_id=NEW.id AND e.status='pending') THEN
  RAISE EXCEPTION 'rosters with pending duty exchanges cannot reopen or be discarded' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;

-- Allocations change in a draft at the version being written, or in a frozen roster only within its current exchanged revision.
CREATE OR REPLACE FUNCTION workforce_roster_allocation_valid() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE r workforce_rosters%ROWTYPE; exchanged boolean;
BEGIN
 SELECT * INTO r FROM workforce_rosters WHERE tenant_id=NEW.tenant_id AND hospital_id=NEW.hospital_id AND id=NEW.roster_id;
 exchanged := r.status = 'frozen' AND EXISTS(SELECT 1 FROM workforce_roster_revisions v WHERE v.tenant_id=r.tenant_id AND v.hospital_id=r.hospital_id AND v.id=r.revision_id AND v.action='exchanged');
 IF TG_OP = 'INSERT' THEN
  IF NOT (r.status = 'draft' OR exchanged) OR NEW.added_version <> r.version OR NEW.removed_version IS NOT NULL
    OR (exchanged AND NEW.added_revision_id IS DISTINCT FROM r.revision_id)
    OR NEW.roster_date NOT BETWEEN r.start_date AND r.end_date THEN
   RAISE EXCEPTION 'allocations can only be added to the current version of a draft roster within its period' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;
 IF OLD.removed_version IS NOT NULL OR NEW.removed_version IS DISTINCT FROM r.version OR NOT (r.status IN ('draft','discarded') OR exchanged)
   OR (exchanged AND NEW.removed_revision_id IS DISTINCT FROM r.revision_id)
   OR (NEW.tenant_id,NEW.hospital_id,NEW.id,NEW.roster_id,NEW.staff_id,NEW.roster_date,NEW.shift_revision_id,NEW.area_revision_id,NEW.starts_at,NEW.ends_at,NEW.additional,NEW.added_version,NEW.added_revision_id,NEW.created_at)
   IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.id,OLD.roster_id,OLD.staff_id,OLD.roster_date,OLD.shift_revision_id,OLD.area_revision_id,OLD.starts_at,OLD.ends_at,OLD.additional,OLD.added_version,OLD.added_revision_id,OLD.created_at) THEN
  RAISE EXCEPTION 'allocations are immutable except for a single removal in the current draft version' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;

DO $$DECLARE tbl text;BEGIN
 FOREACH tbl IN ARRAY ARRAY['workforce_duty_exchanges','workforce_duty_exchange_holds','workforce_duty_exchange_events'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',tbl);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',tbl);
  EXECUTE format('CREATE POLICY hospital_isolation ON %I USING (hms_tenant_visible(tenant_id) AND workforce_staff_allowed(tenant_id,hospital_id,false)) WITH CHECK(tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND workforce_staff_allowed(tenant_id,hospital_id,true) AND current_setting(''app.workforce_duty'',true)=''authorized'')',tbl);
  IF tbl = 'workforce_duty_exchange_events' THEN
   EXECUTE format('CREATE TRIGGER workforce_duty_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION workforce_audit_immutable()',tbl);
  ELSE
   EXECUTE format('CREATE TRIGGER workforce_duty_no_delete BEFORE DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal()',tbl);
  END IF;
 END LOOP;
END $$;
