-- Credit notes for an admission's room invoices (#193 slice 2). A room day on
-- an issued invoice can still be reversed; the invoice never changes, so the
-- reversal's tax document is a credit note (CGST Act s.34(1)) with the
-- particulars of CGST Rule 53(1A). It credits exactly what was invoiced: each
-- line is a snapshot of its invoice line, never recomputed.

-- Open until 30 November after the end of the supply's financial year
-- (s.34(2), from 1 Oct 2022). The supply's year is its invoice's. The annual
-- return may close it earlier; the system does not know that date.
CREATE FUNCTION billing_credit_note_open(invoice_year text, today date) RETURNS boolean LANGUAGE sql IMMUTABLE AS $$
 SELECT today <= make_date(2000 + substr(invoice_year,1,2)::int + 1, 11, 30)
$$;

CREATE TABLE billing_credit_notes (
 tenant_id uuid NOT NULL,
 hospital_id uuid NOT NULL,
 admission_id uuid NOT NULL,
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 invoice_id uuid NOT NULL,
 issued_by text NOT NULL CHECK (length(btrim(issued_by)) BETWEEN 1 AND 200),
 -- Set by the trigger from the invoice and the hospital's date.
 gstin text NOT NULL,
 supplier_name text NOT NULL,
 supplier_address text NOT NULL,
 state_code text NOT NULL,
 recipient_name text NOT NULL,
 recipient_address text NOT NULL,
 invoice_number text NOT NULL,
 invoice_date date NOT NULL,
 financial_year text NOT NULL CHECK (financial_year ~ '^[0-9]{4}$'),
 serial integer NOT NULL CHECK (serial BETWEEN 1 AND 9999999),
 number text GENERATED ALWAYS AS ('CN/' || financial_year || '/' || serial::text) STORED,
 issue_date date NOT NULL,
 issued_at timestamptz NOT NULL,
 issuing_xact xid8 NOT NULL,
 PRIMARY KEY (tenant_id,id),
 UNIQUE (tenant_id,hospital_id,admission_id,invoice_id,id),
 CONSTRAINT billing_credit_note_serial UNIQUE (tenant_id,gstin,financial_year,serial),
 FOREIGN KEY (tenant_id,hospital_id,admission_id,invoice_id) REFERENCES billing_tax_invoices (tenant_id,hospital_id,admission_id,id)
);
CREATE INDEX billing_credit_notes_page ON billing_credit_notes (tenant_id,admission_id,issued_at DESC,id DESC);

ALTER TABLE billing_tax_invoice_lines ADD CONSTRAINT billing_tax_invoice_lines_invoice_entry UNIQUE (tenant_id,invoice_id,entry_id);

CREATE TABLE billing_credit_note_lines (
 tenant_id uuid NOT NULL,
 hospital_id uuid NOT NULL,
 admission_id uuid NOT NULL,
 credit_note_id uuid NOT NULL,
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
 -- An invoiced charge is credited once.
 PRIMARY KEY (tenant_id,entry_id),
 FOREIGN KEY (tenant_id,hospital_id,admission_id,invoice_id,credit_note_id) REFERENCES billing_credit_notes (tenant_id,hospital_id,admission_id,invoice_id,id),
 FOREIGN KEY (tenant_id,invoice_id,entry_id) REFERENCES billing_tax_invoice_lines (tenant_id,invoice_id,entry_id)
);
CREATE INDEX billing_credit_note_lines_note ON billing_credit_note_lines (tenant_id,credit_note_id,census_date,entry_id);

