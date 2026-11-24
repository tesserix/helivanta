SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
LOCK TABLE pharmacy_catalogue_products,pharmacy_reorder_levels,pharmacy_stock_batches,
 pharmacy_stock_movements,pharmacy_stock_locations IN SHARE ROW EXCLUSIVE MODE;

-- Preserve the row boundary; only make its unchanged grant lookup an InitPlan.
DO $migration$
DECLARE
 relation regclass;
 prior text;
 prior_check text;
 replacement text;
 before_state jsonb;
 after_state jsonb;
 before_policies jsonb;
 after_policies jsonb;
 body text;
 intact boolean;
 old_call text := 'pharmacy_outlet_allowed(tenant_id, outlet_id, false)';
 new_call text := $predicate$(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND outlet_id=NULLIF(current_setting('app.pharmacy_outlet',true),'')::uuid
 AND (SELECT pharmacy_outlet_allowed(NULLIF(current_setting('app.tenant_id',true),'')::uuid,
 NULLIF(current_setting('app.pharmacy_outlet',true),'')::uuid,false)))$predicate$;
BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_proc WHERE oid=to_regprocedure('pharmacy_stocktake_hospital_integrity()')
  AND NOT prosecdef AND NOT proleakproof AND provolatile='s'AND proconfig IS NULL
  AND prolang=(SELECT oid FROM pg_language WHERE lanname='sql'))THEN
  RAISE EXCEPTION 'hospital source seal properties changed' USING ERRCODE='23514';
 END IF;
 SELECT pharmacy_stocktake_hospital_integrity(),pharmacy_stocktake_hospital_support_state()
 INTO intact,before_state;
 IF intact IS DISTINCT FROM true OR pharmacy_operating_choice_integrity()IS DISTINCT FROM true THEN
  RAISE EXCEPTION 'low-stock planning requires unchanged source authority' USING ERRCODE='23514';
 END IF;
 SELECT prosrc INTO STRICT body FROM pg_proc WHERE oid=to_regprocedure('pharmacy_stocktake_hospital_integrity()');
 IF body IS DISTINCT FROM format('SELECT COALESCE(pharmacy_operating_choice_integrity()AND pharmacy_stocktake_source_recovery_integrity()AND pharmacy_stocktake_hospital_support_state()=%L::jsonb,false)',before_state::text)THEN
  RAISE EXCEPTION 'hospital source seal predecessor changed' USING ERRCODE='23514';
 END IF;
 SELECT prosrc INTO STRICT body FROM pg_proc WHERE oid=to_regprocedure('pharmacy_stocktake_hospital_choice_integrity()');
 IF body IS DISTINCT FROM format('SELECT COALESCE(pharmacy_operating_choice_support()AND pharmacy_stocktake_hospital_choice_state()=%L::jsonb,false)',pharmacy_stocktake_hospital_choice_state()::text)THEN
  RAISE EXCEPTION 'hospital choice structure changed' USING ERRCODE='23514';
 END IF;
 IF(SELECT count(*)FROM jsonb_array_elements(before_state->'policies')entry WHERE entry->>0='pharmacy_stock_movements'AND entry->>1='tenant_isolation')<>1 THEN
  RAISE EXCEPTION 'expected one movement policy in source seal' USING ERRCODE='23514';
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
 IF pharmacy_operating_choice_integrity()IS DISTINCT FROM true THEN
  RAISE EXCEPTION 'pharmacy purpose policies changed' USING ERRCODE='23514';
 END IF;
 SELECT pharmacy_stocktake_hospital_support_state()INTO after_state;
 SELECT jsonb_agg(CASE WHEN entry->>0='pharmacy_stock_movements'AND entry->>1='tenant_isolation'
  THEN jsonb_set(entry,'{5}',to_jsonb('controlled-low-stock-read'::text))ELSE entry END ORDER BY ordinal)
 INTO before_policies FROM jsonb_array_elements(before_state->'policies')WITH ORDINALITY items(entry,ordinal);
 SELECT jsonb_agg(CASE WHEN entry->>0='pharmacy_stock_movements'AND entry->>1='tenant_isolation'
  THEN jsonb_set(entry,'{5}',to_jsonb('controlled-low-stock-read'::text))ELSE entry END ORDER BY ordinal)
 INTO after_policies FROM jsonb_array_elements(after_state->'policies')WITH ORDINALITY items(entry,ordinal);
 IF jsonb_set(before_state,'{policies}',before_policies)IS DISTINCT FROM jsonb_set(after_state,'{policies}',after_policies)THEN
  RAISE EXCEPTION 'unrelated hospital source authority changed' USING ERRCODE='23514';
 END IF;
 -- Re-pin the exact new clause only after the complete remaining source matches.
 EXECUTE format('CREATE OR REPLACE FUNCTION pharmacy_stocktake_hospital_integrity()RETURNS boolean LANGUAGE sql STABLE AS %L',format('SELECT COALESCE(pharmacy_operating_choice_integrity()AND pharmacy_stocktake_source_recovery_integrity()AND pharmacy_stocktake_hospital_support_state()=%L::jsonb,false)',after_state::text));
