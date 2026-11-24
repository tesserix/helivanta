SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

-- Preserve the row boundary; only make its unchanged grant lookup an InitPlan.
DO $migration$
DECLARE
 relation regclass;
 prior text;
 prior_check text;
 replacement text;
 old_call text := 'pharmacy_outlet_allowed(tenant_id, outlet_id, false)';
 new_call text := $predicate$(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND outlet_id=NULLIF(current_setting('app.pharmacy_outlet',true),'')::uuid
 AND (SELECT pharmacy_outlet_allowed(NULLIF(current_setting('app.tenant_id',true),'')::uuid,
 NULLIF(current_setting('app.pharmacy_outlet',true),'')::uuid,false)))$predicate$;
BEGIN
 IF NOT pharmacy_operating_choice_integrity() THEN
  RAISE EXCEPTION 'existing pharmacy purpose policies changed' USING ERRCODE='23514';
 END IF;
 FOREACH relation IN ARRAY ARRAY['pharmacy_catalogue_products'::regclass,'pharmacy_reorder_levels'::regclass,
 'pharmacy_stock_batches'::regclass,'pharmacy_stock_movements'::regclass,'pharmacy_stock_locations'::regclass]
 LOOP
  SELECT pg_get_expr(polqual,polrelid),pg_get_expr(polwithcheck,polrelid)
  INTO STRICT prior,prior_check FROM pg_policy WHERE polrelid=relation AND polname='tenant_isolation';
  IF array_length(string_to_array(prior,old_call),1)<>2 THEN
   RAISE EXCEPTION 'expected one legacy outlet read guard on %',relation USING ERRCODE='23514';
  END IF;
  replacement:=replace(prior,old_call,new_call);
  EXECUTE format('ALTER POLICY tenant_isolation ON %s USING (%s)',relation,replacement);
  IF prior_check IS DISTINCT FROM (SELECT pg_get_expr(polwithcheck,polrelid)FROM pg_policy WHERE polrelid=relation AND polname='tenant_isolation') THEN
   RAISE EXCEPTION 'outlet write policy changed on %',relation USING ERRCODE='23514';
  END IF;
 END LOOP;
 IF NOT pharmacy_operating_choice_integrity() THEN
  RAISE EXCEPTION 'pharmacy purpose policies changed' USING ERRCODE='23514';
 END IF;
END $migration$;
