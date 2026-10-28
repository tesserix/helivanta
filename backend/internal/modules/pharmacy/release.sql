SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Sellable stock (#1386 A3). A batch carries its printed MRP, set at
-- receipt and never changed; batches received before this have none and
-- can never be released. A location is quarantine or sellable for life.
-- Stock becomes sellable only by a release: minus from quarantine, plus
-- into sellable, in one transaction, after the checks in release.go.
ALTER TABLE pharmacy_stock_batches
 ADD COLUMN mrp_minor bigint CHECK (mrp_minor IS NULL OR mrp_minor BETWEEN 1 AND 1000000000000),
 ADD COLUMN purchase_cost_minor bigint CHECK (purchase_cost_minor IS NULL OR purchase_cost_minor BETWEEN 0 AND 1000000000000);

ALTER TABLE pharmacy_stock_locations DROP CONSTRAINT pharmacy_stock_locations_stock_status_check;
ALTER TABLE pharmacy_stock_locations ADD CONSTRAINT pharmacy_stock_locations_stock_status_check
 CHECK (stock_status IN ('quarantine','sellable')) NOT VALID;
ALTER TABLE pharmacy_stock_locations VALIDATE CONSTRAINT pharmacy_stock_locations_stock_status_check;

ALTER TABLE pharmacy_stock_movements DROP CONSTRAINT pharmacy_stock_movements_kind_check;
ALTER TABLE pharmacy_stock_movements ADD CONSTRAINT pharmacy_stock_movements_kind_check
 CHECK (kind IN ('receipt','correction','release')) NOT VALID;
ALTER TABLE pharmacy_stock_movements VALIDATE CONSTRAINT pharmacy_stock_movements_kind_check;

-- A receipt or correction lands in quarantine only: nothing reaches a
-- sellable location without a release (spec A4).
CREATE FUNCTION pharmacy_stock_movement_location_guard() RETURNS trigger LANGUAGE plpgsql AS $$
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
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_stock_movement_location_guard BEFORE INSERT ON pharmacy_stock_movements
 FOR EACH ROW EXECUTE FUNCTION pharmacy_stock_movement_location_guard();

CREATE TABLE pharmacy_stock_releases (
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 tenant_id uuid NOT NULL,
 outlet_id uuid NOT NULL,
 batch_id uuid NOT NULL,
 from_location_id uuid NOT NULL,
 to_location_id uuid NOT NULL,
 quantity_units bigint NOT NULL CHECK (quantity_units BETWEEN 1 AND 1000000000),
 reason text NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 500),
 -- The class the product was released under, as declared then.
 regulatory_class text NOT NULL CHECK (regulatory_class IN ('none','G','H','H1')),
 schedule_declaration_id uuid NOT NULL,
 intent_sha256 text NOT NULL CHECK (intent_sha256 ~ '^[0-9a-f]{64}$'),
 actor_subject text NOT NULL CHECK (length(btrim(actor_subject)) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,outlet_id,id),
 CHECK (from_location_id <> to_location_id),
 FOREIGN KEY (tenant_id,outlet_id,batch_id) REFERENCES pharmacy_stock_batches (tenant_id,outlet_id,id),
 FOREIGN KEY (tenant_id,outlet_id,from_location_id) REFERENCES pharmacy_stock_locations (tenant_id,outlet_id,id),
 FOREIGN KEY (tenant_id,outlet_id,to_location_id) REFERENCES pharmacy_stock_locations (tenant_id,outlet_id,id),
 FOREIGN KEY (tenant_id,outlet_id,schedule_declaration_id) REFERENCES pharmacy_product_schedule_declarations (tenant_id,outlet_id,id)
);
ALTER TABLE pharmacy_stock_releases ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_stock_releases FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_stock_releases
 USING (hms_tenant_visible(tenant_id) AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK (pharmacy_outlet_allowed(tenant_id,outlet_id,true) AND actor_subject=NULLIF(current_setting('app.pharmacy_subject',true),''));
CREATE INDEX pharmacy_stock_release_page ON pharmacy_stock_releases (tenant_id,outlet_id,created_at DESC,id DESC);
CREATE TRIGGER pharmacy_stock_release_audit AFTER INSERT ON pharmacy_stock_releases
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_stock_release_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_stock_releases
 FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();
