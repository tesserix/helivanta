SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE FUNCTION emergency_schema_owner() RETURNS boolean LANGUAGE sql STABLE SET search_path=pg_catalog,public AS $$
 SELECT current_user=pg_get_userbyid(relowner) FROM pg_class WHERE oid='public.emergency_dispatches'::regclass
$$;
REVOKE ALL ON FUNCTION emergency_schema_owner() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION emergency_schema_owner() TO hms_app,helivanta_emergency_worker;

DO $$
DECLARE function_oid oid;definition text;rewritten text;policy record;qual text;checked text;
BEGIN
 FOR function_oid IN SELECT p.oid FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
  WHERE n.nspname='public' AND p.proname IN('emergency_worker_allowed','emergency_receiving_owner_allowed') LOOP
  definition=pg_get_functiondef(function_oid);
  rewritten=replace(definition,'current_user=''hms_owner''','emergency_schema_owner()');
  IF definition=rewritten THEN RAISE EXCEPTION 'emergency owner function contract changed';END IF;
  EXECUTE rewritten;
 END LOOP;
 FOR policy IN SELECT p.*,c.relname,n.nspname FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid JOIN pg_namespace n ON n.oid=c.relnamespace
  WHERE n.nspname='public' AND c.relname LIKE 'emergency\_%' ESCAPE '\' AND p.polname='tenant_isolation' LOOP
  qual=pg_get_expr(policy.polqual,policy.polrelid);
  checked=pg_get_expr(policy.polwithcheck,policy.polrelid);
  IF qual IS NOT NULL AND position('CURRENT_USER = ''hms_owner''::name' IN qual)>0 THEN
   EXECUTE format('ALTER POLICY %I ON %I.%I USING(%s)',policy.polname,policy.nspname,policy.relname,replace(qual,'CURRENT_USER = ''hms_owner''::name','emergency_schema_owner()'));
  END IF;
  IF checked IS NOT NULL AND position('CURRENT_USER = ''hms_owner''::name' IN checked)>0 THEN
   EXECUTE format('ALTER POLICY %I ON %I.%I WITH CHECK(%s)',policy.polname,policy.nspname,policy.relname,replace(checked,'CURRENT_USER = ''hms_owner''::name','emergency_schema_owner()'));
  END IF;
 END LOOP;
END $$;
