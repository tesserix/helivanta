-- 0050_iam_operating_governance attached iam_operating_native_global_mutation_guard
-- to every global iam_ table that existed then. Its loop does not run again,
-- so a global table created later must attach the guard itself, and the
-- captured operating-governance fingerprint must then count the new trigger.
DO $prerequisites$
BEGIN
 IF iam_assignment_governance_integrity()IS DISTINCT FROM true
 OR iam_operating_governance_integrity()IS DISTINCT FROM true
 OR iam_staff_selection_integrity()IS DISTINCT FROM true THEN
  RAISE EXCEPTION 'security key registry requires unchanged prior IAM authority';
 END IF;
END $prerequisites$;

CREATE TRIGGER iam_operating_native_global_mutation_guard BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON iam_security_keys FOR EACH STATEMENT EXECUTE FUNCTION iam_operating_native_global_mutation_guard();

-- Recapture: the prerequisite above proved every other fingerprint unchanged
-- in this transaction, so the only difference is the trigger just created.
DO $migration$
DECLARE policies text;functions text;triggers text;policy_count bigint;trigger_count bigint;
BEGIN
 SELECT string_agg(format('(%L::regclass,%L,%L)',p.polrelid::regclass::text,p.polname,md5(p.polcmd::text||p.polpermissive::text||p.polroles::text||COALESCE(pg_get_expr(p.polqual,p.polrelid),'')||COALESCE(pg_get_expr(p.polwithcheck,p.polrelid),''))),','),count(*)INTO policies,policy_count
 FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname=current_schema()AND(c.relname LIKE 'iam_operating_%'OR p.polname LIKE 'operating_native_%');
 SELECT string_agg(format('(%L,%L)',p.oid::regprocedure::text,md5(pg_get_functiondef(p.oid))),',')INTO functions FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
 WHERE n.nspname=current_schema()AND p.proname LIKE 'iam_operating_%'AND p.proname NOT IN('iam_operating_governance_integrity','iam_operating_governance_support');
 SELECT string_agg(format('(%L::regclass,%L,%L)',t.tgrelid::regclass::text,t.tgname,md5(pg_get_triggerdef(t.oid,true))),','),count(*)INTO triggers,trigger_count FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname=current_schema()AND NOT t.tgisinternal AND(c.relname LIKE 'iam_operating_%'OR t.tgname='iam_operating_native_global_mutation_guard');
 EXECUTE format($definition$CREATE OR REPLACE FUNCTION iam_operating_governance_integrity()RETURNS boolean LANGUAGE sql STABLE AS $body$
 SELECT NOT EXISTS(SELECT 1 FROM(VALUES %s)expected(relation,name,digest)LEFT JOIN pg_policy p ON p.polrelid=expected.relation AND p.polname=expected.name WHERE p.oid IS NULL OR md5(p.polcmd::text||p.polpermissive::text||p.polroles::text||COALESCE(pg_get_expr(p.polqual,p.polrelid),'')||COALESCE(pg_get_expr(p.polwithcheck,p.polrelid),''))<>expected.digest)
 AND(SELECT count(*)FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema()AND(c.relname LIKE 'iam_operating_%%'OR p.polname LIKE 'operating_native_%%'))=%s
 AND NOT EXISTS(SELECT 1 FROM(VALUES %s)expected(name,digest)LEFT JOIN pg_proc p ON p.oid=to_regprocedure(expected.name)WHERE p.oid IS NULL OR md5(pg_get_functiondef(p.oid))<>expected.digest)
 AND NOT EXISTS(SELECT 1 FROM(VALUES %s)expected(relation,name,digest)LEFT JOIN pg_trigger t ON t.tgrelid=expected.relation AND t.tgname=expected.name WHERE t.oid IS NULL OR t.tgenabled NOT IN('O','A')OR md5(pg_get_triggerdef(t.oid,true))<>expected.digest)
 AND(SELECT count(*)FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema()AND NOT t.tgisinternal AND(c.relname LIKE 'iam_operating_%%'OR t.tgname='iam_operating_native_global_mutation_guard'))=%s
 $body$ $definition$,policies,policy_count,functions,triggers,trigger_count);
 IF iam_assignment_governance_integrity()IS DISTINCT FROM true
 OR iam_operating_governance_integrity()IS DISTINCT FROM true
 OR iam_staff_selection_integrity()IS DISTINCT FROM true THEN
  RAISE EXCEPTION 'security key registry changed unrelated IAM authority';
 END IF;
END $migration$;
