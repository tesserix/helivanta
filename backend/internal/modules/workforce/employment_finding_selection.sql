SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE FUNCTION workforce_employment_finding_selection_context(row_tenant uuid,row_hospital uuid,row_staff uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT workforce_hr_intake_context(row_tenant,row_hospital)
 AND workforce_identity_source_context(row_tenant,row_hospital,row_staff)
 AND COALESCE(current_setting('app.workforce_employment_finding_selection',true)='authorized',false)
$$;
DO $migration$ DECLARE item record;prior record;BEGIN
 FOR item IN SELECT * FROM (VALUES
  ('workforce_person_reviews',$read$workforce_employment_finding_selection_context(tenant_id,hospital_id,staff_id)$read$),
  ('workforce_person_review_evidence',$read$workforce_employment_finding_selection_context(tenant_id,hospital_id,staff_id)
    AND review_id::text=current_setting('app.workforce_employment_person_review',true)$read$)
 ) AS scopes(table_name,read_context) LOOP
  SELECT p.polname,pg_get_expr(p.polqual,p.polrelid) AS read_expression,
   pg_get_expr(p.polwithcheck,p.polrelid) AS write_expression INTO STRICT prior
   FROM pg_policy p WHERE p.polrelid=('public.'||quote_ident(item.table_name))::regclass AND p.polpermissive;
  IF prior.write_expression IS NULL THEN RAISE EXCEPTION 'finding selection requires an explicit unchanged write policy';END IF;
  EXECUTE format('CREATE POLICY employment_finding_delete_guard ON %I AS RESTRICTIVE FOR DELETE USING (%s)',item.table_name,prior.read_expression);
  EXECUTE format('ALTER POLICY %I ON %I USING ((%s) OR (%s))',prior.polname,item.table_name,prior.read_expression,item.read_context);
 END LOOP;
END $migration$;
