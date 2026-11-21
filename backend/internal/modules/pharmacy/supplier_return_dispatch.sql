SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
DO $$BEGIN
 IF NOT pharmacy_operating_choice_integrity() THEN
  RAISE EXCEPTION 'existing pharmacy purpose policies changed' USING ERRCODE='23514';
 END IF;
END $$;

ALTER TABLE pharmacy_stock_movements DROP CONSTRAINT pharmacy_stock_movements_kind_check;
ALTER TABLE pharmacy_stock_movements ADD CONSTRAINT pharmacy_stock_movements_kind_check
 CHECK(kind IN('receipt','correction','release','sale','return','supplier_dispatch','quarantine_hold','supplier_return')) NOT VALID;
ALTER TABLE pharmacy_stock_movements VALIDATE CONSTRAINT pharmacy_stock_movements_kind_check;

CREATE TABLE pharmacy_supplier_return_dispatches(
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),tenant_id uuid NOT NULL,outlet_id uuid NOT NULL,return_id uuid NOT NULL,
 licence_id uuid NOT NULL,licence_review_id uuid NOT NULL,
 document_reference text NOT NULL CHECK(length(btrim(document_reference))BETWEEN 1 AND 100),
 recipient_name text NOT NULL CHECK(length(btrim(recipient_name))BETWEEN 1 AND 200),
 handed_over_at timestamptz NOT NULL CHECK(handed_over_at>='1970-01-01'),
 physical_disposition text NOT NULL CHECK(physical_disposition='handed_to_supplier'),
 reason text NOT NULL CHECK(length(btrim(reason))BETWEEN 1 AND 500),
 intent_sha256 text NOT NULL CHECK(intent_sha256~'^[a-f0-9]{64}$'),
 actor_subject text NOT NULL CHECK(length(btrim(actor_subject))BETWEEN 1 AND 200),created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,outlet_id,id),UNIQUE(tenant_id,outlet_id,return_id),
 FOREIGN KEY(tenant_id,outlet_id,return_id)REFERENCES pharmacy_supplier_returns(tenant_id,outlet_id,id)ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,outlet_id,licence_id)REFERENCES pharmacy_outlet_licences(tenant_id,outlet_id,id)ON DELETE RESTRICT
);
CREATE TABLE pharmacy_supplier_return_dispatch_lines(
 tenant_id uuid NOT NULL,outlet_id uuid NOT NULL,dispatch_id uuid NOT NULL,return_line_id uuid NOT NULL,
 movement_id uuid NOT NULL CHECK(substring(movement_id::text,15,1)='7'),schedule_declaration_id uuid NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,outlet_id,dispatch_id,return_line_id),UNIQUE(tenant_id,outlet_id,return_line_id),UNIQUE(tenant_id,outlet_id,movement_id),
 FOREIGN KEY(tenant_id,outlet_id,dispatch_id)REFERENCES pharmacy_supplier_return_dispatches(tenant_id,outlet_id,id)ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,outlet_id,return_line_id)REFERENCES pharmacy_supplier_return_lines(tenant_id,outlet_id,id)ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,outlet_id,movement_id)REFERENCES pharmacy_stock_movements(tenant_id,outlet_id,id)ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(tenant_id,outlet_id,schedule_declaration_id)REFERENCES pharmacy_product_schedule_declarations(tenant_id,outlet_id,id)ON DELETE RESTRICT
);
CREATE INDEX pharmacy_supplier_return_dispatch_licence ON pharmacy_supplier_return_dispatches(tenant_id,outlet_id,licence_id);
CREATE INDEX pharmacy_supplier_return_dispatch_schedule ON pharmacy_supplier_return_dispatch_lines(tenant_id,outlet_id,schedule_declaration_id);

DO $policies$
DECLARE target text;
BEGIN
 FOREACH target IN ARRAY ARRAY['pharmacy_supplier_return_dispatches','pharmacy_supplier_return_dispatch_lines'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',target);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',target);
  EXECUTE format($policy$CREATE POLICY tenant_isolation ON %I
   USING(hms_tenant_visible(tenant_id)AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
   WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND pharmacy_outlet_allowed(tenant_id,outlet_id,true))$policy$,target);
  EXECUTE format($policy$CREATE POLICY operating_choice_purpose ON %I AS RESTRICTIVE
   USING(hms_tenant_visible(tenant_id)AND COALESCE(current_setting('app.pharmacy_operating_choices',true),'')='')
   WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND COALESCE(current_setting('app.pharmacy_operating_choices',true),'')='')$policy$,target);
  EXECUTE format($policy$CREATE POLICY operating_choice_delete ON %I AS RESTRICTIVE FOR DELETE
   USING(hms_tenant_visible(tenant_id)AND COALESCE(current_setting('app.pharmacy_operating_choices',true),'')='')$policy$,target);
  EXECUTE format('CREATE TRIGGER pharmacy_supplier_return_dispatch_audit AFTER INSERT ON %I FOR EACH ROW EXECUTE FUNCTION audit_record_change()',target);
  EXECUTE format('CREATE TRIGGER pharmacy_supplier_return_dispatch_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable()',target);
 END LOOP;
