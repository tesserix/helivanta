SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

DO $before$
DECLARE policies jsonb;guards jsonb;
BEGIN
 IF workforce_assignment_selection_integrity()IS DISTINCT FROM true
 OR workforce_operating_selection_integrity()IS DISTINCT FROM true THEN
 RAISE EXCEPTION 'hospital stocktake history requires unchanged prior Workforce authority';
 END IF;
 SELECT jsonb_agg(jsonb_build_object('table',c.relname,'policy',p.polname,'digest',md5(p.polcmd::text||p.polpermissive::text||p.polroles::text||COALESCE(pg_get_expr(p.polqual,p.polrelid),'')||COALESCE(pg_get_expr(p.polwithcheck,p.polrelid),''))))INTO policies
 FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname=current_schema()AND c.relname IN('workforce_login_links','workforce_login_link_history','workforce_employment_generations')
 AND p.polname=CASE WHEN c.relname='workforce_employment_generations' THEN 'generation_isolation' ELSE 'login_link_isolation' END;
 IF jsonb_array_length(policies)<>3 THEN RAISE EXCEPTION 'hospital identity policies are unavailable';END IF;
 SELECT jsonb_object_agg(name,pg_get_functiondef(name::regprocedure))INTO guards
 FROM unnest(ARRAY['workforce_assignment_selection_integrity()','workforce_operating_selection_integrity()'])name;
 PERFORM set_config('migration.stocktake_history_policies',policies::text,true),set_config('migration.stocktake_history_guards',guards::text,true);
END $before$;

CREATE FUNCTION workforce_stocktake_history_context(t uuid,h uuid)RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(hms_tenant_visible(t)AND t=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND h=NULLIF(current_setting('app.workforce_hospital',true),'')::uuid
 AND current_setting('app.workforce_stocktake_history',true)='authorized'
 AND NULLIF(current_setting('app.workforce_stocktake_history_actor',true),'')=NULLIF(current_setting('app.workforce_subject',true),'')
 AND NULLIF(current_setting('app.workforce_subject',true),'')=NULLIF(current_setting('app.actor_subject',true),'')
 AND NULLIF(current_setting('app.workforce_stocktake_history_subject',true),'')IS NOT NULL,false)
$$;
CREATE FUNCTION workforce_stocktake_history_link(t uuid,h uuid,account text)RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(workforce_stocktake_history_context(t,h)AND account IN(NULLIF(current_setting('app.workforce_stocktake_history_actor',true),''),NULLIF(current_setting('app.workforce_stocktake_history_subject',true),'')),false)
$$;
CREATE FUNCTION workforce_stocktake_history_link_revision(t uuid,h uuid,l uuid)RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(workforce_stocktake_history_context(t,h)AND EXISTS(SELECT 1 FROM workforce_login_links p WHERE p.tenant_id=t AND p.hospital_id=h AND p.id=l AND workforce_stocktake_history_link(t,h,p.subject)),false)
$$;
CREATE FUNCTION workforce_stocktake_history_generation(t uuid,h uuid,s uuid,g uuid)RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(workforce_stocktake_history_context(t,h)
 AND s=NULLIF(current_setting('app.workforce_stocktake_history_staff',true),'')::uuid
 AND g=NULLIF(current_setting('app.workforce_stocktake_history_generation',true),'')::uuid,false)
$$;

DO $policies$
DECLARE target record;prior record;
BEGIN
 FOR target IN SELECT *FROM(VALUES
 ('workforce_login_links','login_link_isolation','workforce_stocktake_history_link(tenant_id,hospital_id,subject)'),
 ('workforce_login_link_history','login_link_isolation','workforce_stocktake_history_link_revision(tenant_id,hospital_id,id)'),
 ('workforce_employment_generations','generation_isolation','workforce_stocktake_history_generation(tenant_id,hospital_id,staff_id,id)')
 )targets(tab,policy,reading)LOOP
 SELECT pg_get_expr(polqual,polrelid)reading,pg_get_expr(polwithcheck,polrelid)writing INTO STRICT prior
 FROM pg_policy WHERE polrelid=to_regclass('public.'||target.tab)AND polname=target.policy AND polpermissive AND polcmd='*';
 IF prior.reading IS NULL OR prior.writing IS NULL THEN RAISE EXCEPTION 'history requires explicit prior owner policies';END IF;
 EXECUTE format('ALTER POLICY %I ON %I USING(CASE WHEN COALESCE(current_setting(''app.workforce_stocktake_history'',true),'''')<>'''' THEN(%s)ELSE(%s)END)WITH CHECK((%s)AND COALESCE(current_setting(''app.workforce_stocktake_history'',true),'''')='''')',target.policy,target.tab,target.reading,prior.reading,prior.writing);
 END LOOP;
END $policies$;

DO $after$
DECLARE guards jsonb:=current_setting('migration.stocktake_history_guards')::jsonb;
 policies jsonb:=current_setting('migration.stocktake_history_policies')::jsonb;
 guard record;item jsonb;definition text;needle text;replacement text;fresh text;changed integer;
