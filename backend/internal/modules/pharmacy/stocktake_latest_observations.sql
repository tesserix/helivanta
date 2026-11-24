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

DO $hospital_integrity$DECLARE reviewed jsonb;BEGIN
 reviewed:=pharmacy_stocktake_hospital_support_state();
 EXECUTE format('CREATE OR REPLACE FUNCTION pharmacy_stocktake_hospital_integrity()RETURNS boolean LANGUAGE sql STABLE AS %L',format('SELECT COALESCE(pharmacy_operating_choice_integrity()AND pharmacy_stocktake_hospital_support_state()=%L::jsonb,false)',reviewed::text));
END $hospital_integrity$;