END $policies$;

CREATE FUNCTION pharmacy_supplier_return_dispatch_guard()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE item record;review uuid;
BEGIN
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id::text||':'||NEW.outlet_id::text||':supplier-return:'||NEW.return_id::text,0));
 IF NEW.actor_subject IS DISTINCT FROM NULLIF(current_setting('app.pharmacy_subject',true),'')OR
  EXISTS(SELECT 1 FROM pharmacy_supplier_return_cancellations c WHERE c.tenant_id=NEW.tenant_id AND c.outlet_id=NEW.outlet_id AND c.return_id=NEW.return_id)THEN
  RAISE EXCEPTION 'dispatch requires a current draft and actor' USING ERRCODE='23514',CONSTRAINT='pharmacy_supplier_return_dispatch_state';
 END IF;
 SELECT review_id INTO review FROM pharmacy_trading_licence_source(NEW.tenant_id,NEW.outlet_id,NEW.licence_id);
 IF review IS NULL OR review IS DISTINCT FROM NEW.licence_review_id THEN
  RAISE EXCEPTION 'dispatch requires current reviewed outlet licence' USING ERRCODE='23514',CONSTRAINT='pharmacy_supplier_return_dispatch_licence';
 END IF;
 FOR item IN SELECT DISTINCT product_id FROM pharmacy_supplier_return_lines l WHERE l.tenant_id=NEW.tenant_id AND l.outlet_id=NEW.outlet_id AND l.return_id=NEW.return_id ORDER BY product_id LOOP
  PERFORM pharmacy_schedule_lock(item.product_id);
  IF NOT EXISTS(SELECT 1 FROM pharmacy_product_schedule_in_force(NEW.tenant_id,NEW.outlet_id,item.product_id) s WHERE s.regulatory_class<>'X'AND NOT s.ndps)THEN
   RAISE EXCEPTION 'dispatch requires supported current product classification' USING ERRCODE='23514',CONSTRAINT='pharmacy_supplier_return_dispatch_schedule';
  END IF;
 END LOOP;
 FOR item IN SELECT receipt_id,MIN(receipt_units) receipt_units,SUM(quantity_units) quantity_units,MAX(source_created_at) source_created_at
  FROM pharmacy_supplier_return_lines l WHERE l.tenant_id=NEW.tenant_id AND l.outlet_id=NEW.outlet_id AND l.return_id=NEW.return_id GROUP BY receipt_id ORDER BY receipt_id LOOP
  PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id::text||':'||NEW.outlet_id::text||':supplier-return-receipt:'||item.receipt_id::text,0));
  IF NEW.handed_over_at<item.source_created_at OR NEW.handed_over_at>clock_timestamp()OR
   item.quantity_units+COALESCE((SELECT SUM(l.quantity_units) FROM pharmacy_supplier_return_lines l
    JOIN pharmacy_supplier_return_dispatches d ON d.tenant_id=l.tenant_id AND d.outlet_id=l.outlet_id AND d.return_id=l.return_id
    WHERE l.tenant_id=NEW.tenant_id AND l.outlet_id=NEW.outlet_id AND l.receipt_id=item.receipt_id),0)>item.receipt_units THEN
   RAISE EXCEPTION 'dispatch exceeds original receipt or invalid handoff time' USING ERRCODE='23514',CONSTRAINT='pharmacy_supplier_return_dispatch_receipt_cap';
  END IF;
 END LOOP;
 FOR item IN SELECT DISTINCT quarantine_location_id,batch_id FROM pharmacy_supplier_return_lines l
  WHERE l.tenant_id=NEW.tenant_id AND l.outlet_id=NEW.outlet_id AND l.return_id=NEW.return_id ORDER BY quarantine_location_id,batch_id LOOP
  PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id::text||':'||NEW.outlet_id::text||':'||item.quarantine_location_id::text||':'||item.batch_id::text,0));
 END LOOP;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_supplier_return_dispatch_guard BEFORE INSERT ON pharmacy_supplier_return_dispatches FOR EACH ROW EXECUTE FUNCTION pharmacy_supplier_return_dispatch_guard();

CREATE FUNCTION pharmacy_supplier_return_cancellation_state_guard()RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id::text||':'||NEW.outlet_id::text||':supplier-return:'||NEW.return_id::text,0));
 IF EXISTS(SELECT 1 FROM pharmacy_supplier_return_dispatches d WHERE d.tenant_id=NEW.tenant_id AND d.outlet_id=NEW.outlet_id AND d.return_id=NEW.return_id)THEN
  RAISE EXCEPTION 'dispatched returns cannot be cancelled' USING ERRCODE='23514',CONSTRAINT='pharmacy_supplier_return_dispatch_cancelled';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_supplier_return_cancellation_state_guard BEFORE INSERT ON pharmacy_supplier_return_cancellations FOR EACH ROW EXECUTE FUNCTION pharmacy_supplier_return_cancellation_state_guard();

