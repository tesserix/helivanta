SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- #1634: one rule decides whether a lot may leave a sellable shelf, on the outlet's own date.
CREATE FUNCTION pharmacy_outlet_today(tenant uuid,outlet uuid)RETURNS date LANGUAGE plpgsql VOLATILE AS $$
DECLARE zone text;
BEGIN
 SELECT timezone INTO zone FROM pharmacy_outlets WHERE tenant_id=tenant AND id=outlet;
 IF zone IS NULL THEN RAISE EXCEPTION 'outlet not visible' USING ERRCODE='P0002';END IF;
 RETURN(clock_timestamp()AT TIME ZONE zone)::date;
END $$;
-- Null means the lot may leave, so a call the rule cannot answer raises instead of returning null.
CREATE FUNCTION pharmacy_lot_block_reason(tenant uuid,outlet uuid,batch uuid,day date)RETURNS text LANGUAGE plpgsql STABLE AS $$
DECLARE b pharmacy_stock_batches;
BEGIN
 IF day IS NULL THEN RAISE EXCEPTION 'block rule needs a day' USING ERRCODE='22004';END IF;
 SELECT *INTO b FROM pharmacy_stock_batches WHERE tenant_id=tenant AND outlet_id=outlet AND id=batch;
 IF b.id IS NULL THEN RAISE EXCEPTION 'lot not visible' USING ERRCODE='P0002';END IF;
 IF b.expires_on<=day THEN RETURN 'expired';END IF;
 IF b.mrp_minor IS NULL THEN RETURN 'mrp_missing';END IF;
 IF EXISTS(SELECT 1 FROM pharmacy_batch_safety_incidents i WHERE i.tenant_id=tenant AND i.outlet_id=outlet AND i.batch_id=batch)THEN RETURN 'safety_incident';END IF;
 RETURN NULL;
END $$;
-- A frame sees a lot's incidents exactly when it sees the lot, so the rule never reads "no incident" for a hidden one.
ALTER POLICY tenant_isolation ON pharmacy_batch_safety_incidents USING(hms_tenant_visible(tenant_id)AND EXISTS(SELECT 1 FROM pharmacy_stock_batches b WHERE b.tenant_id=pharmacy_batch_safety_incidents.tenant_id AND b.outlet_id=pharmacy_batch_safety_incidents.outlet_id AND b.id=pharmacy_batch_safety_incidents.batch_id));
CREATE OR REPLACE FUNCTION pharmacy_sellable_buckets(tenant uuid,outlet uuid,product uuid,day date)RETURNS TABLE(location_id uuid,batch_id uuid,lot_number text,expires_on date,mrp_minor bigint,on_hand bigint)LANGUAGE sql STABLE AS $$
 SELECT m.location_id,b.id,b.lot_number,b.expires_on,b.mrp_minor,(SUM(m.quantity_units)-pharmacy_supplier_reserved(tenant,outlet,m.location_id,b.id))::bigint
 FROM pharmacy_stock_batches b JOIN pharmacy_stock_movements m ON m.tenant_id=b.tenant_id AND m.outlet_id=b.outlet_id AND m.batch_id=b.id JOIN pharmacy_stock_locations l ON l.tenant_id=m.tenant_id AND l.outlet_id=m.outlet_id AND l.id=m.location_id
 WHERE b.tenant_id=tenant AND b.outlet_id=outlet AND b.product_id=product AND l.stock_status='sellable'AND pharmacy_lot_block_reason(b.tenant_id,b.outlet_id,b.id,day)IS NULL
 GROUP BY m.location_id,b.id,b.lot_number,b.expires_on,b.mrp_minor HAVING SUM(m.quantity_units)>pharmacy_supplier_reserved(tenant,outlet,m.location_id,b.id)
 ORDER BY b.expires_on,b.id,m.location_id
