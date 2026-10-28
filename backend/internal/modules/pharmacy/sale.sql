SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Counter sale (#1386 B-1). One transaction posts an invoice, its lines and
-- tenders, and the stock leaving sellable locations. Everything here is
-- insert-only except the invoice series counter, which only ever steps by
-- one inside a sale's transaction, so a rolled-back sale returns its number.

-- What the outlet prints as the seller. The highest seq is in force.
CREATE TABLE pharmacy_outlet_invoice_profiles (
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 tenant_id uuid NOT NULL,
 outlet_id uuid NOT NULL,
 seq bigint GENERATED ALWAYS AS IDENTITY,
 legal_name text NOT NULL CHECK (length(btrim(legal_name)) BETWEEN 1 AND 200),
 address text NOT NULL CHECK (length(btrim(address)) BETWEEN 1 AND 500),
 -- PREFIX/FY/N must fit the 16 characters GST allows an invoice number.
 invoice_prefix text NOT NULL CHECK (invoice_prefix ~ '^[A-Z0-9]{1,4}$'),
 intent_sha256 text NOT NULL CHECK (intent_sha256 ~ '^[0-9a-f]{64}$'),
 recorded_by text NOT NULL CHECK (length(btrim(recorded_by)) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,outlet_id,id),
 UNIQUE (seq),
 FOREIGN KEY (tenant_id,outlet_id) REFERENCES pharmacy_outlets (tenant_id,id)
);
ALTER TABLE pharmacy_outlet_invoice_profiles ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_outlet_invoice_profiles FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_outlet_invoice_profiles
 USING (hms_tenant_visible(tenant_id) AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK (pharmacy_outlet_allowed(tenant_id,outlet_id,true) AND recorded_by=NULLIF(current_setting('app.pharmacy_subject',true),''));
CREATE INDEX pharmacy_invoice_profile_current ON pharmacy_outlet_invoice_profiles (tenant_id,outlet_id,seq DESC);
CREATE TRIGGER pharmacy_invoice_profile_audit AFTER INSERT ON pharmacy_outlet_invoice_profiles
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_invoice_profile_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_outlet_invoice_profiles
 FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

-- One gap-free series per outlet and Indian financial year (decision 4).
CREATE TABLE pharmacy_invoice_series (
 id uuid NOT NULL DEFAULT gen_random_uuid(),
 tenant_id uuid NOT NULL,
 outlet_id uuid NOT NULL,
 financial_year text NOT NULL CHECK (financial_year ~ '^[0-9]{4}$'),
 last_number bigint NOT NULL DEFAULT 0 CHECK (last_number >= 0),
 PRIMARY KEY (tenant_id,outlet_id,financial_year),
 UNIQUE (tenant_id,id),
 FOREIGN KEY (tenant_id,outlet_id) REFERENCES pharmacy_outlets (tenant_id,id)
);
ALTER TABLE pharmacy_invoice_series ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_invoice_series FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_invoice_series
 USING (hms_tenant_visible(tenant_id) AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK (pharmacy_outlet_allowed(tenant_id,outlet_id,true));
CREATE TRIGGER pharmacy_invoice_series_audit AFTER INSERT OR UPDATE ON pharmacy_invoice_series
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_invoice_series_no_delete BEFORE DELETE OR TRUNCATE ON pharmacy_invoice_series
 FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();
-- A series only steps forward by one: skipping would leave a gap, going
-- back would reissue a number.
CREATE FUNCTION pharmacy_invoice_series_step() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.id,NEW.tenant_id,NEW.outlet_id,NEW.financial_year) IS DISTINCT FROM (OLD.id,OLD.tenant_id,OLD.outlet_id,OLD.financial_year)
    OR NEW.last_number <> OLD.last_number+1 THEN
  RAISE EXCEPTION 'an invoice series steps by one' USING ERRCODE='23514',CONSTRAINT='pharmacy_invoice_series_step';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_invoice_series_step BEFORE UPDATE ON pharmacy_invoice_series
 FOR EACH ROW EXECUTE FUNCTION pharmacy_invoice_series_step();

CREATE TABLE pharmacy_sales (
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 tenant_id uuid NOT NULL,
 outlet_id uuid NOT NULL,
 financial_year text NOT NULL CHECK (financial_year ~ '^[0-9]{4}$'),
 sequence_number bigint NOT NULL CHECK (sequence_number >= 1),
 invoice_number text NOT NULL CHECK (length(invoice_number) <= 16 AND invoice_number ~ '^[A-Z0-9]{1,4}/[0-9]{4}/[1-9][0-9]*$'),
 sale_date date NOT NULL,
 invoice_profile_id uuid NOT NULL,
 seller_legal_name text NOT NULL,
 seller_address text NOT NULL,
 seller_gstin text NOT NULL CHECK (seller_gstin ~ '^[0-9]{2}[0-9A-Z]{13}$'),
 place_of_supply text NOT NULL CHECK (place_of_supply ~ '^[0-9]{2}$' AND substring(seller_gstin,1,2)=place_of_supply),
 currency text NOT NULL CHECK (currency ~ '^[A-Z]{3}$'),
 total_minor bigint NOT NULL CHECK (total_minor >= 0),
 taxable_minor bigint NOT NULL CHECK (taxable_minor >= 0),
 cgst_minor bigint NOT NULL CHECK (cgst_minor >= 0),
 sgst_minor bigint NOT NULL CHECK (sgst_minor >= 0),
 intent_sha256 text NOT NULL CHECK (intent_sha256 ~ '^[0-9a-f]{64}$'),
 actor_subject text NOT NULL CHECK (length(btrim(actor_subject)) BETWEEN 1 AND 200),
 -- The posting transaction, exactly: what may still add lines and tenders.
 posted_xact xid8 NOT NULL DEFAULT pg_current_xact_id(),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,outlet_id,id),
 UNIQUE (tenant_id,outlet_id,invoice_number),
 -- The number names its own year and place in the series.
 CHECK (invoice_number LIKE '%/'||financial_year||'/'||sequence_number),
 CHECK (financial_year = to_char(sale_date - interval '3 months','YY')||to_char(sale_date + interval '9 months','YY')),
 UNIQUE (tenant_id,outlet_id,financial_year,sequence_number),
 CHECK (total_minor = taxable_minor + cgst_minor + sgst_minor),
 FOREIGN KEY (tenant_id,outlet_id,invoice_profile_id) REFERENCES pharmacy_outlet_invoice_profiles (tenant_id,outlet_id,id)
);
ALTER TABLE pharmacy_sales ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_sales FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_sales
 USING (hms_tenant_visible(tenant_id) AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK (pharmacy_outlet_allowed(tenant_id,outlet_id,true) AND actor_subject=NULLIF(current_setting('app.pharmacy_subject',true),''));
CREATE INDEX pharmacy_sale_page ON pharmacy_sales (tenant_id,outlet_id,created_at DESC,id DESC);
CREATE TRIGGER pharmacy_sale_audit AFTER INSERT ON pharmacy_sales
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_sale_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_sales
 FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

CREATE TABLE pharmacy_sale_lines (
 id uuid NOT NULL DEFAULT gen_random_uuid(),
 tenant_id uuid NOT NULL,
 outlet_id uuid NOT NULL,
 sale_id uuid NOT NULL,
 line_number integer NOT NULL CHECK (line_number BETWEEN 1 AND 1000),
 product_id uuid NOT NULL,
 batch_id uuid NOT NULL,
 location_id uuid NOT NULL,
 lot_number text NOT NULL,
 expires_on date NOT NULL,
 -- B-1 sells OTC only; prescription classes arrive with their evidence (B-2).
 regulatory_class text NOT NULL CHECK (regulatory_class = 'none'),
 schedule_declaration_id uuid NOT NULL,
 classification_code text NOT NULL CHECK (classification_code ~ '^[0-9A-Z]{1,20}$'),
 supply_nature text NOT NULL CHECK (supply_nature IN ('taxable','nil_rated','exempt')),
 rate_ppm bigint NOT NULL CHECK (rate_ppm BETWEEN 0 AND 1000000),
 mrp_minor bigint NOT NULL CHECK (mrp_minor BETWEEN 1 AND 1000000000000),
 pack_size integer NOT NULL CHECK (pack_size BETWEEN 1 AND 1000000),
 quantity_units bigint NOT NULL CHECK (quantity_units BETWEEN 1 AND 1000000),
 amount_minor bigint NOT NULL CHECK (amount_minor >= 0),
 taxable_minor bigint NOT NULL CHECK (taxable_minor >= 0),
 cgst_minor bigint NOT NULL CHECK (cgst_minor >= 0),
 sgst_minor bigint NOT NULL CHECK (sgst_minor >= 0),
 PRIMARY KEY (tenant_id,outlet_id,sale_id,line_number),
 UNIQUE (tenant_id,id),
 FOREIGN KEY (tenant_id,outlet_id,sale_id) REFERENCES pharmacy_sales (tenant_id,outlet_id,id),
 FOREIGN KEY (tenant_id,outlet_id,batch_id) REFERENCES pharmacy_stock_batches (tenant_id,outlet_id,id),
 FOREIGN KEY (tenant_id,outlet_id,location_id) REFERENCES pharmacy_stock_locations (tenant_id,outlet_id,id),
 FOREIGN KEY (tenant_id,outlet_id,schedule_declaration_id) REFERENCES pharmacy_product_schedule_declarations (tenant_id,outlet_id,id),
 CHECK (amount_minor = taxable_minor + cgst_minor + sgst_minor),
 CHECK (cgst_minor = sgst_minor),
 CHECK (supply_nature = 'taxable' OR (rate_ppm = 0 AND cgst_minor = 0)),
 -- Never above MRP (#172): the line costs at most its share of the pack price.
 CHECK (amount_minor::numeric * pack_size <= mrp_minor::numeric * quantity_units)
);
ALTER TABLE pharmacy_sale_lines ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_sale_lines FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_sale_lines
 USING (hms_tenant_visible(tenant_id) AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK (pharmacy_outlet_allowed(tenant_id,outlet_id,true));
CREATE TRIGGER pharmacy_sale_line_audit AFTER INSERT ON pharmacy_sale_lines
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_sale_line_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_sale_lines
 FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

CREATE TABLE pharmacy_sale_tenders (
 id uuid NOT NULL DEFAULT gen_random_uuid(),
 tenant_id uuid NOT NULL,
 outlet_id uuid NOT NULL,
 sale_id uuid NOT NULL,
 tender_number integer NOT NULL CHECK (tender_number BETWEEN 1 AND 5),
 kind text NOT NULL CHECK (kind IN ('cash','upi','card')),
 amount_minor bigint NOT NULL CHECK (amount_minor >= 1),
 tendered_minor bigint,
 change_minor bigint,
 reference text,
 PRIMARY KEY (tenant_id,outlet_id,sale_id,tender_number),
 UNIQUE (tenant_id,id),
 FOREIGN KEY (tenant_id,outlet_id,sale_id) REFERENCES pharmacy_sales (tenant_id,outlet_id,id),
 CHECK (CASE kind
  WHEN 'cash' THEN reference IS NULL AND tendered_minor >= amount_minor AND change_minor = tendered_minor - amount_minor
  ELSE tendered_minor IS NULL AND change_minor IS NULL AND length(btrim(reference)) BETWEEN 1 AND 64 END)
);
CREATE UNIQUE INDEX pharmacy_sale_one_cash ON pharmacy_sale_tenders (tenant_id,outlet_id,sale_id) WHERE kind='cash';
ALTER TABLE pharmacy_sale_tenders ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_sale_tenders FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_sale_tenders
 USING (hms_tenant_visible(tenant_id) AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK (pharmacy_outlet_allowed(tenant_id,outlet_id,true));
CREATE TRIGGER pharmacy_sale_tender_audit AFTER INSERT ON pharmacy_sale_tenders
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_sale_tender_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_sale_tenders
 FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

-- Lines and tenders join a sale only in the transaction that posted it, so
-- nothing is added to an invoice once it is committed and checked.
CREATE FUNCTION pharmacy_sale_sealed() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS (SELECT 1 FROM pharmacy_sales s WHERE s.tenant_id=NEW.tenant_id AND s.outlet_id=NEW.outlet_id AND s.id=NEW.sale_id
    AND s.posted_xact=pg_current_xact_id()) THEN
  RAISE EXCEPTION 'a posted sale cannot be added to' USING ERRCODE='23514',CONSTRAINT='pharmacy_sale_sealed';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_sale_line_sealed BEFORE INSERT ON pharmacy_sale_lines
 FOR EACH ROW EXECUTE FUNCTION pharmacy_sale_sealed();
CREATE TRIGGER pharmacy_sale_tender_sealed BEFORE INSERT ON pharmacy_sale_tenders
 FOR EACH ROW EXECUTE FUNCTION pharmacy_sale_sealed();

-- At commit, an invoice is exactly its lines and is paid exactly by its
-- tenders, and each line's stock left the shelf. Deferred, so the sale is
-- written header first and checked whole.
CREATE FUNCTION pharmacy_sale_balanced() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE lines record; paid bigint; moved bigint;
BEGIN
 SELECT count(*) AS n, COALESCE(sum(amount_minor),0) AS amount, COALESCE(sum(taxable_minor),0) AS taxable,
  COALESCE(sum(cgst_minor),0) AS cgst, COALESCE(sum(sgst_minor),0) AS sgst INTO lines
  FROM pharmacy_sale_lines WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND sale_id=NEW.id;
 SELECT COALESCE(sum(amount_minor),0) INTO paid FROM pharmacy_sale_tenders
  WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND sale_id=NEW.id;
 -- Equality on source_reference, so the unique index serves it.
 SELECT count(*) INTO moved FROM pharmacy_sale_lines l JOIN pharmacy_stock_movements m
  ON m.tenant_id=l.tenant_id AND m.outlet_id=l.outlet_id AND m.source_reference='sale:'||l.sale_id||':'||l.line_number AND m.kind='sale'
  WHERE l.tenant_id=NEW.tenant_id AND l.outlet_id=NEW.outlet_id AND l.sale_id=NEW.id;
 IF lines.n = 0 OR lines.amount <> NEW.total_minor OR lines.taxable <> NEW.taxable_minor OR lines.cgst <> NEW.cgst_minor
    OR lines.sgst <> NEW.sgst_minor OR paid <> NEW.total_minor OR moved <> lines.n THEN
  RAISE EXCEPTION 'a sale is its lines, paid by its tenders, with its stock moved' USING ERRCODE='23514',CONSTRAINT='pharmacy_sale_balanced';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER pharmacy_sale_balanced AFTER INSERT ON pharmacy_sales
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION pharmacy_sale_balanced();

ALTER TABLE pharmacy_stock_movements DROP CONSTRAINT pharmacy_stock_movements_kind_check;
ALTER TABLE pharmacy_stock_movements ADD CONSTRAINT pharmacy_stock_movements_kind_check
 CHECK (kind IN ('receipt','correction','release','sale')) NOT VALID;
ALTER TABLE pharmacy_stock_movements VALIDATE CONSTRAINT pharmacy_stock_movements_kind_check;

-- A sale movement takes exactly its line's units off a sellable shelf
-- (spec B2); nothing else reaches stock through kind 'sale'.
CREATE OR REPLACE FUNCTION pharmacy_stock_movement_location_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE status text;
BEGIN
 SELECT stock_status INTO status FROM pharmacy_stock_locations
  WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.location_id;
 IF NEW.kind IN ('receipt','correction') AND status<>'quarantine' THEN
  RAISE EXCEPTION 'stock is received and corrected in quarantine only' USING ERRCODE='23514',CONSTRAINT='pharmacy_stock_quarantine_only';
 END IF;
 IF NEW.kind='release' AND NOT EXISTS (SELECT 1 FROM pharmacy_stock_releases r WHERE r.tenant_id=NEW.tenant_id
    AND r.outlet_id=NEW.outlet_id AND NEW.source_reference IN ('release:'||r.id||':out','release:'||r.id||':in')) THEN
  RAISE EXCEPTION 'a release movement belongs to a recorded release' USING ERRCODE='23514',CONSTRAINT='pharmacy_stock_release_movement';
 END IF;
 -- The reference is parsed, so the line is found by its primary key.
 IF NEW.kind='sale' AND (status<>'sellable' OR NEW.source_reference !~ '^sale:[0-9a-f-]{36}:[1-9][0-9]{0,3}$'
    OR NOT EXISTS (SELECT 1 FROM pharmacy_sale_lines l WHERE l.tenant_id=NEW.tenant_id AND l.outlet_id=NEW.outlet_id
    AND l.sale_id=split_part(NEW.source_reference,':',2)::uuid AND l.line_number=split_part(NEW.source_reference,':',3)::int
    AND l.batch_id=NEW.batch_id AND l.location_id=NEW.location_id AND l.quantity_units=-NEW.quantity_units)) THEN
  RAISE EXCEPTION 'a sale movement takes its line''s units off a sellable shelf' USING ERRCODE='23514',CONSTRAINT='pharmacy_stock_sale_movement';
 END IF;
 RETURN NEW;
END $$;

-- What a sale may take of a product today, earliest expiry first: the one
-- predicate allocation and availability share (plan review focus 1).
CREATE FUNCTION pharmacy_sellable_buckets(tenant uuid, outlet uuid, product uuid, day date)
RETURNS TABLE (location_id uuid, batch_id uuid, lot_number text, expires_on date, mrp_minor bigint, on_hand bigint)
LANGUAGE sql STABLE AS $$
 SELECT m.location_id, b.id, b.lot_number, b.expires_on, b.mrp_minor, sum(m.quantity_units)::bigint
 FROM pharmacy_stock_batches b
 JOIN pharmacy_stock_movements m ON m.tenant_id=b.tenant_id AND m.outlet_id=b.outlet_id AND m.batch_id=b.id
 JOIN pharmacy_stock_locations l ON l.tenant_id=m.tenant_id AND l.outlet_id=m.outlet_id AND l.id=m.location_id
 WHERE b.tenant_id=tenant AND b.outlet_id=outlet AND b.product_id=product AND l.stock_status='sellable'
  AND b.mrp_minor IS NOT NULL AND b.expires_on > day
  AND NOT EXISTS (SELECT 1 FROM pharmacy_batch_safety_incidents i WHERE i.tenant_id=b.tenant_id AND i.outlet_id=b.outlet_id AND i.batch_id=b.id)
 GROUP BY m.location_id, b.id, b.lot_number, b.expires_on, b.mrp_minor
 HAVING sum(m.quantity_units) > 0
 ORDER BY b.expires_on, b.id, m.location_id
$$;

-- A counter sale is a retail sale: only a reviewed retail licence valid on
-- the day allows it; a wholesale licence does not.
CREATE FUNCTION pharmacy_outlet_retail_licence_in_force(tenant uuid, outlet uuid, day date) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT EXISTS (SELECT 1 FROM pharmacy_outlet_licences l JOIN pharmacy_outlet_licence_reviews r
  ON r.tenant_id=l.tenant_id AND r.outlet_id=l.outlet_id AND r.licence_id=l.id
  WHERE l.tenant_id=tenant AND l.outlet_id=outlet AND l.licence_kind='retail' AND r.decision='approved' AND day BETWEEN l.valid_from AND l.valid_until)
$$;

-- What a line copies, the database checks against the source (spec B2, B5):
-- the batch is the product's and sellable on the sale date, its lot, expiry
-- and MRP are the batch's, and the class is the product's approved
-- declaration in force, OTC and not NDPS. A bug in the handler cannot sell
-- an expired, held or prescription batch.
CREATE FUNCTION pharmacy_sale_line_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE day date; licensed boolean;
BEGIN
 SELECT s.sale_date INTO day FROM pharmacy_sales s WHERE s.tenant_id=NEW.tenant_id AND s.outlet_id=NEW.outlet_id AND s.id=NEW.sale_id;
 IF NOT pharmacy_outlet_retail_licence_in_force(NEW.tenant_id,NEW.outlet_id,day)
    OR NOT EXISTS (SELECT 1 FROM pharmacy_stock_batches b WHERE b.tenant_id=NEW.tenant_id AND b.outlet_id=NEW.outlet_id
     AND b.id=NEW.batch_id AND b.product_id=NEW.product_id AND b.lot_number=NEW.lot_number AND b.expires_on=NEW.expires_on AND b.mrp_minor=NEW.mrp_minor)
    OR NOT EXISTS (SELECT 1 FROM pharmacy_sellable_buckets(NEW.tenant_id,NEW.outlet_id,NEW.product_id,day) k
     WHERE k.batch_id=NEW.batch_id AND k.location_id=NEW.location_id)
    OR NOT EXISTS (SELECT 1 FROM pharmacy_product_schedule_in_force(NEW.tenant_id,NEW.outlet_id,NEW.product_id) c
     WHERE c.declaration_id=NEW.schedule_declaration_id AND c.regulatory_class=NEW.regulatory_class AND NOT c.ndps)
    OR NOT EXISTS (SELECT 1 FROM pharmacy_catalogue_products p WHERE p.tenant_id=NEW.tenant_id AND p.outlet_id=NEW.outlet_id
     AND p.id=NEW.product_id AND p.status='active' AND p.pack_size=NEW.pack_size) THEN
  RAISE EXCEPTION 'a sale line must be a licensed, sellable OTC batch at its own price' USING ERRCODE='23514',CONSTRAINT='pharmacy_sale_line_source';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_sale_line_guard BEFORE INSERT ON pharmacy_sale_lines
 FOR EACH ROW EXECUTE FUNCTION pharmacy_sale_line_guard();
