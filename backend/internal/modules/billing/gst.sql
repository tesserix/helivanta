SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- GST on hospital rooms and the admission tax invoice (#193 slice 1).
-- Health care by a clinical establishment is exempt (12/2017-CT(Rate) s.74),
-- except a room other than ICU/CCU/ICCU/NICU charged above Rs 5000 a day
-- (proviso by 04/2022-CT(Rate)), taxed under 03/2022-CT(Rate) entry 31A,
-- Heading 9993, 2.5% CGST without input tax credit, and SGST likewise; both in
-- force from 18 July 2022. A hospital is taxed only once it records its GSTIN.

-- A GSTIN: state, PAN, entity number, Z, and the base-36 check character.
CREATE FUNCTION billing_gstin_valid(g text) RETURNS boolean LANGUAGE plpgsql IMMUTABLE AS $$
DECLARE chars constant text := '0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ'; total int := 0; p int;
BEGIN
 IF g IS NULL OR g !~ '^(0[1-9]|[12][0-9]|3[0-8])[A-Z]{5}[0-9]{4}[A-Z][1-9A-Z]Z[0-9A-Z]$' THEN
  RETURN false;
 END IF;
 FOR i IN 1..14 LOOP
  p := (strpos(chars, substr(g,i,1)) - 1) * (2 - i % 2);
  total := total + p / 36 + p % 36;
 END LOOP;
 RETURN substr(chars, (36 - total % 36) % 36 + 1, 1) = substr(g,15,1);
END $$;

-- The room policy in force on a date, one row per version with its source.
-- An amendment is a new version in a reviewed migration, never an edit here.
CREATE FUNCTION billing_gst_room_policy(on_date date)
 RETURNS TABLE (version text, threshold_minor bigint, exempt_classes text[], cgst_bp integer, sgst_bp integer, sac text)
 LANGUAGE sql IMMUTABLE AS $$
 SELECT v.version, v.threshold_minor, v.exempt_classes, v.cgst_bp, v.sgst_bp, v.sac FROM (VALUES
  -- 04/2022-CT(Rate) proviso to 12/2017 s.74 and 03/2022-CT(Rate) entry 31A, from 2022-07-18:
  -- taxable when the room charge exceeds Rs 5000 a day and the room is not ICU/CCU/ICCU/NICU.
  ('IN-ROOM-2022-07-18', DATE '2022-07-18', 500000::bigint, ARRAY['icu','nicu'], 250, 250, '9993')
 ) v(version, effective_from, threshold_minor, exempt_classes, cgst_bp, sgst_bp, sac)
 WHERE v.effective_from <= on_date ORDER BY v.effective_from DESC LIMIT 1
$$;

-- A hospital's GST registration, set once, from its own tomorrow or later.
CREATE TABLE billing_gst_registrations (
 tenant_id uuid NOT NULL,
 hospital_id uuid NOT NULL,
 id uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
 gstin text NOT NULL CONSTRAINT billing_gstin CHECK (billing_gstin_valid(gstin)),
 legal_name text NOT NULL CHECK (legal_name=btrim(legal_name) AND length(legal_name) BETWEEN 1 AND 200),
 address text NOT NULL CHECK (address=btrim(address) AND length(address) BETWEEN 1 AND 500),
 effective_from date NOT NULL,
 created_by text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 applied_at timestamptz,
 PRIMARY KEY (tenant_id,hospital_id),
 -- The hospital's timezone dates the registration and the invoices.
 FOREIGN KEY (tenant_id,hospital_id) REFERENCES billing_bed_charging (tenant_id,hospital_id)
);

-- From tomorrow at the earliest: a room day dated today or before may already
-- be on a final bill the patient has settled, and adding tax to it would
-- reopen that account. Taxing past days is a finance decision. Rates at a
-- registered hospital are INR, the only currency GST is computed in. The
-- hospital's bed-charging row is locked so a rate and a registration being
-- written at once are checked one after the other.
CREATE FUNCTION billing_gst_registration_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.effective_from <= (SELECT (now() AT TIME ZONE c.timezone)::date FROM billing_bed_charging c WHERE c.tenant_id=NEW.tenant_id AND c.hospital_id=NEW.hospital_id FOR UPDATE) THEN
  RAISE EXCEPTION 'a GST registration takes effect from the hospital''s tomorrow or later' USING ERRCODE='23514', CONSTRAINT='billing_gst_backdated';
 END IF;
 IF EXISTS (SELECT 1 FROM billing_bed_rates r WHERE r.tenant_id=NEW.tenant_id AND r.hospital_id=NEW.hospital_id AND r.currency<>'INR') THEN
  RAISE EXCEPTION 'a GST-registered hospital''s bed rates are in INR' USING ERRCODE='23514', CONSTRAINT='billing_gst_inr';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER billing_gst_registration_guard BEFORE INSERT ON billing_gst_registrations FOR EACH ROW EXECUTE FUNCTION billing_gst_registration_guard();
CREATE TRIGGER billing_gst_registration_unapplied BEFORE INSERT ON billing_gst_registrations FOR EACH ROW EXECUTE FUNCTION billing_bed_setup_unapplied();
CREATE TRIGGER billing_gst_registration_fixed BEFORE UPDATE ON billing_gst_registrations FOR EACH ROW EXECUTE FUNCTION billing_bed_setup_guard();

CREATE FUNCTION billing_bed_rate_inr() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM 1 FROM billing_bed_charging c WHERE c.tenant_id=NEW.tenant_id AND c.hospital_id=NEW.hospital_id FOR UPDATE;
 IF NEW.currency<>'INR' AND EXISTS (SELECT 1 FROM billing_gst_registrations g WHERE g.tenant_id=NEW.tenant_id AND g.hospital_id=NEW.hospital_id) THEN
  RAISE EXCEPTION 'a GST-registered hospital''s bed rates are in INR' USING ERRCODE='23514', CONSTRAINT='billing_gst_inr';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER billing_bed_rate_inr BEFORE INSERT ON billing_bed_rates FOR EACH ROW EXECUTE FUNCTION billing_bed_rate_inr();

-- The tax on one taxable bed day, computed here from the day, the policy in
-- force on its date and the registration; a caller supplies only the day.
CREATE TABLE billing_gst_lines (
 tenant_id uuid NOT NULL,
 bed_day_id uuid NOT NULL,
 id uuid GENERATED ALWAYS AS (bed_day_id) STORED,
 hospital_id uuid NOT NULL,
 admission_id uuid NOT NULL,
 census_date date NOT NULL,
 bed_class text NOT NULL,
 policy_version text NOT NULL,
 sac text NOT NULL,
 taxable_minor bigint NOT NULL CHECK (taxable_minor >= 1),
 cgst_bp integer NOT NULL CHECK (cgst_bp BETWEEN 0 AND 10000),
 sgst_bp integer NOT NULL CHECK (sgst_bp BETWEEN 0 AND 10000),
 cgst_minor bigint NOT NULL CHECK (cgst_minor >= 0),
 sgst_minor bigint NOT NULL CHECK (sgst_minor >= 0),
 recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY (tenant_id,bed_day_id),
 FOREIGN KEY (tenant_id,bed_day_id) REFERENCES billing_bed_days (tenant_id,id),
 FOREIGN KEY (tenant_id,hospital_id,admission_id) REFERENCES billing_admission_accounts (tenant_id,hospital_id,admission_id)
);

CREATE FUNCTION billing_gst_line_compute() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE d record; p record;
BEGIN
 SELECT b.hospital_id,b.admission_id,b.census_date,b.bed_class,b.daily_rate_minor,b.currency INTO d
  FROM billing_bed_days b JOIN billing_gst_registrations g ON g.tenant_id=b.tenant_id AND g.hospital_id=b.hospital_id AND g.effective_from<=b.census_date
  WHERE b.tenant_id=NEW.tenant_id AND b.id=NEW.bed_day_id AND b.priced_at IS NOT NULL AND b.voided_at IS NULL;
 SELECT * INTO p FROM billing_gst_room_policy(d.census_date);
 IF d.census_date IS NULL OR p.version IS NULL OR d.currency<>'INR' OR d.bed_class = ANY(p.exempt_classes) OR d.daily_rate_minor <= p.threshold_minor THEN
  RAISE EXCEPTION 'GST is recorded only for a priced, live, taxable room day at a registered hospital' USING ERRCODE='23514', CONSTRAINT='billing_gst_line_taxable';
 END IF;
 NEW.hospital_id := d.hospital_id; NEW.admission_id := d.admission_id;
 NEW.census_date := d.census_date; NEW.bed_class := d.bed_class;
 NEW.policy_version := p.version; NEW.sac := p.sac; NEW.taxable_minor := d.daily_rate_minor;
 NEW.cgst_bp := p.cgst_bp; NEW.sgst_bp := p.sgst_bp;
 -- Half up, in paise, each tax on its own.
 NEW.cgst_minor := (d.daily_rate_minor * p.cgst_bp + 5000) / 10000;
 NEW.sgst_minor := (d.daily_rate_minor * p.sgst_bp + 5000) / 10000;
 NEW.recorded_at := clock_timestamp();
 RETURN NEW;
END $$;
CREATE TRIGGER billing_gst_line_compute BEFORE INSERT ON billing_gst_lines FOR EACH ROW EXECUTE FUNCTION billing_gst_line_compute();

-- Tax is a charge on the account like any other ('gst', keyed on the day),
-- of exactly the day's CGST and SGST.
ALTER TABLE billing_account_entries DROP CONSTRAINT billing_account_entries_source_module_check;
ALTER TABLE billing_account_entries ADD CONSTRAINT billing_account_entries_source_module_check CHECK (source_module IN ('pharmacy','bed','gst'));
CREATE FUNCTION billing_gst_entry_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.kind='charge' AND NOT EXISTS (SELECT 1 FROM billing_gst_lines l WHERE l.tenant_id=NEW.tenant_id AND l.bed_day_id=NEW.source_reference
   AND l.admission_id=NEW.admission_id AND l.cgst_minor+l.sgst_minor=NEW.amount_minor AND NEW.currency='INR') THEN
  RAISE EXCEPTION 'a GST charge is its day''s recorded tax' USING ERRCODE='23514', CONSTRAINT='billing_gst_entry';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER billing_gst_entry_guard BEFORE INSERT ON billing_account_entries FOR EACH ROW WHEN (NEW.source_module='gst') EXECUTE FUNCTION billing_gst_entry_guard();

-- A final bill also waits for a new registration to reach the account.
CREATE OR REPLACE FUNCTION billing_bed_days_settled(row_tenant uuid,row_hospital uuid,row_admission uuid) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT EXISTS (SELECT 1 FROM billing_bed_charging c WHERE c.tenant_id=row_tenant AND c.hospital_id=row_hospital AND c.applied_at IS NOT NULL)
 AND NOT EXISTS (SELECT 1 FROM billing_bed_rates r WHERE r.tenant_id=row_tenant AND r.hospital_id=row_hospital AND r.applied_at IS NULL)
 AND NOT EXISTS (SELECT 1 FROM billing_gst_registrations g WHERE g.tenant_id=row_tenant AND g.hospital_id=row_hospital AND g.applied_at IS NULL)
 AND EXISTS (SELECT 1 FROM billing_bed_stays s WHERE s.tenant_id=row_tenant AND s.admission_id=row_admission)
 AND NOT EXISTS (SELECT 1 FROM billing_bed_stays s JOIN billing_admission_accounts a ON a.tenant_id=s.tenant_id AND a.admission_id=s.admission_id
  WHERE s.tenant_id=row_tenant AND s.admission_id=row_admission
  AND (s.started_at IS NULL
   OR (s.ended_at IS NULL AND (a.status='closed' OR s.next_census_at IS NULL OR s.next_census_at - interval '15 minutes' <= clock_timestamp()))))
$$;

-- The Indian financial year of a date, April to March: 2026-04-01 to
-- 2027-03-31 is 2627.
CREATE FUNCTION billing_financial_year(d date) RETURNS text LANGUAGE sql IMMUTABLE AS $$
 SELECT to_char(d - interval '3 months','YY') || to_char(d + interval '9 months','YY')
$$;

-- The invoice-cum-bill of supply (CGST Rules 46, 46A) for an admission's
-- rooms: numbered per GSTIN and financial year, set here; never changed.
CREATE TABLE billing_tax_invoices (
 tenant_id uuid NOT NULL,
 hospital_id uuid NOT NULL,
 admission_id uuid NOT NULL,
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 issued_by text NOT NULL CHECK (length(btrim(issued_by)) BETWEEN 1 AND 200),
 recipient_name text NOT NULL CHECK (recipient_name=btrim(recipient_name) AND length(recipient_name) BETWEEN 1 AND 200),
 recipient_address text NOT NULL CHECK (recipient_address=btrim(recipient_address) AND length(recipient_address) BETWEEN 1 AND 500),
 -- Set by the trigger from the registration and the hospital's date.
 gstin text NOT NULL,
 supplier_name text NOT NULL,
 supplier_address text NOT NULL,
 state_code text NOT NULL CHECK (state_code ~ '^[0-9]{2}$'),
 financial_year text NOT NULL CHECK (financial_year ~ '^[0-9]{4}$'),
 serial integer NOT NULL CHECK (serial BETWEEN 1 AND 9999999),
 number text GENERATED ALWAYS AS ('IP/' || financial_year || '/' || serial::text) STORED,
 issue_date date NOT NULL,
 issued_at timestamptz NOT NULL,
 issuing_xact xid8 NOT NULL,
 PRIMARY KEY (tenant_id,id),
 UNIQUE (tenant_id,hospital_id,admission_id,id),
 CONSTRAINT billing_tax_invoice_serial UNIQUE (tenant_id,gstin,financial_year,serial),
 FOREIGN KEY (tenant_id,hospital_id,admission_id) REFERENCES billing_admission_accounts (tenant_id,hospital_id,admission_id)
);
CREATE INDEX billing_tax_invoices_page ON billing_tax_invoices (tenant_id,admission_id,issued_at DESC,id DESC);

-- One line per bed charge, a snapshot of the day and its tax.
CREATE TABLE billing_tax_invoice_lines (
 tenant_id uuid NOT NULL,
 hospital_id uuid NOT NULL,
 admission_id uuid NOT NULL,
 invoice_id uuid NOT NULL,
 entry_id uuid NOT NULL,
 id uuid GENERATED ALWAYS AS (entry_id) STORED,
 census_date date NOT NULL,
 bed_class text NOT NULL,
 sac text NOT NULL,
 policy_version text NOT NULL,
 value_minor bigint NOT NULL CHECK (value_minor >= 1),
 taxable boolean NOT NULL,
 cgst_bp integer NOT NULL,
 sgst_bp integer NOT NULL,
 cgst_minor bigint NOT NULL,
 sgst_minor bigint NOT NULL,
 -- A bed charge is on one invoice.
 PRIMARY KEY (tenant_id,entry_id),
 FOREIGN KEY (tenant_id,hospital_id,admission_id,invoice_id) REFERENCES billing_tax_invoices (tenant_id,hospital_id,admission_id,id),
 FOREIGN KEY (tenant_id,admission_id,entry_id) REFERENCES billing_account_entries (tenant_id,admission_id,id),
 CHECK (taxable OR (cgst_minor=0 AND sgst_minor=0 AND cgst_bp=0 AND sgst_bp=0))
);
CREATE INDEX billing_tax_invoice_lines_invoice ON billing_tax_invoice_lines (tenant_id,invoice_id,census_date,entry_id);

-- What an admission can be invoiced for now: its standing bed charges dated
-- from the registration and on no invoice yet.
CREATE FUNCTION billing_invoiceable(row_tenant uuid,row_admission uuid) RETURNS TABLE (entry_id uuid) LANGUAGE sql STABLE AS $$
 SELECT e.id FROM billing_account_entries e
  JOIN billing_bed_days d ON d.tenant_id=e.tenant_id AND d.id=e.source_reference
  JOIN billing_gst_registrations g ON g.tenant_id=e.tenant_id AND g.hospital_id=e.hospital_id AND g.effective_from<=d.census_date
 WHERE e.tenant_id=row_tenant AND e.admission_id=row_admission AND e.source_module='bed' AND e.kind='charge'
  AND NOT EXISTS (SELECT 1 FROM billing_account_entries r WHERE r.tenant_id=e.tenant_id AND r.source_module='bed' AND r.kind='reversal' AND r.charge_reference=e.source_reference)
  AND NOT EXISTS (SELECT 1 FROM billing_tax_invoice_lines l WHERE l.tenant_id=e.tenant_id AND l.entry_id=e.id)
$$;

-- An invoice needs an applied registration and a final bill, and no
-- invoiced charge since reversed: that needs a credit note, not built yet.
-- Its number is the next in the GSTIN's year, under a lock on that series.
CREATE FUNCTION billing_tax_invoice_next() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE g record; today date;
BEGIN
 PERFORM 1 FROM billing_admission_accounts WHERE tenant_id=NEW.tenant_id AND hospital_id=NEW.hospital_id AND admission_id=NEW.admission_id FOR SHARE;
 SELECT r.gstin,r.legal_name,r.address,(now() AT TIME ZONE c.timezone)::date AS today INTO g
  FROM billing_gst_registrations r JOIN billing_bed_charging c ON c.tenant_id=r.tenant_id AND c.hospital_id=r.hospital_id
  WHERE r.tenant_id=NEW.tenant_id AND r.hospital_id=NEW.hospital_id AND r.applied_at IS NOT NULL;
 IF NOT FOUND THEN
  RAISE EXCEPTION 'the hospital is not registered for GST' USING ERRCODE='23514', CONSTRAINT='billing_tax_invoice_registered';
 END IF;
 IF NOT EXISTS (SELECT 1 FROM billing_interim_bills b WHERE b.tenant_id=NEW.tenant_id AND b.admission_id=NEW.admission_id AND b.kind='final') THEN
  RAISE EXCEPTION 'a tax invoice follows a final bill' USING ERRCODE='23514', CONSTRAINT='billing_tax_invoice_final_bill';
 END IF;
 IF EXISTS (SELECT 1 FROM billing_tax_invoice_lines l JOIN billing_account_entries e ON e.tenant_id=l.tenant_id AND e.id=l.entry_id
   JOIN billing_account_entries r ON r.tenant_id=e.tenant_id AND r.source_module='bed' AND r.kind='reversal' AND r.charge_reference=e.source_reference
   WHERE l.tenant_id=NEW.tenant_id AND l.admission_id=NEW.admission_id) THEN
  RAISE EXCEPTION 'an invoiced charge was reversed: a credit note is needed first' USING ERRCODE='23514', CONSTRAINT='billing_tax_invoice_credit_note';
 END IF;
 IF NEW.serial IS NOT NULL OR NEW.issued_at IS NOT NULL OR NEW.issuing_xact IS NOT NULL OR NEW.issued_by IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'') THEN
  RAISE EXCEPTION 'an invoice is numbered and dated as it is issued, by its issuer' USING ERRCODE='23514';
 END IF;
 today := g.today;
 NEW.gstin := g.gstin; NEW.supplier_name := g.legal_name; NEW.supplier_address := g.address;
 NEW.state_code := substr(g.gstin,1,2);
 NEW.issue_date := today;
 NEW.financial_year := billing_financial_year(today);
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id::text||':billing-tax-invoice:'||NEW.gstin||':'||NEW.financial_year, 0));
 NEW.serial := (SELECT COALESCE(max(serial),0)+1 FROM billing_tax_invoices WHERE tenant_id=NEW.tenant_id AND gstin=NEW.gstin AND financial_year=NEW.financial_year);
 NEW.issued_at := clock_timestamp();
 NEW.issuing_xact := pg_current_xact_id();
 RETURN NEW;
