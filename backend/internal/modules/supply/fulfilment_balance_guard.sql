SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

ALTER POLICY tenant_isolation ON supply_stock_fulfilment_balances
WITH CHECK (
 pg_trigger_depth()=1
 AND supply_stock_scope(tenant_id,party_id,premise_id,'read')
 AND current_setting('app.supply_stock_permission',true)='supply.stock.fulfil'
 AND supply_supplier_scope(tenant_id,party_id,premise_id,'00000000-0000-0000-0000-000000000000','00000000-0000-0000-0000-000000000000',true)
);

CREATE FUNCTION supply_fulfilment_balance_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE quantity bigint; journal_xid xid; balance_xid xid;
BEGIN
 IF pg_trigger_depth()<>2 THEN
  RAISE EXCEPTION 'stock balances are journal-derived' USING ERRCODE='42501';
 END IF;
 IF TG_OP='UPDATE' AND (NEW.tenant_id,NEW.party_id,NEW.premise_id,NEW.receipt_id)
  IS DISTINCT FROM (OLD.tenant_id,OLD.party_id,OLD.premise_id,OLD.receipt_id) THEN
  RAISE EXCEPTION 'stock balance identity is immutable' USING ERRCODE='42501';
 END IF;
 SELECT d.quantity_units,d.xmin INTO quantity,journal_xid FROM supply_stock_dispatches d
  WHERE d.tenant_id=NEW.tenant_id AND d.id=NULLIF(current_setting('app.supply_balance_dispatch',true),'')::uuid
  AND d.party_id=NEW.party_id AND d.premise_id=NEW.premise_id AND d.receipt_id=NEW.receipt_id
  AND d.actor_subject=current_setting('app.actor_subject',true);
 IF NOT FOUND OR journal_xid<>pg_current_xact_id()::xid THEN
  RAISE EXCEPTION 'stock balance requires the current dispatch journal' USING ERRCODE='42501';
 END IF;
 SELECT b.xmin INTO balance_xid FROM supply_stock_fulfilment_balances b
  WHERE b.tenant_id=NEW.tenant_id AND b.party_id=NEW.party_id AND b.premise_id=NEW.premise_id AND b.receipt_id=NEW.receipt_id;
 IF balance_xid=pg_current_xact_id()::xid THEN
  RAISE EXCEPTION 'stock balance already posted in this command' USING ERRCODE='42501';
 END IF;
 IF (TG_OP='INSERT' AND NEW.dispatched_units<>quantity)
  OR (TG_OP='UPDATE' AND NEW.dispatched_units<>OLD.dispatched_units+quantity) THEN
  RAISE EXCEPTION 'stock balance differs from dispatch evidence'
   USING ERRCODE='23514',CONSTRAINT='supply_fulfilment_balance_evidence';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER supply_fulfilment_balance_guard BEFORE INSERT OR UPDATE ON supply_stock_fulfilment_balances
 FOR EACH ROW EXECUTE FUNCTION supply_fulfilment_balance_guard();
CREATE TRIGGER supply_fulfilment_balance_immutable BEFORE DELETE OR TRUNCATE ON supply_stock_fulfilment_balances
 FOR EACH STATEMENT EXECUTE FUNCTION supply_supplier_evidence_immutable();

CREATE OR REPLACE FUNCTION supply_fulfilment_count_dispatch() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE prior text;
BEGIN
 prior:=current_setting('app.supply_balance_dispatch',true);
 PERFORM set_config('app.supply_balance_dispatch',NEW.id::text,true);
 INSERT INTO supply_stock_fulfilment_balances(tenant_id,party_id,premise_id,receipt_id,dispatched_units)
 VALUES(NEW.tenant_id,NEW.party_id,NEW.premise_id,NEW.receipt_id,NEW.quantity_units)
 ON CONFLICT(tenant_id,party_id,premise_id,receipt_id) DO UPDATE
  SET dispatched_units=supply_stock_fulfilment_balances.dispatched_units+EXCLUDED.dispatched_units,updated_at=clock_timestamp();
 PERFORM set_config('app.supply_balance_dispatch',COALESCE(prior,''),true);
 RETURN NEW;
END $$;
