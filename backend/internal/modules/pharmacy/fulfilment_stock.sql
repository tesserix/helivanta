SET LOCAL lock_timeout='5s';
CREATE OR REPLACE FUNCTION pharmacy_operating_stock_scope(t uuid,o uuid,operation text)RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(hms_tenant_visible(t)AND t=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND o=NULLIF(current_setting('app.pharmacy_stock_outlet',true),'')::uuid AND current_setting('app.pharmacy_stock_access',true)='authorized'AND NULLIF(current_setting('app.pharmacy_subject',true),'')IS NOT NULL AND current_setting('app.pharmacy_subject',true)=current_setting('app.actor_subject',true)
 AND((operation='read'AND current_setting('app.pharmacy_stock_permission',true)IN('pharmacy.stock.read','pharmacy.stock.receive','pharmacy.stock.fulfil'))OR(operation='write'AND current_setting('app.pharmacy_stock_permission',true)='pharmacy.stock.receive'))
 AND EXISTS(SELECT 1 FROM pharmacy_outlets x WHERE x.tenant_id=t AND x.id=o AND x.pharmacy_id=NULLIF(current_setting('app.pharmacy_stock_pharmacy',true),'')::uuid),false)
$$;
ALTER TABLE pharmacy_supplier_allocations ADD CONSTRAINT pharmacy_supplier_batch FOREIGN KEY(tenant_id,location_id,stock_id)REFERENCES pharmacy_stock_batches(tenant_id,outlet_id,id)ON DELETE RESTRICT;
ALTER TABLE pharmacy_supplier_allocations ADD CONSTRAINT pharmacy_supplier_location FOREIGN KEY(tenant_id,location_id,stock_location_id)REFERENCES pharmacy_stock_locations(tenant_id,outlet_id,id)ON DELETE RESTRICT;
ALTER TABLE pharmacy_stock_movements DROP CONSTRAINT pharmacy_stock_movements_kind_check;
ALTER TABLE pharmacy_stock_movements ADD CONSTRAINT pharmacy_stock_movements_kind_check CHECK(kind IN('receipt','correction','release','sale','return','supplier_dispatch'))NOT VALID;
ALTER TABLE pharmacy_stock_movements VALIDATE CONSTRAINT pharmacy_stock_movements_kind_check;
DO $$DECLARE prior text;BEGIN
 SELECT pg_get_expr(polwithcheck,polrelid)INTO STRICT prior FROM pg_policy WHERE polrelid='pharmacy_stock_movements'::regclass AND polname='tenant_isolation';
 EXECUTE format('ALTER POLICY tenant_isolation ON pharmacy_stock_movements WITH CHECK((%s)OR(kind=''supplier_dispatch''AND current_setting(''app.pharmacy_stock_permission'',true)=''pharmacy.stock.fulfil''AND pharmacy_operating_stock_scope(tenant_id,outlet_id,''read'')AND actor_subject=current_setting(''app.actor_subject'',true)))',prior);
END $$;
CREATE FUNCTION pharmacy_supplier_reserved(t uuid,o uuid,l uuid,b uuid)RETURNS bigint LANGUAGE sql VOLATILE AS $$
 SELECT COALESCE(SUM(reserved_units),0)::bigint FROM pharmacy_supplier_holds WHERE tenant_id=t AND location_id=o AND stock_location_id=l AND stock_id=b AND reserved_units>0 AND expires_at>clock_timestamp()
$$;
CREATE OR REPLACE FUNCTION pharmacy_sellable_buckets(tenant uuid,outlet uuid,product uuid,day date)RETURNS TABLE(location_id uuid,batch_id uuid,lot_number text,expires_on date,mrp_minor bigint,on_hand bigint)LANGUAGE sql STABLE AS $$
 SELECT m.location_id,b.id,b.lot_number,b.expires_on,b.mrp_minor,(SUM(m.quantity_units)-pharmacy_supplier_reserved(tenant,outlet,m.location_id,b.id))::bigint
 FROM pharmacy_stock_batches b JOIN pharmacy_stock_movements m ON m.tenant_id=b.tenant_id AND m.outlet_id=b.outlet_id AND m.batch_id=b.id JOIN pharmacy_stock_locations l ON l.tenant_id=m.tenant_id AND l.outlet_id=m.outlet_id AND l.id=m.location_id
 WHERE b.tenant_id=tenant AND b.outlet_id=outlet AND b.product_id=product AND l.stock_status='sellable'AND b.mrp_minor IS NOT NULL AND b.expires_on>day AND NOT EXISTS(SELECT 1 FROM pharmacy_batch_safety_incidents i WHERE i.tenant_id=b.tenant_id AND i.outlet_id=b.outlet_id AND i.batch_id=b.id)
 GROUP BY m.location_id,b.id,b.lot_number,b.expires_on,b.mrp_minor HAVING SUM(m.quantity_units)>pharmacy_supplier_reserved(tenant,outlet,m.location_id,b.id)
 ORDER BY b.expires_on,b.id,m.location_id
$$;
CREATE OR REPLACE FUNCTION pharmacy_stock_quantity_guard()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE total numeric;held bigint;
BEGIN
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id::text||':'||NEW.outlet_id::text||':'||NEW.location_id::text||':'||NEW.batch_id::text,0));
 SELECT COALESCE(SUM(quantity_units),0)+NEW.quantity_units INTO total FROM pharmacy_stock_movements WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND location_id=NEW.location_id AND batch_id=NEW.batch_id;
 held:=pharmacy_supplier_reserved(NEW.tenant_id,NEW.outlet_id,NEW.location_id,NEW.batch_id);
 IF total<0 OR total>1000000000000 OR(NEW.quantity_units<0 AND total<held)THEN RAISE EXCEPTION 'physical stock is reserved or unavailable' USING ERRCODE='23514',CONSTRAINT='pharmacy_stock_quantity_bounds';END IF;RETURN NEW;
