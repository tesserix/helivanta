SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- Discounts on room charges (#200 slice 1): a reason with the desk's and the
-- approver's limits, a request decided here, and its amount split over the
-- admission's exempt, uninvoiced room charges as discount entries.

-- A version 7 id for rows the database writes itself (PostgreSQL 16 has none):
-- the time in milliseconds over a random id, with the version bits set to 7.
CREATE FUNCTION billing_uuid_v7() RETURNS uuid LANGUAGE sql VOLATILE AS $$
 SELECT encode(set_bit(set_bit(overlay(uuid_send(gen_random_uuid()) PLACING
  substring(int8send((extract(epoch FROM clock_timestamp())*1000)::bigint) FROM 3) FROM 1 FOR 6), 52, 1), 53, 1), 'hex')::uuid
$$;

-- A hospital's reason: what the desk may give alone and the most anyone may
-- approve, cumulative per admission. Fixed once set; retired once.
CREATE TABLE billing_discount_reasons (
 tenant_id uuid NOT NULL,
 hospital_id uuid NOT NULL CHECK (hospital_id <> '00000000-0000-0000-0000-000000000000'::uuid),
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 code text NOT NULL CHECK (code ~ '^[a-z0-9_-]{1,32}$'),
 name text NOT NULL CHECK (name=btrim(name) AND length(name) BETWEEN 1 AND 100),
 desk_percent_bp integer NOT NULL CHECK (desk_percent_bp BETWEEN 0 AND 10000),
 desk_max_minor bigint NOT NULL CHECK (desk_max_minor BETWEEN 0 AND 1000000000),
 approval_percent_bp integer NOT NULL CHECK (approval_percent_bp BETWEEN 0 AND 10000),
 approval_max_minor bigint NOT NULL CHECK (approval_max_minor BETWEEN 0 AND 1000000000),
 currency text NOT NULL CHECK (currency='INR'),
 created_by text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 retired_by text,
 retired_at timestamptz,
 PRIMARY KEY (tenant_id,id),
 UNIQUE (tenant_id,hospital_id,id),
 CONSTRAINT billing_discount_reason_limits CHECK (desk_percent_bp <= approval_percent_bp AND desk_max_minor <= approval_max_minor),
 CHECK ((retired_by IS NULL) = (retired_at IS NULL))
);
CREATE UNIQUE INDEX billing_discount_reasons_code ON billing_discount_reasons (tenant_id,hospital_id,code) WHERE retired_at IS NULL;
CREATE INDEX billing_discount_reasons_page ON billing_discount_reasons (tenant_id,hospital_id,created_at DESC,id DESC);

-- A discount asked for on an admission. Its status is set by the database:
-- applied at once within the desk's limit, otherwise pending a decision by
-- someone else.
CREATE TABLE billing_discounts (
 tenant_id uuid NOT NULL,
 hospital_id uuid NOT NULL,
 admission_id uuid NOT NULL,
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 reason_id uuid NOT NULL,
 amount_minor bigint NOT NULL CHECK (amount_minor BETWEEN 1 AND 1000000000),
 currency text NOT NULL CHECK (currency='INR'),
 justification text NOT NULL CHECK (justification=btrim(justification) AND length(justification) BETWEEN 1 AND 500),
 status text NOT NULL CHECK (status IN ('pending','applied','rejected')),
 requested_by text NOT NULL,
 requested_at timestamptz NOT NULL,
 decided_by text,
 decided_at timestamptz,
 decision_note text CHECK (decision_note=btrim(decision_note) AND length(decision_note) BETWEEN 1 AND 500),
 applied_at timestamptz,
 applying_xact xid8,
 PRIMARY KEY (tenant_id,id),
 FOREIGN KEY (tenant_id,hospital_id,admission_id) REFERENCES billing_admission_accounts (tenant_id,hospital_id,admission_id),
 FOREIGN KEY (tenant_id,hospital_id,reason_id) REFERENCES billing_discount_reasons (tenant_id,hospital_id,id),
 CHECK ((decided_by IS NULL) = (decided_at IS NULL) AND (decided_by IS NULL) = (decision_note IS NULL)),
 CHECK ((status='applied') = (applied_at IS NOT NULL) AND (applied_at IS NULL) = (applying_xact IS NULL)),
 CHECK ((status='pending') <= (decided_by IS NULL) AND (status='rejected') <= (decided_by IS NOT NULL)),
 CONSTRAINT billing_discount_own_request CHECK (decided_by IS DISTINCT FROM requested_by)
);
CREATE INDEX billing_discounts_admission ON billing_discounts (tenant_id,admission_id,requested_at DESC,id DESC);
CREATE INDEX billing_discounts_pending ON billing_discounts (tenant_id,hospital_id,requested_at DESC,id DESC) WHERE status='pending';

DO $$ DECLARE t text; BEGIN
 FOREACH t IN ARRAY ARRAY['billing_discount_reasons','billing_discounts'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
  EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND billing_hospital_allowed(tenant_id,hospital_id)) WITH CHECK (tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND billing_hospital_allowed(tenant_id,hospital_id))', t);
  EXECUTE format('CREATE TRIGGER %I AFTER INSERT OR UPDATE ON %I FOR EACH ROW EXECUTE FUNCTION audit_record_change()', t||'_audit', t);
  EXECUTE format('CREATE TRIGGER %I BEFORE DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION billing_history_retained()', t||'_kept', t);
 END LOOP;
END $$;

CREATE FUNCTION billing_discount_reason_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.created_by IS DISTINCT FROM billing_actor() OR NEW.retired_by IS NOT NULL THEN
   RAISE EXCEPTION 'a reason is set by the acting subject, unretired' USING ERRCODE='23514';
  END IF;
  NEW.created_at := clock_timestamp();
  RETURN NEW;
 END IF;
 IF (NEW.tenant_id,NEW.hospital_id,NEW.id,NEW.code,NEW.name,NEW.desk_percent_bp,NEW.desk_max_minor,NEW.approval_percent_bp,NEW.approval_max_minor,NEW.currency,NEW.created_by,NEW.created_at)
  IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.id,OLD.code,OLD.name,OLD.desk_percent_bp,OLD.desk_max_minor,OLD.approval_percent_bp,OLD.approval_max_minor,OLD.currency,OLD.created_by,OLD.created_at)
  OR OLD.retired_at IS NOT NULL OR NEW.retired_by IS DISTINCT FROM billing_actor() THEN
  RAISE EXCEPTION 'a reason is fixed, and retired once by the acting subject' USING ERRCODE='23514';
 END IF;
 NEW.retired_at := clock_timestamp();
 RETURN NEW;