END $$;
CREATE TRIGGER billing_tax_invoice_next BEFORE INSERT ON billing_tax_invoices FOR EACH ROW EXECUTE FUNCTION billing_tax_invoice_next();

-- A line is written only as its invoice is issued, for a charge it may
-- carry, and is the snapshot of that day and its tax, set here.
CREATE FUNCTION billing_tax_invoice_line_fill() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE d record; t record; p record;
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
 NEW.census_date := d.census_date; NEW.bed_class := d.bed_class; NEW.value_minor := d.daily_rate_minor;
 -- The version the tax was computed under, not a later one.
 NEW.sac := COALESCE(t.sac, p.sac); NEW.policy_version := COALESCE(t.policy_version, p.version);
 NEW.taxable := t.bed_day_id IS NOT NULL;
 NEW.cgst_bp := COALESCE(t.cgst_bp,0); NEW.sgst_bp := COALESCE(t.sgst_bp,0);
 NEW.cgst_minor := COALESCE(t.cgst_minor,0); NEW.sgst_minor := COALESCE(t.sgst_minor,0);
 RETURN NEW;
END $$;
CREATE TRIGGER billing_tax_invoice_line_fill BEFORE INSERT ON billing_tax_invoice_lines FOR EACH ROW EXECUTE FUNCTION billing_tax_invoice_line_fill();

