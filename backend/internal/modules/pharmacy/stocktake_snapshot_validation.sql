SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE FUNCTION pharmacy_stocktake_snapshot_insert_guard()RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM s.id FROM pharmacy_stocktakes s JOIN(
  SELECT DISTINCT tenant_id,outlet_id,stocktake_id FROM inserted_lines
 )i ON(s.tenant_id,s.outlet_id,s.id)=(i.tenant_id,i.outlet_id,i.stocktake_id)
 ORDER BY s.tenant_id,s.outlet_id,s.id FOR UPDATE OF s;
 IF EXISTS(
  SELECT 1 FROM(
   SELECT tenant_id,outlet_id,stocktake_id,min(created_at) first_created,max(created_at) last_created
   FROM inserted_lines GROUP BY tenant_id,outlet_id,stocktake_id
  )i LEFT JOIN pharmacy_stocktakes s
   ON(s.tenant_id,s.outlet_id,s.id)=(i.tenant_id,i.outlet_id,i.stocktake_id)
  WHERE s.id IS NULL OR s.sealed OR s.actor_subject IS DISTINCT FROM NULLIF(current_setting('app.pharmacy_subject',true),'')OR i.first_created<>s.created_at OR i.last_created<>s.created_at
 )THEN
  RAISE EXCEPTION 'stocktake snapshot is sealed or unavailable' USING ERRCODE='23514',CONSTRAINT='pharmacy_stocktake_sealed';
 END IF;
 IF EXISTS(
  SELECT 1 FROM inserted_lines i LEFT JOIN pharmacy_stock_batches b
   ON(b.tenant_id,b.outlet_id,b.id)=(i.tenant_id,i.outlet_id,i.batch_id)
  WHERE b.id IS NULL OR(b.product_id,b.lot_number,b.expires_on)IS DISTINCT FROM(i.product_id,i.lot_number,i.expires_on)
 )THEN
  RAISE EXCEPTION 'stocktake batch identity changed' USING ERRCODE='23514',CONSTRAINT='pharmacy_stocktake_batch';
 END IF;
 RETURN NULL;
END $$;

DROP TRIGGER pharmacy_stocktake_line_guard ON pharmacy_stocktake_lines;
CREATE TRIGGER pharmacy_stocktake_line_guard AFTER INSERT ON pharmacy_stocktake_lines
 REFERENCING NEW TABLE AS inserted_lines FOR EACH STATEMENT
 EXECUTE FUNCTION pharmacy_stocktake_snapshot_insert_guard();

DO $scope$DECLARE target text;BEGIN
 FOREACH target IN ARRAY ARRAY['pharmacy_stocktakes','pharmacy_stocktake_lines','pharmacy_stocktake_commands','pharmacy_stocktake_counts','pharmacy_stocktake_adjustments'] LOOP
  EXECUTE format($policy$ALTER POLICY tenant_isolation ON %I
   USING(hms_tenant_visible(tenant_id)
    AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
    AND outlet_id=NULLIF(current_setting('app.pharmacy_outlet',true),'')::uuid
    AND(SELECT pharmacy_outlet_allowed(NULLIF(current_setting('app.tenant_id',true),'')::uuid,NULLIF(current_setting('app.pharmacy_outlet',true),'')::uuid,false)))
   WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
    AND outlet_id=NULLIF(current_setting('app.pharmacy_outlet',true),'')::uuid
    AND(SELECT pharmacy_outlet_allowed(NULLIF(current_setting('app.tenant_id',true),'')::uuid,NULLIF(current_setting('app.pharmacy_outlet',true),'')::uuid,true)))$policy$,target);
 END LOOP;
END $scope$;

