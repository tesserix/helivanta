SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Selling against a prescription (#1386 B-2b). Drugs Rules 1945, Rule 65:
-- Schedule H and H1 are sold only on a prescription (65(9)); it names the
-- patient's address and the total quantity (65(10)); it is dispensed only as
-- often as the prescriber allows (65(11)); nothing is supplied in its place
-- (65(11A)); the register needs the prescriber's address (65(3)). Schedule G
-- is held to the same rule by product decision 5.

ALTER TABLE pharmacy_prescriptions
 ADD COLUMN prescriber_address text CHECK (length(prescriber_address) BETWEEN 1 AND 500 AND prescriber_address=btrim(prescriber_address) AND prescriber_address !~ '[[:cntrl:]]'),
 -- The transaction that recorded it: its lines are written in that one
 -- transaction, so a recorded prescription never gains a line. Rows from
 -- B-2a get the migration's own id and so can never gain lines.
 ADD COLUMN recorded_xact xid8 NOT NULL DEFAULT pg_current_xact_id();

CREATE FUNCTION pharmacy_prescription_recorded_now() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.recorded_xact <> pg_current_xact_id() THEN
  RAISE EXCEPTION 'a prescription is recorded by its own transaction' USING ERRCODE='23514',CONSTRAINT='pharmacy_prescription_recorded_xact';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_prescription_recorded_now BEFORE INSERT ON pharmacy_prescriptions
 FOR EACH ROW EXECUTE FUNCTION pharmacy_prescription_recorded_now();

-- What the prescriber wrote: the drug as written, the product it is
-- dispensed as, the total units and the dispensings allowed.
CREATE TABLE pharmacy_prescription_lines (
 id uuid NOT NULL DEFAULT gen_random_uuid(),
 tenant_id uuid NOT NULL,
 outlet_id uuid NOT NULL,
 prescription_id uuid NOT NULL,
 line_number integer NOT NULL CHECK (line_number BETWEEN 1 AND 20),
 prescribed_as text NOT NULL CHECK (length(prescribed_as) BETWEEN 1 AND 200 AND prescribed_as=btrim(prescribed_as) AND prescribed_as !~ '[[:cntrl:]]'),
 product_id uuid NOT NULL,
 quantity_units bigint NOT NULL CHECK (quantity_units BETWEEN 1 AND 100000),
 dispensings_allowed integer NOT NULL CHECK (dispensings_allowed BETWEEN 1 AND 12),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,outlet_id,prescription_id,line_number),
 UNIQUE (tenant_id,id),
 FOREIGN KEY (tenant_id,outlet_id,prescription_id) REFERENCES pharmacy_prescriptions (tenant_id,outlet_id,id),
 FOREIGN KEY (tenant_id,outlet_id,product_id) REFERENCES pharmacy_catalogue_products (tenant_id,outlet_id,id)
);
ALTER TABLE pharmacy_prescription_lines ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_prescription_lines FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_prescription_lines
 USING (hms_tenant_visible(tenant_id) AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK (pharmacy_outlet_allowed(tenant_id,outlet_id,true));
CREATE INDEX pharmacy_prescription_lines_product ON pharmacy_prescription_lines (tenant_id,outlet_id,product_id);
CREATE TRIGGER pharmacy_prescription_line_audit AFTER INSERT ON pharmacy_prescription_lines
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_prescription_line_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_prescription_lines
 FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

CREATE FUNCTION pharmacy_prescription_line_with_its_prescription() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS (SELECT 1 FROM pharmacy_prescriptions p WHERE p.tenant_id=NEW.tenant_id AND p.outlet_id=NEW.outlet_id
    AND p.id=NEW.prescription_id AND p.recorded_xact=pg_current_xact_id()) THEN
  RAISE EXCEPTION 'prescription lines are recorded with their prescription' USING ERRCODE='23514',CONSTRAINT='pharmacy_prescription_line_recorded';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_prescription_line_with_its_prescription BEFORE INSERT ON pharmacy_prescription_lines
 FOR EACH ROW EXECUTE FUNCTION pharmacy_prescription_line_with_its_prescription();

-- At commit, a prescription recorded from B-2b on has at least one line.
CREATE FUNCTION pharmacy_prescription_has_lines() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS (SELECT 1 FROM pharmacy_prescription_lines l WHERE l.tenant_id=NEW.tenant_id AND l.outlet_id=NEW.outlet_id AND l.prescription_id=NEW.id) THEN
  RAISE EXCEPTION 'a prescription records at least one prescribed line' USING ERRCODE='23514',CONSTRAINT='pharmacy_prescription_has_lines';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER pharmacy_prescription_has_lines AFTER INSERT ON pharmacy_prescriptions
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION pharmacy_prescription_has_lines();

-- The walk-in buyer (decision 7): optional, pharmacy-owned, never published.
ALTER TABLE pharmacy_sales
 ADD COLUMN buyer_name text CHECK (length(buyer_name) BETWEEN 1 AND 200 AND buyer_name=btrim(buyer_name) AND buyer_name !~ '[[:cntrl:]]'),
 ADD COLUMN buyer_phone text CHECK (buyer_phone ~ '^\+?[0-9]{6,15}$');

-- Prescription classes are sold now, each line citing the prescription line
-- it dispenses; an OTC line cites none.
ALTER TABLE pharmacy_sale_lines DROP CONSTRAINT pharmacy_sale_lines_regulatory_class_check;
ALTER TABLE pharmacy_sale_lines
 ADD CONSTRAINT pharmacy_sale_lines_regulatory_class_check CHECK (regulatory_class IN ('none','G','H','H1')),
 ADD COLUMN prescription_id uuid,
 ADD COLUMN prescription_line integer,
 ADD CONSTRAINT pharmacy_sale_line_prescription_cited CHECK ((prescription_id IS NULL) = (prescription_line IS NULL)
  AND (regulatory_class = 'none') = (prescription_id IS NULL)),
 ADD CONSTRAINT pharmacy_sale_line_prescription FOREIGN KEY (tenant_id,outlet_id,prescription_id,prescription_line)
  REFERENCES pharmacy_prescription_lines (tenant_id,outlet_id,prescription_id,line_number);
CREATE INDEX pharmacy_sale_lines_prescription ON pharmacy_sale_lines (tenant_id,outlet_id,prescription_id,prescription_line)
 WHERE prescription_id IS NOT NULL;

-- What a line copies, the database checks against the source (spec B2, B5).
-- B-1's checks stand; the class is now any sold class other than X or NDPS,
-- and a prescription line is the one cited, locked first so two tills
-- dispensing one prescription serialise and each counts what the other sold.
CREATE OR REPLACE FUNCTION pharmacy_sale_line_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE day date; rx record; sold bigint; dispensed bigint;
BEGIN
 SELECT s.sale_date INTO day FROM pharmacy_sales s WHERE s.tenant_id=NEW.tenant_id AND s.outlet_id=NEW.outlet_id AND s.id=NEW.sale_id;
 IF NOT pharmacy_outlet_retail_licence_in_force(NEW.tenant_id,NEW.outlet_id,day)
    OR NOT EXISTS (SELECT 1 FROM pharmacy_stock_batches b WHERE b.tenant_id=NEW.tenant_id AND b.outlet_id=NEW.outlet_id
     AND b.id=NEW.batch_id AND b.product_id=NEW.product_id AND b.lot_number=NEW.lot_number AND b.expires_on=NEW.expires_on AND b.mrp_minor=NEW.mrp_minor)
    OR NOT EXISTS (SELECT 1 FROM pharmacy_sellable_buckets(NEW.tenant_id,NEW.outlet_id,NEW.product_id,day) k
     WHERE k.batch_id=NEW.batch_id AND k.location_id=NEW.location_id)
    OR NOT EXISTS (SELECT 1 FROM pharmacy_product_schedule_in_force(NEW.tenant_id,NEW.outlet_id,NEW.product_id) c
     WHERE c.declaration_id=NEW.schedule_declaration_id AND c.regulatory_class=NEW.regulatory_class AND c.regulatory_class<>'X' AND NOT c.ndps)
    OR NOT EXISTS (SELECT 1 FROM pharmacy_catalogue_products p WHERE p.tenant_id=NEW.tenant_id AND p.outlet_id=NEW.outlet_id
     AND p.id=NEW.product_id AND p.status='active' AND p.pack_size=NEW.pack_size) THEN
  RAISE EXCEPTION 'a sale line must be a licensed, sellable batch at its own price' USING ERRCODE='23514',CONSTRAINT='pharmacy_sale_line_source';
 END IF;
 IF NEW.prescription_id IS NULL THEN
  RETURN NEW;
 END IF;
 SELECT l.product_id, l.quantity_units, l.dispensings_allowed, p.prescribed_on, p.prescriber_address, p.patient_address INTO rx
  FROM pharmacy_prescription_lines l JOIN pharmacy_prescriptions p ON p.tenant_id=l.tenant_id AND p.outlet_id=l.outlet_id AND p.id=l.prescription_id
  WHERE l.tenant_id=NEW.tenant_id AND l.outlet_id=NEW.outlet_id AND l.prescription_id=NEW.prescription_id AND l.line_number=NEW.prescription_line
  FOR UPDATE OF l;
 -- Each statement below reads after the lock, so a till that waited counts
 -- what the other committed.
 SELECT COALESCE(sum(quantity_units),0), count(DISTINCT sale_id) FILTER (WHERE sale_id<>NEW.sale_id) INTO sold, dispensed
  FROM pharmacy_sale_lines WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id
  AND prescription_id=NEW.prescription_id AND prescription_line=NEW.prescription_line;
 IF rx.product_id IS DISTINCT FROM NEW.product_id OR rx.prescribed_on > day
    OR sold + NEW.quantity_units > rx.quantity_units OR dispensed + 1 > rx.dispensings_allowed
    OR (NEW.regulatory_class IN ('H','H1') AND (rx.prescriber_address IS NULL OR rx.patient_address IS NULL)) THEN
  RAISE EXCEPTION 'a prescription line is dispensed as written, within its units and dispensings' USING ERRCODE='23514',CONSTRAINT='pharmacy_sale_line_prescription_limit';
 END IF;
 RETURN NEW;
END $$;