-- At commit, an issued invoice has lines and leaves nothing invoiceable
-- behind: it carries every charge it could, so none is skipped.
CREATE FUNCTION billing_tax_invoice_complete() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS (SELECT 1 FROM billing_tax_invoice_lines l WHERE l.tenant_id=NEW.tenant_id AND l.invoice_id=NEW.id)
  OR EXISTS (SELECT 1 FROM billing_invoiceable(NEW.tenant_id,NEW.admission_id)) THEN
  RAISE EXCEPTION 'an invoice carries every room charge it can, and at least one' USING ERRCODE='23514', CONSTRAINT='billing_tax_invoice_complete';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER billing_tax_invoice_complete AFTER INSERT ON billing_tax_invoices DEFERRABLE INITIALLY DEFERRED
 FOR EACH ROW EXECUTE FUNCTION billing_tax_invoice_complete();

DO $$ DECLARE t text; BEGIN
 -- Written by billing's consumers and census; read under the grant.
 ALTER TABLE billing_gst_lines ENABLE ROW LEVEL SECURITY;
 ALTER TABLE billing_gst_lines FORCE ROW LEVEL SECURITY;
 CREATE POLICY tenant_isolation ON billing_gst_lines
  USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND (billing_consumer() OR billing_hospital_allowed(tenant_id,hospital_id)))
  WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND billing_consumer());
 -- Set by an administrator holding the grant, as themselves; applied by the census.
 ALTER TABLE billing_gst_registrations ENABLE ROW LEVEL SECURITY;
 ALTER TABLE billing_gst_registrations FORCE ROW LEVEL SECURITY;
 CREATE POLICY tenant_isolation ON billing_gst_registrations
  USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND (billing_consumer() OR billing_hospital_allowed(tenant_id,hospital_id)))
  WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND (billing_consumer() OR (created_by=NULLIF(current_setting('app.actor_subject',true),'') AND billing_hospital_allowed(tenant_id,hospital_id))));
 FOREACH t IN ARRAY ARRAY['billing_tax_invoices','billing_tax_invoice_lines'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
  EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND billing_hospital_allowed(tenant_id,hospital_id)) WITH CHECK (tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND billing_hospital_allowed(tenant_id,hospital_id))', t);
 END LOOP;
 FOREACH t IN ARRAY ARRAY['billing_gst_lines','billing_gst_registrations','billing_tax_invoices','billing_tax_invoice_lines'] LOOP
  EXECUTE format('CREATE TRIGGER %I AFTER INSERT OR UPDATE ON %I FOR EACH ROW EXECUTE FUNCTION audit_record_change()', t||'_audit', t);
  EXECUTE format('CREATE TRIGGER %I BEFORE DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION billing_history_retained()', t||'_kept', t);
 END LOOP;
 FOREACH t IN ARRAY ARRAY['billing_gst_lines','billing_tax_invoices','billing_tax_invoice_lines'] LOOP
  EXECUTE format('CREATE TRIGGER %I BEFORE UPDATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION billing_history_retained()', t||'_fixed', t);
 END LOOP;
END $$;

-- The census finds new registrations across tenants by these columns alone.
GRANT SELECT (tenant_id, hospital_id, effective_from, applied_at, created_at) ON billing_gst_registrations TO helivanta_system;
