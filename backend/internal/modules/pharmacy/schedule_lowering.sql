SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- #1681: inside the tenant a schedule class can only be made stricter. A
-- declaration that is less strict than the approved class "lowers" it; the
-- database decides that, and only Helivanta (helivanta_drug_approver) may
-- approve one. Fail closed: the refusal lives in the review trigger, so no
-- route, role pair or direct write under hms_app can lower a class.

-- Strictness: none < G < H < H1 < X; an unknown class ranks highest.
CREATE FUNCTION pharmacy_schedule_rank(class text) RETURNS int LANGUAGE sql IMMUTABLE AS $$
 SELECT CASE class WHEN 'none' THEN 0 WHEN 'G' THEN 1 WHEN 'H' THEN 2 WHEN 'H1' THEN 3 ELSE 4 END
$$;

-- A lower rank or dropping the NDPS control is lower, even with the other
-- one raised.
CREATE FUNCTION pharmacy_schedule_is_lower(class text, ndps boolean, than_class text, than_ndps boolean)
RETURNS boolean LANGUAGE sql IMMUTABLE AS $$
 SELECT pharmacy_schedule_rank(class) < pharmacy_schedule_rank(than_class) OR (than_ndps AND NOT ndps)
$$;

-- The latest approved declaration, ignoring newer ones still waiting: the
-- class a new declaration is compared with.
CREATE FUNCTION pharmacy_schedule_last_approved(tenant uuid, outlet uuid, product uuid)
RETURNS TABLE (regulatory_class text, ndps boolean) LANGUAGE sql STABLE AS $$
 SELECT d.regulatory_class, d.ndps FROM pharmacy_product_schedule_declarations d
 JOIN pharmacy_product_schedule_reviews r ON r.tenant_id=d.tenant_id AND r.outlet_id=d.outlet_id AND r.declaration_id=d.id
 WHERE d.tenant_id=tenant AND d.outlet_id=outlet AND d.product_id=product AND r.decision='approved'
 ORDER BY d.seq DESC LIMIT 1
$$;

-- One product's declarations and reviews are decided one at a time, so a
-- declaration cannot be compared with a class a concurrent review replaces.
CREATE FUNCTION pharmacy_schedule_lock(product uuid) RETURNS void LANGUAGE sql AS $$
 SELECT pg_advisory_xact_lock(hashtextextended('pharmacy_schedule:'||product::text, 0))
$$;

ALTER TABLE pharmacy_product_schedule_declarations ADD COLUMN lowers boolean NOT NULL DEFAULT false;

-- Recorded at insert, whatever the client sent. Earlier rows stay false:
-- they were all reviewed under the earlier rule.
CREATE FUNCTION pharmacy_schedule_declaration_lowers() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE prior record;
BEGIN
 PERFORM pharmacy_schedule_lock(NEW.product_id);
 SELECT * INTO prior FROM pharmacy_schedule_last_approved(NEW.tenant_id, NEW.outlet_id, NEW.product_id);
 NEW.lowers := FOUND AND pharmacy_schedule_is_lower(NEW.regulatory_class, NEW.ndps, prior.regulatory_class, prior.ndps);
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_schedule_declaration_lowers BEFORE INSERT ON pharmacy_product_schedule_declarations
 FOR EACH ROW EXECUTE FUNCTION pharmacy_schedule_declaration_lowers();

-- A tenant approval is refused for a recorded lowering, and for any
-- declaration that is lower than the class approved by the time it is
-- reviewed: two declarations filed before either was approved must not let
-- the tenant approve the stricter one, then the laxer one. Rejecting is
-- always allowed; it only keeps the stricter class.
CREATE OR REPLACE FUNCTION pharmacy_schedule_review_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE d record; prior record;
BEGIN
 SELECT * INTO d FROM pharmacy_product_schedule_declarations
  WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.declaration_id;
 IF FOUND AND lower(btrim(d.declared_by))=lower(btrim(NEW.reviewed_by)) THEN
  RAISE EXCEPTION 'a schedule class is reviewed by someone other than who declared it' USING ERRCODE='23514',CONSTRAINT='pharmacy_schedule_four_eyes';
 END IF;
 IF FOUND AND NEW.decision='approved' AND current_user <> 'helivanta_drug_approver' THEN
  PERFORM pharmacy_schedule_lock(d.product_id);
  SELECT * INTO prior FROM pharmacy_schedule_last_approved(d.tenant_id, d.outlet_id, d.product_id);
  IF d.lowers OR (FOUND AND pharmacy_schedule_is_lower(d.regulatory_class, d.ndps, prior.regulatory_class, prior.ndps)) THEN
   RAISE EXCEPTION 'a lower schedule class is decided by Helivanta' USING ERRCODE='23514',CONSTRAINT='pharmacy_schedule_platform_review';
  END IF;
 END IF;
 RETURN NEW;
