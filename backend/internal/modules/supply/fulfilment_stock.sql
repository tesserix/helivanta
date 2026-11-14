SET LOCAL lock_timeout='5s';
CREATE OR REPLACE FUNCTION supply_stock_scope(t uuid,p uuid,l uuid,operation text)RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(t=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND p=NULLIF(current_setting('app.supply_stock_party',true),'')::uuid AND l=NULLIF(current_setting('app.supply_stock_premise',true),'')::uuid AND current_setting('app.supply_stock_access',true)='authorized'AND NULLIF(current_setting('app.supply_subject',true),'')IS NOT NULL AND current_setting('app.supply_subject',true)=current_setting('app.actor_subject',true)
 AND((operation='read'AND current_setting('app.supply_stock_permission',true)IN('supply.operations.read','supply.operations.write','supply.stock.review','supply.stock.fulfil'))OR(operation='write'AND current_setting('app.supply_stock_permission',true)='supply.operations.write')OR(operation='review'AND current_setting('app.supply_stock_permission',true)='supply.stock.review')),false)
$$;
ALTER TABLE supply_supplier_allocations ADD CONSTRAINT supply_allocation_receipt FOREIGN KEY(tenant_id,organization_id,location_id,stock_id)REFERENCES supply_stock_receipts(tenant_id,party_id,premise_id,id)ON DELETE RESTRICT;
CREATE TABLE supply_stock_fulfilment_balances(
 tenant_id uuid NOT NULL,party_id uuid NOT NULL,premise_id uuid NOT NULL,receipt_id uuid NOT NULL,
 dispatched_units bigint NOT NULL CHECK(dispatched_units BETWEEN 0 AND 9007199254740991),updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,party_id,premise_id,receipt_id),FOREIGN KEY(tenant_id,party_id,premise_id,receipt_id)REFERENCES supply_stock_receipts(tenant_id,party_id,premise_id,id)ON DELETE RESTRICT
);
ALTER TABLE supply_stock_fulfilment_balances ENABLE ROW LEVEL SECURITY;ALTER TABLE supply_stock_fulfilment_balances FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON supply_stock_fulfilment_balances USING(hms_tenant_visible(tenant_id)AND(supply_stock_scope(tenant_id,party_id,premise_id,'read')OR supply_supplier_scope(tenant_id,party_id,premise_id,'00000000-0000-0000-0000-000000000000','00000000-0000-0000-0000-000000000000',false)))WITH CHECK(pg_trigger_depth()>1 AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid);
CREATE TABLE supply_stock_dispatches(
 tenant_id uuid NOT NULL,id uuid NOT NULL,party_id uuid NOT NULL,premise_id uuid NOT NULL,receipt_id uuid NOT NULL,allocation_id uuid NOT NULL,quantity_units bigint NOT NULL CHECK(quantity_units BETWEEN 1 AND 1000000000),actor_subject text NOT NULL,created_at timestamptz NOT NULL,updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,id),FOREIGN KEY(tenant_id,id)REFERENCES supply_supplier_commands(tenant_id,id)ON DELETE RESTRICT,FOREIGN KEY(tenant_id,allocation_id)REFERENCES supply_supplier_allocations(tenant_id,id)ON DELETE RESTRICT,FOREIGN KEY(tenant_id,party_id,premise_id,receipt_id)REFERENCES supply_stock_receipts(tenant_id,party_id,premise_id,id)ON DELETE RESTRICT
);
CREATE INDEX ON supply_stock_dispatches(tenant_id,party_id,premise_id,receipt_id,created_at,id);
ALTER TABLE supply_stock_dispatches ENABLE ROW LEVEL SECURITY;ALTER TABLE supply_stock_dispatches FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON supply_stock_dispatches USING(hms_tenant_visible(tenant_id)AND supply_stock_scope(tenant_id,party_id,premise_id,'read'))WITH CHECK(supply_supplier_scope(tenant_id,party_id,premise_id,allocation_id,'00000000-0000-0000-0000-000000000000',true)AND current_setting('app.supply_stock_permission',true)='supply.stock.fulfil'AND actor_subject=current_setting('app.actor_subject',true));
CREATE TRIGGER supply_stock_dispatch_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON supply_stock_dispatches FOR EACH STATEMENT EXECUTE FUNCTION supply_supplier_evidence_immutable();
CREATE TRIGGER supply_stock_dispatch_audit AFTER INSERT ON supply_stock_dispatches FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE FUNCTION supply_fulfilment_physical_guard()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE r supply_stock_receipts;i supply_stock_identity_checks;q supply_stock_quality_decisions;a supply_supplier_allocations;c supply_supplier_commands;used bigint;held bigint;n bigint;
BEGIN
 IF TG_TABLE_NAME='supply_stock_dispatches'THEN
  SELECT *INTO a FROM supply_supplier_allocations WHERE tenant_id=NEW.tenant_id AND id=NEW.allocation_id;
  SELECT *INTO c FROM supply_supplier_commands WHERE tenant_id=NEW.tenant_id AND id=NEW.id;
  IF a.id IS NULL OR c.id IS NULL OR c.allocation_id<>a.id OR a.organization_id<>NEW.party_id OR a.location_id<>NEW.premise_id OR a.stock_id<>NEW.receipt_id OR c.body#>>'{input,decision}'<>'dispatch'OR(c.body#>>'{input,quantity_units}')::bigint<>NEW.quantity_units OR c.actor_subject<>NEW.actor_subject THEN RAISE EXCEPTION 'dispatch evidence differs' USING ERRCODE='23514';END IF;
  SELECT *INTO r FROM supply_stock_receipts WHERE tenant_id=NEW.tenant_id AND party_id=NEW.party_id AND premise_id=NEW.premise_id AND id=NEW.receipt_id FOR UPDATE;
 ELSE
  IF pg_trigger_depth()<2 THEN RAISE EXCEPTION 'holds are owner-derived' USING ERRCODE='42501';END IF;
  IF TG_OP='UPDATE'THEN IF NEW.reserved_units>OLD.reserved_units THEN RAISE EXCEPTION 'hold cannot grow' USING ERRCODE='23514';END IF;RETURN NEW;END IF;
  SELECT *INTO a FROM supply_supplier_allocations WHERE tenant_id=NEW.tenant_id AND id=NEW.allocation_id;
  SELECT *INTO r FROM supply_stock_receipts WHERE tenant_id=NEW.tenant_id AND party_id=NEW.organization_id AND premise_id=NEW.location_id AND id=NEW.stock_id FOR UPDATE;
 END IF;
 IF r.id IS NULL OR r.expires_on<=(clock_timestamp()AT TIME ZONE 'Asia/Kolkata')::date OR a.expires_at<=clock_timestamp()THEN RAISE EXCEPTION 'stock or lease expired' USING ERRCODE='23514';END IF;
 SELECT *INTO i FROM supply_stock_identity_checks WHERE tenant_id=r.tenant_id AND party_id=r.party_id AND premise_id=r.premise_id AND receipt_id=r.id ORDER BY version DESC LIMIT 1;
 SELECT *INTO q FROM supply_stock_quality_decisions WHERE tenant_id=r.tenant_id AND party_id=r.party_id AND premise_id=r.premise_id AND receipt_id=r.id ORDER BY version DESC LIMIT 1;
 IF i.id IS NULL OR q.id IS NULL OR q.decision<>'release'OR q.identity_check_id<>i.id OR q.identity_version<>i.version OR q.released_base_units<>i.quantity_base_units OR i.product_version_id::text IS DISTINCT FROM a.body#>>'{plan,product,version_id}'THEN RAISE EXCEPTION 'stock identity or quality changed' USING ERRCODE='23514';END IF;
 SELECT COALESCE(dispatched_units,0)INTO used FROM supply_stock_fulfilment_balances WHERE tenant_id=r.tenant_id AND party_id=r.party_id AND premise_id=r.premise_id AND receipt_id=r.id;used:=COALESCE(used,0);
 SELECT COALESCE(SUM(reserved_units),0),COUNT(*)INTO held,n FROM supply_supplier_holds WHERE tenant_id=r.tenant_id AND location_id=r.premise_id AND stock_id=r.id AND reserved_units>0 AND expires_at>clock_timestamp();
 IF TG_TABLE_NAME='supply_stock_dispatches'THEN
  IF used+held+NEW.quantity_units>q.released_base_units THEN RAISE EXCEPTION 'physical stock is committed' USING ERRCODE='23514';END IF;
 ELSE
  IF n>=1000 OR used+held+NEW.reserved_units>q.released_base_units THEN RAISE EXCEPTION 'physical stock reservation exceeded' USING ERRCODE='23514';END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER supply_fulfilment_hold_guard BEFORE INSERT OR UPDATE ON supply_supplier_holds FOR EACH ROW EXECUTE FUNCTION supply_fulfilment_physical_guard();
