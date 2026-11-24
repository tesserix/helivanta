SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

-- Expand immutable financial snapshots; default zero preserves historical tax.
ALTER TABLE pharmacy_sales ADD COLUMN igst_minor bigint NOT NULL DEFAULT 0 CHECK(igst_minor>=0);
ALTER TABLE pharmacy_sale_lines ADD COLUMN igst_minor bigint NOT NULL DEFAULT 0 CHECK(igst_minor>=0);
ALTER TABLE pharmacy_sale_charge_lines ADD COLUMN igst_minor bigint NOT NULL DEFAULT 0 CHECK(igst_minor>=0);
ALTER TABLE pharmacy_credit_notes ADD COLUMN igst_minor bigint NOT NULL DEFAULT 0 CHECK(igst_minor>=0);
ALTER TABLE pharmacy_credit_note_lines ADD COLUMN igst_minor bigint NOT NULL DEFAULT 0 CHECK(igst_minor>=0);
ALTER TABLE pharmacy_credit_note_charge_lines ADD COLUMN igst_minor bigint NOT NULL DEFAULT 0 CHECK(igst_minor>=0);

-- PostgreSQL assigned names to the original unnamed amount/place checks.
-- Select only the old balance checks, leaving nonnegative and source guards.
DO $$DECLARE c record; BEGIN
 FOR c IN SELECT conrelid::regclass AS tbl,conname FROM pg_constraint
  WHERE contype='c' AND conrelid=ANY(ARRAY['pharmacy_sales'::regclass,'pharmacy_sale_lines'::regclass,'pharmacy_sale_charge_lines'::regclass,
   'pharmacy_credit_notes'::regclass,'pharmacy_credit_note_lines'::regclass,'pharmacy_credit_note_charge_lines'::regclass])
  AND ((pg_get_constraintdef(oid) LIKE '%taxable_minor%' AND pg_get_constraintdef(oid) LIKE '%cgst_minor%' AND pg_get_constraintdef(oid) LIKE '%sgst_minor%')
   OR (conrelid='pharmacy_sales'::regclass AND pg_get_constraintdef(oid) LIKE '%place_of_supply%' AND pg_get_constraintdef(oid) LIKE '%seller_gstin%'))
 LOOP EXECUTE format('ALTER TABLE %s DROP CONSTRAINT %I',c.tbl,c.conname); END LOOP;
END $$;
ALTER TABLE pharmacy_sales ADD CONSTRAINT pharmacy_sales_gst_balance CHECK(total_minor=taxable_minor+cgst_minor+sgst_minor+igst_minor);
ALTER TABLE pharmacy_sale_lines ADD CONSTRAINT pharmacy_sale_lines_gst_balance CHECK(amount_minor=taxable_minor+cgst_minor+sgst_minor+igst_minor);
ALTER TABLE pharmacy_sale_charge_lines ADD CONSTRAINT pharmacy_sale_charge_lines_gst_balance CHECK(amount_minor=taxable_minor+cgst_minor+sgst_minor+igst_minor);
ALTER TABLE pharmacy_credit_notes ADD CONSTRAINT pharmacy_credit_notes_gst_balance CHECK(total_minor=taxable_minor+cgst_minor+sgst_minor+igst_minor);
ALTER TABLE pharmacy_credit_note_lines ADD CONSTRAINT pharmacy_credit_note_lines_gst_balance CHECK(amount_minor=taxable_minor+cgst_minor+sgst_minor+igst_minor);
ALTER TABLE pharmacy_credit_note_charge_lines ADD CONSTRAINT pharmacy_credit_note_charge_lines_gst_balance CHECK(amount_minor=taxable_minor+cgst_minor+sgst_minor+igst_minor);

ALTER TABLE pharmacy_sales ADD CONSTRAINT pharmacy_sale_gst_destination CHECK(
 place_of_supply ~ '^[0-9]{2}$' AND cgst_minor=sgst_minor AND
 CASE WHEN place_of_supply=substring(seller_gstin,1,2) THEN igst_minor=0 ELSE cgst_minor=0 AND sgst_minor=0 END);

-- One rounded IGST component; the existing intrastate halves stay unchanged.
CREATE FUNCTION pharmacy_sale_tax_matches(amount bigint,nature text,rate bigint,interstate boolean,cg bigint,sg bigint,ig bigint)
RETURNS boolean LANGUAGE sql IMMUTABLE AS $$
 SELECT CASE WHEN nature='taxable' THEN rate BETWEEN 100 AND 1000000 AND rate%100=0 AND
  CASE WHEN interstate THEN cg=0 AND sg=0 AND ig=div(2*amount::numeric*rate+(1000000+rate),2*(1000000+rate))
  ELSE ig=0 AND cg=sg AND cg=div(2*rate::numeric*amount+2*(1000000+rate),4*(1000000+rate::numeric)) END
 ELSE nature IN ('nil_rated','exempt') AND rate=0 AND cg=0 AND sg=0 AND ig=0 END
