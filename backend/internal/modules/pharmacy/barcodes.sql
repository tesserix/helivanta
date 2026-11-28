SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE TABLE pharmacy_product_barcodes (
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 tenant_id uuid NOT NULL,
 outlet_id uuid NOT NULL,
 gtin text NOT NULL CHECK (gtin ~ '^[0-9]{14}$'),
 product_id uuid NOT NULL,
 actor_subject text NOT NULL CHECK (length(btrim(actor_subject)) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,outlet_id,gtin),
 UNIQUE (tenant_id,id),
 FOREIGN KEY (tenant_id,outlet_id,product_id) REFERENCES pharmacy_catalogue_products(tenant_id,outlet_id,id)
);
ALTER TABLE pharmacy_product_barcodes ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_product_barcodes FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_product_barcodes
 USING (hms_tenant_visible(tenant_id) AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK (pharmacy_outlet_allowed(tenant_id,outlet_id,true));
CREATE TRIGGER pharmacy_product_barcode_audit AFTER INSERT ON pharmacy_product_barcodes
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_product_barcode_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_product_barcodes
 FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();