CREATE TRIGGER supply_fulfilment_dispatch_guard BEFORE INSERT ON supply_stock_dispatches FOR EACH ROW EXECUTE FUNCTION supply_fulfilment_physical_guard();
CREATE FUNCTION supply_fulfilment_count_dispatch()RETURNS trigger LANGUAGE plpgsql AS $$BEGIN
 INSERT INTO supply_stock_fulfilment_balances(tenant_id,party_id,premise_id,receipt_id,dispatched_units)VALUES(NEW.tenant_id,NEW.party_id,NEW.premise_id,NEW.receipt_id,NEW.quantity_units)
 ON CONFLICT(tenant_id,party_id,premise_id,receipt_id)DO UPDATE SET dispatched_units=supply_stock_fulfilment_balances.dispatched_units+EXCLUDED.dispatched_units,updated_at=clock_timestamp();RETURN NEW;END $$;
CREATE TRIGGER supply_fulfilment_count_dispatch AFTER INSERT ON supply_stock_dispatches FOR EACH ROW EXECUTE FUNCTION supply_fulfilment_count_dispatch();
CREATE FUNCTION supply_stock_identity_fulfilment_guard()RETURNS trigger LANGUAGE plpgsql AS $$BEGIN
 IF EXISTS(SELECT 1 FROM supply_stock_fulfilment_balances WHERE tenant_id=NEW.tenant_id AND party_id=NEW.party_id AND premise_id=NEW.premise_id AND receipt_id=NEW.receipt_id AND dispatched_units>0)OR EXISTS(SELECT 1 FROM supply_supplier_holds WHERE tenant_id=NEW.tenant_id AND organization_id=NEW.party_id AND location_id=NEW.premise_id AND stock_id=NEW.receipt_id AND reserved_units>0 AND expires_at>clock_timestamp())THEN RAISE EXCEPTION 'committed stock identity requires reconciliation' USING ERRCODE='23514';END IF;RETURN NEW;END $$;
CREATE TRIGGER supply_stock_identity_fulfilment_guard BEFORE INSERT ON supply_stock_identity_checks FOR EACH ROW EXECUTE FUNCTION supply_stock_identity_fulfilment_guard();