END $$;
CREATE TRIGGER billing_discount_reason_guard BEFORE INSERT OR UPDATE ON billing_discount_reasons FOR EACH ROW EXECUTE FUNCTION billing_discount_reason_guard();

-- Discount entries name their discount; only they do.
ALTER TABLE billing_account_entries DROP CONSTRAINT billing_account_entries_source_module_check;
ALTER TABLE billing_account_entries ADD CONSTRAINT billing_account_entries_source_module_check CHECK (source_module IN ('pharmacy','bed','gst','discount'));
ALTER TABLE billing_account_entries ADD COLUMN discount_id uuid;
ALTER TABLE billing_account_entries ADD CONSTRAINT billing_account_entry_discount CHECK ((source_module='discount') = (discount_id IS NOT NULL));
ALTER TABLE billing_account_entries ADD CONSTRAINT billing_account_entry_discount_fk FOREIGN KEY (tenant_id,discount_id) REFERENCES billing_discounts (tenant_id,id);
CREATE INDEX billing_account_entries_charge ON billing_account_entries (tenant_id,charge_reference);
CREATE INDEX billing_account_entries_discount ON billing_account_entries (tenant_id,discount_id) WHERE discount_id IS NOT NULL;
-- As in 0001, and the desk writes discount lines under its grant; the entry
-- guard admits them only from the transaction that applies the discount.
DROP POLICY tenant_isolation ON billing_account_entries;
CREATE POLICY tenant_isolation ON billing_account_entries
 USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND (billing_consumer() OR billing_hospital_allowed(tenant_id,hospital_id)))
 WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND (billing_consumer()
  OR (source_module='discount' AND kind='reversal' AND billing_hospital_allowed(tenant_id,hospital_id))));

