SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Only this operational read can span outlets. Every row still requires a
-- current explicit grant, the requested pharmacy, and the exact tenant.
-- Do not extend pharmacy_outlet_allowed: prescriptions and all writes retain
-- their single-outlet guard. No SECURITY DEFINER or bypass role is involved.
CREATE FUNCTION pharmacy_stock_overview_allowed(row_tenant uuid, row_outlet uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT hms_tenant_visible(row_tenant)
 AND row_tenant = NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND EXISTS (
  SELECT 1 FROM pharmacy_outlet_access a
  JOIN pharmacy_outlets o ON o.tenant_id=a.tenant_id AND o.id=a.outlet_id
  WHERE a.tenant_id=row_tenant AND a.outlet_id=row_outlet
   AND a.subject=NULLIF(current_setting('app.pharmacy_subject',true),'')
   AND o.pharmacy_id=NULLIF(current_setting('app.pharmacy_stock_overview',true),'')::uuid
 )
$$;
-- Keep a single permissive policy per table. WITH CHECK stays unchanged and
-- requires the existing single-outlet editor scope for every write.
ALTER POLICY tenant_isolation ON pharmacy_stock_movements
 USING (hms_tenant_visible(tenant_id) AND (pharmacy_outlet_allowed(tenant_id,outlet_id,false) OR pharmacy_stock_overview_allowed(tenant_id,outlet_id)));
ALTER POLICY tenant_isolation ON pharmacy_stock_batches
 USING (hms_tenant_visible(tenant_id) AND (pharmacy_outlet_allowed(tenant_id,outlet_id,false) OR pharmacy_stock_overview_allowed(tenant_id,outlet_id)));
ALTER POLICY tenant_isolation ON pharmacy_stock_locations
 USING (hms_tenant_visible(tenant_id) AND (pharmacy_outlet_allowed(tenant_id,outlet_id,false) OR pharmacy_stock_overview_allowed(tenant_id,outlet_id)));
ALTER POLICY tenant_isolation ON pharmacy_catalogue_products
 USING (hms_tenant_visible(tenant_id) AND (pharmacy_outlet_allowed(tenant_id,outlet_id,false) OR pharmacy_stock_overview_allowed(tenant_id,outlet_id)));
