SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

DO $plans$
DECLARE helper text;integrity_name text;definition text;old_digests text[]:='{}';new_digests text[]:='{}';index integer;
BEGIN
 IF workforce_assignment_selection_integrity()IS DISTINCT FROM true
 OR workforce_operating_selection_integrity()IS DISTINCT FROM true THEN
  RAISE EXCEPTION 'Staff generic plans require unchanged authority definitions' USING ERRCODE='55000';
 END IF;
 FOREACH helper IN ARRAY ARRAY['workforce_staff_read_policy','workforce_staff_lock_policy']LOOP
  SELECT array_append(old_digests,md5(pg_get_functiondef(to_regprocedure(format('%I(workforce_staff)',helper)))))INTO old_digests;
  EXECUTE format('ALTER FUNCTION %I(workforce_staff)SET plan_cache_mode=''force_generic_plan''',helper);
  SELECT array_append(new_digests,md5(pg_get_functiondef(to_regprocedure(format('%I(workforce_staff)',helper)))))INTO new_digests;
 END LOOP;
 FOREACH integrity_name IN ARRAY ARRAY['workforce_assignment_selection_integrity','workforce_operating_selection_integrity']LOOP
  SELECT p.prosrc INTO STRICT definition FROM pg_proc p JOIN pg_language l ON l.oid=p.prolang
  WHERE p.oid=to_regprocedure(format('%I()',integrity_name))AND l.lanname='sql';
  FOR index IN 1..array_length(old_digests,1)LOOP
   IF position(quote_literal(old_digests[index])IN definition)=0 THEN
    RAISE EXCEPTION 'Staff generic plans require retained predicate fingerprints' USING ERRCODE='55000';
   END IF;
   definition:=replace(definition,quote_literal(old_digests[index]),quote_literal(new_digests[index]));
  END LOOP;
  EXECUTE format('CREATE OR REPLACE FUNCTION %I()RETURNS boolean LANGUAGE sql STABLE SECURITY INVOKER AS %L',integrity_name,definition);
 END LOOP;
END $plans$;
