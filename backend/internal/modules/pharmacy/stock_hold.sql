SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
DO $$BEGIN
 IF NOT pharmacy_operating_choice_integrity() THEN
  RAISE EXCEPTION 'existing pharmacy purpose policies changed' USING ERRCODE='23514';
 END IF;
END $$;

ALTER TABLE pharmacy_stock_movements DROP CONSTRAINT pharmacy_stock_movements_kind_check;
ALTER TABLE pharmacy_stock_movements ADD CONSTRAINT pharmacy_stock_movements_kind_check
 CHECK(kind IN('receipt','correction','release','sale','return','supplier_dispatch','quarantine_hold')) NOT VALID;
ALTER TABLE pharmacy_stock_movements VALIDATE CONSTRAINT pharmacy_stock_movements_kind_check;

CREATE TABLE pharmacy_stock_holds(
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 tenant_id uuid NOT NULL,
 outlet_id uuid NOT NULL,
 batch_id uuid NOT NULL,
 from_location_id uuid NOT NULL,
 quarantine_location_id uuid NOT NULL,
 quantity_units bigint NOT NULL CHECK(quantity_units BETWEEN 1 AND 1000000000),
 reason text NOT NULL CHECK(length(btrim(reason))BETWEEN 1 AND 500),
 physical_disposition text NOT NULL CHECK(physical_disposition='moved_to_quarantine'),
 out_movement_id uuid NOT NULL CHECK(substring(out_movement_id::text,15,1)='7'),
 in_movement_id uuid NOT NULL CHECK(substring(in_movement_id::text,15,1)='7'),
 intent_sha256 text NOT NULL CHECK(intent_sha256 ~ '^[0-9a-f]{64}$'),
 actor_subject text NOT NULL CHECK(length(btrim(actor_subject))BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,outlet_id,id),
 UNIQUE(tenant_id,outlet_id,out_movement_id),
 UNIQUE(tenant_id,outlet_id,in_movement_id),
 CHECK(from_location_id<>quarantine_location_id AND out_movement_id<>in_movement_id),
 FOREIGN KEY(tenant_id,outlet_id,batch_id)REFERENCES pharmacy_stock_batches(tenant_id,outlet_id,id)ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,outlet_id,from_location_id)REFERENCES pharmacy_stock_locations(tenant_id,outlet_id,id)ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,outlet_id,quarantine_location_id)REFERENCES pharmacy_stock_locations(tenant_id,outlet_id,id)ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,outlet_id,out_movement_id)REFERENCES pharmacy_stock_movements(tenant_id,outlet_id,id)ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,outlet_id,in_movement_id)REFERENCES pharmacy_stock_movements(tenant_id,outlet_id,id)ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED
);
ALTER TABLE pharmacy_stock_holds ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_stock_holds FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_stock_holds
 USING(hms_tenant_visible(tenant_id)AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND pharmacy_outlet_allowed(tenant_id,outlet_id,true)AND actor_subject=NULLIF(current_setting('app.pharmacy_subject',true),''));
CREATE INDEX pharmacy_stock_hold_batch ON pharmacy_stock_holds(tenant_id,outlet_id,batch_id);
CREATE INDEX pharmacy_stock_hold_source ON pharmacy_stock_holds(tenant_id,outlet_id,from_location_id);
CREATE INDEX pharmacy_stock_hold_quarantine ON pharmacy_stock_holds(tenant_id,outlet_id,quarantine_location_id);
CREATE TRIGGER pharmacy_stock_hold_audit AFTER INSERT ON pharmacy_stock_holds
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_stock_hold_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_stock_holds
 FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

CREATE FUNCTION pharmacy_stock_hold_parent_guard()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE source_status text;destination_status text;
BEGIN
 SELECT stock_status INTO source_status FROM pharmacy_stock_locations WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.from_location_id;
 SELECT stock_status INTO destination_status FROM pharmacy_stock_locations WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.quarantine_location_id;
 IF source_status IS DISTINCT FROM 'sellable'OR destination_status IS DISTINCT FROM 'quarantine'THEN
  RAISE EXCEPTION 'a hold moves sellable stock to quarantine' USING ERRCODE='23514',CONSTRAINT='pharmacy_stock_hold_locations';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_stock_hold_parent_guard BEFORE INSERT ON pharmacy_stock_holds
 FOR EACH ROW EXECUTE FUNCTION pharmacy_stock_hold_parent_guard();

CREATE FUNCTION pharmacy_stock_hold_movement_matches(m pharmacy_stock_movements,h pharmacy_stock_holds)RETURNS boolean LANGUAGE sql IMMUTABLE AS $$
 SELECT m.tenant_id=h.tenant_id AND m.outlet_id=h.outlet_id AND m.batch_id=h.batch_id AND m.kind='quarantine_hold'
 AND m.actor_subject=h.actor_subject AND m.reason=h.reason
 AND((m.id=h.out_movement_id AND m.location_id=h.from_location_id AND m.quantity_units=-h.quantity_units AND m.source_reference='hold:'||h.id::text||':out')
 OR(m.id=h.in_movement_id AND m.location_id=h.quarantine_location_id AND m.quantity_units=h.quantity_units AND m.source_reference='hold:'||h.id::text||':in'))
