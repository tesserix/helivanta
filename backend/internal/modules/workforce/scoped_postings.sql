SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE FUNCTION workforce_scoped_posting_context(t uuid,h uuid,s uuid,writing boolean)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(hms_tenant_visible(t)
 AND t=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND h=NULLIF(current_setting('app.workforce_hospital',true),'')::uuid
 AND s=NULLIF(current_setting('app.workforce_posting_staff',true),'')::uuid
 AND NULLIF(current_setting('app.workforce_subject',true),'') IS NOT NULL
 AND current_setting('app.workforce_posting_scoped',true) IN ('read','write')
 AND (NOT writing OR current_setting('app.workforce_posting_scoped',true)='write')
 AND NULLIF(current_setting('app.workforce_posting_actor',true),'')::uuid IS NOT NULL
 AND NULLIF(current_setting('app.workforce_posting_link',true),'')::uuid IS NOT NULL
 AND NULLIF(current_setting('app.workforce_posting_generation',true),'')::uuid IS NOT NULL
 AND NULLIF(current_setting('app.workforce_posting_assignment',true),'')::uuid IS NOT NULL
 AND NULLIF(current_setting('app.workforce_posting_assignment_version',true),'')::bigint>0
 AND NULLIF(current_setting('app.workforce_posting_scope_epoch',true),'')::bigint>0
 AND NULLIF(current_setting('app.workforce_posting_membership_epoch',true),'')::bigint>0,false)
$$;

CREATE OR REPLACE FUNCTION workforce_posting_draft_context(t uuid,h uuid,s uuid,writing boolean)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(hms_tenant_visible(t) AND t=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND h=NULLIF(current_setting('app.workforce_hospital',true),'')::uuid
 AND s=NULLIF(current_setting('app.workforce_posting_staff',true),'')::uuid
 AND current_setting('app.workforce_posting',true) IN ('read','write')
 AND (NOT writing OR current_setting('app.workforce_posting',true)='write')
 AND (workforce_staff_allowed(t,h,writing) OR workforce_scoped_posting_context(t,h,s,writing)),false)
$$;


DO $migration$
DECLARE prior record;
BEGIN
 SELECT p.polname,pg_get_expr(p.polqual,p.polrelid) AS read_expression
 INTO STRICT prior FROM pg_policy p
 WHERE p.polrelid='public.workforce_staff'::regclass AND p.polpermissive;
 -- Preserve every earlier owner purpose, including reviewed identity and HR intake.
 EXECUTE format($policy$ALTER POLICY %I ON workforce_staff USING ((%s)
 OR workforce_scoped_posting_context(tenant_id,hospital_id,id,false)
 OR (current_setting('app.workforce_posting_directory',true)='read'
 AND workforce_scoped_posting_context(tenant_id,hospital_id,NULLIF(current_setting('app.workforce_posting_staff',true),'')::uuid,false)))$policy$,
 prior.polname,prior.read_expression);
 -- Row locks need UPDATE visibility; the unchanged WITH CHECK prevents scoped writes.
 EXECUTE format('CREATE POLICY posting_source_no_delete ON workforce_staff AS RESTRICTIVE FOR DELETE USING (%s)',prior.read_expression);
END $migration$;
