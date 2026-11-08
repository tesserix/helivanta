SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
ALTER TABLE supply_supplier_discrepancy_responses ADD COLUMN source_event_id uuid NOT NULL DEFAULT gen_random_uuid();
CREATE INDEX ON supply_supplier_discrepancy_responses(tenant_id,order_id,shipment_id,created_at DESC,id DESC);
CREATE TABLE supply_supplier_received_responses(
 tenant_id uuid NOT NULL, id uuid NOT NULL, order_id uuid NOT NULL,
 shipment_id uuid NOT NULL, source_tenant_id uuid NOT NULL,
 summary_version bigint NOT NULL CHECK(summary_version>0),
 body jsonb NOT NULL, source_event_id uuid NOT NULL, created_at timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,id),UNIQUE(tenant_id,shipment_id,summary_version),
 FOREIGN KEY(tenant_id,order_id)REFERENCES supply_purchase_orders(tenant_id,id),
 FOREIGN KEY(tenant_id,shipment_id)REFERENCES supply_supplier_shipments(tenant_id,id),
 CHECK(body->>'id'=id::text AND body->>'buyer_tenant_id'=tenant_id::text AND body->>'owner_tenant_id'=source_tenant_id::text AND body->>'order_id'=order_id::text AND body->>'shipment_id'=shipment_id::text AND (body->>'summary_version')::bigint=summary_version)
);
ALTER TABLE supply_supplier_received_responses ENABLE ROW LEVEL SECURITY;
ALTER TABLE supply_supplier_received_responses FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON supply_supplier_received_responses USING(
 hms_tenant_visible(tenant_id)AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND(
 current_setting('app.supply_order_mode',true)='exchange'AND source_tenant_id::text=current_setting('app.supply_order_origin',true)AND order_id=NULLIF(current_setting('app.supply_order_id',true),'')::uuid AND current_setting('app.actor_subject',true)='system:supplier-responses'
 OR current_setting('app.supply_order_mode',true)IN('buyer_read','buyer_write')AND EXISTS(SELECT 1 FROM supply_purchase_orders p WHERE p.tenant_id=supply_supplier_received_responses.tenant_id AND p.id=supply_supplier_received_responses.order_id AND p.side='buyer')
 ))WITH CHECK(
 tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND current_setting('app.supply_order_mode',true)='exchange'AND source_tenant_id::text=current_setting('app.supply_order_origin',true)
 AND order_id=NULLIF(current_setting('app.supply_order_id',true),'')::uuid AND current_setting('app.actor_subject',true)='system:supplier-responses'
);
CREATE INDEX ON supply_supplier_received_responses(tenant_id,order_id,shipment_id,created_at DESC,id DESC);
CREATE TRIGGER audit_row AFTER INSERT OR UPDATE OR DELETE ON supply_supplier_received_responses FOR EACH ROW EXECUTE FUNCTION audit_record_change();
