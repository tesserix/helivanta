SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE FUNCTION workforce_employment_browse_context(row_tenant uuid,row_hospital uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(hms_tenant_visible(row_tenant)
 AND row_tenant=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND row_hospital=NULLIF(current_setting('app.workforce_hospital',true),'')::uuid
 AND current_setting('app.workforce_employment_browse',true)='inbox'
 AND NULLIF(current_setting('app.workforce_subject',true),'') IS NOT NULL
 AND NULLIF(current_setting('app.workforce_employment_browse_actor',true),'') IS NOT NULL
 AND NULLIF(current_setting('app.workforce_employment_browse_link',true),'') IS NOT NULL,false)
$$;

DO $migration$
DECLARE item record; prior record;
BEGIN
 FOR item IN SELECT * FROM (VALUES
  ('workforce_employment_reviews',$scope$workforce_employment_browse_context(tenant_id,hospital_id)$scope$),
  ('workforce_staff',$scope$workforce_employment_browse_context(tenant_id,hospital_id)
   AND EXISTS(SELECT 1 FROM workforce_employment_reviews r WHERE r.tenant_id=workforce_staff.tenant_id
    AND r.hospital_id=workforce_staff.hospital_id AND r.staff_id=workforce_staff.id)$scope$)
 ) AS scopes(table_name,read_context) LOOP
  SELECT p.polname,pg_get_expr(p.polqual,p.polrelid) AS read_expression,
   pg_get_expr(p.polwithcheck,p.polrelid) AS write_expression INTO STRICT prior
   FROM pg_policy p WHERE p.polrelid=('public.'||quote_ident(item.table_name))::regclass AND p.polpermissive;
  IF prior.write_expression IS NULL THEN
   RAISE EXCEPTION 'employment browse requires an explicit unchanged write policy';
  END IF;
  EXECUTE format('CREATE POLICY employment_browse_delete_guard ON %I AS RESTRICTIVE FOR DELETE USING (%s)',item.table_name,prior.read_expression);
  EXECUTE format('ALTER POLICY %I ON %I USING ((%s) OR (%s))',prior.polname,item.table_name,prior.read_expression,item.read_context);
 END LOOP;
END $migration$;