BEGIN
 FOR guard IN SELECT key,value FROM jsonb_each_text(guards)LOOP
  definition:=guard.value;changed:=0;
  IF definition IS DISTINCT FROM pg_get_functiondef(guard.key::regprocedure)THEN RAISE EXCEPTION 'history changed the prior integrity guard';END IF;
  FOR item IN SELECT value FROM jsonb_array_elements(policies)LOOP
   SELECT md5(polcmd::text||polpermissive::text||polroles::text||COALESCE(pg_get_expr(polqual,polrelid),'')||COALESCE(pg_get_expr(polwithcheck,polrelid),''))INTO STRICT fresh
   FROM pg_policy WHERE polrelid=to_regclass('public.'||(item->>'table'))AND polname=item->>'policy';
   needle:=format('(%L::regclass,%L,%L)',item->>'table',item->>'policy',item->>'digest');
   replacement:=format('(%L::regclass,%L,%L)',item->>'table',item->>'policy',fresh);
   IF strpos(definition,needle)>0 THEN
    IF fresh=item->>'digest'OR(length(definition)-length(replace(definition,needle,'')))<>length(needle)THEN RAISE EXCEPTION 'history policy fingerprint is not uniquely captured';END IF;
    definition:=replace(definition,needle,replacement);changed:=changed+1;
   END IF;
  END LOOP;
  IF(guard.key='workforce_assignment_selection_integrity()'AND changed<>3)
  OR(guard.key='workforce_operating_selection_integrity()'AND changed<>1)
  OR guard.key NOT IN('workforce_assignment_selection_integrity()','workforce_operating_selection_integrity()')THEN RAISE EXCEPTION 'unexpected history integrity footprint';END IF;
  -- Preserve all unrelated policy/function fingerprints and every policy count.
  EXECUTE definition;
 END LOOP;
 IF workforce_assignment_selection_integrity()IS DISTINCT FROM true
 OR workforce_operating_selection_integrity()IS DISTINCT FROM true THEN RAISE EXCEPTION 'history changed unrelated Workforce authority';END IF;
 PERFORM set_config('migration.stocktake_history_policies','',true),set_config('migration.stocktake_history_guards','',true);
END $after$;

DO $seal$
DECLARE policies text;functions text;triggers text;policy_count bigint;function_count bigint;trigger_count bigint;
BEGIN
 SELECT string_agg(format('(%L::regclass,%L,%L)',p.polrelid::regclass::text,p.polname,md5(p.polcmd::text||p.polpermissive::text||p.polroles::text||COALESCE(pg_get_expr(p.polqual,p.polrelid),'')||COALESCE(pg_get_expr(p.polwithcheck,p.polrelid),''))),','),count(*)INTO policies,policy_count
 FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname=current_schema()AND c.relname IN('workforce_login_links','workforce_login_link_history','workforce_employment_generations')
 AND p.polname=CASE WHEN c.relname='workforce_employment_generations' THEN 'generation_isolation' ELSE 'login_link_isolation' END;
 SELECT string_agg(format('(%L,%L)',p.oid::regprocedure::text,md5(pg_get_functiondef(p.oid))),','),count(*)INTO functions,function_count
 FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname=current_schema()AND p.proname IN('workforce_stocktake_history_context','workforce_stocktake_history_link','workforce_stocktake_history_link_revision','workforce_stocktake_history_generation','workforce_prevent_removal','workforce_login_link_history_guard');
 SELECT string_agg(format('(%L::regclass,%L,%L)',t.tgrelid::regclass::text,t.tgname,md5(pg_get_triggerdef(t.oid,true))),','),count(*)INTO triggers,trigger_count
 FROM pg_trigger t WHERE NOT t.tgisinternal AND(t.tgrelid,t.tgname)IN(('workforce_login_links'::regclass,'workforce_login_links_no_delete'),('workforce_login_link_history'::regclass,'workforce_login_link_history_guard'),('workforce_login_link_history'::regclass,'workforce_login_link_history_immutable'),('workforce_employment_generations'::regclass,'workforce_employment_generation_immutable'));
 IF policy_count<>3 OR function_count<>6 OR trigger_count<>4 THEN RAISE EXCEPTION 'incomplete stocktake history prerequisites';END IF;
 EXECUTE format($definition$CREATE FUNCTION workforce_stocktake_history_integrity()RETURNS boolean LANGUAGE sql STABLE AS $body$
 SELECT NOT EXISTS(SELECT 1 FROM(VALUES %s)expected(relation,name,digest)LEFT JOIN pg_policy p ON p.polrelid=expected.relation AND p.polname=expected.name WHERE p.oid IS NULL OR md5(p.polcmd::text||p.polpermissive::text||p.polroles::text||COALESCE(pg_get_expr(p.polqual,p.polrelid),'')||COALESCE(pg_get_expr(p.polwithcheck,p.polrelid),''))<>expected.digest)
 AND NOT EXISTS(SELECT 1 FROM(VALUES %s)expected(name,digest)LEFT JOIN pg_proc p ON p.oid=to_regprocedure(expected.name)WHERE p.oid IS NULL OR md5(pg_get_functiondef(p.oid))<>expected.digest)
 AND NOT EXISTS(SELECT 1 FROM(VALUES %s)expected(relation,name,digest)LEFT JOIN pg_trigger t ON t.tgrelid=expected.relation AND t.tgname=expected.name WHERE t.oid IS NULL OR t.tgenabled NOT IN('O','A')OR md5(pg_get_triggerdef(t.oid,true))<>expected.digest)
 $body$ $definition$,policies,functions,triggers);
END $seal$;
CREATE FUNCTION workforce_stocktake_history_support()RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT workforce_stocktake_history_integrity()AND workforce_assignment_selection_integrity()AND workforce_operating_selection_integrity()
 AND NOT EXISTS(SELECT 1 FROM(VALUES('workforce_login_links'),('workforce_login_link_history'),('workforce_employment_generations'))expected(name)
 WHERE NOT EXISTS(SELECT 1 FROM pg_class c WHERE c.oid=to_regclass('public.'||expected.name)AND c.relrowsecurity AND c.relforcerowsecurity AND NOT has_table_privilege(current_user,c.oid,'TRUNCATE')))
$$;
