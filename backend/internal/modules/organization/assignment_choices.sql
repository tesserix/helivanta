SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE FUNCTION organization_assignment_selection_context(t uuid,h uuid)RETURNS boolean LANGUAGE plpgsql STABLE AS $$
DECLARE p jsonb;
BEGIN
 IF COALESCE(current_setting('app.organization_assignment_selection',true),'')='' THEN RETURN false;END IF;
 p:=current_setting('app.organization_assignment_selection')::jsonb;
 RETURN COALESCE(hms_tenant_visible(t)AND t=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND p->>'purpose'='assignment_department_choices'AND p->>'capability'='iam.governance.assignment.manage'
 AND(p->>'tenant_id')::uuid=t AND(p->>'hospital_id')::uuid=h
 AND p->>'actor_subject'=current_setting('app.actor_subject',true)
 AND length(p->>'actor_subject')BETWEEN 1 AND 200
 AND(p->>'membership_epoch')::bigint BETWEEN 1 AND 9007199254740991
 AND(p->>'owner_count')::bigint BETWEEN 1 AND 1000
 AND(p->>'authenticated_at')::timestamptz<=clock_timestamp()
 AND(p->>'idle_deadline')::timestamptz>clock_timestamp()
 AND(p->>'source_expires_at')::timestamptz>clock_timestamp()
 AND((p->>'authority_kind'='owner'AND(p->>'owner_membership_id')::uuid<>'00000000-0000-0000-0000-000000000000'::uuid
 AND p->>'source_grant_id'IS NULL AND p->>'source_grant_version'IS NULL AND p->>'template_key'IS NULL)
 OR(p->>'authority_kind'='grant'AND p->>'owner_membership_id'IS NULL
 AND substring(p->>'source_grant_id',15,1)='7'AND substring(p->>'source_grant_id',20,1)IN('8','9','a','b')
 AND(p->>'source_grant_version')::bigint BETWEEN 1 AND 9007199254740991
 AND p->>'template_key'='iam.scoped_access_reviewer.v1')),false);
END $$;

DO $policies$
DECLARE item record;
BEGIN
 FOR item IN SELECT * FROM(VALUES
 ('organization_hospital_requests','false'),
 ('organization_hospital_registrations','organization_assignment_selection_context(tenant_id,id)AND status=''verified'''),
 ('organization_departments','organization_assignment_selection_context(tenant_id,hospital_id)AND status=''active'''),
 ('organization_department_revisions','false'))AS target(relation,selection)LOOP
  EXECUTE format('CREATE POLICY assignment_selection_purpose ON %I AS RESTRICTIVE USING(hms_tenant_visible(tenant_id)AND(COALESCE(current_setting(''app.organization_assignment_selection'',true),'''')=''''OR(%s)))WITH CHECK(tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND COALESCE(current_setting(''app.organization_assignment_selection'',true),'''')='''')',item.relation,item.selection);
  EXECUTE format('CREATE POLICY assignment_selection_delete ON %I AS RESTRICTIVE FOR DELETE USING(hms_tenant_visible(tenant_id)AND COALESCE(current_setting(''app.organization_assignment_selection'',true),'''')='''')',item.relation);
 END LOOP;
END $policies$;

DO $integrity$
DECLARE policies text;functions text;policy_count bigint;
BEGIN
 SELECT string_agg(format('(%L::regclass,%L,%L)',p.polrelid::regclass::text,p.polname,md5(p.polcmd::text||p.polpermissive::text||p.polroles::text||COALESCE(pg_get_expr(p.polqual,p.polrelid),'')||COALESCE(pg_get_expr(p.polwithcheck,p.polrelid),''))),','),count(*)INTO policies,policy_count
 FROM pg_policy p WHERE p.polrelid IN('organization_hospital_requests'::regclass,'organization_hospital_registrations'::regclass,'organization_departments'::regclass,'organization_department_revisions'::regclass);
 SELECT string_agg(format('(%L,%L)',p.oid::regprocedure::text,md5(pg_get_functiondef(p.oid))),',')INTO functions FROM pg_proc p
 WHERE p.oid='organization_assignment_selection_context(uuid,uuid)'::regprocedure;
 EXECUTE format($definition$CREATE FUNCTION organization_assignment_selection_integrity()RETURNS boolean LANGUAGE sql STABLE AS $body$
 SELECT NOT EXISTS(SELECT 1 FROM(VALUES %s)expected(relation,name,digest)LEFT JOIN pg_policy p ON p.polrelid=expected.relation AND p.polname=expected.name
 WHERE p.oid IS NULL OR md5(p.polcmd::text||p.polpermissive::text||p.polroles::text||COALESCE(pg_get_expr(p.polqual,p.polrelid),'')||COALESCE(pg_get_expr(p.polwithcheck,p.polrelid),''))<>expected.digest)
 AND(SELECT count(*)FROM pg_policy WHERE polrelid IN('organization_hospital_requests'::regclass,'organization_hospital_registrations'::regclass,'organization_departments'::regclass,'organization_department_revisions'::regclass))=%s
 AND NOT EXISTS(SELECT 1 FROM(VALUES %s)expected(name,digest)LEFT JOIN pg_proc p ON p.oid=to_regprocedure(expected.name)WHERE p.oid IS NULL OR md5(pg_get_functiondef(p.oid))<>expected.digest)
 $body$ $definition$,policies,policy_count,functions);
END $integrity$;

CREATE FUNCTION organization_assignment_selection_support()RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT organization_assignment_selection_integrity()AND NOT EXISTS(
 SELECT 1 FROM pg_class c WHERE c.oid IN('organization_hospital_requests'::regclass,'organization_hospital_registrations'::regclass,'organization_departments'::regclass,'organization_department_revisions'::regclass)
 AND(NOT c.relrowsecurity OR NOT c.relforcerowsecurity OR has_table_privilege(current_user,c.oid,'TRUNCATE')))
 AND EXISTS(SELECT 1 FROM pg_index WHERE indexrelid=to_regclass('public.organization_department_page')AND indrelid='organization_departments'::regclass AND indisvalid AND indisready)
$$;