-- An invoice's lines whose bed charge was reversed and is on no credit note.
CREATE FUNCTION billing_creditable(row_tenant uuid,row_invoice uuid) RETURNS TABLE (entry_id uuid) LANGUAGE sql STABLE AS $$
 SELECT l.entry_id FROM billing_tax_invoice_lines l JOIN billing_account_entries e ON e.tenant_id=l.tenant_id AND e.id=l.entry_id
 WHERE l.tenant_id=row_tenant AND l.invoice_id=row_invoice
  AND EXISTS (SELECT 1 FROM billing_account_entries r WHERE r.tenant_id=e.tenant_id AND r.source_module='bed' AND r.kind='reversal' AND r.charge_reference=e.source_reference)
  AND NOT EXISTS (SELECT 1 FROM billing_credit_note_lines c WHERE c.tenant_id=l.tenant_id AND c.entry_id=l.entry_id)
$$;

-- A note copies its supplier and recipient from its invoice, is refused after
-- the deadline, and takes the next number in the GSTIN's year of issue.
CREATE FUNCTION billing_credit_note_next() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE i record; today date;
BEGIN
 PERFORM 1 FROM billing_admission_accounts WHERE tenant_id=NEW.tenant_id AND hospital_id=NEW.hospital_id AND admission_id=NEW.admission_id FOR SHARE;
 SELECT t.gstin,t.supplier_name,t.supplier_address,t.state_code,t.recipient_name,t.recipient_address,t.number,t.issue_date,t.financial_year INTO i
  FROM billing_tax_invoices t WHERE t.tenant_id=NEW.tenant_id AND t.hospital_id=NEW.hospital_id AND t.admission_id=NEW.admission_id AND t.id=NEW.invoice_id;
 IF NOT FOUND THEN
  RAISE EXCEPTION 'a credit note is for an invoice of its admission' USING ERRCODE='23503', CONSTRAINT='billing_credit_note_invoice';
 END IF;
 SELECT (now() AT TIME ZONE c.timezone)::date INTO today FROM billing_bed_charging c WHERE c.tenant_id=NEW.tenant_id AND c.hospital_id=NEW.hospital_id;
 -- Fails closed: no known date is no open window.
 IF today IS NULL OR NOT billing_credit_note_open(i.financial_year, today) THEN
  RAISE EXCEPTION 'the time to issue a credit note for this invoice has passed' USING ERRCODE='23514', CONSTRAINT='billing_credit_note_deadline';
 END IF;
 IF NEW.serial IS NOT NULL OR NEW.issued_at IS NOT NULL OR NEW.issuing_xact IS NOT NULL OR NEW.issued_by IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'') THEN
  RAISE EXCEPTION 'a credit note is numbered and dated as it is issued, by its issuer' USING ERRCODE='23514';
 END IF;
 NEW.gstin := i.gstin; NEW.supplier_name := i.supplier_name; NEW.supplier_address := i.supplier_address; NEW.state_code := i.state_code;
 NEW.recipient_name := i.recipient_name; NEW.recipient_address := i.recipient_address;
 NEW.invoice_number := i.number; NEW.invoice_date := i.issue_date;
 NEW.issue_date := today;
 NEW.financial_year := billing_financial_year(today);
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id::text||':billing-credit-note:'||NEW.gstin||':'||NEW.financial_year, 0));
 NEW.serial := (SELECT COALESCE(max(serial),0)+1 FROM billing_credit_notes WHERE tenant_id=NEW.tenant_id AND gstin=NEW.gstin AND financial_year=NEW.financial_year);
 NEW.issued_at := clock_timestamp();
 NEW.issuing_xact := pg_current_xact_id();
 RETURN NEW;
END $$;
CREATE TRIGGER billing_credit_note_next BEFORE INSERT ON billing_credit_notes FOR EACH ROW EXECUTE FUNCTION billing_credit_note_next();

