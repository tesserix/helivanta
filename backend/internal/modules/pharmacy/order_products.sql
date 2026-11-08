SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE FUNCTION pharmacy_order_catalogue_scope(t uuid,o uuid)RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(t=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND o=NULLIF(current_setting('app.pharmacy_order_outlet',true),'')::uuid
 AND current_setting('app.pharmacy_order_catalogue',true)='read'
 AND NULLIF(current_setting('app.pharmacy_order_subject',true),'')IS NOT NULL
 AND current_setting('app.pharmacy_order_subject',true)=current_setting('app.actor_subject',true),false)
$$;
DO $$DECLARE prior text;BEGIN
 SELECT pg_get_expr(polqual,polrelid)INTO STRICT prior FROM pg_policy WHERE polrelid='pharmacy_catalogue_products'::regclass AND polname='tenant_isolation';
 EXECUTE format('ALTER POLICY tenant_isolation ON pharmacy_catalogue_products USING((%s)OR(hms_tenant_visible(tenant_id)AND pharmacy_order_catalogue_scope(tenant_id,outlet_id)))',prior);
END $$;
