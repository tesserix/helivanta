SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE FUNCTION pharmacy_operating_choice_context(t uuid,e uuid)RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(hms_tenant_visible(t)AND t=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND p->>'purpose'='operating_location_choices'AND p->>'capability'='iam.governance.operating.manage'
 AND p->>'tenant_id'=t::text AND p->>'employer_kind'='pharmacy'AND p->>'employer_id'=e::text
 AND p->>'actor_subject'=NULLIF(current_setting('app.actor_subject',true),'')
 AND(p->>'membership_epoch')::bigint BETWEEN 1 AND 9007199254740991
 AND(p->>'owner_count')::bigint BETWEEN 1 AND 1000
 AND(p->>'source_expires_at')::timestamptz>clock_timestamp()AND(p->>'idle_deadline')::timestamptz>clock_timestamp()
 AND(p->>'authenticated_at')::timestamptz<=clock_timestamp()
 AND(p-ARRAY['purpose','tenant_id','employer_kind','employer_id','actor_subject','capability','authority_kind','membership_epoch','owner_count','authenticated_at','idle_deadline','source_expires_at','owner_membership_id','source_grant_id','source_grant_version','template_key'])='{}'::jsonb
 AND((p->>'authority_kind'='owner'AND(p->>'owner_membership_id')::uuid IS NOT NULL AND(p->>'owner_membership_id')::uuid<>'00000000-0000-0000-0000-000000000000'::uuid
 AND p->>'source_grant_id'IS NULL AND p->>'source_grant_version'IS NULL AND p->>'template_key'IS NULL)
 OR(p->>'authority_kind'='grant'AND(p->>'source_grant_id')::uuid IS NOT NULL AND(p->>'source_grant_id')::uuid<>'00000000-0000-0000-0000-000000000000'::uuid
 AND(p->>'source_grant_version')::bigint BETWEEN 1 AND 9007199254740991 AND p->>'template_key'='iam.operating_access_reviewer.v1'AND p->>'owner_membership_id'IS NULL)),false)
 FROM(SELECT NULLIF(current_setting('app.pharmacy_operating_choices',true),'')::jsonb AS p)proof
$$;
DO $policies$
DECLARE target record;prior text;reading text;
BEGIN
 FOR target IN SELECT * FROM(VALUES
 ('pharmacy_organizations','pharmacy_operating_choice_context(tenant_id,id)AND ownership_type IN(''independent'',''chain'')'),
 ('pharmacy_outlets','pharmacy_operating_choice_context(tenant_id,pharmacy_id)AND country_code=''IN''')
 )targets(tab,expression)LOOP
  SELECT pg_get_expr(polqual,polrelid)INTO STRICT prior FROM pg_policy WHERE polrelid=('public.'||quote_ident(target.tab))::regclass AND polname='tenant_isolation'AND polpermissive AND polcmd='*'AND polwithcheck IS NOT NULL;
  EXECUTE format('ALTER POLICY tenant_isolation ON %I USING(hms_tenant_visible(tenant_id)AND((%s)OR(%s)))',target.tab,prior,target.expression);
 END LOOP;
 FOR target IN SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname=current_schema()AND c.relkind IN('r','p')AND starts_with(c.relname,'pharmacy_')
 AND EXISTS(SELECT 1 FROM pg_attribute a WHERE a.attrelid=c.oid AND a.attname='tenant_id'AND NOT a.attisdropped)LOOP
  reading:='false';
  IF target.relname='pharmacy_organizations'THEN reading:='pharmacy_operating_choice_context(tenant_id,id)AND ownership_type IN(''independent'',''chain'')';
  ELSIF target.relname='pharmacy_outlets'THEN reading:='pharmacy_operating_choice_context(tenant_id,pharmacy_id)AND country_code=''IN''';END IF;
  EXECUTE format('CREATE POLICY operating_choice_purpose ON %I AS RESTRICTIVE USING(hms_tenant_visible(tenant_id)AND(COALESCE(current_setting(''app.pharmacy_operating_choices'',true),'''')=''''OR(%s)))WITH CHECK(tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND COALESCE(current_setting(''app.pharmacy_operating_choices'',true),'''')='''')',target.relname,reading);
  EXECUTE format('CREATE POLICY operating_choice_delete ON %I AS RESTRICTIVE FOR DELETE USING(hms_tenant_visible(tenant_id)AND COALESCE(current_setting(''app.pharmacy_operating_choices'',true),'''')='''')',target.relname);
 END LOOP;