-- A line is written only as its note is issued, for a creditable line of the
-- note's invoice, and copies that invoice line.
CREATE FUNCTION billing_credit_note_line_fill() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE l record;
BEGIN
 PERFORM 1 FROM billing_credit_notes n WHERE n.tenant_id=NEW.tenant_id AND n.id=NEW.credit_note_id AND n.invoice_id=NEW.invoice_id AND n.admission_id=NEW.admission_id
  AND n.issuing_xact=pg_current_xact_id() AND n.issued_by=NULLIF(current_setting('app.actor_subject',true),'');
 IF NOT FOUND THEN
  RAISE EXCEPTION 'a credit note''s lines are written only as it is issued' USING ERRCODE='23514';
 END IF;
 IF NOT EXISTS (SELECT 1 FROM billing_creditable(NEW.tenant_id,NEW.invoice_id) x WHERE x.entry_id=NEW.entry_id) THEN
  RAISE EXCEPTION 'a credit note carries its invoice''s reversed charges, once' USING ERRCODE='23514', CONSTRAINT='billing_credit_note_line';
 END IF;
 SELECT * INTO l FROM billing_tax_invoice_lines WHERE tenant_id=NEW.tenant_id AND entry_id=NEW.entry_id;
 NEW.census_date := l.census_date; NEW.bed_class := l.bed_class; NEW.sac := l.sac; NEW.policy_version := l.policy_version;
 NEW.value_minor := l.value_minor; NEW.taxable := l.taxable;
 NEW.cgst_bp := l.cgst_bp; NEW.sgst_bp := l.sgst_bp; NEW.cgst_minor := l.cgst_minor; NEW.sgst_minor := l.sgst_minor;
 RETURN NEW;
END $$;
CREATE TRIGGER billing_credit_note_line_fill BEFORE INSERT ON billing_credit_note_lines FOR EACH ROW EXECUTE FUNCTION billing_credit_note_line_fill();

-- At commit, a note has lines and leaves nothing on its invoice to credit.
CREATE FUNCTION billing_credit_note_complete() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS (SELECT 1 FROM billing_credit_note_lines l WHERE l.tenant_id=NEW.tenant_id AND l.credit_note_id=NEW.id)
  OR EXISTS (SELECT 1 FROM billing_creditable(NEW.tenant_id,NEW.invoice_id)) THEN
  RAISE EXCEPTION 'a credit note carries every reversed line of its invoice, and at least one' USING ERRCODE='23514', CONSTRAINT='billing_credit_note_complete';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER billing_credit_note_complete AFTER INSERT ON billing_credit_notes DEFERRABLE INITIALLY DEFERRED
 FOR EACH ROW EXECUTE FUNCTION billing_credit_note_complete();

-- An invoice now waits only for a reversal not yet credited.
CREATE OR REPLACE FUNCTION billing_tax_invoice_next() RETURNS trigger LANGUAGE plpgsql AS $$
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
 IF EXISTS (SELECT 1 FROM billing_tax_invoices t CROSS JOIN LATERAL billing_creditable(t.tenant_id,t.id) x
   WHERE t.tenant_id=NEW.tenant_id AND t.admission_id=NEW.admission_id) THEN
  RAISE EXCEPTION 'an invoiced charge was reversed: issue its credit note first' USING ERRCODE='23514', CONSTRAINT='billing_tax_invoice_credit_note';
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

DO $$ DECLARE t text; BEGIN
 -- Issued at the desk by a clerk holding the grant, as themselves.
 FOREACH t IN ARRAY ARRAY['billing_credit_notes','billing_credit_note_lines'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
  EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND billing_hospital_allowed(tenant_id,hospital_id)) WITH CHECK (tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND billing_hospital_allowed(tenant_id,hospital_id))', t);
  EXECUTE format('CREATE TRIGGER %I AFTER INSERT OR UPDATE ON %I FOR EACH ROW EXECUTE FUNCTION audit_record_change()', t||'_audit', t);
  EXECUTE format('CREATE TRIGGER %I BEFORE DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION billing_history_retained()', t||'_kept', t);
  EXECUTE format('CREATE TRIGGER %I BEFORE UPDATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION billing_history_retained()', t||'_fixed', t);
 END LOOP;
END $$;