-- Taxable as the census taxes it: a GST line, or a registration in force on
-- the day and a policy that taxes its class and rate. Refused for now: a tax
-- ruling decides whether a concession can take a room out of the proviso.
CREATE FUNCTION billing_room_taxable(row_tenant uuid,row_day uuid) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT EXISTS (SELECT 1 FROM billing_gst_lines l WHERE l.tenant_id=row_tenant AND l.bed_day_id=row_day)
  OR EXISTS (SELECT 1 FROM billing_bed_days b
   JOIN billing_gst_registrations g ON g.tenant_id=b.tenant_id AND g.hospital_id=b.hospital_id AND g.effective_from<=b.census_date
   CROSS JOIN LATERAL billing_gst_room_policy(b.census_date) p
   WHERE b.tenant_id=row_tenant AND b.id=row_day AND b.bed_class <> ALL(p.exempt_classes) AND b.daily_rate_minor > p.threshold_minor)
$$;

-- What discounts take off a room charge now: lines less their withdrawals.
CREATE FUNCTION billing_discount_on(row_tenant uuid,row_day uuid) RETURNS bigint LANGUAGE sql STABLE AS $$
 SELECT COALESCE(sum(x.amount_minor),0)::bigint FROM billing_account_entries x
  WHERE x.tenant_id=row_tenant AND x.source_module='discount' AND x.kind='reversal' AND x.charge_reference=row_day
  AND NOT EXISTS (SELECT 1 FROM billing_account_entries w WHERE w.tenant_id=x.tenant_id AND w.source_module='discount' AND w.kind='charge' AND w.source_reference=x.source_reference)
$$;

-- An admission's standing INR room charges, with what discounts take off each.
CREATE FUNCTION billing_room_charges(row_tenant uuid,row_admission uuid)
 RETURNS TABLE (day_id uuid, census_date date, gross_minor bigint, discount_minor bigint, invoiced boolean, taxable boolean) LANGUAGE sql STABLE AS $$
 SELECT e.source_reference, d.census_date, e.amount_minor, billing_discount_on(e.tenant_id,e.source_reference),
  EXISTS (SELECT 1 FROM billing_tax_invoice_lines l WHERE l.tenant_id=e.tenant_id AND l.entry_id=e.id),
  billing_room_taxable(e.tenant_id,e.source_reference)
 FROM billing_account_entries e JOIN billing_bed_days d ON d.tenant_id=e.tenant_id AND d.id=e.source_reference
 WHERE e.tenant_id=row_tenant AND e.admission_id=row_admission AND e.source_module='bed' AND e.kind='charge' AND e.currency='INR'
  AND NOT EXISTS (SELECT 1 FROM billing_account_entries r WHERE r.tenant_id=e.tenant_id AND r.source_module='bed' AND r.kind='reversal' AND r.charge_reference=e.source_reference)
$$;

-- The base a percentage is of: the gross of the standing untaxed room charges.
CREATE FUNCTION billing_discount_base(row_tenant uuid,row_admission uuid) RETURNS bigint LANGUAGE sql STABLE AS $$
 SELECT COALESCE(sum(gross_minor),0)::bigint FROM billing_room_charges(row_tenant,row_admission) WHERE NOT taxable
$$;

-- What a new discount can be split over: what each eligible charge still carries.
CREATE FUNCTION billing_discountable(row_tenant uuid,row_admission uuid) RETURNS bigint LANGUAGE sql STABLE AS $$
 SELECT COALESCE(sum(gross_minor-discount_minor),0)::bigint FROM billing_room_charges(row_tenant,row_admission) WHERE NOT taxable AND NOT invoiced
