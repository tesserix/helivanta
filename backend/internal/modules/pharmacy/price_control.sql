SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';
-- NPPA ceiling prices (#1371). An outlet declares whether a product is a
-- scheduled formulation under price control and, if it is, its notified
-- ceiling per unit of issue, excluding GST (DPCO 2013 paras 2(1)(d), 11(2)),
-- with the order it came from. A second person reviews it. A product with no
-- declaration sells under its MRP alone; once declared, it sells only under an
-- approved declaration in force, so a pending correction can never be
-- bypassed by an older approval.
CREATE TABLE pharmacy_product_price_declarations (
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 tenant_id uuid NOT NULL,
 outlet_id uuid NOT NULL,
 product_id uuid NOT NULL,
 seq bigint GENERATED ALWAYS AS IDENTITY,
 controlled boolean NOT NULL,
 ceiling_minor bigint CHECK (ceiling_minor BETWEEN 1 AND 1000000000000),
 order_reference text CHECK (order_reference=btrim(order_reference) AND length(order_reference) BETWEEN 1 AND 120),
 order_date date,
 effective_from date NOT NULL,
 source text NOT NULL CHECK (length(btrim(source)) BETWEEN 1 AND 300),
 intent_sha256 text NOT NULL CHECK (intent_sha256 ~ '^[0-9a-f]{64}$'),
 declared_by text NOT NULL CHECK (length(btrim(declared_by)) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,outlet_id,id),
 UNIQUE (seq),
 FOREIGN KEY (tenant_id,outlet_id,product_id) REFERENCES pharmacy_catalogue_products (tenant_id,outlet_id,id),
 CONSTRAINT pharmacy_price_declaration_form CHECK (
  (controlled AND num_nonnulls(ceiling_minor,order_reference,order_date)=3 AND order_date<=effective_from)
  OR (NOT controlled AND num_nonnulls(ceiling_minor,order_reference,order_date)=0))
);
ALTER TABLE pharmacy_product_price_declarations ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_product_price_declarations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_product_price_declarations
 USING (hms_tenant_visible(tenant_id) AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK (pharmacy_outlet_allowed(tenant_id,outlet_id,true) AND declared_by=NULLIF(current_setting('app.pharmacy_subject',true),''));
CREATE INDEX pharmacy_price_declaration_current ON pharmacy_product_price_declarations (tenant_id,outlet_id,product_id,seq DESC);
CREATE TRIGGER pharmacy_price_declaration_audit AFTER INSERT ON pharmacy_product_price_declarations
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_price_declaration_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_product_price_declarations
 FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

CREATE TABLE pharmacy_product_price_reviews (
 id uuid NOT NULL DEFAULT gen_random_uuid(),
 tenant_id uuid NOT NULL,
 outlet_id uuid NOT NULL,
 declaration_id uuid NOT NULL,
 decision text NOT NULL CHECK (decision IN ('approved','rejected')),
 reason text NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 500),
 reviewed_by text NOT NULL CHECK (length(btrim(reviewed_by)) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,outlet_id,declaration_id),
 UNIQUE (tenant_id,id),
 FOREIGN KEY (tenant_id,outlet_id,declaration_id) REFERENCES pharmacy_product_price_declarations (tenant_id,outlet_id,id)
);
ALTER TABLE pharmacy_product_price_reviews ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_product_price_reviews FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_product_price_reviews
 USING (hms_tenant_visible(tenant_id) AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK (pharmacy_outlet_allowed(tenant_id,outlet_id,true) AND reviewed_by=NULLIF(current_setting('app.pharmacy_subject',true),''));
CREATE TRIGGER pharmacy_price_review_audit AFTER INSERT ON pharmacy_product_price_reviews
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_price_review_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_product_price_reviews
 FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

CREATE FUNCTION pharmacy_price_review_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS (SELECT 1 FROM pharmacy_product_price_declarations d WHERE d.tenant_id=NEW.tenant_id AND d.outlet_id=NEW.outlet_id
    AND d.id=NEW.declaration_id AND lower(btrim(d.declared_by))=lower(btrim(NEW.reviewed_by))) THEN
  RAISE EXCEPTION 'a price control is reviewed by someone other than who declared it' USING ERRCODE='23514',CONSTRAINT='pharmacy_price_four_eyes';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_price_review_guard BEFORE INSERT ON pharmacy_product_price_reviews
 FOR EACH ROW EXECUTE FUNCTION pharmacy_price_review_guard();

-- The price control a product sells under on a day. undeclared: no
-- declaration, sold under its MRP alone. blocked: the latest declaration is
-- not approved, or no approved one has started; it is not sold. in_force: the
-- approved declaration with the latest start on or before the day, so an
-- approved change dated ahead leaves the current one in force until then.
CREATE FUNCTION pharmacy_product_price_in_force(tenant uuid, outlet uuid, product uuid, day date)
RETURNS TABLE (state text, declaration_id uuid, controlled boolean, ceiling_minor bigint) LANGUAGE plpgsql STABLE AS $$
DECLARE latest_approved boolean; pick pharmacy_product_price_declarations;
BEGIN
 SELECT r.decision='approved' INTO latest_approved FROM (
  SELECT * FROM pharmacy_product_price_declarations WHERE tenant_id=tenant AND outlet_id=outlet AND product_id=product ORDER BY seq DESC LIMIT 1) d
  LEFT JOIN pharmacy_product_price_reviews r ON r.tenant_id=d.tenant_id AND r.outlet_id=d.outlet_id AND r.declaration_id=d.id;
 IF NOT FOUND THEN
  RETURN QUERY SELECT 'undeclared'::text, NULL::uuid, NULL::boolean, NULL::bigint;
  RETURN;
 END IF;
 SELECT d.* INTO pick FROM pharmacy_product_price_declarations d JOIN pharmacy_product_price_reviews r
  ON r.tenant_id=d.tenant_id AND r.outlet_id=d.outlet_id AND r.declaration_id=d.id
  WHERE d.tenant_id=tenant AND d.outlet_id=outlet AND d.product_id=product AND r.decision='approved' AND d.effective_from<=day
  ORDER BY d.effective_from DESC, d.seq DESC LIMIT 1;
 IF NOT COALESCE(latest_approved,false) OR pick.id IS NULL THEN
  RETURN QUERY SELECT 'blocked'::text, NULL::uuid, NULL::boolean, NULL::bigint;
  RETURN;
 END IF;
 RETURN QUERY SELECT 'in_force'::text, pick.id, pick.controlled, pick.ceiling_minor;
END $$;

-- A line records the declaration it sold under, the ceiling it was held to
-- and which limit set its list price. Lines sold before this have none.
ALTER TABLE pharmacy_sale_lines
 ADD COLUMN price_declaration_id uuid,
 ADD COLUMN ceiling_minor bigint CHECK (ceiling_minor BETWEEN 1 AND 1000000000000),
 ADD COLUMN price_limit text NOT NULL DEFAULT 'mrp' CHECK (price_limit IN ('mrp','ceiling')),
 ADD CONSTRAINT pharmacy_sale_line_price_declaration FOREIGN KEY (tenant_id,outlet_id,price_declaration_id)
  REFERENCES pharmacy_product_price_declarations (tenant_id,outlet_id,id),
 ADD CONSTRAINT pharmacy_sale_line_price_form CHECK (
  (ceiling_minor IS NULL OR price_declaration_id IS NOT NULL) AND (price_limit='mrp' OR ceiling_minor IS NOT NULL));

-- The database holds every new line to the declaration in force on the sale
-- date: a blocked product is not sold, the line names the declaration and
-- ceiling actually in force, and its value before GST is within the ceiling
-- for its units (DPCO para 26: the lower of label and price list).
CREATE FUNCTION pharmacy_sale_line_price_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE day date; p record;
BEGIN
 SELECT s.sale_date INTO day FROM pharmacy_sales s WHERE s.tenant_id=NEW.tenant_id AND s.outlet_id=NEW.outlet_id AND s.id=NEW.sale_id;
 SELECT * INTO p FROM pharmacy_product_price_in_force(NEW.tenant_id,NEW.outlet_id,NEW.product_id,day);
 IF p.state='blocked' THEN
  RAISE EXCEPTION 'a product under price-control review has no ceiling in force' USING ERRCODE='23514',CONSTRAINT='pharmacy_sale_line_price_unknown';
 END IF;
 IF NEW.price_declaration_id IS DISTINCT FROM p.declaration_id
    OR NEW.ceiling_minor IS DISTINCT FROM (CASE WHEN p.controlled THEN p.ceiling_minor END) THEN
  RAISE EXCEPTION 'a sale line names the price control in force' USING ERRCODE='23514',CONSTRAINT='pharmacy_sale_line_price_source';
 END IF;
 IF NEW.ceiling_minor IS NOT NULL AND NEW.taxable_minor::numeric > NEW.ceiling_minor::numeric * NEW.quantity_units THEN
  RAISE EXCEPTION 'a sale line is above the NPPA ceiling price' USING ERRCODE='23514',CONSTRAINT='pharmacy_sale_line_ceiling';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_sale_line_price_guard BEFORE INSERT ON pharmacy_sale_lines
 FOR EACH ROW EXECUTE FUNCTION pharmacy_sale_line_price_guard();