END $$;
CREATE FUNCTION pharmacy_supplier_hold_guard()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE total bigint;held bigint;n bigint;a pharmacy_supplier_allocations;b pharmacy_stock_batches;status text;
BEGIN
 IF pg_trigger_depth()<2 THEN RAISE EXCEPTION 'holds are owner-derived' USING ERRCODE='42501';END IF;
 IF TG_OP='UPDATE'THEN IF NEW.reserved_units>OLD.reserved_units THEN RAISE EXCEPTION 'hold cannot grow' USING ERRCODE='23514';END IF;RETURN NEW;END IF;
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id::text||':'||NEW.location_id::text||':'||NEW.stock_location_id::text||':'||NEW.stock_id::text,0));
 SELECT *INTO a FROM pharmacy_supplier_allocations WHERE tenant_id=NEW.tenant_id AND id=NEW.allocation_id;
 SELECT *INTO b FROM pharmacy_stock_batches WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.location_id AND id=NEW.stock_id;
 SELECT stock_status INTO status FROM pharmacy_stock_locations WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.location_id AND id=NEW.stock_location_id;
 IF b.id IS NULL OR b.product_id::text IS DISTINCT FROM a.body#>>'{plan,product,product_id}'OR status IS DISTINCT FROM 'sellable'OR b.expires_on<=(clock_timestamp()AT TIME ZONE 'Asia/Kolkata')::date OR a.expires_at<=clock_timestamp()OR EXISTS(SELECT 1 FROM pharmacy_batch_safety_incidents WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.location_id AND batch_id=NEW.stock_id)THEN RAISE EXCEPTION 'stock safety changed' USING ERRCODE='23514';END IF;
 SELECT COALESCE(SUM(quantity_units),0)INTO total FROM pharmacy_stock_movements WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.location_id AND location_id=NEW.stock_location_id AND batch_id=NEW.stock_id;
 SELECT COALESCE(SUM(reserved_units),0),COUNT(*)INTO held,n FROM pharmacy_supplier_holds WHERE tenant_id=NEW.tenant_id AND location_id=NEW.location_id AND stock_location_id=NEW.stock_location_id AND stock_id=NEW.stock_id AND reserved_units>0 AND expires_at>clock_timestamp();
 IF n>=1000 OR held+NEW.reserved_units>total THEN RAISE EXCEPTION 'physical stock reservation exceeded' USING ERRCODE='23514';END IF;RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_supplier_hold_guard BEFORE INSERT OR UPDATE ON pharmacy_supplier_holds FOR EACH ROW EXECUTE FUNCTION pharmacy_supplier_hold_guard();
CREATE FUNCTION pharmacy_supplier_dispatch_guard()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE a pharmacy_supplier_allocations;c pharmacy_supplier_commands;status text;
BEGIN
 IF NEW.kind<>'supplier_dispatch'THEN RETURN NEW;END IF;
 SELECT *INTO c FROM pharmacy_supplier_commands WHERE tenant_id=NEW.tenant_id AND id=NEW.id;
 SELECT *INTO a FROM pharmacy_supplier_allocations WHERE tenant_id=NEW.tenant_id AND id=c.allocation_id;
 SELECT stock_status INTO status FROM pharmacy_stock_locations WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.location_id;
 IF c.id IS NULL OR a.id IS NULL OR a.location_id<>NEW.outlet_id OR a.stock_id<>NEW.batch_id OR a.stock_location_id<>NEW.location_id OR c.body#>>'{input,decision}'<>'dispatch'OR(c.body#>>'{input,quantity_units}')::bigint<>-NEW.quantity_units OR c.actor_subject<>NEW.actor_subject OR NEW.source_reference<>'supplier-dispatch:'||NEW.id::text OR status IS DISTINCT FROM 'sellable'OR a.expires_at<=clock_timestamp()OR( a.body->>'expires_on')::date<=(clock_timestamp()AT TIME ZONE 'Asia/Kolkata')::date THEN RAISE EXCEPTION 'physical dispatch evidence changed' USING ERRCODE='23514';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_supplier_dispatch_guard BEFORE INSERT ON pharmacy_stock_movements FOR EACH ROW EXECUTE FUNCTION pharmacy_supplier_dispatch_guard();

DO $$DECLARE prior text;BEGIN
 SELECT pg_get_expr(polqual,polrelid)INTO STRICT prior FROM pg_policy WHERE polrelid='pharmacy_staff_department_bindings'::regclass AND polname='pharmacy_staff_binding_scope';
 EXECUTE format($policy$ALTER POLICY pharmacy_staff_binding_scope ON pharmacy_staff_department_bindings USING((%s)OR(pharmacy_operating_stock_scope(tenant_id,outlet_id,'read')AND current_setting('app.pharmacy_stock_permission',true)='pharmacy.stock.fulfil'AND hospital_id=NULLIF(current_setting('app.pharmacy_fulfilment_hospital',true),'')::uuid AND department_id=NULLIF(current_setting('app.pharmacy_fulfilment_department',true),'')::uuid))$policy$,prior);
END $$;
