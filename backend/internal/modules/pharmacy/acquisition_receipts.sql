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
DO $$
DECLARE target text; prior text;
BEGIN
 FOREACH target IN ARRAY ARRAY['pharmacy_supplier_receipts','pharmacy_supplier_receiving_heads','pharmacy_supplier_receiving_corrections'] LOOP
  SELECT pg_get_expr(polqual,polrelid) INTO STRICT prior FROM pg_policy WHERE polrelid=target::regclass AND polname='tenant_isolation';
  EXECUTE format('ALTER POLICY tenant_isolation ON %I USING(hms_tenant_visible(tenant_id))',target);
  EXECUTE format('CREATE POLICY financial_acquisition_read ON %I AS RESTRICTIVE FOR SELECT USING((%s)OR pharmacy_acquisition_receipts_read(tenant_id,outlet_id))',target,prior);
  EXECUTE format('CREATE POLICY receiving_update_scope ON %I AS RESTRICTIVE FOR UPDATE USING(%s) WITH CHECK(true)',target,prior);
  EXECUTE format('CREATE POLICY receiving_delete_scope ON %I AS RESTRICTIVE FOR DELETE USING(%s)',target,prior);
 END LOOP;
END $$;
