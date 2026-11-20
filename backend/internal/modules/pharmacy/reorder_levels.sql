SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE TABLE pharmacy_reorder_levels (
 tenant_id uuid NOT NULL, outlet_id uuid NOT NULL, product_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 minimum_units bigint CHECK(minimum_units BETWEEN 0 AND 1000000000),
 expected_version bigint NOT NULL CHECK(expected_version BETWEEN 0 AND 9007199254740990),
 product_version bigint NOT NULL CHECK(product_version BETWEEN 1 AND 9007199254740991),
 version bigint NOT NULL CHECK(version=expected_version+1),
 unit_of_issue text NOT NULL CHECK(length(btrim(unit_of_issue)) BETWEEN 1 AND 40),
 actor_subject text NOT NULL CHECK(length(btrim(actor_subject)) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,outlet_id,product_id,version),
 UNIQUE(tenant_id,outlet_id,id),
 FOREIGN KEY(tenant_id,outlet_id,product_id) REFERENCES pharmacy_catalogue_products(tenant_id,outlet_id,id) ON DELETE RESTRICT
);
ALTER TABLE pharmacy_reorder_levels ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_reorder_levels FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_reorder_levels
 USING(hms_tenant_visible(tenant_id) AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK(pharmacy_outlet_allowed(tenant_id,outlet_id,true));
CREATE TRIGGER pharmacy_reorder_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_reorder_levels
 FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();
CREATE TRIGGER pharmacy_reorder_audit AFTER INSERT ON pharmacy_reorder_levels
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();

CREATE FUNCTION pharmacy_reorder_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE p pharmacy_catalogue_products;current_version bigint;
BEGIN
 SELECT * INTO p FROM pharmacy_catalogue_products WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.product_id FOR UPDATE;
 IF p.id IS NULL OR p.status<>'active' OR p.version<>NEW.product_version OR p.unit_of_issue<>NEW.unit_of_issue
 OR NEW.actor_subject IS DISTINCT FROM NULLIF(current_setting('app.pharmacy_subject',true),'') THEN
  RAISE EXCEPTION 'reorder product changed' USING ERRCODE='23514',CONSTRAINT='pharmacy_reorder_product';
 END IF;
 SELECT COALESCE(MAX(version),0) INTO current_version FROM pharmacy_reorder_levels
 WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND product_id=NEW.product_id;
 IF NEW.expected_version<>current_version THEN
  RAISE EXCEPTION 'reorder version changed' USING ERRCODE='23514',CONSTRAINT='pharmacy_reorder_version';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_reorder_guard BEFORE INSERT ON pharmacy_reorder_levels
 FOR EACH ROW EXECUTE FUNCTION pharmacy_reorder_guard();
