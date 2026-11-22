SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE FUNCTION pharmacy_stocktake_snapshot_insert_guard()RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM s.id FROM pharmacy_stocktakes s JOIN(
  SELECT DISTINCT tenant_id,outlet_id,stocktake_id FROM inserted_lines
 )i ON(s.tenant_id,s.outlet_id,s.id)=(i.tenant_id,i.outlet_id,i.stocktake_id)
 ORDER BY s.tenant_id,s.outlet_id,s.id FOR UPDATE OF s;
 IF EXISTS(
  SELECT 1 FROM inserted_lines i LEFT JOIN pharmacy_stocktakes s
   ON(s.tenant_id,s.outlet_id,s.id)=(i.tenant_id,i.outlet_id,i.stocktake_id)
  WHERE s.id IS NULL OR s.sealed OR s.actor_subject IS DISTINCT FROM NULLIF(current_setting('app.pharmacy_subject',true),'')OR i.created_at<>s.created_at
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
