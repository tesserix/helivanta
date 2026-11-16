SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE FUNCTION workforce_operating_selection_context(t uuid,e uuid)RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(hms_tenant_visible(t)
 AND t=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND e=NULLIF(current_setting('app.workforce_hospital',true),'')::uuid
 AND p->>'purpose'='staff_choices' AND p->>'capability'='iam.governance.operating.manage'
 AND p->>'tenant_id'=t::text AND p->>'employer_id'=e::text
 AND p->>'employer_kind' IN('pharmacy','supplier')
 AND p->>'actor_subject'=NULLIF(current_setting('app.actor_subject',true),'')
 AND p->>'actor_subject'=NULLIF(current_setting('app.workforce_subject',true),'')
 AND (p->>'membership_epoch')::bigint BETWEEN 1 AND 9007199254740991
 AND (p->>'owner_count')::bigint BETWEEN 1 AND 1000
 AND (p->>'source_expires_at')::timestamptz>clock_timestamp()
 AND (p->>'idle_deadline')::timestamptz>clock_timestamp()
 AND (p->>'authenticated_at')::timestamptz<=clock_timestamp()
 AND ((p->>'authority_kind'='owner' AND (p->>'owner_membership_id')::uuid IS NOT NULL
 AND p->>'source_grant_id' IS NULL AND p->>'source_grant_version' IS NULL AND p->>'template_key' IS NULL)
 OR(p->>'authority_kind'='grant' AND (p->>'source_grant_id')::uuid IS NOT NULL
 AND (p->>'source_grant_version')::bigint BETWEEN 1 AND 9007199254740991
 AND p->>'template_key'='iam.operating_access_reviewer.v1' AND p->>'owner_membership_id' IS NULL)),false)
 FROM(SELECT NULLIF(current_setting('app.workforce_operating_selection',true),'')::jsonb AS p)proof
$$;
CREATE FUNCTION workforce_operating_selection_staff(t uuid,e uuid,state text)RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT workforce_operating_selection_context(t,e)AND state='active'
$$;
CREATE FUNCTION workforce_operating_selection_link(t uuid,e uuid,s uuid,state text,generation uuid,basis bigint,revision bigint)RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT workforce_operating_selection_context(t,e)AND state='active' AND generation IS NULL AND revision BETWEEN 2 AND 9007199254740991
 AND EXISTS(SELECT 1 FROM workforce_staff w WHERE w.tenant_id=t AND w.hospital_id=e AND w.id=s AND w.employment_status='active' AND w.version=basis)
$$;
CREATE FUNCTION workforce_operating_selection_audit(t uuid,e uuid,actor text,action text,resource uuid,changes jsonb)RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(workforce_operating_selection_context(t,e)
 AND actor=p->>'actor_subject' AND action='workforce.operating_staff_choice.read'
 AND changes=jsonb_build_object('authority',p,'staff_version',(changes->>'staff_version')::bigint,'link_id',(changes->>'link_id')::uuid,'link_version',(changes->>'link_version')::bigint)
 AND EXISTS(SELECT 1 FROM workforce_staff s JOIN workforce_login_links l ON(l.tenant_id,l.hospital_id,l.staff_id)=(s.tenant_id,s.hospital_id,s.id)
 WHERE s.tenant_id=t AND s.hospital_id=e AND s.id=resource AND s.employment_status='active'
 AND s.version=(changes->>'staff_version')::bigint AND l.id=(changes->>'link_id')::uuid
 AND l.version=(changes->>'link_version')::bigint AND l.status='active' AND l.employment_generation_id IS NULL AND l.independent_staff_version=s.version),false)
 FROM(SELECT NULLIF(current_setting('app.workforce_operating_selection',true),'')::jsonb AS p)proof
$$;

