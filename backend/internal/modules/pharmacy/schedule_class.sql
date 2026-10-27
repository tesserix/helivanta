SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Outlet-declared schedule class (#1386 A2, decision 2). Until the drug
-- master is loaded, a pharmacist declares each product's Drugs Rules class
-- and NDPS control with the source it came from; a second person reviews
-- it. Only the latest declaration counts, and only once approved: a newer
-- one awaiting review blocks the product, so a stricter class can never be
-- bypassed by an older approval.
CREATE TABLE pharmacy_product_schedule_declarations (
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 tenant_id uuid NOT NULL,
 outlet_id uuid NOT NULL,
 product_id uuid NOT NULL,
 seq bigint GENERATED ALWAYS AS IDENTITY,
 regulatory_class text NOT NULL CHECK (regulatory_class IN ('none','G','H','H1','X')),
 ndps boolean NOT NULL,
 source text NOT NULL CHECK (length(btrim(source)) BETWEEN 1 AND 300),
 intent_sha256 text NOT NULL CHECK (intent_sha256 ~ '^[0-9a-f]{64}$'),
 declared_by text NOT NULL CHECK (length(btrim(declared_by)) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,outlet_id,id),
 UNIQUE (seq),
 FOREIGN KEY (tenant_id,outlet_id,product_id) REFERENCES pharmacy_catalogue_products (tenant_id,outlet_id,id)
);
ALTER TABLE pharmacy_product_schedule_declarations ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_product_schedule_declarations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_product_schedule_declarations
 USING (hms_tenant_visible(tenant_id) AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK (pharmacy_outlet_allowed(tenant_id,outlet_id,true) AND declared_by=NULLIF(current_setting('app.pharmacy_subject',true),''));
CREATE INDEX pharmacy_schedule_declaration_current ON pharmacy_product_schedule_declarations (tenant_id,outlet_id,product_id,seq DESC);
CREATE TRIGGER pharmacy_schedule_declaration_audit AFTER INSERT ON pharmacy_product_schedule_declarations
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_schedule_declaration_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_product_schedule_declarations
 FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

CREATE TABLE pharmacy_product_schedule_reviews (
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
 FOREIGN KEY (tenant_id,outlet_id,declaration_id) REFERENCES pharmacy_product_schedule_declarations (tenant_id,outlet_id,id)
);
ALTER TABLE pharmacy_product_schedule_reviews ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_product_schedule_reviews FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_product_schedule_reviews
 USING (hms_tenant_visible(tenant_id) AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK (pharmacy_outlet_allowed(tenant_id,outlet_id,true) AND reviewed_by=NULLIF(current_setting('app.pharmacy_subject',true),''));
CREATE TRIGGER pharmacy_schedule_review_audit AFTER INSERT ON pharmacy_product_schedule_reviews
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_schedule_review_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_product_schedule_reviews
 FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

CREATE FUNCTION pharmacy_schedule_review_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS (SELECT 1 FROM pharmacy_product_schedule_declarations d WHERE d.tenant_id=NEW.tenant_id AND d.outlet_id=NEW.outlet_id
    AND d.id=NEW.declaration_id AND lower(btrim(d.declared_by))=lower(btrim(NEW.reviewed_by))) THEN
  RAISE EXCEPTION 'a schedule class is reviewed by someone other than who declared it' USING ERRCODE='23514',CONSTRAINT='pharmacy_schedule_four_eyes';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_schedule_review_guard BEFORE INSERT ON pharmacy_product_schedule_reviews
 FOR EACH ROW EXECUTE FUNCTION pharmacy_schedule_review_guard();

-- The class a product may be handled under, or no row: its latest
-- declaration, if that one is approved. Later slices read this inside their
-- own transaction.
CREATE FUNCTION pharmacy_product_schedule_in_force(tenant uuid, outlet uuid, product uuid)
RETURNS TABLE (declaration_id uuid, regulatory_class text, ndps boolean) LANGUAGE sql STABLE AS $$
 SELECT d.id, d.regulatory_class, d.ndps FROM (
  SELECT * FROM pharmacy_product_schedule_declarations WHERE tenant_id=tenant AND outlet_id=outlet AND product_id=product
  ORDER BY seq DESC LIMIT 1) d
 JOIN pharmacy_product_schedule_reviews r ON r.tenant_id=d.tenant_id AND r.outlet_id=d.outlet_id AND r.declaration_id=d.id
 WHERE r.decision='approved'
$$;
