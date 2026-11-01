SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE FUNCTION workforce_hr_intake_context(row_tenant uuid,row_hospital uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(hms_tenant_visible(row_tenant)
 AND row_tenant=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND row_hospital=NULLIF(current_setting('app.workforce_hospital',true),'')::uuid
 AND current_setting('app.workforce_hr_intake',true)='authorized'
 AND NULLIF(current_setting('app.workforce_subject',true),'') IS NOT NULL
 AND NULLIF(current_setting('app.workforce_hr_intake_actor',true),'') IS NOT NULL
 AND NULLIF(current_setting('app.workforce_hr_intake_link',true),'') IS NOT NULL,false)
$$;

CREATE OR REPLACE FUNCTION workforce_enrollment_allowed(row_tenant uuid,row_hospital uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(current_setting('app.workforce_enrollment',true)='authorized',false)
 AND (workforce_staff_allowed(row_tenant,row_hospital,true)
      OR workforce_hr_intake_context(row_tenant,row_hospital))
$$;

DO $migration$
DECLARE prior record;
BEGIN
 SELECT p.polname,pg_get_expr(p.polqual,p.polrelid) AS read_expression,
 pg_get_expr(p.polwithcheck,p.polrelid) AS write_expression INTO STRICT prior
 FROM pg_policy p WHERE p.polrelid='public.workforce_staff'::regclass AND p.polpermissive;
 EXECUTE format('ALTER POLICY %I ON workforce_staff USING ((%s) OR workforce_hr_intake_context(tenant_id,hospital_id)) WITH CHECK ((%s) OR workforce_hr_intake_context(tenant_id,hospital_id))',
 prior.polname,prior.read_expression,prior.write_expression);
END $migration$;

DO $migration$
DECLARE table_name text; prior record;
BEGIN
 FOREACH table_name IN ARRAY ARRAY['workforce_hr_masters','workforce_hr_master_revisions'] LOOP
  SELECT p.polname,pg_get_expr(p.polqual,p.polrelid) AS read_expression INTO STRICT prior
  FROM pg_policy p WHERE p.polrelid=format('public.%I',table_name)::regclass AND p.polpermissive;
  EXECUTE format('ALTER POLICY %I ON %I USING ((%s) OR workforce_hr_intake_context(tenant_id,hospital_id))',
   prior.polname,table_name,prior.read_expression);
  EXECUTE format('CREATE POLICY hr_intake_master_delete_guard ON %I AS RESTRICTIVE FOR DELETE USING (%s)',
   table_name,prior.read_expression);
 END LOOP;
END $migration$;