END $migration$;

-- Refuse a different safety rule rather than silently replacing it.
DO $predecessor$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM pg_proc WHERE oid=to_regprocedure('pharmacy_lot_block_reason(uuid,uuid,uuid,date)')
  AND md5(prosrc)='49f8639385777549c935e5484468a077' AND NOT prosecdef AND provolatile='s')
 OR NOT EXISTS(SELECT 1 FROM pg_proc WHERE oid=to_regprocedure('pharmacy_sellable_buckets(uuid,uuid,uuid,date)')
  AND md5(prosrc)='21349f380d9f3180342702cabe4fc0d0' AND NOT prosecdef AND provolatile='s') THEN
  RAISE EXCEPTION 'lot or sellable bucket predecessor changed' USING ERRCODE='23514';
 END IF;
END $predecessor$;

-- The one lot rule reads actual owner rows; scalar callers delegate to it.
CREATE FUNCTION pharmacy_lot_block_reasons(t uuid,o uuid,day date)
RETURNS TABLE(batch_id uuid,reason text)LANGUAGE sql STABLE AS $$
 SELECT b.id,CASE WHEN b.expires_on<=day THEN 'expired'
  WHEN b.mrp_minor IS NULL THEN 'mrp_missing'
  WHEN EXISTS(SELECT 1 FROM pharmacy_batch_safety_incidents i WHERE i.tenant_id=b.tenant_id AND i.outlet_id=b.outlet_id AND i.batch_id=b.id)THEN 'safety_incident'
  ELSE NULL::text END
 FROM pharmacy_stock_batches b WHERE day IS NOT NULL AND b.tenant_id=t AND b.outlet_id=o
$$;

CREATE OR REPLACE FUNCTION pharmacy_lot_block_reason(tenant uuid,outlet uuid,batch uuid,day date)
RETURNS text LANGUAGE plpgsql STABLE AS $$
DECLARE reason text;
BEGIN
 IF day IS NULL THEN RAISE EXCEPTION 'block rule needs a day' USING ERRCODE='22004';END IF;
 SELECT r.reason INTO reason FROM pharmacy_lot_block_reasons(tenant,outlet,day)r WHERE r.batch_id=batch;
 IF NOT FOUND THEN RAISE EXCEPTION 'lot not visible' USING ERRCODE='P0002';END IF;
 RETURN reason;
END $$;

-- One availability calculation serves both bulk alerts and existing product callers.
CREATE FUNCTION pharmacy_sellable_product_buckets(t uuid,o uuid,product uuid,day date)
RETURNS TABLE(product_id uuid,location_id uuid,batch_id uuid,lot_number text,expires_on date,mrp_minor bigint,on_hand bigint)
LANGUAGE plpgsql STABLE AS $$
BEGIN
 IF day IS NULL THEN RAISE EXCEPTION 'block rule needs a day' USING ERRCODE='22004';END IF;
 RETURN QUERY
 SELECT b.product_id,m.location_id,b.id,b.lot_number,b.expires_on,b.mrp_minor,
  (SUM(m.quantity_units)-pharmacy_supplier_reserved(t,o,m.location_id,b.id))::bigint
 FROM pharmacy_stock_batches b
 JOIN pharmacy_lot_block_reasons(t,o,day)r ON r.batch_id=b.id AND r.reason IS NULL
 JOIN pharmacy_stock_movements m ON m.tenant_id=b.tenant_id AND m.outlet_id=b.outlet_id AND m.batch_id=b.id
 JOIN pharmacy_stock_locations l ON l.tenant_id=m.tenant_id AND l.outlet_id=m.outlet_id AND l.id=m.location_id
 WHERE b.tenant_id=t AND b.outlet_id=o AND (product IS NULL OR b.product_id=product) AND l.stock_status='sellable'
 GROUP BY b.product_id,m.location_id,b.id,b.lot_number,b.expires_on,b.mrp_minor
 HAVING SUM(m.quantity_units)>pharmacy_supplier_reserved(t,o,m.location_id,b.id)
 ORDER BY b.expires_on,b.id,m.location_id;
END $$;

CREATE OR REPLACE FUNCTION pharmacy_sellable_buckets(tenant uuid,outlet uuid,product uuid,day date)
RETURNS TABLE(location_id uuid,batch_id uuid,lot_number text,expires_on date,mrp_minor bigint,on_hand bigint)
LANGUAGE sql STABLE AS $$
 SELECT s.location_id,s.batch_id,s.lot_number,s.expires_on,s.mrp_minor,s.on_hand
 FROM pharmacy_sellable_product_buckets(tenant,outlet,product,day)s
 WHERE product IS NOT NULL ORDER BY s.expires_on,s.batch_id,s.location_id
$$;

DO $$BEGIN
 IF pharmacy_operating_choice_integrity()IS DISTINCT FROM true OR pharmacy_stocktake_hospital_integrity()IS DISTINCT FROM true THEN
  RAISE EXCEPTION 'pharmacy source authority changed' USING ERRCODE='23514';
 END IF;
END $$;
