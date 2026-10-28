SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Returns at the counter as GST credit notes (#1386 C-1; #163, #202). CGST Act
-- s.34 and Rule 53(1A): a credit note for goods returned names the original
-- invoice, credits its taxable value and tax, and has its own consecutive
-- series of at most 16 characters per financial year. Returned stock goes to
-- quarantine; only a release puts it back on a shelf.

CREATE TABLE pharmacy_credit_note_series (
 id uuid NOT NULL DEFAULT gen_random_uuid(),
 tenant_id uuid NOT NULL,
 outlet_id uuid NOT NULL,
 financial_year text NOT NULL CHECK (financial_year ~ '^[0-9]{4}$'),
 last_number bigint NOT NULL DEFAULT 0 CHECK (last_number >= 0),
 PRIMARY KEY (tenant_id,outlet_id,financial_year),
 UNIQUE (tenant_id,id),
 FOREIGN KEY (tenant_id,outlet_id) REFERENCES pharmacy_outlets (tenant_id,id)
);
ALTER TABLE pharmacy_credit_note_series ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_credit_note_series FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_credit_note_series
 USING (hms_tenant_visible(tenant_id) AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK (pharmacy_outlet_allowed(tenant_id,outlet_id,true));
CREATE TRIGGER pharmacy_credit_note_series_audit AFTER INSERT OR UPDATE ON pharmacy_credit_note_series
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_credit_note_series_step BEFORE UPDATE ON pharmacy_credit_note_series
 FOR EACH ROW EXECUTE FUNCTION pharmacy_invoice_series_step();
CREATE TRIGGER pharmacy_credit_note_series_retain BEFORE DELETE OR TRUNCATE ON pharmacy_credit_note_series
 FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

CREATE TABLE pharmacy_credit_notes (
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 tenant_id uuid NOT NULL,
 outlet_id uuid NOT NULL,
 sale_id uuid NOT NULL,
 financial_year text NOT NULL CHECK (financial_year ~ '^[0-9]{4}$'),
 sequence_number bigint NOT NULL CHECK (sequence_number >= 1),
 credit_note_number text NOT NULL CHECK (length(credit_note_number) <= 16 AND credit_note_number ~ '^[A-Z0-9]{1,4}/[0-9]{4}/C[1-9][0-9]*$'),
 credit_note_date date NOT NULL,
 currency text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
 total_minor bigint NOT NULL CHECK (total_minor >= 1),
 taxable_minor bigint NOT NULL CHECK (taxable_minor >= 0),
 cgst_minor bigint NOT NULL CHECK (cgst_minor >= 0),
 sgst_minor bigint NOT NULL CHECK (sgst_minor >= 0),
 reason text NOT NULL CHECK (length(reason) BETWEEN 5 AND 200 AND reason=btrim(reason) AND reason !~ '[[:cntrl:]]'),
 recipient_name text NOT NULL CHECK (length(recipient_name) BETWEEN 1 AND 200 AND recipient_name=btrim(recipient_name) AND recipient_name !~ '[[:cntrl:]]'),
 recipient_address text NOT NULL CHECK (length(recipient_address) BETWEEN 1 AND 500 AND recipient_address=btrim(recipient_address) AND recipient_address !~ '[[:cntrl:]]'),
 intent_sha256 text NOT NULL CHECK (intent_sha256 ~ '^[0-9a-f]{64}$'),
 actor_subject text NOT NULL CHECK (length(btrim(actor_subject)) BETWEEN 1 AND 200),
 posted_xact xid8 NOT NULL DEFAULT pg_current_xact_id(),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,outlet_id,id),
 UNIQUE (tenant_id,outlet_id,financial_year,sequence_number),
 UNIQUE (tenant_id,outlet_id,credit_note_number),
 FOREIGN KEY (tenant_id,outlet_id,sale_id) REFERENCES pharmacy_sales (tenant_id,outlet_id,id),
 CHECK (credit_note_number LIKE '%/'||financial_year||'/C'||sequence_number),
 CHECK (financial_year = to_char(credit_note_date - interval '3 months','YY')||to_char(credit_note_date + interval '9 months','YY')),
 CHECK (total_minor = taxable_minor + cgst_minor + sgst_minor)
);
ALTER TABLE pharmacy_credit_notes ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_credit_notes FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_credit_notes
 USING (hms_tenant_visible(tenant_id) AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK (pharmacy_outlet_allowed(tenant_id,outlet_id,true) AND actor_subject=NULLIF(current_setting('app.pharmacy_subject',true),''));
CREATE INDEX pharmacy_credit_notes_page ON pharmacy_credit_notes (tenant_id,outlet_id,created_at DESC,id DESC);
CREATE INDEX pharmacy_credit_notes_sale ON pharmacy_credit_notes (tenant_id,outlet_id,sale_id);
CREATE TRIGGER pharmacy_credit_note_audit AFTER INSERT ON pharmacy_credit_notes
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_credit_note_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_credit_notes
 FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

-- The credit note's date is within s.34(2): by 30 November after the end of
-- the sale's financial year; and never before the sale.
CREATE FUNCTION pharmacy_credit_note_in_time() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE sold record;
BEGIN
 SELECT sale_date, financial_year, place_of_supply, currency INTO sold FROM pharmacy_sales
  WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.sale_id;
 IF NEW.credit_note_date < sold.sale_date
    OR NEW.credit_note_date > make_date(2000 + substring(sold.financial_year,3,2)::int, 11, 30)
    OR NEW.currency <> sold.currency OR NEW.posted_xact <> pg_current_xact_id() THEN
  RAISE EXCEPTION 'a credit note follows its sale, by 30 November after the sale''s financial year' USING ERRCODE='23514',CONSTRAINT='pharmacy_credit_note_in_time';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_credit_note_in_time BEFORE INSERT ON pharmacy_credit_notes
 FOR EACH ROW EXECUTE FUNCTION pharmacy_credit_note_in_time();

CREATE TABLE pharmacy_credit_note_lines (
 id uuid NOT NULL DEFAULT gen_random_uuid(),
 tenant_id uuid NOT NULL,
 outlet_id uuid NOT NULL,
 credit_note_id uuid NOT NULL,
 line_number integer NOT NULL CHECK (line_number BETWEEN 1 AND 1000),
 sale_id uuid NOT NULL,
 sale_line integer NOT NULL,
 location_id uuid NOT NULL,
 quantity_units bigint NOT NULL CHECK (quantity_units BETWEEN 1 AND 1000000),
 amount_minor bigint NOT NULL CHECK (amount_minor >= 0),
 taxable_minor bigint NOT NULL CHECK (taxable_minor >= 0),
 cgst_minor bigint NOT NULL CHECK (cgst_minor >= 0),
 sgst_minor bigint NOT NULL CHECK (sgst_minor >= 0),
 PRIMARY KEY (tenant_id,outlet_id,credit_note_id,line_number),
 UNIQUE (tenant_id,id),
 UNIQUE (tenant_id,outlet_id,credit_note_id,sale_line),
 FOREIGN KEY (tenant_id,outlet_id,credit_note_id) REFERENCES pharmacy_credit_notes (tenant_id,outlet_id,id),
 FOREIGN KEY (tenant_id,outlet_id,sale_id,sale_line) REFERENCES pharmacy_sale_lines (tenant_id,outlet_id,sale_id,line_number),
 FOREIGN KEY (tenant_id,outlet_id,location_id) REFERENCES pharmacy_stock_locations (tenant_id,outlet_id,id),
 CHECK (amount_minor = taxable_minor + cgst_minor + sgst_minor),
 CHECK (cgst_minor = sgst_minor)
);
ALTER TABLE pharmacy_credit_note_lines ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_credit_note_lines FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_credit_note_lines
 USING (hms_tenant_visible(tenant_id) AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK (pharmacy_outlet_allowed(tenant_id,outlet_id,true));
CREATE INDEX pharmacy_credit_note_lines_sale_line ON pharmacy_credit_note_lines (tenant_id,outlet_id,sale_id,sale_line);
CREATE TRIGGER pharmacy_credit_note_line_audit AFTER INSERT ON pharmacy_credit_note_lines
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_credit_note_line_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_credit_note_lines
 FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

CREATE TABLE pharmacy_credit_note_refunds (
 id uuid NOT NULL DEFAULT gen_random_uuid(),
 tenant_id uuid NOT NULL,
 outlet_id uuid NOT NULL,
 credit_note_id uuid NOT NULL,
 refund_number integer NOT NULL CHECK (refund_number BETWEEN 1 AND 5),
 kind text NOT NULL CHECK (kind IN ('cash','upi','card')),
 amount_minor bigint NOT NULL CHECK (amount_minor >= 1),
 reference text CHECK (length(btrim(reference)) BETWEEN 1 AND 64),
 PRIMARY KEY (tenant_id,outlet_id,credit_note_id,refund_number),
 UNIQUE (tenant_id,id),
 FOREIGN KEY (tenant_id,outlet_id,credit_note_id) REFERENCES pharmacy_credit_notes (tenant_id,outlet_id,id),
 CHECK ((kind = 'cash') = (reference IS NULL))
);
ALTER TABLE pharmacy_credit_note_refunds ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_credit_note_refunds FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_credit_note_refunds
 USING (hms_tenant_visible(tenant_id) AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK (pharmacy_outlet_allowed(tenant_id,outlet_id,true));
CREATE TRIGGER pharmacy_credit_note_refund_audit AFTER INSERT ON pharmacy_credit_note_refunds
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_credit_note_refund_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_credit_note_refunds
 FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

-- Lines and refunds are written only in their credit note's own transaction.
CREATE FUNCTION pharmacy_credit_note_sealed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS (SELECT 1 FROM pharmacy_credit_notes n WHERE n.tenant_id=NEW.tenant_id AND n.outlet_id=NEW.outlet_id
    AND n.id=NEW.credit_note_id AND n.posted_xact=pg_current_xact_id()) THEN
  RAISE EXCEPTION 'an issued credit note cannot be added to' USING ERRCODE='23514',CONSTRAINT='pharmacy_credit_note_sealed';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_credit_note_line_sealed BEFORE INSERT ON pharmacy_credit_note_lines
 FOR EACH ROW EXECUTE FUNCTION pharmacy_credit_note_sealed();
CREATE TRIGGER pharmacy_credit_note_refund_sealed BEFORE INSERT ON pharmacy_credit_note_refunds
 FOR EACH ROW EXECUTE FUNCTION pharmacy_credit_note_sealed();

-- A credit line reverses its sale line: the cumulative units stay within
-- those sold, and the taxable value and each tax half are their cumulative
-- pro-rata step (decision 2). Prorated apart, no step is negative and all
-- returns of a line together credit exactly what it charged; the amount is
-- their sum (the line CHECK). The sale line is locked first, so two returns
-- of one line serialise.
CREATE FUNCTION pharmacy_credit_note_line_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE sold record; before bigint; after bigint;
BEGIN
 SELECT l.quantity_units, l.taxable_minor, l.cgst_minor INTO sold FROM pharmacy_sale_lines l
  JOIN pharmacy_credit_notes n ON n.tenant_id=l.tenant_id AND n.outlet_id=l.outlet_id AND n.id=NEW.credit_note_id AND n.sale_id=l.sale_id
  WHERE l.tenant_id=NEW.tenant_id AND l.outlet_id=NEW.outlet_id AND l.sale_id=NEW.sale_id AND l.line_number=NEW.sale_line
  FOR UPDATE OF l;
 IF NOT FOUND THEN
  RAISE EXCEPTION 'a credit line returns a line of its own credit note''s sale' USING ERRCODE='23514',CONSTRAINT='pharmacy_credit_note_line_source';
 END IF;
 SELECT COALESCE(sum(quantity_units),0) INTO before FROM pharmacy_credit_note_lines
  WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND sale_id=NEW.sale_id AND sale_line=NEW.sale_line;
 after := before + NEW.quantity_units;
 IF after > sold.quantity_units
    OR NEW.taxable_minor <> div(sold.taxable_minor::numeric*after, sold.quantity_units) - div(sold.taxable_minor::numeric*before, sold.quantity_units)
    OR NEW.cgst_minor <> div(sold.cgst_minor::numeric*after, sold.quantity_units) - div(sold.cgst_minor::numeric*before, sold.quantity_units)
    OR NOT EXISTS (SELECT 1 FROM pharmacy_stock_locations s WHERE s.tenant_id=NEW.tenant_id AND s.outlet_id=NEW.outlet_id
     AND s.id=NEW.location_id AND s.stock_status='quarantine') THEN
  RAISE EXCEPTION 'a credit line returns at most what was sold, at its pro-rata amount, into quarantine' USING ERRCODE='23514',CONSTRAINT='pharmacy_credit_note_line_limit';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_credit_note_line_guard BEFORE INSERT ON pharmacy_credit_note_lines
 FOR EACH ROW EXECUTE FUNCTION pharmacy_credit_note_line_guard();

-- At commit, a credit note is its lines, refunded by its refunds, with each
-- line's stock received into quarantine.
CREATE FUNCTION pharmacy_credit_note_balanced() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE lines record; refunded bigint; moved bigint;
BEGIN
 SELECT count(*) AS n, COALESCE(sum(amount_minor),0) AS amount, COALESCE(sum(taxable_minor),0) AS taxable,
  COALESCE(sum(cgst_minor),0) AS cgst, COALESCE(sum(sgst_minor),0) AS sgst INTO lines
  FROM pharmacy_credit_note_lines WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND credit_note_id=NEW.id;
 SELECT COALESCE(sum(amount_minor),0) INTO refunded FROM pharmacy_credit_note_refunds
  WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND credit_note_id=NEW.id;
 SELECT count(*) INTO moved FROM pharmacy_credit_note_lines l JOIN pharmacy_stock_movements m
  ON m.tenant_id=l.tenant_id AND m.outlet_id=l.outlet_id AND m.source_reference='return:'||l.credit_note_id||':'||l.line_number AND m.kind='return'
  WHERE l.tenant_id=NEW.tenant_id AND l.outlet_id=NEW.outlet_id AND l.credit_note_id=NEW.id;
 IF lines.n = 0 OR lines.amount <> NEW.total_minor OR lines.taxable <> NEW.taxable_minor OR lines.cgst <> NEW.cgst_minor
    OR lines.sgst <> NEW.sgst_minor OR refunded <> NEW.total_minor OR moved <> lines.n THEN
  RAISE EXCEPTION 'a credit note is its lines, refunded in full, with its stock received' USING ERRCODE='23514',CONSTRAINT='pharmacy_credit_note_balanced';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER pharmacy_credit_note_balanced AFTER INSERT ON pharmacy_credit_notes
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION pharmacy_credit_note_balanced();

-- Returned stock arrives as kind 'return': into quarantine, exactly its
-- credit line's units of its sale line's batch, and through nothing else.
ALTER TABLE pharmacy_stock_movements DROP CONSTRAINT pharmacy_stock_movements_kind_check;
ALTER TABLE pharmacy_stock_movements ADD CONSTRAINT pharmacy_stock_movements_kind_check
 CHECK (kind IN ('receipt','correction','release','sale','return')) NOT VALID;
ALTER TABLE pharmacy_stock_movements VALIDATE CONSTRAINT pharmacy_stock_movements_kind_check;

CREATE FUNCTION pharmacy_stock_return_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.kind='return' AND (NEW.source_reference !~ '^return:[0-9a-f-]{36}:[1-9][0-9]{0,3}$'
    OR NOT EXISTS (SELECT 1 FROM pharmacy_credit_note_lines c JOIN pharmacy_sale_lines s
     ON s.tenant_id=c.tenant_id AND s.outlet_id=c.outlet_id AND s.sale_id=c.sale_id AND s.line_number=c.sale_line
     WHERE c.tenant_id=NEW.tenant_id AND c.outlet_id=NEW.outlet_id
     AND c.credit_note_id=split_part(NEW.source_reference,':',2)::uuid AND c.line_number=split_part(NEW.source_reference,':',3)::int
     AND c.location_id=NEW.location_id AND s.batch_id=NEW.batch_id AND c.quantity_units=NEW.quantity_units)) THEN
  RAISE EXCEPTION 'a return movement receives its credit line''s units into quarantine' USING ERRCODE='23514',CONSTRAINT='pharmacy_stock_return_movement';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_stock_return_guard BEFORE INSERT ON pharmacy_stock_movements
 FOR EACH ROW EXECUTE FUNCTION pharmacy_stock_return_guard();
