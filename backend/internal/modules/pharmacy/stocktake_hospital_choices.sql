SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
DO $$BEGIN
 IF pharmacy_stocktake_hospital_integrity()IS DISTINCT FROM true OR pharmacy_operating_choice_support()IS DISTINCT FROM true THEN
 RAISE EXCEPTION 'hospital outlet choices require unchanged owner authority';
 END IF;
END $$;
CREATE FUNCTION pharmacy_stocktake_hospital_choices_context(t uuid,h uuid,d uuid)RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(hms_tenant_visible(t)AND t=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND p->>'purpose'='hospital_stocktake_outlet_choices'AND p->>'permission'='pharmacy.stocktake.read'
 AND p->>'tenant_id'=t::text AND p->>'hospital_id'=h::text AND p->>'department_id'=d::text
 AND p->>'actor_subject'=NULLIF(current_setting('app.actor_subject',true),'')
 AND(p->>'idle_deadline')::timestamptz>clock_timestamp()AND(p->>'authenticated_at')::timestamptz<=clock_timestamp()
 AND(p#>>'{actor,StaffID}')::uuid<>'00000000-0000-0000-0000-000000000000'::uuid
 AND(p#>>'{actor,LinkID}')::uuid<>'00000000-0000-0000-0000-000000000000'::uuid AND(p#>>'{actor,LinkVersion}')::bigint BETWEEN 2 AND 9007199254740991
 AND(p#>>'{actor,GenerationID}')::uuid<>'00000000-0000-0000-0000-000000000000'::uuid AND(p#>>'{actor,EmploymentEpoch}')::bigint BETWEEN 1 AND 9007199254740991
 AND p#>>'{operation,ScopeKind}'='department'AND p#>>'{source,ScopeKind}'='department'
 AND(p#>>'{operation,AssignmentID}')::uuid<>'00000000-0000-0000-0000-000000000000'::uuid AND(p#>>'{source,AssignmentID}')::uuid<>'00000000-0000-0000-0000-000000000000'::uuid
 AND(p#>>'{operation,DepartmentID}')::uuid<>'00000000-0000-0000-0000-000000000000'::uuid AND(p#>>'{source,DepartmentID}')::uuid<>'00000000-0000-0000-0000-000000000000'::uuid
 AND(p#>>'{operation,Version}')::bigint BETWEEN 1 AND 9007199254740991 AND(p#>>'{source,Version}')::bigint BETWEEN 1 AND 9007199254740991
 AND(p#>>'{operation,ScopeEpoch}')::bigint BETWEEN 1 AND 9007199254740991 AND p#>>'{operation,ScopeEpoch}'=p#>>'{source,ScopeEpoch}'
 AND(p#>>'{operation,MembershipEpoch}')::bigint BETWEEN 1 AND 9007199254740991 AND p#>>'{operation,MembershipEpoch}'=p#>>'{source,MembershipEpoch}'
 AND(p-ARRAY['purpose','permission','tenant_id','hospital_id','department_id','actor_subject','authenticated_at','idle_deadline','actor','operation','source','outlet_id','pharmacy_id'])='{}'::jsonb,false)
 FROM(SELECT NULLIF(current_setting('app.pharmacy_stocktake_choices',true),'')::jsonb AS p)proof
$$;
DO $$DECLARE prior text;BEGIN
 SELECT pg_get_expr(polqual,polrelid)INTO STRICT prior FROM pg_policy WHERE polrelid='pharmacy_staff_department_bindings'::regclass AND polname='pharmacy_staff_binding_scope';
 EXECUTE format('ALTER POLICY pharmacy_staff_binding_scope ON pharmacy_staff_department_bindings USING((%s)OR(status=''approved''AND pharmacy_stocktake_hospital_choices_context(tenant_id,hospital_id,department_id)))',prior);
END $$;
DO $policies$
DECLARE target record;reading text;BEGIN
 FOR target IN SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname=current_schema()AND c.relkind IN('r','p')AND starts_with(c.relname,'pharmacy_')
 AND EXISTS(SELECT 1 FROM pg_attribute a WHERE a.attrelid=c.oid AND a.attname='tenant_id'AND NOT a.attisdropped)LOOP
 reading:='false';
 IF target.relname='pharmacy_staff_department_bindings'THEN reading:='status=''approved''AND pharmacy_stocktake_hospital_choices_context(tenant_id,hospital_id,department_id)';
 ELSIF target.relname='pharmacy_outlets'THEN reading:='country_code=''IN''AND id=(NULLIF(current_setting(''app.pharmacy_stocktake_choices'',true),'''')::jsonb->>''outlet_id'')::uuid AND pharmacy_stocktake_hospital_choices_context(tenant_id,(NULLIF(current_setting(''app.pharmacy_stocktake_choices'',true),'''')::jsonb->>''hospital_id'')::uuid,(NULLIF(current_setting(''app.pharmacy_stocktake_choices'',true),'''')::jsonb->>''department_id'')::uuid)';
 ELSIF target.relname='pharmacy_organizations'THEN reading:='ownership_type=''hospital_owned''AND id=(NULLIF(current_setting(''app.pharmacy_stocktake_choices'',true),'''')::jsonb->>''pharmacy_id'')::uuid AND pharmacy_stocktake_hospital_choices_context(tenant_id,(NULLIF(current_setting(''app.pharmacy_stocktake_choices'',true),'''')::jsonb->>''hospital_id'')::uuid,(NULLIF(current_setting(''app.pharmacy_stocktake_choices'',true),'''')::jsonb->>''department_id'')::uuid)';END IF;
 EXECUTE format('CREATE POLICY hospital_stocktake_choice_purpose ON %I AS RESTRICTIVE USING(hms_tenant_visible(tenant_id)AND(COALESCE(current_setting(''app.pharmacy_stocktake_choices'',true),'''')=''''OR(%s)))WITH CHECK(tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND COALESCE(current_setting(''app.pharmacy_stocktake_choices'',true),'''')='''')',target.relname,reading);
 EXECUTE format('CREATE POLICY hospital_stocktake_choice_delete ON %I AS RESTRICTIVE FOR DELETE USING(hms_tenant_visible(tenant_id)AND COALESCE(current_setting(''app.pharmacy_stocktake_choices'',true),'''')='''')',target.relname);
 END LOOP;
END $policies$;
CREATE FUNCTION pharmacy_stocktake_hospital_choice_state()RETURNS jsonb LANGUAGE sql STABLE AS $$
 SELECT jsonb_agg(jsonb_build_array(c.relname,c.relrowsecurity,c.relforcerowsecurity,p.polname,p.polcmd,p.polpermissive,p.polroles,pg_get_expr(p.polqual,p.polrelid),pg_get_expr(p.polwithcheck,p.polrelid))ORDER BY c.relname,p.polname)
 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace LEFT JOIN pg_policy p ON p.polrelid=c.oid AND p.polname IN('hospital_stocktake_choice_purpose','hospital_stocktake_choice_delete')
 WHERE n.nspname=current_schema()AND c.relkind IN('r','p')AND starts_with(c.relname,'pharmacy_')
 AND EXISTS(SELECT 1 FROM pg_attribute a WHERE a.attrelid=c.oid AND a.attname='tenant_id'AND NOT a.attisdropped)
$$;
DO $choice_integrity$DECLARE reviewed jsonb;BEGIN
 reviewed:=pharmacy_stocktake_hospital_choice_state();
 EXECUTE format('CREATE FUNCTION pharmacy_stocktake_hospital_choice_integrity()RETURNS boolean LANGUAGE sql STABLE AS %L',format('SELECT COALESCE(pharmacy_operating_choice_support()AND pharmacy_stocktake_hospital_choice_state()=%L::jsonb,false)',reviewed::text));
END $choice_integrity$;

DO $integrity$
DECLARE policies text;functions text;policy_count bigint;
BEGIN
 SELECT string_agg(format('(%L::regclass,%L,%L)',p.polrelid::regclass::text,p.polname,md5(p.polcmd::text||p.polpermissive::text||p.polroles::text||COALESCE(pg_get_expr(p.polqual,p.polrelid),'')||COALESCE(pg_get_expr(p.polwithcheck,p.polrelid),''))),','),count(*)INTO policies,policy_count
 FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname=current_schema()AND(c.relname IN('pharmacy_organizations','pharmacy_outlets')OR starts_with(c.relname,'pharmacy_')AND p.polname IN('operating_choice_purpose','operating_choice_delete'));
 SELECT string_agg(format('(%L,%L)',p.oid::regprocedure::text,md5(pg_get_functiondef(p.oid))),',')INTO functions FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
 WHERE n.nspname=current_schema()AND p.proname='pharmacy_operating_choice_context';
 EXECUTE format($definition$CREATE OR REPLACE FUNCTION pharmacy_operating_choice_integrity()RETURNS boolean LANGUAGE sql STABLE AS $body$
 SELECT NOT EXISTS(SELECT 1 FROM(VALUES %s)expected(relation,name,digest)LEFT JOIN pg_policy p ON p.polrelid=expected.relation AND p.polname=expected.name
 WHERE p.oid IS NULL OR md5(p.polcmd::text||p.polpermissive::text||p.polroles::text||COALESCE(pg_get_expr(p.polqual,p.polrelid),'')||COALESCE(pg_get_expr(p.polwithcheck,p.polrelid),''))<>expected.digest)
 AND(SELECT count(*)FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema()AND(c.relname IN('pharmacy_organizations','pharmacy_outlets')OR starts_with(c.relname,'pharmacy_')AND p.polname IN('operating_choice_purpose','operating_choice_delete')))=%s
 AND NOT EXISTS(SELECT 1 FROM(VALUES %s)expected(name,digest)LEFT JOIN pg_proc p ON p.oid=to_regprocedure(expected.name)WHERE p.oid IS NULL OR md5(pg_get_functiondef(p.oid))<>expected.digest)
 $body$ $definition$,policies,policy_count,functions);
END $integrity$;


DO $hospital_integrity$DECLARE reviewed jsonb;BEGIN
 reviewed:=pharmacy_stocktake_hospital_support_state();
 EXECUTE format('CREATE OR REPLACE FUNCTION pharmacy_stocktake_hospital_integrity()RETURNS boolean LANGUAGE sql STABLE AS %L',format('SELECT COALESCE(pharmacy_operating_choice_integrity()AND pharmacy_stocktake_hospital_support_state()=%L::jsonb,false)',reviewed::text));
END $hospital_integrity$;
