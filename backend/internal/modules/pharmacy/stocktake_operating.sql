SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE FUNCTION pharmacy_stocktake_operating_scope(t uuid,o uuid,operation text)RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(hms_tenant_visible(t)
 AND t=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND o=NULLIF(current_setting('app.pharmacy_stock_outlet',true),'')::uuid
 AND current_setting('app.pharmacy_stock_access',true)='authorized'
 AND NULLIF(current_setting('app.pharmacy_subject',true),'')IS NOT NULL
 AND current_setting('app.pharmacy_subject',true)=current_setting('app.actor_subject',true)
 AND((operation='read'AND current_setting('app.pharmacy_stock_permission',true)IN('pharmacy.stocktake.read','pharmacy.stocktake.count'))
 OR(operation='count'AND current_setting('app.pharmacy_stock_permission',true)='pharmacy.stocktake.count'))
 AND EXISTS(SELECT 1 FROM pharmacy_outlets x WHERE x.tenant_id=t AND x.id=o
 AND x.pharmacy_id=NULLIF(current_setting('app.pharmacy_stock_pharmacy',true),'')::uuid),false)
$$;

DO $$DECLARE tab text;BEGIN
 FOREACH tab IN ARRAY ARRAY['pharmacy_stocktakes','pharmacy_stocktake_lines','pharmacy_stocktake_commands','pharmacy_stocktake_counts','pharmacy_stocktake_adjustments',
 'pharmacy_catalogue_products','pharmacy_catalogue_revisions','pharmacy_stock_batches','pharmacy_stock_locations','pharmacy_stock_movements','pharmacy_product_schedule_declarations','pharmacy_product_schedule_reviews']LOOP
 EXECUTE format('CREATE POLICY stocktake_operating_read ON %I FOR SELECT USING(tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND outlet_id=NULLIF(current_setting(''app.pharmacy_stock_outlet'',true),'''')::uuid AND(SELECT pharmacy_stocktake_operating_scope(NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid,NULLIF(current_setting(''app.pharmacy_stock_outlet'',true),'''')::uuid,''read'')))',tab);
 END LOOP;
 FOREACH tab IN ARRAY ARRAY['pharmacy_stocktakes','pharmacy_stocktake_lines']LOOP
 EXECUTE format('CREATE POLICY stocktake_operating_start ON %I FOR INSERT WITH CHECK(tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND outlet_id=NULLIF(current_setting(''app.pharmacy_stock_outlet'',true),'''')::uuid AND(SELECT pharmacy_stocktake_operating_scope(NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid,NULLIF(current_setting(''app.pharmacy_stock_outlet'',true),'''')::uuid,''count'')))',tab);
 END LOOP;
END $$;
CREATE POLICY stocktake_operating_seal ON pharmacy_stocktakes FOR UPDATE
 USING(pharmacy_stocktake_operating_scope(tenant_id,outlet_id,'count'))
 WITH CHECK(pharmacy_stocktake_operating_scope(tenant_id,outlet_id,'count'));

CREATE TABLE pharmacy_stocktake_operating_evidence(
 tenant_id uuid NOT NULL,outlet_id uuid NOT NULL,id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 pharmacy_id uuid NOT NULL,stocktake_id uuid NOT NULL,
 kind text NOT NULL CHECK(kind IN('start','lines_read')),
 permission text NOT NULL CHECK(permission IN('pharmacy.stocktake.read','pharmacy.stocktake.count')),
 duty_id uuid NOT NULL CHECK(duty_id<>'00000000-0000-0000-0000-000000000000'),
 snapshot_version bigint NOT NULL CHECK(snapshot_version BETWEEN 1 AND 9007199254740991),
 authority_epoch bigint NOT NULL CHECK(authority_epoch BETWEEN 1 AND 9007199254740991),
 staff_id uuid NOT NULL CHECK(staff_id<>'00000000-0000-0000-0000-000000000000'),
 staff_version bigint NOT NULL CHECK(staff_version BETWEEN 1 AND 9007199254740991),
 link_id uuid NOT NULL CHECK(link_id<>'00000000-0000-0000-0000-000000000000'),
 link_version bigint NOT NULL CHECK(link_version BETWEEN 2 AND 9007199254740991),
 actor_subject text NOT NULL CHECK(length(btrim(actor_subject))BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,outlet_id,id),
 FOREIGN KEY(tenant_id,outlet_id,stocktake_id)REFERENCES pharmacy_stocktakes(tenant_id,outlet_id,id)DEFERRABLE INITIALLY DEFERRED,
 CHECK(kind<>'start'OR(id=stocktake_id AND permission='pharmacy.stocktake.count'))
);
CREATE INDEX ON pharmacy_stocktake_operating_evidence(tenant_id,outlet_id,stocktake_id,staff_id,kind);
ALTER TABLE pharmacy_stocktake_operating_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_stocktake_operating_evidence FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_stocktake_operating_evidence
 USING(pharmacy_stocktake_operating_scope(tenant_id,outlet_id,'read'))
 WITH CHECK(pharmacy_stocktake_operating_scope(tenant_id,outlet_id,CASE WHEN kind='start'THEN 'count'ELSE 'read'END)
 AND pharmacy_id=NULLIF(current_setting('app.pharmacy_stock_pharmacy',true),'')::uuid
 AND permission=current_setting('app.pharmacy_stock_permission',true)
 AND actor_subject=NULLIF(current_setting('app.actor_subject',true),''));
CREATE TRIGGER pharmacy_stocktake_operating_evidence_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_stocktake_operating_evidence FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

CREATE FUNCTION pharmacy_stocktake_operating_evidence_guard()RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.kind='start'AND EXISTS(SELECT 1 FROM pharmacy_stocktakes WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.stocktake_id)THEN
 RAISE EXCEPTION 'historical stocktake authority cannot be invented' USING ERRCODE='23514',CONSTRAINT='pharmacy_stocktake_operating_provenance';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_stocktake_operating_evidence_guard BEFORE INSERT ON pharmacy_stocktake_operating_evidence FOR EACH ROW EXECUTE FUNCTION pharmacy_stocktake_operating_evidence_guard();

CREATE FUNCTION pharmacy_stocktake_operating_start_guard()RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF pharmacy_stocktake_operating_scope(NEW.tenant_id,NEW.outlet_id,'count')AND NOT EXISTS(
 SELECT 1 FROM pharmacy_stocktake_operating_evidence WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND stocktake_id=NEW.id
 AND id=NEW.id AND kind='start'AND actor_subject=NEW.actor_subject)THEN
 RAISE EXCEPTION 'reviewed stocktake needs source evidence' USING ERRCODE='23514',CONSTRAINT='pharmacy_stocktake_operating_provenance';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_stocktake_operating_start_guard BEFORE INSERT ON pharmacy_stocktakes FOR EACH ROW EXECUTE FUNCTION pharmacy_stocktake_operating_start_guard();