$$;

-- A reason's net discount on an admission: its lines less their withdrawals.
CREATE FUNCTION billing_discount_given(row_tenant uuid,row_admission uuid,row_reason uuid) RETURNS bigint LANGUAGE sql STABLE AS $$
 SELECT COALESCE(sum(CASE x.kind WHEN 'reversal' THEN x.amount_minor ELSE -x.amount_minor END),0)::bigint
  FROM billing_account_entries x JOIN billing_discounts d ON d.tenant_id=x.tenant_id AND d.id=x.discount_id
  WHERE x.tenant_id=row_tenant AND x.admission_id=row_admission AND d.reason_id=row_reason
$$;

-- Whether an amount fits: the room, the balance, and then the reason's limit
-- at the level asked ('desk' or 'approval'). Raises with the constraint the
-- API maps; returns whether the desk's own limit covers it.
CREATE FUNCTION billing_discount_fits(d billing_discounts, level text) RETURNS boolean LANGUAGE plpgsql STABLE AS $$
DECLARE r record; given bigint; base bigint; total bigint;
BEGIN
 SELECT * INTO r FROM billing_discount_reasons WHERE tenant_id=d.tenant_id AND hospital_id=d.hospital_id AND id=d.reason_id;
 IF NOT FOUND OR r.retired_at IS NOT NULL THEN
  RAISE EXCEPTION 'the reason is retired' USING ERRCODE='23514', CONSTRAINT='billing_discount_reason_retired';
 END IF;
 IF d.amount_minor > billing_discountable(d.tenant_id,d.admission_id) THEN
  RAISE EXCEPTION 'a discount is never more than the exempt, uninvoiced room charges carry' USING ERRCODE='23514', CONSTRAINT='billing_discount_room';
 END IF;
 IF d.amount_minor > billing_payable(d.tenant_id,d.admission_id,d.currency) THEN
  RAISE EXCEPTION 'a discount is never more than the balance payable' USING ERRCODE='23514', CONSTRAINT='billing_discount_balance';
 END IF;
 given := billing_discount_given(d.tenant_id,d.admission_id,d.reason_id);
 base := billing_discount_base(d.tenant_id,d.admission_id);
 total := given + d.amount_minor;
 IF total > r.approval_max_minor OR total::numeric*10000 > r.approval_percent_bp::numeric*base THEN
  RAISE EXCEPTION 'the discount is beyond what the reason allows' USING ERRCODE='23514', CONSTRAINT='billing_discount_limit';
 END IF;
 RETURN level='desk' AND total <= r.desk_max_minor AND total::numeric*10000 <= r.desk_percent_bp::numeric*base;
END $$;

-- A new discount is asked by the acting subject and decided here; a pending
-- one is decided once, by someone else, re-checked as things stand then.
CREATE FUNCTION billing_discount_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id::text||':billing-bed-days:'||NEW.admission_id::text, 0));
 IF TG_OP='INSERT' THEN
  IF NEW.requested_by IS DISTINCT FROM billing_actor() OR NEW.decided_by IS NOT NULL OR NEW.decision_note IS NOT NULL THEN
   RAISE EXCEPTION 'a discount is asked by the acting subject' USING ERRCODE='23514';
  END IF;
  NEW.requested_at := clock_timestamp();
  IF billing_discount_fits(NEW,'desk') THEN
   NEW.status := 'applied'; NEW.applied_at := clock_timestamp(); NEW.applying_xact := pg_current_xact_id();
  ELSE
   NEW.status := 'pending'; NEW.applied_at := NULL; NEW.applying_xact := NULL;
  END IF;
  RETURN NEW;
 END IF;
 IF (NEW.tenant_id,NEW.hospital_id,NEW.admission_id,NEW.id,NEW.reason_id,NEW.amount_minor,NEW.currency,NEW.justification,NEW.requested_by,NEW.requested_at)
  IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.admission_id,OLD.id,OLD.reason_id,OLD.amount_minor,OLD.currency,OLD.justification,OLD.requested_by,OLD.requested_at)
  OR OLD.status<>'pending' OR NEW.status NOT IN ('applied','rejected') OR NEW.decided_by IS DISTINCT FROM billing_actor() THEN
  RAISE EXCEPTION 'a pending discount is decided once, by the acting subject' USING ERRCODE='23514';
 END IF;
 IF NEW.decided_by = OLD.requested_by THEN
  RAISE EXCEPTION 'a discount is decided by someone other than who asked' USING ERRCODE='23514', CONSTRAINT='billing_discount_own_request';
 END IF;
 NEW.decided_at := clock_timestamp();
 IF NEW.status='applied' THEN
  PERFORM billing_discount_fits(NEW,'approval');
  NEW.applied_at := clock_timestamp(); NEW.applying_xact := pg_current_xact_id();
 ELSE
  NEW.applied_at := NULL; NEW.applying_xact := NULL;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER billing_discount_guard BEFORE INSERT OR UPDATE ON billing_discounts FOR EACH ROW EXECUTE FUNCTION billing_discount_guard();

