SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Item categories (#988 slice 1): an outlet's categories and, under one
-- category, its subcategories. Two levels, as the legacy item category and
-- subcategory. A category's code and level are fixed; its name may change;
-- it is retired, never deleted.
CREATE TABLE pharmacy_item_categories (
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 tenant_id uuid NOT NULL,
 outlet_id uuid NOT NULL,
 parent_id uuid,
 code text NOT NULL CHECK (code ~ '^[A-Z0-9][A-Z0-9_-]{0,19}$'),
 name text NOT NULL CHECK (length(name) BETWEEN 1 AND 100 AND name=btrim(name) AND name !~ '[[:cntrl:]]'),
 retired_at timestamptz,
 created_by text NOT NULL CHECK (length(btrim(created_by)) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,outlet_id,id),
 FOREIGN KEY (tenant_id,outlet_id,parent_id) REFERENCES pharmacy_item_categories (tenant_id,outlet_id,id),
 CONSTRAINT pharmacy_item_category_code UNIQUE NULLS NOT DISTINCT (tenant_id,outlet_id,parent_id,code)
);
ALTER TABLE pharmacy_item_categories ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_item_categories FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_item_categories
 USING (hms_tenant_visible(tenant_id) AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK (pharmacy_outlet_allowed(tenant_id,outlet_id,true));
CREATE INDEX pharmacy_item_category_page ON pharmacy_item_categories (tenant_id,outlet_id,created_at,id);
CREATE TRIGGER pharmacy_item_category_audit AFTER INSERT OR UPDATE ON pharmacy_item_categories
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();

-- Two levels; a retired category takes no new subcategory; code, level and
-- creation are fixed and a retirement is recorded once.
CREATE FUNCTION pharmacy_item_category_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
 p record;
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'an item category is retired, never deleted' USING ERRCODE='23514'; END IF;
 IF TG_OP='INSERT' AND NEW.parent_id IS NOT NULL THEN
  -- FOR SHARE: a retirement being applied is wholly before or after this.
  SELECT parent_id,retired_at INTO p FROM pharmacy_item_categories WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.parent_id FOR SHARE;
  IF p.parent_id IS NOT NULL THEN
   RAISE EXCEPTION 'a subcategory belongs to a category, not to another subcategory' USING ERRCODE='23514', CONSTRAINT='pharmacy_item_category_levels';
  END IF;
  IF p.retired_at IS NOT NULL THEN
   RAISE EXCEPTION 'a retired category takes no new subcategory' USING ERRCODE='23514', CONSTRAINT='pharmacy_item_category_retired';
  END IF;
 END IF;
 IF TG_OP='UPDATE' AND ((NEW.tenant_id,NEW.outlet_id,NEW.id,NEW.parent_id,NEW.code,NEW.created_by,NEW.created_at)
   IS DISTINCT FROM (OLD.tenant_id,OLD.outlet_id,OLD.id,OLD.parent_id,OLD.code,OLD.created_by,OLD.created_at)
   OR (OLD.retired_at IS NOT NULL AND NEW.retired_at IS DISTINCT FROM OLD.retired_at)) THEN
  RAISE EXCEPTION 'an item category''s code and level are fixed and it is retired once' USING ERRCODE='23514', CONSTRAINT='pharmacy_item_category_fixed';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_item_category_guard BEFORE INSERT OR UPDATE OR DELETE ON pharmacy_item_categories FOR EACH ROW EXECUTE FUNCTION pharmacy_item_category_guard();
CREATE TRIGGER pharmacy_item_category_no_truncate BEFORE TRUNCATE ON pharmacy_item_categories FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

-- A product's category is declared by one person with the date it takes
-- effect and a reason, and reviewed by another, as a schedule class is.
CREATE TABLE pharmacy_product_category_declarations (
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 tenant_id uuid NOT NULL,
 outlet_id uuid NOT NULL,
 product_id uuid NOT NULL,
 category_id uuid NOT NULL,
 subcategory_id uuid,
 effective_from date NOT NULL,
 reason text NOT NULL CHECK (length(reason) BETWEEN 5 AND 300 AND reason=btrim(reason) AND reason !~ '[[:cntrl:]]'),
 intent_sha256 text NOT NULL CHECK (intent_sha256 ~ '^[0-9a-f]{64}$'),
 declared_by text NOT NULL CHECK (length(btrim(declared_by)) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,outlet_id,id),
 FOREIGN KEY (tenant_id,outlet_id,product_id) REFERENCES pharmacy_catalogue_products (tenant_id,outlet_id,id),
 FOREIGN KEY (tenant_id,outlet_id,category_id) REFERENCES pharmacy_item_categories (tenant_id,outlet_id,id),
 FOREIGN KEY (tenant_id,outlet_id,subcategory_id) REFERENCES pharmacy_item_categories (tenant_id,outlet_id,id)
);
ALTER TABLE pharmacy_product_category_declarations ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_product_category_declarations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_product_category_declarations
 USING (hms_tenant_visible(tenant_id) AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK (pharmacy_outlet_allowed(tenant_id,outlet_id,true) AND declared_by=NULLIF(current_setting('app.pharmacy_subject',true),''));
CREATE INDEX pharmacy_product_category_history ON pharmacy_product_category_declarations (tenant_id,outlet_id,product_id,created_at DESC);
CREATE TRIGGER pharmacy_product_category_declaration_audit AFTER INSERT ON pharmacy_product_category_declarations
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_product_category_declaration_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_product_category_declarations
 FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

-- A product is put in a live top-level category, and a subcategory only of
-- that category: checked here so no code path files it anywhere else.
CREATE FUNCTION pharmacy_product_category_declaration_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
 c record;
 s record;
BEGIN
 SELECT parent_id,retired_at INTO c FROM pharmacy_item_categories WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.category_id FOR SHARE;
 IF c.parent_id IS NOT NULL OR c.retired_at IS NOT NULL THEN
  RAISE EXCEPTION 'a product goes in a category in use, not a subcategory or a retired category' USING ERRCODE='23514', CONSTRAINT='pharmacy_product_category_live';
 END IF;
 IF NEW.subcategory_id IS NOT NULL THEN
  SELECT parent_id,retired_at INTO s FROM pharmacy_item_categories WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.subcategory_id FOR SHARE;
  IF s.parent_id IS DISTINCT FROM NEW.category_id OR s.retired_at IS NOT NULL THEN
   RAISE EXCEPTION 'the subcategory is a live one of that category' USING ERRCODE='23514', CONSTRAINT='pharmacy_product_category_live';
  END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_product_category_declaration_guard BEFORE INSERT ON pharmacy_product_category_declarations FOR EACH ROW EXECUTE FUNCTION pharmacy_product_category_declaration_guard();

-- A review records when an approval takes effect: the later of the
-- declaration's date and the outlet's date when it was approved, so an
-- approval never reaches back over sales already made.
CREATE TABLE pharmacy_product_category_reviews (
 id uuid NOT NULL DEFAULT gen_random_uuid(),
 tenant_id uuid NOT NULL,
 outlet_id uuid NOT NULL,
 declaration_id uuid NOT NULL,
 decision text NOT NULL CHECK (decision IN ('approved','rejected')),
 reason text NOT NULL CHECK (length(reason) BETWEEN 1 AND 500 AND reason=btrim(reason)),
 in_force_from date,
 reviewed_by text NOT NULL CHECK (length(btrim(reviewed_by)) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,outlet_id,declaration_id),
 UNIQUE (tenant_id,id),
 FOREIGN KEY (tenant_id,outlet_id,declaration_id) REFERENCES pharmacy_product_category_declarations (tenant_id,outlet_id,id),
 CHECK ((decision='approved') = (in_force_from IS NOT NULL))
);
ALTER TABLE pharmacy_product_category_reviews ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_product_category_reviews FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_product_category_reviews
 USING (hms_tenant_visible(tenant_id) AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK (pharmacy_outlet_allowed(tenant_id,outlet_id,true) AND reviewed_by=NULLIF(current_setting('app.pharmacy_subject',true),''));
CREATE TRIGGER pharmacy_product_category_review_audit AFTER INSERT ON pharmacy_product_category_reviews
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_product_category_review_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_product_category_reviews
 FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

-- Four eyes; an approval in force no earlier than its declaration's date;
-- only the product's newest declaration approved, so an older one left
-- pending never comes back over a newer decision; and never into a category
-- or subcategory retired since it was declared (locked, so a retirement being
-- applied is wholly before or after the approval).
CREATE FUNCTION pharmacy_product_category_review_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
 d record;
BEGIN
 SELECT product_id,category_id,subcategory_id,declared_by,effective_from,created_at INTO d FROM pharmacy_product_category_declarations
  WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.declaration_id;
 IF lower(btrim(d.declared_by))=lower(btrim(NEW.reviewed_by)) THEN
  RAISE EXCEPTION 'a product category is reviewed by someone other than who declared it' USING ERRCODE='23514', CONSTRAINT='pharmacy_product_category_four_eyes';
 END IF;
 IF NEW.decision <> 'approved' THEN RETURN NEW; END IF;
 IF NEW.in_force_from < d.effective_from THEN
  RAISE EXCEPTION 'an approval is in force no earlier than its declaration''s date' USING ERRCODE='23514', CONSTRAINT='pharmacy_product_category_in_force';
 END IF;
 IF EXISTS (SELECT 1 FROM pharmacy_product_category_declarations n WHERE n.tenant_id=NEW.tenant_id AND n.outlet_id=NEW.outlet_id
    AND n.product_id=d.product_id AND (n.created_at,n.id) > (d.created_at,NEW.declaration_id)) THEN
  RAISE EXCEPTION 'a newer category declaration for this product superseded this one' USING ERRCODE='23514', CONSTRAINT='pharmacy_product_category_superseded';
 END IF;
 -- Lock first: under READ COMMITTED the check, a later statement, then sees
 -- any retirement committed while the lock waited.
 PERFORM 1 FROM pharmacy_item_categories c WHERE c.tenant_id=NEW.tenant_id AND c.outlet_id=NEW.outlet_id AND c.id IN (d.category_id,d.subcategory_id) FOR SHARE;
 IF EXISTS (SELECT 1 FROM pharmacy_item_categories c WHERE c.tenant_id=NEW.tenant_id AND c.outlet_id=NEW.outlet_id
    AND c.id IN (d.category_id,d.subcategory_id) AND c.retired_at IS NOT NULL) THEN
  RAISE EXCEPTION 'a product goes in a category in use, not a retired one' USING ERRCODE='23514', CONSTRAINT='pharmacy_product_category_live';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_product_category_review_guard BEFORE INSERT ON pharmacy_product_category_reviews FOR EACH ROW EXECUTE FUNCTION pharmacy_product_category_review_guard();

-- The category a product counted as on a day: the approved declaration in
-- force then, the latest to come into force winning, then the latest approved.
CREATE FUNCTION pharmacy_product_category_on(tenant uuid, outlet uuid, product uuid, day date)
RETURNS TABLE (declaration_id uuid, category_id uuid, subcategory_id uuid, in_force_from date) LANGUAGE sql STABLE AS $$
 SELECT d.id, d.category_id, d.subcategory_id, r.in_force_from
 FROM pharmacy_product_category_declarations d
 JOIN pharmacy_product_category_reviews r ON r.tenant_id=d.tenant_id AND r.outlet_id=d.outlet_id AND r.declaration_id=d.id
 WHERE d.tenant_id=tenant AND d.outlet_id=outlet AND d.product_id=product AND r.decision='approved' AND r.in_force_from <= day
 ORDER BY r.in_force_from DESC, r.created_at DESC, d.id DESC
 LIMIT 1
$$;
