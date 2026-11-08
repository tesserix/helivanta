SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE TABLE pharmacy_supplier_receiving_releases(
 tenant_id uuid NOT NULL,id uuid NOT NULL,shipment_id uuid NOT NULL,receipt_id uuid NOT NULL,outlet_id uuid NOT NULL,
 batch_id uuid NOT NULL,from_location_id uuid NOT NULL,to_location_id uuid NOT NULL,
 quantity_units bigint NOT NULL CHECK(quantity_units BETWEEN 1 AND 1000000000),summary_version bigint NOT NULL CHECK(summary_version>0),
 input jsonb NOT NULL,body jsonb NOT NULL,actor_subject text NOT NULL,actor_staff_id uuid NOT NULL,created_at timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,id),
 FOREIGN KEY(tenant_id,receipt_id)REFERENCES pharmacy_supplier_receipts(tenant_id,id),
 FOREIGN KEY(tenant_id,outlet_id,id)REFERENCES pharmacy_stock_releases(tenant_id,outlet_id,id)DEFERRABLE INITIALLY DEFERRED,
 CHECK(body->>'id'IS NOT DISTINCT FROM id::text AND body->>'owner_tenant_id'IS NOT DISTINCT FROM tenant_id::text AND body->>'receipt_id'IS NOT DISTINCT FROM receipt_id::text AND body->>'shipment_id'IS NOT DISTINCT FROM shipment_id::text AND body->>'outlet_id'IS NOT DISTINCT FROM outlet_id::text AND body->>'batch_id'IS NOT DISTINCT FROM batch_id::text AND body->>'from_location_id'IS NOT DISTINCT FROM from_location_id::text AND body->>'actor_subject'IS NOT DISTINCT FROM actor_subject AND body->>'actor_staff_id'IS NOT DISTINCT FROM actor_staff_id::text AND body->'input'=input),
 CHECK(input->>'to_location_id'IS NOT DISTINCT FROM to_location_id::text AND input->>'quantity_units'IS NOT DISTINCT FROM quantity_units::text AND input->>'expected_summary_version'IS NOT DISTINCT FROM summary_version::text)
);
ALTER TABLE pharmacy_supplier_receiving_releases ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_supplier_receiving_releases FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_supplier_receiving_releases USING(hms_tenant_visible(tenant_id)AND pharmacy_hospital_receiving_scope(tenant_id,outlet_id,false))WITH CHECK(
 tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND pharmacy_hospital_receiving_scope(tenant_id,outlet_id,true)AND current_setting('app.pharmacy_receiving_purpose',true)='review'
 AND actor_subject=current_setting('app.actor_subject',true)AND actor_staff_id=NULLIF(current_setting('app.pharmacy_receiving_staff',true),'')::uuid
 AND EXISTS(SELECT 1 FROM pharmacy_supplier_receipts r JOIN pharmacy_supplier_receiving_heads h ON h.tenant_id=r.tenant_id AND h.shipment_id=r.shipment_id JOIN pharmacy_supplier_receiving_clearances c ON c.tenant_id=r.tenant_id AND c.receipt_id=r.id AND c.summary_version=h.version
 WHERE r.tenant_id=pharmacy_supplier_receiving_releases.tenant_id AND r.id=pharmacy_supplier_receiving_releases.receipt_id AND r.shipment_id=pharmacy_supplier_receiving_releases.shipment_id AND r.outlet_id=pharmacy_supplier_receiving_releases.outlet_id AND r.destination_batch_id=pharmacy_supplier_receiving_releases.batch_id AND r.quarantine_location_id=pharmacy_supplier_receiving_releases.from_location_id AND h.version=pharmacy_supplier_receiving_releases.summary_version AND h.closed AND r.actor_staff_id<>pharmacy_supplier_receiving_releases.actor_staff_id)
);
CREATE INDEX ON pharmacy_supplier_receiving_releases(tenant_id,receipt_id,created_at DESC,id DESC);
CREATE TRIGGER pharmacy_supplier_receiving_release_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_supplier_receiving_releases FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_supplier_evidence_immutable();
CREATE TRIGGER pharmacy_supplier_receiving_release_audit AFTER INSERT ON pharmacy_supplier_receiving_releases FOR EACH ROW EXECUTE FUNCTION audit_record_change();
DO $$DECLARE tab text;prior text;BEGIN
 FOREACH tab IN ARRAY ARRAY['pharmacy_stock_releases','pharmacy_product_tax_classifications','pharmacy_product_schedule_declarations','pharmacy_product_schedule_reviews','pharmacy_batch_safety_incidents']LOOP
 SELECT pg_get_expr(polqual,polrelid)INTO STRICT prior FROM pg_policy WHERE polrelid=tab::regclass AND polname='tenant_isolation';
 EXECUTE format('ALTER POLICY tenant_isolation ON %I USING((%s)OR(pharmacy_hospital_receiving_scope(tenant_id,outlet_id,false)AND current_setting(''app.pharmacy_receiving_purpose'',true)=''review''))',tab,prior);
 END LOOP;
 SELECT pg_get_expr(polwithcheck,polrelid)INTO STRICT prior FROM pg_policy WHERE polrelid='pharmacy_stock_releases'::regclass AND polname='tenant_isolation';
 EXECUTE format($policy$ALTER POLICY tenant_isolation ON pharmacy_stock_releases WITH CHECK((%s)OR(pharmacy_hospital_receiving_scope(tenant_id,outlet_id,true)AND current_setting('app.pharmacy_receiving_purpose',true)='review'AND actor_subject=current_setting('app.actor_subject',true)AND EXISTS(SELECT 1 FROM pharmacy_supplier_receiving_releases r WHERE r.tenant_id=pharmacy_stock_releases.tenant_id AND r.outlet_id=pharmacy_stock_releases.outlet_id AND r.id=pharmacy_stock_releases.id AND r.batch_id=pharmacy_stock_releases.batch_id AND r.from_location_id=pharmacy_stock_releases.from_location_id AND r.to_location_id=pharmacy_stock_releases.to_location_id AND r.quantity_units=pharmacy_stock_releases.quantity_units AND r.input->>'reason'=pharmacy_stock_releases.reason)))$policy$,prior);
 SELECT pg_get_expr(polwithcheck,polrelid)INTO STRICT prior FROM pg_policy WHERE polrelid='pharmacy_stock_movements'::regclass AND polname='tenant_isolation';
 EXECUTE format($policy$ALTER POLICY tenant_isolation ON pharmacy_stock_movements WITH CHECK((%s)OR(pharmacy_hospital_receiving_scope(tenant_id,outlet_id,true)AND current_setting('app.pharmacy_receiving_purpose',true)='review'AND kind='release'AND actor_subject=current_setting('app.actor_subject',true)AND EXISTS(SELECT 1 FROM pharmacy_supplier_receiving_releases r WHERE r.tenant_id=pharmacy_stock_movements.tenant_id AND r.outlet_id=pharmacy_stock_movements.outlet_id AND r.batch_id=pharmacy_stock_movements.batch_id AND ((source_reference='release:'||r.id::text||':out'AND location_id=r.from_location_id AND quantity_units=-r.quantity_units)OR(source_reference='release:'||r.id::text||':in'AND location_id=r.to_location_id AND quantity_units=r.quantity_units)))))$policy$,prior);
END $$;
