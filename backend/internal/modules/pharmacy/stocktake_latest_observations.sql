SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- The migration owner has DDL privileges; runtime choice support also rejects TRUNCATE privilege.
-- Keep that runtime seal unchanged and verify the role-neutral source seals here.
DO $$BEGIN
 IF pharmacy_stocktake_hospital_integrity()IS DISTINCT FROM true OR pharmacy_operating_choice_integrity()IS DISTINCT FROM true THEN
  RAISE EXCEPTION 'stocktake observation planning requires unchanged owner authority';
 END IF;
END $$;

CREATE OR REPLACE FUNCTION pharmacy_stocktake_differences(t uuid,o uuid,s uuid)RETURNS TABLE(line_id uuid,batch_id uuid,from_location_id uuid,quantity_units bigint)LANGUAGE sql STABLE AS $$
 WITH latest AS MATERIALIZED(
  SELECT DISTINCT ON(line_id)line_id,counted_units
  FROM pharmacy_stocktake_counts
  WHERE tenant_id=t AND outlet_id=o AND stocktake_id=s
  ORDER BY line_id,command_version DESC
 )
 SELECT l.id,l.batch_id,l.location_id,e.counted_units-l.book_units
 FROM pharmacy_stocktake_lines l JOIN latest e ON e.line_id=l.id
 WHERE l.tenant_id=t AND l.outlet_id=o AND l.stocktake_id=s AND e.counted_units<>l.book_units
$$;

-- Batch validation remains authoritative for actor, transaction, quantity,
-- destination and timestamp. Share only that immutable timestamp across rows.
CREATE OR REPLACE FUNCTION pharmacy_stocktake_adjustment_guard()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE command pharmacy_stocktake_commands;stamp timestamptz:=NULLIF(current_setting('app.pharmacy_stocktake_command_time',true),'')::timestamptz;
BEGIN
 IF stamp IS NOT NULL THEN NEW.created_at:=stamp;RETURN NEW;END IF;
 SELECT *INTO command FROM pharmacy_stocktake_commands WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.command_id AND stocktake_id=NEW.stocktake_id;
 IF command.id IS NULL OR command.kind<>'approve'OR command.posted_xact<>pg_current_xact_id()OR command.actor_subject IS DISTINCT FROM NULLIF(current_setting('app.pharmacy_subject',true),'')THEN
  RAISE EXCEPTION 'correction is outside its reviewed count' USING ERRCODE='23514',CONSTRAINT='pharmacy_stocktake_adjustment';
 END IF;
 NEW.created_at:=command.created_at;
 RETURN NEW;
END $$;

-- During one native snapshot, evaluate the existing access predicate once for
-- its held coordinates. Every other policy clause and all write checks remain.
DO $native_snapshot$
DECLARE target record; prior text; revised text; matched text[]; replacement text;
BEGIN
 FOR target IN SELECT p.polrelid,p.polname,c.relname FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid
 WHERE c.relnamespace=current_schema()::regnamespace AND p.polname='tenant_isolation'
 AND c.relname IN('pharmacy_catalogue_products','pharmacy_catalogue_revisions','pharmacy_stock_batches','pharmacy_stock_locations','pharmacy_stock_movements','pharmacy_product_schedule_declarations','pharmacy_product_schedule_reviews')LOOP
  SELECT pg_get_expr(polqual,polrelid)INTO STRICT prior FROM pg_policy WHERE polrelid=target.polrelid AND polname=target.polname;
  matched:=regexp_match(prior,'((public[.])?pharmacy_outlet_allowed[a-z0-9_]*)[(]tenant_id, outlet_id, false[)]');
  IF matched IS NULL THEN RAISE EXCEPTION 'native snapshot legacy predicate missing on %',target.relname;END IF;
  replacement:=format($predicate$(CASE WHEN current_setting('app.pharmacy_stocktake_native_snapshot',true)='1'
   THEN tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
    AND outlet_id=NULLIF(current_setting('app.pharmacy_outlet',true),'')::uuid
    AND(SELECT %s(NULLIF(current_setting('app.tenant_id',true),'')::uuid,NULLIF(current_setting('app.pharmacy_outlet',true),'')::uuid,false))
   ELSE %s(tenant_id,outlet_id,false)END)$predicate$,matched[1],matched[1]);
  revised:=regexp_replace(prior,'((public[.])?pharmacy_outlet_allowed[a-z0-9_]*)[(]tenant_id, outlet_id, false[)]',replacement,'g');
  EXECUTE format('ALTER POLICY %I ON %I USING(%s)',target.polname,target.relname,revised);
 END LOOP;
END $native_snapshot$;

DO $hospital_integrity$DECLARE reviewed jsonb;BEGIN
 reviewed:=pharmacy_stocktake_hospital_support_state();
 EXECUTE format('CREATE OR REPLACE FUNCTION pharmacy_stocktake_hospital_integrity()RETURNS boolean LANGUAGE sql STABLE AS %L',format('SELECT COALESCE(pharmacy_operating_choice_integrity()AND pharmacy_stocktake_hospital_support_state()=%L::jsonb,false)',reviewed::text));
END $hospital_integrity$;