CREATE FUNCTION pharmacy_stocktake_snapshot_insert_audit()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE actor text:=NULLIF(current_setting('app.actor_subject',true),'');
BEGIN
 IF actor IS NULL THEN
  RAISE EXCEPTION 'actor subject is required for an audited write' USING ERRCODE='42501';
 END IF;
 INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id,changed_fields)
 SELECT tenant_id,actor,'insert','pharmacy_stocktake_lines',id,'{}'::text[] FROM inserted_lines;
 RETURN NULL;
END $$;
DROP TRIGGER pharmacy_stocktake_lines_audit ON pharmacy_stocktake_lines;
CREATE TRIGGER pharmacy_stocktake_lines_audit AFTER INSERT ON pharmacy_stocktake_lines
 REFERENCING NEW TABLE AS inserted_lines FOR EACH STATEMENT
 EXECUTE FUNCTION pharmacy_stocktake_snapshot_insert_audit();

CREATE OR REPLACE FUNCTION pharmacy_stocktake_adjustment_guard()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE command pharmacy_stocktake_commands;
BEGIN
 SELECT *INTO command FROM pharmacy_stocktake_commands WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.command_id AND stocktake_id=NEW.stocktake_id;
 IF command.id IS NULL OR command.kind<>'approve'OR command.posted_xact<>pg_current_xact_id()OR command.actor_subject IS DISTINCT FROM NULLIF(current_setting('app.pharmacy_subject',true),'')THEN
  RAISE EXCEPTION 'correction is outside its reviewed count' USING ERRCODE='23514',CONSTRAINT='pharmacy_stocktake_adjustment';
 END IF;
 NEW.created_at:=command.created_at;
 RETURN NEW;
END $$;

CREATE FUNCTION pharmacy_stocktake_adjustment_insert_guard()RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(
  SELECT 1 FROM inserted_adjustments i
  LEFT JOIN pharmacy_stocktake_commands c ON(c.tenant_id,c.outlet_id,c.id,c.stocktake_id)=(i.tenant_id,i.outlet_id,i.command_id,i.stocktake_id)
  LEFT JOIN pharmacy_stocktakes p ON(p.tenant_id,p.outlet_id,p.id)=(i.tenant_id,i.outlet_id,i.stocktake_id)
  LEFT JOIN pharmacy_stocktake_lines l ON(l.tenant_id,l.outlet_id,l.id,l.stocktake_id)=(i.tenant_id,i.outlet_id,i.line_id,i.stocktake_id)
  LEFT JOIN LATERAL(
   SELECT counted_units FROM pharmacy_stocktake_counts e
   WHERE(e.tenant_id,e.outlet_id,e.stocktake_id,e.line_id)=(i.tenant_id,i.outlet_id,i.stocktake_id,i.line_id)
   ORDER BY command_version DESC LIMIT 1
  )observed ON true
  WHERE c.id IS NULL OR p.id IS NULL OR NOT p.sealed OR l.id IS NULL
   OR c.kind<>'approve'OR c.posted_xact<>pg_current_xact_id()
   OR c.actor_subject IS DISTINCT FROM NULLIF(current_setting('app.pharmacy_subject',true),'')
   OR i.created_at IS DISTINCT FROM c.created_at
   OR i.batch_id IS DISTINCT FROM l.batch_id
   OR i.quantity_units IS DISTINCT FROM NULLIF(observed.counted_units-l.book_units,0)
   OR i.location_id IS DISTINCT FROM(CASE WHEN observed.counted_units>l.book_units THEN p.quarantine_location_id ELSE l.location_id END)
 )THEN
  RAISE EXCEPTION 'correction is outside its reviewed count' USING ERRCODE='23514',CONSTRAINT='pharmacy_stocktake_adjustment';
 END IF;
 RETURN NULL;
END $$;
CREATE TRIGGER pharmacy_stocktake_adjustment_insert_guard AFTER INSERT ON pharmacy_stocktake_adjustments
 REFERENCING NEW TABLE AS inserted_adjustments FOR EACH STATEMENT
 EXECUTE FUNCTION pharmacy_stocktake_adjustment_insert_guard();
