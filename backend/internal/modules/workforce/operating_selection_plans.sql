SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

DO $prerequisites$
BEGIN
 IF to_regprocedure('public.workforce_operating_selection_link(uuid,uuid,uuid,text,uuid,bigint,bigint)')IS NULL
 OR to_regprocedure('public.workforce_operating_selection_integrity()')IS NULL THEN
  RAISE EXCEPTION 'operating selection plans require the complete operating choice schema';
 END IF;
 IF workforce_operating_selection_integrity()IS DISTINCT FROM true THEN
  RAISE EXCEPTION 'operating selection plans require unchanged prior authority definitions';
 END IF;
END $prerequisites$;

CREATE OR REPLACE FUNCTION workforce_operating_selection_link(t uuid,e uuid,s uuid,state text,generation uuid,basis bigint,revision bigint)RETURNS boolean LANGUAGE plpgsql STABLE SECURITY INVOKER AS $link$
BEGIN
 IF NULLIF(current_setting('app.workforce_operating_selection',true),'')IS NULL THEN
  RETURN false;
 END IF;
 RETURN (
 SELECT workforce_operating_selection_context(t,e)AND state='active' AND generation IS NULL AND revision BETWEEN 2 AND 9007199254740991
 AND EXISTS(SELECT 1 FROM workforce_staff w WHERE w.tenant_id=t AND w.hospital_id=e AND w.id=s AND w.employment_status='active' AND w.version=basis)
);
END
$link$;

DO $integrity$
DECLARE policies text;functions text;policy_count bigint;
BEGIN
 SELECT string_agg(format('(%L::regclass,%L,%L)',p.polrelid::regclass::text,p.polname,md5(p.polcmd::text||p.polpermissive::text||p.polroles::text||COALESCE(pg_get_expr(p.polqual,p.polrelid),'')||COALESCE(pg_get_expr(p.polwithcheck,p.polrelid),''))),','),count(*)INTO policies,policy_count
 FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname=current_schema()AND(c.relname IN('workforce_hospitals','workforce_staff','workforce_login_links','workforce_audit')OR p.polname IN('operating_selection_purpose','operating_selection_delete'));
 SELECT string_agg(format('(%L,%L)',p.oid::regprocedure::text,md5(pg_get_functiondef(p.oid))),',')INTO functions FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
 WHERE n.nspname=current_schema()AND p.proname IN('workforce_operating_selection_context','workforce_operating_selection_staff','workforce_operating_selection_link','workforce_operating_selection_audit');
 EXECUTE format($definition$CREATE OR REPLACE FUNCTION workforce_operating_selection_integrity()RETURNS boolean LANGUAGE sql STABLE AS $body$
 SELECT NOT EXISTS(SELECT 1 FROM(VALUES %s)expected(relation,name,digest)LEFT JOIN pg_policy p ON p.polrelid=expected.relation AND p.polname=expected.name
 WHERE p.oid IS NULL OR md5(p.polcmd::text||p.polpermissive::text||p.polroles::text||COALESCE(pg_get_expr(p.polqual,p.polrelid),'')||COALESCE(pg_get_expr(p.polwithcheck,p.polrelid),''))<>expected.digest)
 AND(SELECT count(*)FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema()AND(c.relname IN('workforce_hospitals','workforce_staff','workforce_login_links','workforce_audit')OR p.polname IN('operating_selection_purpose','operating_selection_delete')))=%s
 AND NOT EXISTS(SELECT 1 FROM(VALUES %s)expected(name,digest)LEFT JOIN pg_proc p ON p.oid=to_regprocedure(expected.name)WHERE p.oid IS NULL OR md5(pg_get_functiondef(p.oid))<>expected.digest)
 $body$ $definition$,policies,policy_count,functions);
END $integrity$;