$$;

-- This replaces only the charge's former halves-only rounding check.
ALTER TABLE pharmacy_sale_charge_lines DROP CONSTRAINT pharmacy_sale_charge_tax;
CREATE FUNCTION pharmacy_sale_line_gst_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE sold record;
BEGIN
 SELECT place_of_supply<>substring(seller_gstin,1,2) AS interstate,admission_id INTO sold
 FROM pharmacy_sales WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.sale_id;
 IF NOT FOUND OR NOT pharmacy_sale_tax_matches(NEW.amount_minor,NEW.supply_nature,NEW.rate_ppm,sold.interstate,NEW.cgst_minor,NEW.sgst_minor,NEW.igst_minor) THEN
  RAISE EXCEPTION 'sale tax components must match the invoice destination and recorded rate' USING ERRCODE='23514',CONSTRAINT='pharmacy_sale_line_gst';
 END IF;
 IF TG_TABLE_NAME='pharmacy_sale_charge_lines' THEN
  IF sold.interstate AND NEW.tax_treatment='own_service' THEN
   RAISE EXCEPTION 'an interstate sale cannot include an independently taxed service charge' USING ERRCODE='23514',CONSTRAINT='pharmacy_sale_charge_destination';
  END IF;
 END IF;
 IF sold.interstate AND sold.admission_id IS NOT NULL THEN
  RAISE EXCEPTION 'an admission-account sale uses the outlet state' USING ERRCODE='23514',CONSTRAINT='pharmacy_sale_admission_destination';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_sale_line_gst_guard BEFORE INSERT ON pharmacy_sale_lines FOR EACH ROW EXECUTE FUNCTION pharmacy_sale_line_gst_guard();
CREATE TRIGGER pharmacy_sale_charge_gst_guard BEFORE INSERT ON pharmacy_sale_charge_lines FOR EACH ROW EXECUTE FUNCTION pharmacy_sale_line_gst_guard();

CREATE OR REPLACE FUNCTION pharmacy_sale_balanced() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE lines record; charges record; paid bigint; moved bigint;
BEGIN
 SELECT count(*) AS n, COALESCE(sum(amount_minor),0) AS amount, COALESCE(sum(taxable_minor),0) AS taxable,
  COALESCE(sum(cgst_minor),0) AS cgst, COALESCE(sum(sgst_minor),0) AS sgst, COALESCE(sum(igst_minor),0) AS igst INTO lines
  FROM pharmacy_sale_lines WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND sale_id=NEW.id;
 SELECT COALESCE(sum(amount_minor),0) AS amount, COALESCE(sum(taxable_minor),0) AS taxable,
  COALESCE(sum(cgst_minor),0) AS cgst, COALESCE(sum(sgst_minor),0) AS sgst, COALESCE(sum(igst_minor),0) AS igst INTO charges
  FROM pharmacy_sale_charge_lines WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND sale_id=NEW.id;
 SELECT COALESCE(sum(amount_minor),0) INTO paid FROM pharmacy_sale_tenders
  WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND sale_id=NEW.id;
 -- Equality on source_reference, so the unique index serves it.
 SELECT count(*) INTO moved FROM pharmacy_sale_lines l JOIN pharmacy_stock_movements m
  ON m.tenant_id=l.tenant_id AND m.outlet_id=l.outlet_id AND m.source_reference='sale:'||l.sale_id||':'||l.line_number AND m.kind='sale'
  WHERE l.tenant_id=NEW.tenant_id AND l.outlet_id=NEW.outlet_id AND l.sale_id=NEW.id;
 IF lines.n = 0 OR lines.amount + charges.amount <> NEW.total_minor OR lines.taxable + charges.taxable <> NEW.taxable_minor
    OR lines.cgst + charges.cgst <> NEW.cgst_minor OR lines.sgst + charges.sgst <> NEW.sgst_minor
    OR lines.igst + charges.igst <> NEW.igst_minor
    OR paid <> NEW.total_minor OR moved <> lines.n THEN
  RAISE EXCEPTION 'a sale is its lines, paid by its tenders, with its stock moved' USING ERRCODE='23514',CONSTRAINT='pharmacy_sale_balanced';
 END IF;
 RETURN NULL;
END $$;