END $$;

-- The class in force: the latest declaration, skipping lowerings that are
-- not approved, if that one is approved. A waiting or rejected lowering
-- leaves the stricter class in force; a waiting raise still blocks.
CREATE OR REPLACE FUNCTION pharmacy_product_schedule_in_force(tenant uuid, outlet uuid, product uuid)
RETURNS TABLE (declaration_id uuid, regulatory_class text, ndps boolean) LANGUAGE sql STABLE AS $$
 SELECT d.id, d.regulatory_class, d.ndps FROM (
  SELECT * FROM pharmacy_product_schedule_declarations x WHERE x.tenant_id=tenant AND x.outlet_id=outlet AND x.product_id=product
   AND (NOT x.lowers OR EXISTS (SELECT 1 FROM pharmacy_product_schedule_reviews a WHERE a.tenant_id=x.tenant_id AND a.outlet_id=x.outlet_id
    AND a.declaration_id=x.id AND a.decision='approved'))
  ORDER BY x.seq DESC LIMIT 1) d
 JOIN pharmacy_product_schedule_reviews r ON r.tenant_id=d.tenant_id AND r.outlet_id=d.outlet_id AND r.declaration_id=d.id
 WHERE r.decision='approved'
$$;

-- The pharmacy owner countersigns a lowering before Helivanta decides it:
-- the owner answers for asking, but cannot decide (#1681).
CREATE TABLE pharmacy_product_schedule_countersigns (
 id uuid NOT NULL DEFAULT gen_random_uuid(),
 tenant_id uuid NOT NULL,
 outlet_id uuid NOT NULL,
 declaration_id uuid NOT NULL,
 reason text NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 500),
 countersigned_by text NOT NULL CHECK (length(btrim(countersigned_by)) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,outlet_id,declaration_id),
 UNIQUE (tenant_id,id),
 FOREIGN KEY (tenant_id,outlet_id,declaration_id) REFERENCES pharmacy_product_schedule_declarations (tenant_id,outlet_id,id)
);
ALTER TABLE pharmacy_product_schedule_countersigns ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_product_schedule_countersigns FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_product_schedule_countersigns
 USING (hms_tenant_visible(tenant_id) AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK (pharmacy_outlet_allowed(tenant_id,outlet_id,true) AND countersigned_by=NULLIF(current_setting('app.pharmacy_subject',true),''));
CREATE TRIGGER pharmacy_schedule_countersign_audit AFTER INSERT ON pharmacy_product_schedule_countersigns
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_schedule_countersign_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_product_schedule_countersigns
 FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

CREATE FUNCTION pharmacy_schedule_countersign_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE d record;
BEGIN
 SELECT * INTO d FROM pharmacy_product_schedule_declarations
  WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.declaration_id;
 IF FOUND AND NOT d.lowers THEN
  RAISE EXCEPTION 'only a lower schedule class is countersigned' USING ERRCODE='23514',CONSTRAINT='pharmacy_schedule_countersign_lowering_only';
 END IF;
 IF FOUND AND lower(btrim(d.declared_by))=lower(btrim(NEW.countersigned_by)) THEN
  RAISE EXCEPTION 'a lower schedule class is countersigned by someone other than who declared it' USING ERRCODE='23514',CONSTRAINT='pharmacy_schedule_four_eyes';
 END IF;
 IF EXISTS (SELECT 1 FROM pharmacy_product_schedule_reviews r WHERE r.tenant_id=NEW.tenant_id AND r.outlet_id=NEW.outlet_id AND r.declaration_id=NEW.declaration_id) THEN
  RAISE EXCEPTION 'this declaration has already been decided' USING ERRCODE='23514',CONSTRAINT='pharmacy_schedule_countersign_undecided';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_schedule_countersign_guard BEFORE INSERT ON pharmacy_product_schedule_countersigns
 FOR EACH ROW EXECUTE FUNCTION pharmacy_schedule_countersign_guard();
