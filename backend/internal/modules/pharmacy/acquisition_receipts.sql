SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';
CREATE FUNCTION pharmacy_acquisition_receipts_read(t uuid,o uuid) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(hms_tenant_visible(t)
 AND t=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND o=NULLIF(current_setting('app.pharmacy_acquisition_outlet',true),'')::uuid
 AND current_setting('app.pharmacy_acquisition_read',true)='authorized'
 AND current_setting('app.pharmacy_subject',true)=current_setting('app.actor_subject',true)
 AND pharmacy_outlet_allowed(t,o,false),false)
$$;
CREATE POLICY financial_acquisition_read ON pharmacy_supplier_receipts FOR SELECT USING(pharmacy_acquisition_receipts_read(tenant_id,outlet_id));
CREATE POLICY financial_acquisition_read ON pharmacy_supplier_receiving_heads FOR SELECT USING(pharmacy_acquisition_receipts_read(tenant_id,outlet_id));
CREATE POLICY financial_acquisition_read ON pharmacy_supplier_receiving_corrections FOR SELECT USING(pharmacy_acquisition_receipts_read(tenant_id,outlet_id));