-- An applied discount is split over the eligible charges in proportion to what
-- each still carries: each share floored, the leftover paise to the largest
-- remainders, then the earlier day, then the day's id. Each share is at most
-- what its charge carries, so the lines sum to the amount exactly.
CREATE FUNCTION billing_discount_allocate() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.status<>'applied' OR (TG_OP='UPDATE' AND OLD.status='applied') THEN RETURN NULL; END IF;
 INSERT INTO billing_account_entries (tenant_id,hospital_id,id,admission_id,kind,source_module,source_reference,source_document,charge_reference,amount_minor,currency,occurred_at,discount_id)
 WITH room AS (
  SELECT day_id, census_date, gross_minor-discount_minor AS left_minor FROM billing_room_charges(NEW.tenant_id,NEW.admission_id)
   WHERE NOT taxable AND NOT invoiced AND gross_minor-discount_minor > 0),
 shares AS (
  SELECT r.*, floor(NEW.amount_minor::numeric*r.left_minor/t.total)::bigint AS floor_minor, mod(NEW.amount_minor::numeric*r.left_minor, t.total) AS rem
   FROM room r CROSS JOIN (SELECT sum(left_minor)::numeric AS total FROM room) t),
 ranked AS (
  SELECT s.*, row_number() OVER (ORDER BY s.rem DESC, s.census_date, s.day_id) AS k,
   NEW.amount_minor - sum(s.floor_minor) OVER () AS leftover FROM shares s)
 SELECT NEW.tenant_id,NEW.hospital_id,billing_uuid_v7(),NEW.admission_id,'reversal','discount',gen_random_uuid(),
  'Discount, bed day '||to_char(census_date,'YYYY-MM-DD'),day_id,floor_minor+CASE WHEN k<=leftover THEN 1 ELSE 0 END,NEW.currency,clock_timestamp(),NEW.id
 FROM ranked WHERE floor_minor+CASE WHEN k<=leftover THEN 1 ELSE 0 END > 0;
 RETURN NULL;
END $$;
CREATE TRIGGER billing_discount_allocate AFTER INSERT OR UPDATE ON billing_discounts FOR EACH ROW EXECUTE FUNCTION billing_discount_allocate();

-- At commit an applied discount's lines are exactly its amount.
CREATE FUNCTION billing_discount_complete() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.status='applied' AND (SELECT COALESCE(sum(amount_minor),0) FROM billing_account_entries
   WHERE tenant_id=NEW.tenant_id AND discount_id=NEW.id AND kind='reversal') <> NEW.amount_minor THEN
  RAISE EXCEPTION 'an applied discount''s lines sum to its amount' USING ERRCODE='23514', CONSTRAINT='billing_discount_complete';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER billing_discount_complete AFTER INSERT OR UPDATE ON billing_discounts DEFERRABLE INITIALLY DEFERRED
 FOR EACH ROW EXECUTE FUNCTION billing_discount_complete();

