SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE pharmacy_stock_locations (
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 tenant_id uuid NOT NULL,
 outlet_id uuid NOT NULL,
 code text NOT NULL CHECK (code ~ '^[A-Z0-9][A-Z0-9._-]{0,39}$'),
 name text NOT NULL CHECK (length(btrim(name)) BETWEEN 1 AND 200),
 stock_status text NOT NULL DEFAULT 'quarantine' CHECK (stock_status='quarantine'),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,outlet_id,id),
 UNIQUE (tenant_id,outlet_id,code),
 FOREIGN KEY (tenant_id,outlet_id) REFERENCES pharmacy_outlets (tenant_id,id)
);
ALTER TABLE pharmacy_stock_locations ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_stock_locations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_stock_locations
 USING (hms_tenant_visible(tenant_id) AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK (pharmacy_outlet_allowed(tenant_id,outlet_id,true));
CREATE INDEX pharmacy_stock_location_page ON pharmacy_stock_locations (tenant_id,outlet_id,created_at DESC,id DESC);
CREATE TRIGGER pharmacy_stock_location_audit AFTER INSERT ON pharmacy_stock_locations
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_stock_location_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_stock_locations
 FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

CREATE TABLE pharmacy_stock_batches (
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 tenant_id uuid NOT NULL,
 outlet_id uuid NOT NULL,
 product_id uuid NOT NULL,
 product_version bigint NOT NULL,
 lot_number text NOT NULL CHECK (length(btrim(lot_number)) BETWEEN 1 AND 100),
 expires_on date NOT NULL,
 manufactured_on date,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,outlet_id,id),
 UNIQUE (tenant_id,outlet_id,product_id,lot_number),
 FOREIGN KEY (tenant_id,outlet_id,product_id,product_version)
 REFERENCES pharmacy_catalogue_revisions (tenant_id,outlet_id,product_id,version),
 CHECK (manufactured_on IS NULL OR manufactured_on <= expires_on)
);
ALTER TABLE pharmacy_stock_batches ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_stock_batches FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_stock_batches
 USING (hms_tenant_visible(tenant_id) AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK (pharmacy_outlet_allowed(tenant_id,outlet_id,true));
CREATE INDEX pharmacy_stock_batch_page ON pharmacy_stock_batches (tenant_id,outlet_id,created_at DESC,id DESC);
CREATE INDEX pharmacy_stock_batch_product ON pharmacy_stock_batches (tenant_id,outlet_id,product_id,product_version);
CREATE INDEX pharmacy_stock_batch_expiry ON pharmacy_stock_batches (tenant_id,outlet_id,expires_on,id);
CREATE TRIGGER pharmacy_stock_batch_audit AFTER INSERT ON pharmacy_stock_batches
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_stock_batch_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_stock_batches
 FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

CREATE TABLE pharmacy_stock_movements (
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 tenant_id uuid NOT NULL,
 outlet_id uuid NOT NULL,
 location_id uuid NOT NULL,
 batch_id uuid NOT NULL,
 kind text NOT NULL CHECK (kind IN ('receipt','correction')),
 quantity_units bigint NOT NULL CHECK (quantity_units <> 0 AND quantity_units BETWEEN -1000000000 AND 1000000000),
 reason text NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 500),
 source_reference text NOT NULL CHECK (length(btrim(source_reference)) BETWEEN 1 AND 100),
 actor_subject text NOT NULL CHECK (length(btrim(actor_subject)) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,outlet_id,id),
 UNIQUE (tenant_id,outlet_id,source_reference),
 FOREIGN KEY (tenant_id,outlet_id,location_id) REFERENCES pharmacy_stock_locations (tenant_id,outlet_id,id),
 FOREIGN KEY (tenant_id,outlet_id,batch_id) REFERENCES pharmacy_stock_batches (tenant_id,outlet_id,id),
 CHECK (kind <> 'receipt' OR quantity_units > 0)
);
ALTER TABLE pharmacy_stock_movements ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_stock_movements FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_stock_movements
 USING (hms_tenant_visible(tenant_id) AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK (pharmacy_outlet_allowed(tenant_id,outlet_id,true));
CREATE INDEX pharmacy_stock_movement_page ON pharmacy_stock_movements (tenant_id,outlet_id,created_at DESC,id DESC);
CREATE INDEX pharmacy_stock_movement_bucket ON pharmacy_stock_movements (tenant_id,outlet_id,location_id,batch_id) INCLUDE (quantity_units);
CREATE INDEX pharmacy_stock_movement_batch ON pharmacy_stock_movements (tenant_id,outlet_id,batch_id);
CREATE TRIGGER pharmacy_stock_movement_audit AFTER INSERT ON pharmacy_stock_movements
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_stock_movement_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_stock_movements
 FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

CREATE FUNCTION pharmacy_stock_quantity_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE total numeric;
BEGIN
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id::text||':'||NEW.outlet_id::text||':'||NEW.location_id::text||':'||NEW.batch_id::text,0));
 SELECT COALESCE(sum(quantity_units),0)+NEW.quantity_units INTO total FROM pharmacy_stock_movements
 WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND location_id=NEW.location_id AND batch_id=NEW.batch_id;
 IF total < 0 OR total > 1000000000000 THEN
  RAISE EXCEPTION 'quarantine stock balance out of bounds' USING ERRCODE='23514',CONSTRAINT='pharmacy_stock_quantity_bounds';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_stock_quantity_guard BEFORE INSERT ON pharmacy_stock_movements
 FOR EACH ROW EXECUTE FUNCTION pharmacy_stock_quantity_guard();

CREATE FUNCTION pharmacy_stock_product_identity_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.pack_size,NEW.unit_of_issue,NEW.ingredients,NEW.generic_name,NEW.dosage_form,NEW.route,NEW.manufacturer,NEW.currency)
 IS DISTINCT FROM (OLD.pack_size,OLD.unit_of_issue,OLD.ingredients,OLD.generic_name,OLD.dosage_form,OLD.route,OLD.manufacturer,OLD.currency)
 AND EXISTS (SELECT 1 FROM pharmacy_stock_batches WHERE tenant_id=OLD.tenant_id AND outlet_id=OLD.outlet_id AND product_id=OLD.id) THEN
  RAISE EXCEPTION 'stocked product identity requires a new product' USING ERRCODE='23514',CONSTRAINT='pharmacy_stock_product_identity';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_stock_product_identity_guard BEFORE UPDATE ON pharmacy_catalogue_products
 FOR EACH ROW EXECUTE FUNCTION pharmacy_stock_product_identity_guard();
