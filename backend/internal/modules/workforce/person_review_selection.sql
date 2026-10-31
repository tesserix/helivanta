SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE FUNCTION workforce_person_evidence_selection_context(row_tenant uuid,row_hospital uuid,row_staff uuid,row_version bigint)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(workforce_person_review_context(row_tenant,row_hospital,row_staff)
 AND current_setting('app.workforce_person_review',true)='manage'
 AND current_setting('app.workforce_person_evidence_selection',true)='authorized'
 AND row_version::text=current_setting('app.workforce_person_evidence_version',true),false)
$$;

DO $migration$
DECLARE prior record;
BEGIN
 SELECT p.polname,pg_get_expr(p.polqual,p.polrelid) AS read_expression INTO STRICT prior
 FROM pg_policy p WHERE p.polrelid='public.workforce_documents'::regclass AND p.polpermissive;
 EXECUTE format('CREATE POLICY person_selection_delete_guard ON workforce_documents AS RESTRICTIVE FOR DELETE USING (%s)',prior.read_expression);
 EXECUTE format('ALTER POLICY %I ON workforce_documents USING ((%s) OR
 (workforce_person_evidence_selection_context(tenant_id,hospital_id,staff_id,record_version)
 AND record_kind=''enrollment'' AND record_id=staff_id AND category=''identity_evidence''
 AND status=''available'' AND content_type IN (''image/png'',''image/jpeg'')))',prior.polname,prior.read_expression);
END $migration$;