DO $policies$
DECLARE target record;prior record;reading text;writing text;
BEGIN
 FOR target IN SELECT * FROM(VALUES
 ('workforce_staff','tenant_isolation','workforce_operating_selection_staff(tenant_id,hospital_id,employment_status)'),
 ('workforce_login_links','login_link_isolation','workforce_operating_selection_link(tenant_id,hospital_id,staff_id,status,employment_generation_id,independent_staff_version,version)')
 )targets(tab,policy,expression)LOOP
  SELECT pg_get_expr(polqual,polrelid)AS reading,pg_get_expr(polwithcheck,polrelid)AS writing INTO STRICT prior FROM pg_policy
  WHERE polrelid=('public.'||quote_ident(target.tab))::regclass AND polname=target.policy;
  IF prior.writing IS NULL THEN RAISE EXCEPTION 'operating selection requires explicit retained write checks';END IF;
  EXECUTE format('ALTER POLICY %I ON %I USING(hms_tenant_visible(tenant_id)AND((%s)OR(%s)))',target.policy,target.tab,prior.reading,target.expression);
  SELECT pg_get_expr(polqual,polrelid)INTO STRICT reading FROM pg_policy
  WHERE polrelid=('public.'||quote_ident(target.tab))::regclass AND polname='partner_candidate_update_guard' AND NOT polpermissive AND polcmd='w';
  EXECUTE format('ALTER POLICY partner_candidate_update_guard ON %I USING(hms_tenant_visible(tenant_id)AND((%s)OR(%s)))',target.tab,reading,target.expression);
 END LOOP;
 SELECT polname,pg_get_expr(polwithcheck,polrelid)AS writing INTO STRICT prior FROM pg_policy WHERE polrelid='public.workforce_audit'::regclass AND polpermissive;
 IF prior.writing IS NULL THEN RAISE EXCEPTION 'operating selection requires explicit retained audit checks';END IF;
 EXECUTE format('ALTER POLICY %I ON workforce_audit WITH CHECK((%s)OR workforce_operating_selection_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes))',prior.polname,prior.writing);
 FOR target IN SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname=current_schema()AND c.relkind IN('r','p')AND starts_with(c.relname,'workforce_')
 AND EXISTS(SELECT 1 FROM pg_attribute a WHERE a.attrelid=c.oid AND a.attname='tenant_id'AND NOT a.attisdropped)LOOP
  reading:='false';writing:='false';
  IF target.relname='workforce_staff'THEN reading:='workforce_operating_selection_staff(tenant_id,hospital_id,employment_status)';
  ELSIF target.relname='workforce_login_links'THEN reading:='workforce_operating_selection_link(tenant_id,hospital_id,staff_id,status,employment_generation_id,independent_staff_version,version)';
  ELSIF target.relname='workforce_hospitals'THEN reading:='workforce_operating_selection_context(tenant_id,id) AND establishment_kind=NULLIF(current_setting(''app.workforce_operating_selection'',true),'''')::jsonb->>''employer_kind''';
  ELSIF target.relname='workforce_audit'THEN writing:='workforce_operating_selection_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes)';END IF;
  EXECUTE format('CREATE POLICY operating_selection_purpose ON %I AS RESTRICTIVE USING(hms_tenant_visible(tenant_id)AND(COALESCE(current_setting(''app.workforce_operating_selection'',true),'''')=''''OR(%s)))WITH CHECK(tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND(COALESCE(current_setting(''app.workforce_operating_selection'',true),'''')=''''OR(%s)))',target.relname,reading,writing);
  EXECUTE format('CREATE POLICY operating_selection_delete ON %I AS RESTRICTIVE FOR DELETE USING(hms_tenant_visible(tenant_id)AND COALESCE(current_setting(''app.workforce_operating_selection'',true),'''')='''')',target.relname);
 END LOOP;
END $policies$;

DO $integrity$
DECLARE policies text;functions text;policy_count bigint;
BEGIN
 SELECT string_agg(format('(%L::regclass,%L,%L)',p.polrelid::regclass::text,p.polname,md5(p.polcmd::text||p.polpermissive::text||p.polroles::text||COALESCE(pg_get_expr(p.polqual,p.polrelid),'')||COALESCE(pg_get_expr(p.polwithcheck,p.polrelid),''))),','),count(*)INTO policies,policy_count
 FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname=current_schema()AND(c.relname IN('workforce_hospitals','workforce_staff','workforce_login_links','workforce_audit')OR p.polname IN('operating_selection_purpose','operating_selection_delete'));
 SELECT string_agg(format('(%L,%L)',p.oid::regprocedure::text,md5(pg_get_functiondef(p.oid))),',')INTO functions FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
 WHERE n.nspname=current_schema()AND p.proname IN('workforce_operating_selection_context','workforce_operating_selection_staff','workforce_operating_selection_link','workforce_operating_selection_audit');
 EXECUTE format($definition$CREATE FUNCTION workforce_operating_selection_integrity()RETURNS boolean LANGUAGE sql STABLE AS $body$
 SELECT NOT EXISTS(SELECT 1 FROM(VALUES %s)expected(relation,name,digest)LEFT JOIN pg_policy p ON p.polrelid=expected.relation AND p.polname=expected.name
 WHERE p.oid IS NULL OR md5(p.polcmd::text||p.polpermissive::text||p.polroles::text||COALESCE(pg_get_expr(p.polqual,p.polrelid),'')||COALESCE(pg_get_expr(p.polwithcheck,p.polrelid),''))<>expected.digest)
 AND(SELECT count(*)FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema()AND(c.relname IN('workforce_hospitals','workforce_staff','workforce_login_links','workforce_audit')OR p.polname IN('operating_selection_purpose','operating_selection_delete')))=%s
 AND NOT EXISTS(SELECT 1 FROM(VALUES %s)expected(name,digest)LEFT JOIN pg_proc p ON p.oid=to_regprocedure(expected.name)WHERE p.oid IS NULL OR md5(pg_get_functiondef(p.oid))<>expected.digest)
 $body$ $definition$,policies,policy_count,functions);
END $integrity$;
CREATE FUNCTION workforce_operating_selection_support()RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT workforce_operating_selection_integrity()AND NOT EXISTS(
 SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema()AND c.relkind IN('r','p')AND starts_with(c.relname,'workforce_')
 AND EXISTS(SELECT 1 FROM pg_attribute a WHERE a.attrelid=c.oid AND a.attname='tenant_id'AND NOT a.attisdropped)
 AND(NOT c.relrowsecurity OR NOT c.relforcerowsecurity OR has_table_privilege(current_user,c.oid,'TRUNCATE')
 OR NOT EXISTS(SELECT 1 FROM pg_policy p WHERE p.polrelid=c.oid AND p.polname='operating_selection_purpose'AND NOT p.polpermissive AND p.polcmd='*'AND p.polqual IS NOT NULL AND p.polwithcheck IS NOT NULL)
 OR NOT EXISTS(SELECT 1 FROM pg_policy p WHERE p.polrelid=c.oid AND p.polname='operating_selection_delete'AND NOT p.polpermissive AND p.polcmd='d'AND p.polqual IS NOT NULL)))
 AND EXISTS(SELECT 1 FROM pg_index WHERE indexrelid=to_regclass('public.workforce_login_links_one_staff')AND indrelid='public.workforce_login_links'::regclass AND indisvalid AND indisready AND indisunique)
$$;