CREATE FUNCTION pharmacy_supplier_return_dispatch_line_guard()RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM pharmacy_supplier_return_dispatches d JOIN pharmacy_supplier_return_lines l
  ON l.tenant_id=d.tenant_id AND l.outlet_id=d.outlet_id AND l.return_id=d.return_id
  CROSS JOIN LATERAL pharmacy_product_schedule_in_force(l.tenant_id,l.outlet_id,l.product_id)s
  WHERE d.tenant_id=NEW.tenant_id AND d.outlet_id=NEW.outlet_id AND d.id=NEW.dispatch_id AND l.id=NEW.return_line_id
  AND s.declaration_id=NEW.schedule_declaration_id AND s.regulatory_class<>'X'AND NOT s.ndps
  AND d.actor_subject=NULLIF(current_setting('app.pharmacy_subject',true),''))THEN
  RAISE EXCEPTION 'dispatch requires exact original line and classification' USING ERRCODE='23514',CONSTRAINT='pharmacy_supplier_return_dispatch_line';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_supplier_return_dispatch_line_guard BEFORE INSERT ON pharmacy_supplier_return_dispatch_lines FOR EACH ROW EXECUTE FUNCTION pharmacy_supplier_return_dispatch_line_guard();

CREATE FUNCTION pharmacy_supplier_return_movement_matches(m pharmacy_stock_movements)RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT EXISTS(SELECT 1 FROM pharmacy_supplier_return_dispatch_lines p
 JOIN pharmacy_supplier_return_dispatches d ON d.tenant_id=p.tenant_id AND d.outlet_id=p.outlet_id AND d.id=p.dispatch_id
 JOIN pharmacy_supplier_return_lines l ON l.tenant_id=p.tenant_id AND l.outlet_id=p.outlet_id AND l.id=p.return_line_id AND l.return_id=d.return_id
 JOIN pharmacy_stock_locations q ON q.tenant_id=l.tenant_id AND q.outlet_id=l.outlet_id AND q.id=l.quarantine_location_id AND q.stock_status='quarantine'
 WHERE p.tenant_id=m.tenant_id AND p.outlet_id=m.outlet_id AND p.movement_id=m.id
 AND m.kind='supplier_return'AND m.batch_id=l.batch_id AND m.location_id=l.quarantine_location_id AND m.quantity_units=-l.quantity_units
 AND m.actor_subject=d.actor_subject AND m.reason=d.reason AND m.source_reference='supplier-return:'||d.id::text||':'||l.position::text)
$$;
CREATE FUNCTION pharmacy_supplier_return_movement_guard()RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF(NEW.kind='supplier_return'OR NEW.source_reference LIKE 'supplier-return:%')AND NOT pharmacy_supplier_return_movement_matches(NEW)THEN
  RAISE EXCEPTION 'supplier return movements require immutable dispatch proof' USING ERRCODE='23514',CONSTRAINT='pharmacy_supplier_return_dispatch_movement';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_supplier_return_movement_guard BEFORE INSERT ON pharmacy_stock_movements FOR EACH ROW EXECUTE FUNCTION pharmacy_supplier_return_movement_guard();

CREATE FUNCTION pharmacy_supplier_return_dispatch_complete_guard()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE expected bigint;actual bigint;
BEGIN
 SELECT line_count INTO STRICT expected FROM pharmacy_supplier_returns r WHERE r.tenant_id=NEW.tenant_id AND r.outlet_id=NEW.outlet_id AND r.id=NEW.return_id;
 SELECT COUNT(*) INTO actual FROM pharmacy_supplier_return_dispatch_lines l JOIN pharmacy_stock_movements m
  ON m.tenant_id=l.tenant_id AND m.outlet_id=l.outlet_id AND m.id=l.movement_id
  WHERE l.tenant_id=NEW.tenant_id AND l.outlet_id=NEW.outlet_id AND l.dispatch_id=NEW.id AND pharmacy_supplier_return_movement_matches(m);
 IF actual<>expected THEN
  RAISE EXCEPTION 'dispatch requires every original line and movement' USING ERRCODE='23514',CONSTRAINT='pharmacy_supplier_return_dispatch_complete';
 END IF;
 RETURN NEW;
END $$;
CREATE CONSTRAINT TRIGGER pharmacy_supplier_return_dispatch_complete_guard AFTER INSERT ON pharmacy_supplier_return_dispatches DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION pharmacy_supplier_return_dispatch_complete_guard();

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
