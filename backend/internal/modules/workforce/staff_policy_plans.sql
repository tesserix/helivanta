SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

DO $prerequisites$
BEGIN
 IF workforce_assignment_selection_integrity()IS DISTINCT FROM true
 OR workforce_operating_selection_integrity()IS DISTINCT FROM true THEN
  RAISE EXCEPTION 'Staff read plans require unchanged authority definitions' USING ERRCODE='55000';
 END IF;
END $prerequisites$;

DO $plans$
DECLARE policy_name text;helper text;prior record;old_digests text[]:='{}';new_digests text[]:='{}';
 integrity_name text;definition text;fingerprints text;helper_count bigint;index integer;
BEGIN
 FOREACH policy_name IN ARRAY ARRAY['tenant_isolation','partner_candidate_update_guard']LOOP
  SELECT p.polname,p.polcmd,p.polpermissive,pg_get_expr(p.polqual,p.polrelid)AS reading,
   md5(p.polcmd::text||p.polpermissive::text||p.polroles::text||COALESCE(pg_get_expr(p.polqual,p.polrelid),'')||COALESCE(pg_get_expr(p.polwithcheck,p.polrelid),''))AS digest
  INTO STRICT prior FROM pg_policy p WHERE p.polrelid='workforce_staff'::regclass AND p.polname=policy_name;
  IF prior.reading IS NULL OR(policy_name='tenant_isolation'AND(prior.polcmd<>'*'OR NOT prior.polpermissive))
  OR(policy_name='partner_candidate_update_guard'AND(prior.polcmd<>'w'OR prior.polpermissive))THEN
   RAISE EXCEPTION 'Staff read plans require the exact canonical and locking policies' USING ERRCODE='55000';
  END IF;
  helper:=CASE policy_name WHEN 'tenant_isolation'THEN 'workforce_staff_read_policy'ELSE 'workforce_staff_lock_policy'END;
  old_digests:=array_append(old_digests,prior.digest);
  EXECUTE format($ddl$
   CREATE FUNCTION %I(staff_record workforce_staff)RETURNS boolean LANGUAGE plpgsql STABLE SECURITY INVOKER AS $predicate$
   BEGIN
    RETURN(SELECT(%s)FROM(SELECT(staff_record).*)AS workforce_staff);
   END
   $predicate$;
  $ddl$,helper,prior.reading);
  IF policy_name='tenant_isolation'THEN
   IF position('hms_tenant_visible'IN prior.reading)=0 THEN RAISE EXCEPTION 'Staff read plans require retained tenant visibility' USING ERRCODE='55000';END IF;
   EXECUTE format('ALTER POLICY %I ON workforce_staff USING(hms_tenant_visible(tenant_id)AND %I(workforce_staff))',policy_name,helper);
  ELSE
   EXECUTE format('ALTER POLICY %I ON workforce_staff USING(%I(workforce_staff))',policy_name,helper);
  END IF;
  SELECT array_append(new_digests,md5(p.polcmd::text||p.polpermissive::text||p.polroles::text||COALESCE(pg_get_expr(p.polqual,p.polrelid),'')||COALESCE(pg_get_expr(p.polwithcheck,p.polrelid),'')))
  INTO new_digests FROM pg_policy p WHERE p.polrelid='workforce_staff'::regclass AND p.polname=policy_name;
 END LOOP;
 SELECT string_agg(format('(%L,%L)',p.oid::regprocedure::text,md5(pg_get_functiondef(p.oid))),','),count(*)INTO fingerprints,helper_count
 FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
 WHERE n.nspname=current_schema()AND p.proname IN('workforce_staff_read_policy','workforce_staff_lock_policy');
 IF helper_count<>2 THEN RAISE EXCEPTION 'Staff read plan predicates are incomplete' USING ERRCODE='55000';END IF;
 FOREACH integrity_name IN ARRAY ARRAY['workforce_assignment_selection_integrity','workforce_operating_selection_integrity']LOOP
  SELECT p.prosrc INTO STRICT definition FROM pg_proc p JOIN pg_language l ON l.oid=p.prolang
  WHERE p.oid=to_regprocedure(format('%I()',integrity_name))AND l.lanname='sql';
  FOR index IN 1..array_length(old_digests,1)LOOP
   IF position(quote_literal(old_digests[index])IN definition)=0 THEN
    RAISE EXCEPTION 'Staff read plans require retained prior policy fingerprints' USING ERRCODE='55000';
   END IF;
   definition:=replace(definition,quote_literal(old_digests[index]),quote_literal(new_digests[index]));
  END LOOP;
  EXECUTE format($ddl$
   CREATE OR REPLACE FUNCTION %I()RETURNS boolean LANGUAGE sql STABLE SECURITY INVOKER AS $integrity$
   SELECT(%s)AND NOT EXISTS(SELECT 1 FROM(VALUES %s)expected(name,digest)
   LEFT JOIN pg_proc p ON p.oid=to_regprocedure(expected.name)
   WHERE p.oid IS NULL OR md5(pg_get_functiondef(p.oid))<>expected.digest)
   $integrity$;
  $ddl$,integrity_name,definition,fingerprints);
 END LOOP;
END $plans$;