END $policies$;
DO $integrity$
DECLARE policies text;functions text;policy_count bigint;
BEGIN
 SELECT string_agg(format('(%L::regclass,%L,%L)',p.polrelid::regclass::text,p.polname,md5(p.polcmd::text||p.polpermissive::text||p.polroles::text||COALESCE(pg_get_expr(p.polqual,p.polrelid),'')||COALESCE(pg_get_expr(p.polwithcheck,p.polrelid),''))),','),count(*)INTO policies,policy_count
 FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname=current_schema()AND(c.relname IN('pharmacy_organizations','pharmacy_outlets')OR starts_with(c.relname,'pharmacy_')AND p.polname IN('operating_choice_purpose','operating_choice_delete'));
 SELECT string_agg(format('(%L,%L)',p.oid::regprocedure::text,md5(pg_get_functiondef(p.oid))),',')INTO functions FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
 WHERE n.nspname=current_schema()AND p.proname='pharmacy_operating_choice_context';
 EXECUTE format($definition$CREATE FUNCTION pharmacy_operating_choice_integrity()RETURNS boolean LANGUAGE sql STABLE AS $body$
 SELECT NOT EXISTS(SELECT 1 FROM(VALUES %s)expected(relation,name,digest)LEFT JOIN pg_policy p ON p.polrelid=expected.relation AND p.polname=expected.name
 WHERE p.oid IS NULL OR md5(p.polcmd::text||p.polpermissive::text||p.polroles::text||COALESCE(pg_get_expr(p.polqual,p.polrelid),'')||COALESCE(pg_get_expr(p.polwithcheck,p.polrelid),''))<>expected.digest)
 AND(SELECT count(*)FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema()AND(c.relname IN('pharmacy_organizations','pharmacy_outlets')OR starts_with(c.relname,'pharmacy_')AND p.polname IN('operating_choice_purpose','operating_choice_delete')))=%s
 AND NOT EXISTS(SELECT 1 FROM(VALUES %s)expected(name,digest)LEFT JOIN pg_proc p ON p.oid=to_regprocedure(expected.name)WHERE p.oid IS NULL OR md5(pg_get_functiondef(p.oid))<>expected.digest)
 $body$ $definition$,policies,policy_count,functions);
END $integrity$;
CREATE FUNCTION pharmacy_operating_choice_support()RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT pharmacy_operating_choice_integrity()AND NOT EXISTS(
 SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema()AND c.relkind IN('r','p')AND starts_with(c.relname,'pharmacy_')
 AND EXISTS(SELECT 1 FROM pg_attribute a WHERE a.attrelid=c.oid AND a.attname='tenant_id'AND NOT a.attisdropped)
 AND(NOT c.relrowsecurity OR NOT c.relforcerowsecurity OR has_table_privilege(current_user,c.oid,'TRUNCATE')
 OR NOT EXISTS(SELECT 1 FROM pg_policy p WHERE p.polrelid=c.oid AND p.polname='operating_choice_purpose'AND NOT p.polpermissive AND p.polcmd='*'AND p.polqual IS NOT NULL AND p.polwithcheck IS NOT NULL)
 OR NOT EXISTS(SELECT 1 FROM pg_policy p WHERE p.polrelid=c.oid AND p.polname='operating_choice_delete'AND NOT p.polpermissive AND p.polcmd='d'AND p.polqual IS NOT NULL)))
 AND EXISTS(SELECT 1 FROM pg_index WHERE indexrelid=to_regclass('public.pharmacy_outlet_page')AND indrelid='public.pharmacy_outlets'::regclass AND indisvalid AND indisready)
$$;
