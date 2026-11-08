CREATE TABLE supply_supplier_shipments(
 tenant_id uuid NOT NULL,id uuid NOT NULL,order_id uuid NOT NULL,hospital_id uuid NOT NULL,department_id uuid,pharmacy_id uuid NOT NULL,outlet_id uuid NOT NULL,source_tenant_id uuid NOT NULL,source_product text NOT NULL CHECK(source_product IN('supply','pharmacy')),body jsonb NOT NULL,actor_subject text NOT NULL,created_at timestamptz NOT NULL,updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 origin_tenant_id uuid GENERATED ALWAYS AS(source_tenant_id)STORED NOT NULL,origin_record_id uuid GENERATED ALWAYS AS(id)STORED NOT NULL,
 PRIMARY KEY(tenant_id,id),FOREIGN KEY(tenant_id,order_id)REFERENCES supply_purchase_orders(tenant_id,id)ON DELETE RESTRICT,
 CHECK(jsonb_typeof(body)='object'AND body->>'id'IS NOT DISTINCT FROM id::text AND body->>'order_id'IS NOT DISTINCT FROM order_id::text AND body->>'buyer_tenant_id'IS NOT DISTINCT FROM tenant_id::text AND body->>'source_tenant_id'IS NOT DISTINCT FROM source_tenant_id::text AND body->>'hospital_id'IS NOT DISTINCT FROM hospital_id::text AND body->>'buyer_organization_id'IS NOT DISTINCT FROM pharmacy_id::text AND body->>'buyer_location_id'IS NOT DISTINCT FROM outlet_id::text AND body#>>'{product,owner}'IS NOT DISTINCT FROM source_product)
);
ALTER TABLE supply_supplier_shipments ENABLE ROW LEVEL SECURITY;ALTER TABLE supply_supplier_shipments FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON supply_supplier_shipments USING(hms_tenant_visible(tenant_id)AND((current_setting('app.supply_order_mode',true)='exchange'AND order_id=NULLIF(current_setting('app.supply_order_id',true),'')::uuid)OR(current_setting('app.supply_order_mode',true)IN('buyer_read','buyer_write')AND hospital_id=NULLIF(current_setting('app.supply_order_hospital',true),'')::uuid AND department_id=NULLIF(current_setting('app.supply_order_department',true),'')::uuid AND outlet_id=NULLIF(current_setting('app.supply_order_outlet',true),'')::uuid)))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.supply_order_mode',true)='exchange'AND order_id=NULLIF(current_setting('app.supply_order_id',true),'')::uuid AND source_tenant_id::text=current_setting('app.supply_order_origin',true)AND actor_subject='system:supplier-shipments');
CREATE INDEX ON supply_supplier_shipments(tenant_id,hospital_id,department_id,outlet_id,created_at DESC,id DESC);
CREATE TRIGGER supply_supplier_shipments_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON supply_supplier_shipments FOR EACH STATEMENT EXECUTE FUNCTION supply_history_immutable();
CREATE TRIGGER supply_supplier_shipments_audit AFTER INSERT ON supply_supplier_shipments FOR EACH ROW EXECUTE FUNCTION audit_record_change();

CREATE TABLE supply_supplier_dispatched_shipments(
 tenant_id uuid NOT NULL,id uuid NOT NULL,order_id uuid NOT NULL,buyer_tenant_id uuid NOT NULL,body jsonb NOT NULL,created_at timestamptz NOT NULL,updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,id),FOREIGN KEY(tenant_id,order_id)REFERENCES supply_purchase_orders(tenant_id,id)ON DELETE RESTRICT,
 CHECK(body->>'id'IS NOT DISTINCT FROM id::text AND body->>'order_id'IS NOT DISTINCT FROM order_id::text AND body->>'source_tenant_id'IS NOT DISTINCT FROM tenant_id::text AND body->>'buyer_tenant_id'IS NOT DISTINCT FROM buyer_tenant_id::text)
);
ALTER TABLE supply_supplier_dispatched_shipments ENABLE ROW LEVEL SECURITY;ALTER TABLE supply_supplier_dispatched_shipments FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON supply_supplier_dispatched_shipments USING(hms_tenant_visible(tenant_id)AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.supply_order_mode',true)='exchange'AND order_id=NULLIF(current_setting('app.supply_order_id',true),'')::uuid AND buyer_tenant_id::text=current_setting('app.supply_order_origin',true))WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.supply_order_mode',true)='exchange'AND order_id=NULLIF(current_setting('app.supply_order_id',true),'')::uuid AND buyer_tenant_id::text=current_setting('app.supply_order_origin',true)AND current_setting('app.actor_subject',true)='system:supplier-shipments');
CREATE INDEX ON supply_supplier_dispatched_shipments(tenant_id,order_id,id);
CREATE TRIGGER supply_supplier_dispatched_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON supply_supplier_dispatched_shipments FOR EACH STATEMENT EXECUTE FUNCTION supply_history_immutable();
CREATE TRIGGER supply_supplier_dispatched_audit AFTER INSERT ON supply_supplier_dispatched_shipments FOR EACH ROW EXECUTE FUNCTION audit_record_change();

CREATE POLICY supplier_source_read ON supply_supplier_dispatched_shipments FOR SELECT USING(
 hms_tenant_visible(tenant_id)AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND current_setting('app.supply_order_mode',true)IN('seller_read','seller_write')
 AND body#>>'{product,owner}'=current_setting('app.supply_order_product',true)
 AND body->>'source_organization_id'=current_setting('app.supply_order_organization',true)
 AND body->>'source_location_id'=current_setting('app.supply_order_location',true)
);
CREATE INDEX ON supply_supplier_dispatched_shipments(tenant_id,order_id,created_at DESC,id DESC);