-- A discount line is written only by the transaction applying its discount,
-- against an eligible charge of its admission; a withdrawal only by billing's
-- consumers, for a standing line of a voided day, in full.
CREATE FUNCTION billing_discount_entry_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE line record;
BEGIN
 IF NEW.source_module<>'discount' THEN RETURN NEW; END IF;
 IF NEW.kind='reversal' THEN
  IF NOT EXISTS (SELECT 1 FROM billing_discounts d WHERE d.tenant_id=NEW.tenant_id AND d.id=NEW.discount_id AND d.hospital_id=NEW.hospital_id
    AND d.admission_id=NEW.admission_id AND d.currency=NEW.currency AND d.status='applied' AND d.applying_xact=pg_current_xact_id())
   OR NOT EXISTS (SELECT 1 FROM billing_room_charges(NEW.tenant_id,NEW.admission_id) c WHERE c.day_id=NEW.charge_reference AND NOT c.taxable AND NOT c.invoiced) THEN
   RAISE EXCEPTION 'a discount line is written as its discount is applied, on an eligible room charge' USING ERRCODE='23514', CONSTRAINT='billing_discount_entry';
  END IF;
  RETURN NEW;
 END IF;
 SELECT x.* INTO line FROM billing_account_entries x WHERE x.tenant_id=NEW.tenant_id AND x.source_module='discount' AND x.kind='reversal' AND x.source_reference=NEW.source_reference;
 IF NOT billing_consumer() OR line.id IS NULL OR line.discount_id<>NEW.discount_id OR line.admission_id<>NEW.admission_id OR line.amount_minor<>NEW.amount_minor
  OR line.currency<>NEW.currency OR NOT EXISTS (SELECT 1 FROM billing_bed_days b WHERE b.tenant_id=NEW.tenant_id AND b.id=line.charge_reference AND b.voided_at IS NOT NULL) THEN
  RAISE EXCEPTION 'a discount is withdrawn in full, by billing, from a voided day' USING ERRCODE='23514', CONSTRAINT='billing_discount_entry';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER billing_discount_entry_guard BEFORE INSERT ON billing_account_entries FOR EACH ROW EXECUTE FUNCTION billing_discount_entry_guard();

-- As in 0001, and: a discount line bounds against its room charge, and a room
-- charge's reversals count its standing discount lines.
CREATE OR REPLACE FUNCTION billing_reversal_bounded() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE charge record; reversed bigint; charge_module text;
BEGIN
 IF NEW.kind<>'reversal' THEN RETURN NEW; END IF;
 charge_module := CASE NEW.source_module WHEN 'discount' THEN 'bed' ELSE NEW.source_module END;
 -- Reversals of one charge are taken one at a time: two concurrent ones
 -- would each sum only committed reversals and together exceed the charge.
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id::text||':billing-charge:'||NEW.charge_reference::text, 0));
 SELECT admission_id,amount_minor,currency INTO charge FROM billing_account_entries
  WHERE tenant_id=NEW.tenant_id AND source_module=charge_module AND source_reference=NEW.charge_reference AND kind='charge' FOR SHARE;
 IF NOT FOUND OR charge.admission_id<>NEW.admission_id OR charge.currency<>NEW.currency THEN
  RAISE EXCEPTION 'a reversal names a recorded charge of the same admission and currency' USING ERRCODE='23514', CONSTRAINT='billing_reversal_bounded';
 END IF;
 SELECT COALESCE(sum(amount_minor),0) INTO reversed FROM billing_account_entries
  WHERE tenant_id=NEW.tenant_id AND kind='reversal' AND source_module=charge_module AND charge_reference=NEW.charge_reference;
 IF charge_module='bed' THEN
  reversed := reversed + billing_discount_on(NEW.tenant_id,NEW.charge_reference);
 END IF;
 IF reversed + NEW.amount_minor > charge.amount_minor THEN
  RAISE EXCEPTION 'reversals cannot exceed the charge' USING ERRCODE='23514', CONSTRAINT='billing_reversal_bounded';
 END IF;
 RETURN NEW;