$$;
CREATE OR REPLACE FUNCTION pharmacy_supplier_hold_guard()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE total bigint;held bigint;n bigint;a pharmacy_supplier_allocations;b pharmacy_stock_batches;status text;
BEGIN
 IF pg_trigger_depth()<2 THEN RAISE EXCEPTION 'holds are owner-derived' USING ERRCODE='42501';END IF;
 IF TG_OP='UPDATE'THEN IF NEW.reserved_units>OLD.reserved_units THEN RAISE EXCEPTION 'hold cannot grow' USING ERRCODE='23514';END IF;RETURN NEW;END IF;
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id::text||':'||NEW.location_id::text||':'||NEW.stock_location_id::text||':'||NEW.stock_id::text,0));
 SELECT *INTO a FROM pharmacy_supplier_allocations WHERE tenant_id=NEW.tenant_id AND id=NEW.allocation_id;
 SELECT *INTO b FROM pharmacy_stock_batches WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.location_id AND id=NEW.stock_id;
 SELECT stock_status INTO status FROM pharmacy_stock_locations WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.location_id AND id=NEW.stock_location_id;
 IF b.id IS NULL OR b.product_id::text IS DISTINCT FROM a.body#>>'{plan,product,product_id}'OR status IS DISTINCT FROM 'sellable'OR a.expires_at<=clock_timestamp()THEN RAISE EXCEPTION 'stock safety changed' USING ERRCODE='23514';END IF;
 IF pharmacy_lot_block_reason(NEW.tenant_id,NEW.location_id,NEW.stock_id,pharmacy_outlet_today(NEW.tenant_id,NEW.location_id))IS NOT NULL THEN RAISE EXCEPTION 'stock safety changed' USING ERRCODE='23514';END IF;
 SELECT COALESCE(SUM(quantity_units),0)INTO total FROM pharmacy_stock_movements WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.location_id AND location_id=NEW.stock_location_id AND batch_id=NEW.stock_id;
 SELECT COALESCE(SUM(reserved_units),0),COUNT(*)INTO held,n FROM pharmacy_supplier_holds WHERE tenant_id=NEW.tenant_id AND location_id=NEW.location_id AND stock_location_id=NEW.stock_location_id AND stock_id=NEW.stock_id AND reserved_units>0 AND expires_at>clock_timestamp();
 IF n>=1000 OR held+NEW.reserved_units>total THEN RAISE EXCEPTION 'physical stock reservation exceeded' USING ERRCODE='23514';END IF;RETURN NEW;
END $$;
CREATE OR REPLACE FUNCTION pharmacy_supplier_dispatch_guard()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE a pharmacy_supplier_allocations;c pharmacy_supplier_commands;status text;
BEGIN
 IF NEW.kind<>'supplier_dispatch'THEN RETURN NEW;END IF;
 SELECT *INTO c FROM pharmacy_supplier_commands WHERE tenant_id=NEW.tenant_id AND id=NEW.id;
 SELECT *INTO a FROM pharmacy_supplier_allocations WHERE tenant_id=NEW.tenant_id AND id=c.allocation_id;
 SELECT stock_status INTO status FROM pharmacy_stock_locations WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.location_id;
 IF c.id IS NULL OR a.id IS NULL OR a.location_id<>NEW.outlet_id OR a.stock_id<>NEW.batch_id OR a.stock_location_id<>NEW.location_id OR c.body#>>'{input,decision}'<>'dispatch'OR(c.body#>>'{input,quantity_units}')::bigint<>-NEW.quantity_units OR c.actor_subject<>NEW.actor_subject OR NEW.source_reference<>'supplier-dispatch:'||NEW.id::text OR status IS DISTINCT FROM 'sellable'OR a.expires_at<=clock_timestamp()THEN RAISE EXCEPTION 'physical dispatch evidence changed' USING ERRCODE='23514';END IF;
 IF pharmacy_lot_block_reason(NEW.tenant_id,NEW.outlet_id,NEW.batch_id,pharmacy_outlet_today(NEW.tenant_id,NEW.outlet_id))IS NOT NULL THEN RAISE EXCEPTION 'physical dispatch evidence changed' USING ERRCODE='23514';END IF;
 RETURN NEW;
END $$;
-- Only the correction guard's date changes; rewriting its one expression beats copying its body (plan D5).
DO $$DECLARE def text;fixed_zone text:=$x$b.expires_on>(clock_timestamp()AT TIME ZONE 'Asia/Kolkata')::date$x$;BEGIN
 def:=pg_get_functiondef('pharmacy_supplier_correction_guard()'::regprocedure);
 IF array_length(string_to_array(def,fixed_zone),1)<>2 THEN RAISE EXCEPTION 'correction guard date must appear exactly once';END IF;
 EXECUTE replace(def,fixed_zone,'b.expires_on>pharmacy_outlet_today(NEW.tenant_id,NEW.outlet_id)');
END $$;