$$;
CREATE FUNCTION pharmacy_stock_hold_movement_guard()RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.kind='quarantine_hold'OR NEW.source_reference LIKE 'hold:%'THEN
  IF NOT EXISTS(SELECT 1 FROM pharmacy_stock_holds h WHERE h.tenant_id=NEW.tenant_id AND h.outlet_id=NEW.outlet_id
   AND(h.out_movement_id=NEW.id OR h.in_movement_id=NEW.id)AND pharmacy_stock_hold_movement_matches(NEW,h))THEN
   RAISE EXCEPTION 'hold movements require exact immutable evidence' USING ERRCODE='23514',CONSTRAINT='pharmacy_stock_hold_movement';
  END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_stock_hold_movement_guard BEFORE INSERT ON pharmacy_stock_movements
 FOR EACH ROW EXECUTE FUNCTION pharmacy_stock_hold_movement_guard();

CREATE FUNCTION pharmacy_stock_hold_pair_guard()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE legs bigint;
BEGIN
 SELECT COUNT(*)INTO legs FROM pharmacy_stock_movements m WHERE m.tenant_id=NEW.tenant_id AND m.outlet_id=NEW.outlet_id
 AND m.id IN(NEW.out_movement_id,NEW.in_movement_id)AND pharmacy_stock_hold_movement_matches(m,NEW);
 IF legs<>2 THEN
  RAISE EXCEPTION 'hold requires both completed physical movements' USING ERRCODE='23514',CONSTRAINT='pharmacy_stock_hold_pair';
 END IF;
 RETURN NEW;
END $$;
CREATE CONSTRAINT TRIGGER pharmacy_stock_hold_pair_guard AFTER INSERT ON pharmacy_stock_holds
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION pharmacy_stock_hold_pair_guard();

CREATE POLICY operating_choice_purpose ON pharmacy_stock_holds AS RESTRICTIVE
 USING(hms_tenant_visible(tenant_id)AND COALESCE(current_setting('app.pharmacy_operating_choices',true),'')='')
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND COALESCE(current_setting('app.pharmacy_operating_choices',true),'')='');
CREATE POLICY operating_choice_delete ON pharmacy_stock_holds AS RESTRICTIVE FOR DELETE
 USING(hms_tenant_visible(tenant_id)AND COALESCE(current_setting('app.pharmacy_operating_choices',true),'')='');

-- Extend the checked purpose inventory only after validating its existing fingerprint.
DO $integrity$
DECLARE policies text;functions text;policy_count bigint;
BEGIN
 SELECT string_agg(format('(%L::regclass,%L,%L)',p.polrelid::regclass::text,p.polname,md5(p.polcmd::text||p.polpermissive::text||p.polroles::text||COALESCE(pg_get_expr(p.polqual,p.polrelid),'')||COALESCE(pg_get_expr(p.polwithcheck,p.polrelid),''))),','),count(*)INTO policies,policy_count
 FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname=current_schema()AND(c.relname IN('pharmacy_organizations','pharmacy_outlets')OR starts_with(c.relname,'pharmacy_')AND p.polname IN('operating_choice_purpose','operating_choice_delete'));
 SELECT string_agg(format('(%L,%L)',p.oid::regprocedure::text,md5(pg_get_functiondef(p.oid))),',')INTO functions FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
 WHERE n.nspname=current_schema()AND p.proname='pharmacy_operating_choice_context';
 EXECUTE format($definition$CREATE OR REPLACE FUNCTION pharmacy_operating_choice_integrity()RETURNS boolean LANGUAGE sql STABLE AS $body$
 SELECT NOT EXISTS(SELECT 1 FROM(VALUES %s)expected(relation,name,digest)LEFT JOIN pg_policy p ON p.polrelid=expected.relation AND p.polname=expected.name
 WHERE p.oid IS NULL OR md5(p.polcmd::text||p.polpermissive::text||p.polroles::text||COALESCE(pg_get_expr(p.polqual,p.polrelid),'')||COALESCE(pg_get_expr(p.polwithcheck,p.polrelid),''))<>expected.digest)
 AND(SELECT count(*)FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema()AND(c.relname IN('pharmacy_organizations','pharmacy_outlets')OR starts_with(c.relname,'pharmacy_')AND p.polname IN('operating_choice_purpose','operating_choice_delete')))=%s
 AND NOT EXISTS(SELECT 1 FROM(VALUES %s)expected(name,digest)LEFT JOIN pg_proc p ON p.oid=to_regprocedure(expected.name)WHERE p.oid IS NULL OR md5(pg_get_functiondef(p.oid))<>expected.digest)
 $body$ $definition$,policies,policy_count,functions);
END $integrity$;