END $$;

-- A bill of supply line shows the discount and the value after it (CGST Rule
-- 46(1)(i)); a fully conceded day is a line of nil value.
ALTER TABLE billing_tax_invoice_lines ADD COLUMN discount_minor bigint NOT NULL DEFAULT 0 CHECK (discount_minor >= 0);
ALTER TABLE billing_tax_invoice_lines ADD CONSTRAINT billing_tax_invoice_line_discount_exempt CHECK (discount_minor=0 OR NOT taxable);
ALTER TABLE billing_tax_invoice_lines DROP CONSTRAINT billing_tax_invoice_lines_value_minor_check;
ALTER TABLE billing_tax_invoice_lines ADD CONSTRAINT billing_tax_invoice_lines_value_minor_check CHECK (value_minor >= 0);
ALTER TABLE billing_credit_note_lines DROP CONSTRAINT billing_credit_note_lines_value_minor_check;
ALTER TABLE billing_credit_note_lines ADD CONSTRAINT billing_credit_note_lines_value_minor_check CHECK (value_minor >= 0);

-- As in 0009, with the day's discount taken off its value.
CREATE OR REPLACE FUNCTION billing_tax_invoice_line_fill() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE d record; t record; p record; off bigint;
BEGIN
 PERFORM 1 FROM billing_tax_invoices i WHERE i.tenant_id=NEW.tenant_id AND i.id=NEW.invoice_id AND i.admission_id=NEW.admission_id
  AND i.issuing_xact=pg_current_xact_id() AND i.issued_by=NULLIF(current_setting('app.actor_subject',true),'');
 IF NOT FOUND THEN
  RAISE EXCEPTION 'an invoice''s lines are written only as it is issued' USING ERRCODE='23514';
 END IF;
 IF NOT EXISTS (SELECT 1 FROM billing_invoiceable(NEW.tenant_id,NEW.admission_id) x WHERE x.entry_id=NEW.entry_id) THEN
  RAISE EXCEPTION 'an invoice carries standing room charges from the registration, once' USING ERRCODE='23514', CONSTRAINT='billing_tax_invoice_line';
 END IF;
 SELECT b.census_date,b.bed_class,b.daily_rate_minor,b.id INTO d FROM billing_account_entries e JOIN billing_bed_days b ON b.tenant_id=e.tenant_id AND b.id=e.source_reference
  WHERE e.tenant_id=NEW.tenant_id AND e.id=NEW.entry_id;
 SELECT * INTO p FROM billing_gst_room_policy(d.census_date);
 SELECT * INTO t FROM billing_gst_lines l WHERE l.tenant_id=NEW.tenant_id AND l.bed_day_id=d.id;
 -- The day's tax must already be on the account if the policy taxes it.
 IF (t.bed_day_id IS NOT NULL) <> (d.bed_class <> ALL(p.exempt_classes) AND d.daily_rate_minor > p.threshold_minor) THEN
  -- Not the desk's error: the census and the policy disagree.
  RAISE EXCEPTION 'a room day''s tax is not yet recorded' USING ERRCODE='23514', CONSTRAINT='billing_tax_invoice_untaxed';
 END IF;
 off := billing_discount_on(NEW.tenant_id,d.id);
 NEW.census_date := d.census_date; NEW.bed_class := d.bed_class;
 NEW.discount_minor := off; NEW.value_minor := d.daily_rate_minor - off;
 -- The version the tax was computed under, not a later one.
 NEW.sac := COALESCE(t.sac, p.sac); NEW.policy_version := COALESCE(t.policy_version, p.version);
 NEW.taxable := t.bed_day_id IS NOT NULL;
 NEW.cgst_bp := COALESCE(t.cgst_bp,0); NEW.sgst_bp := COALESCE(t.sgst_bp,0);
 NEW.cgst_minor := COALESCE(t.cgst_minor,0); NEW.sgst_minor := COALESCE(t.sgst_minor,0);
 RETURN NEW;
END $$;