CREATE OR REPLACE FUNCTION pharmacy_credit_note_balanced() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE lines record; charges record; refunded bigint; moved bigint;
BEGIN
 SELECT count(*) AS n, COALESCE(sum(amount_minor),0) AS amount, COALESCE(sum(taxable_minor),0) AS taxable,
  COALESCE(sum(cgst_minor),0) AS cgst, COALESCE(sum(sgst_minor),0) AS sgst, COALESCE(sum(igst_minor),0) AS igst INTO lines
  FROM pharmacy_credit_note_lines WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND credit_note_id=NEW.id;
 SELECT count(*) AS n, COALESCE(sum(amount_minor),0) AS amount, COALESCE(sum(taxable_minor),0) AS taxable,
  COALESCE(sum(cgst_minor),0) AS cgst, COALESCE(sum(sgst_minor),0) AS sgst, COALESCE(sum(igst_minor),0) AS igst INTO charges
  FROM pharmacy_credit_note_charge_lines WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND credit_note_id=NEW.id;
 SELECT COALESCE(sum(amount_minor),0) INTO refunded FROM pharmacy_credit_note_refunds
  WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND credit_note_id=NEW.id;
 SELECT count(*) INTO moved FROM pharmacy_credit_note_lines l JOIN pharmacy_stock_movements m
  ON m.tenant_id=l.tenant_id AND m.outlet_id=l.outlet_id AND m.source_reference='return:'||l.credit_note_id||':'||l.line_number AND m.kind='return'
  WHERE l.tenant_id=NEW.tenant_id AND l.outlet_id=NEW.outlet_id AND l.credit_note_id=NEW.id;
 IF lines.n + charges.n = 0 OR lines.amount + charges.amount <> NEW.total_minor OR lines.taxable + charges.taxable <> NEW.taxable_minor
    OR lines.cgst + charges.cgst <> NEW.cgst_minor OR lines.sgst + charges.sgst <> NEW.sgst_minor
    OR lines.igst + charges.igst <> NEW.igst_minor
    OR refunded <> NEW.total_minor OR moved <> lines.n THEN
  RAISE EXCEPTION 'a credit note is its lines, refunded in full, with its stock received' USING ERRCODE='23514',CONSTRAINT='pharmacy_credit_note_balanced';
 END IF;
 RETURN NULL;
END $$;

CREATE OR REPLACE FUNCTION pharmacy_credit_note_line_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE sold record; before bigint; after bigint;
BEGIN
 SELECT l.quantity_units, l.taxable_minor, l.cgst_minor,l.sgst_minor,l.igst_minor INTO sold FROM pharmacy_sale_lines l
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
    OR NEW.sgst_minor <> div(sold.sgst_minor::numeric*after, sold.quantity_units) - div(sold.sgst_minor::numeric*before, sold.quantity_units)
    OR NEW.igst_minor <> div(sold.igst_minor::numeric*after, sold.quantity_units) - div(sold.igst_minor::numeric*before, sold.quantity_units)
    OR NOT EXISTS (SELECT 1 FROM pharmacy_stock_locations s WHERE s.tenant_id=NEW.tenant_id AND s.outlet_id=NEW.outlet_id
     AND s.id=NEW.location_id AND s.stock_status='quarantine') THEN
  RAISE EXCEPTION 'a credit line returns at most what was sold, at its pro-rata amount, into quarantine' USING ERRCODE='23514',CONSTRAINT='pharmacy_credit_note_line_limit';
 END IF;
 RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION pharmacy_credit_note_charge_line_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS (SELECT 1 FROM pharmacy_sale_charge_lines c JOIN pharmacy_credit_notes n
    ON n.tenant_id=c.tenant_id AND n.outlet_id=c.outlet_id AND n.id=NEW.credit_note_id AND n.sale_id=c.sale_id
    WHERE c.tenant_id=NEW.tenant_id AND c.outlet_id=NEW.outlet_id AND c.sale_id=NEW.sale_id AND c.charge_number=NEW.sale_charge
    AND (c.amount_minor,c.taxable_minor,c.cgst_minor,c.sgst_minor,c.igst_minor) = (NEW.amount_minor,NEW.taxable_minor,NEW.cgst_minor,NEW.sgst_minor,NEW.igst_minor)) THEN
  RAISE EXCEPTION 'a credited charge reverses a charge of its credit note''s sale, whole' USING ERRCODE='23514',CONSTRAINT='pharmacy_credit_note_charge_source';
 END IF;
 RETURN NEW;
END $$;

ALTER TABLE pharmacy_tally_ledger_map_entries DROP CONSTRAINT pharmacy_tally_ledger_map_head;
ALTER TABLE pharmacy_tally_ledger_map_entries ADD CONSTRAINT pharmacy_tally_ledger_map_head CHECK(head~'^(voucher_type:(sale|credit_note|supplier_payment|supplier_payment_reversal)|tender:(cash|upi|upi_verified|card|admission_account)|sales_(goods|service):(taxable:(0|[1-9][0-9]{0,5}|1000000)|(nil_rated|exempt):0)|[csi]gst:(0|[1-9][0-9]{0,5}|1000000)|payment_method:(cash|bank|card|upi|other)|supplier:.+)$'
  AND(head!~'^supplier:'OR(length(head)BETWEEN 10 AND 109 AND substr(head,10)=upper(btrim(substr(head,10)))))AND head!~'[\x01-\x1f\x7f-\x9f￾￿]');
