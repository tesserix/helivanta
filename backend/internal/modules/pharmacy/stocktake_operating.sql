SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
DO $$BEGIN
 IF pharmacy_operating_choice_integrity()IS DISTINCT FROM true THEN
 RAISE EXCEPTION 'reviewed stocktake requires unchanged prior pharmacy selection authority';
 END IF;
END $$;

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

DO $policies$DECLARE tab text;prior record;reading text;writing text;scope text;
 active text:='(SELECT starts_with(COALESCE(current_setting(''app.pharmacy_stock_permission'',true),''''),''pharmacy.stocktake.''))';
BEGIN
 reading:='tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND outlet_id=NULLIF(current_setting(''app.pharmacy_stock_outlet'',true),'''')::uuid AND(SELECT pharmacy_stocktake_operating_scope(NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid,NULLIF(current_setting(''app.pharmacy_stock_outlet'',true),'''')::uuid,''read''))';
 FOREACH tab IN ARRAY ARRAY['pharmacy_stocktakes','pharmacy_stocktake_lines','pharmacy_stocktake_commands','pharmacy_stocktake_counts','pharmacy_stocktake_adjustments',
 'pharmacy_catalogue_products','pharmacy_catalogue_revisions','pharmacy_stock_batches','pharmacy_stock_locations','pharmacy_stock_movements','pharmacy_product_schedule_declarations','pharmacy_product_schedule_reviews']LOOP
 SELECT pg_get_expr(polqual,polrelid)AS reading,pg_get_expr(polwithcheck,polrelid)AS writing INTO STRICT prior
 FROM pg_policy WHERE polrelid=tab::regclass AND polname='tenant_isolation'AND polpermissive AND polcmd='*';
 IF prior.reading IS NULL OR prior.writing IS NULL THEN RAISE EXCEPTION 'reviewed stocktake requires explicit prior tenant checks';END IF;
 writing:='false';
 IF tab IN('pharmacy_stocktakes','pharmacy_stocktake_lines','pharmacy_stocktake_commands','pharmacy_stocktake_counts')THEN
 writing:=replace(reading,'''read''','''count''');
 IF tab='pharmacy_stocktake_commands'THEN writing:='('||writing||')AND kind IN(''count'',''submit'')';END IF;
 END IF;
 IF tab IN('pharmacy_stocktakes','pharmacy_stocktake_lines','pharmacy_stocktake_commands','pharmacy_stocktake_counts','pharmacy_stocktake_adjustments')THEN
 scope:=format('tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND outlet_id=(SELECT CASE WHEN %s THEN NULLIF(current_setting(''app.pharmacy_stock_outlet'',true),'''')::uuid ELSE NULLIF(current_setting(''app.pharmacy_outlet'',true),'''')::uuid END)',active);
 EXECUTE format('ALTER POLICY tenant_isolation ON %I USING((%s)AND CASE WHEN %s THEN(%s)ELSE(%s)END)WITH CHECK((%s)AND CASE WHEN %s THEN(%s)ELSE(%s)END)',tab,scope,active,reading,prior.reading,scope,active,writing,prior.writing);
 ELSE
 EXECUTE format('ALTER POLICY tenant_isolation ON %I USING((%s)OR(%s))WITH CHECK((%s)OR(%s))',tab,prior.reading,reading,prior.writing,writing);
 END IF;
 EXECUTE format('CREATE POLICY stocktake_operating_purpose ON %I AS RESTRICTIVE USING(CASE WHEN %s THEN(%s)ELSE true END)WITH CHECK(CASE WHEN %s THEN(%s)ELSE true END)',tab,active,reading,active,writing);
 EXECUTE format('CREATE POLICY stocktake_operating_delete ON %I AS RESTRICTIVE FOR DELETE USING(NOT(%s))',tab,active);
 END LOOP;
END $policies$;

CREATE TABLE pharmacy_stocktake_operating_evidence(
 tenant_id uuid NOT NULL,outlet_id uuid NOT NULL,id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 pharmacy_id uuid NOT NULL,stocktake_id uuid NOT NULL,
 kind text NOT NULL CHECK(kind IN('start','lines_read','count','count_read','submit','submit_read')),
 command_id uuid,
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
 FOREIGN KEY(tenant_id,outlet_id,stocktake_id,command_id)REFERENCES pharmacy_stocktake_commands(tenant_id,outlet_id,stocktake_id,id)DEFERRABLE INITIALLY DEFERRED,
 CHECK((kind IN('count','count_read','submit','submit_read'))=(command_id IS NOT NULL)),
 CHECK(kind NOT IN('count','submit')OR(id=command_id AND permission='pharmacy.stocktake.count')),
 CHECK(kind<>'start'OR(id=stocktake_id AND permission='pharmacy.stocktake.count'))
);
CREATE INDEX ON pharmacy_stocktake_operating_evidence(tenant_id,outlet_id,stocktake_id,staff_id,kind);
CREATE INDEX ON pharmacy_stocktake_operating_evidence(tenant_id,outlet_id,stocktake_id,command_id,kind);
ALTER TABLE pharmacy_stocktake_operating_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_stocktake_operating_evidence FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_stocktake_operating_evidence
 USING(hms_tenant_visible(tenant_id)AND pharmacy_stocktake_operating_scope(tenant_id,outlet_id,'read'))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND pharmacy_stocktake_operating_scope(tenant_id,outlet_id,CASE WHEN kind IN('start','count','submit')THEN 'count'ELSE 'read'END)
 AND pharmacy_id=NULLIF(current_setting('app.pharmacy_stock_pharmacy',true),'')::uuid
 AND permission=current_setting('app.pharmacy_stock_permission',true)
 AND actor_subject=NULLIF(current_setting('app.actor_subject',true),''));
CREATE POLICY operating_choice_purpose ON pharmacy_stocktake_operating_evidence AS RESTRICTIVE
 USING(hms_tenant_visible(tenant_id)AND COALESCE(current_setting('app.pharmacy_operating_choices',true),'')='')
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND COALESCE(current_setting('app.pharmacy_operating_choices',true),'')='');
CREATE POLICY operating_choice_delete ON pharmacy_stocktake_operating_evidence AS RESTRICTIVE FOR DELETE
 USING(hms_tenant_visible(tenant_id)AND COALESCE(current_setting('app.pharmacy_operating_choices',true),'')='');
CREATE TRIGGER pharmacy_stocktake_operating_evidence_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_stocktake_operating_evidence FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

CREATE FUNCTION pharmacy_stocktake_operating_evidence_guard()RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.kind='start'AND EXISTS(SELECT 1 FROM pharmacy_stocktakes WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.stocktake_id)THEN
 RAISE EXCEPTION 'historical stocktake authority cannot be invented' USING ERRCODE='23514',CONSTRAINT='pharmacy_stocktake_operating_provenance';
 END IF;
 IF NEW.kind IN('count','submit')AND EXISTS(SELECT 1 FROM pharmacy_stocktake_commands WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.command_id)THEN
 RAISE EXCEPTION 'historical count authority cannot be invented' USING ERRCODE='23514',CONSTRAINT='pharmacy_stocktake_operating_provenance';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_stocktake_operating_evidence_guard BEFORE INSERT ON pharmacy_stocktake_operating_evidence FOR EACH ROW EXECUTE FUNCTION pharmacy_stocktake_operating_evidence_guard();

ALTER TABLE pharmacy_stocktakes ADD COLUMN authority_kind text NOT NULL DEFAULT 'legacy' CHECK(authority_kind IN('legacy','operating'));

CREATE FUNCTION pharmacy_stocktake_operating_start_guard()RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF pharmacy_stocktake_operating_scope(NEW.tenant_id,NEW.outlet_id,'count')AND NOT EXISTS(
 SELECT 1 FROM pharmacy_stocktake_operating_evidence WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND stocktake_id=NEW.id
 AND id=NEW.id AND kind='start'AND actor_subject=NEW.actor_subject)THEN
 RAISE EXCEPTION 'reviewed stocktake needs source evidence' USING ERRCODE='23514',CONSTRAINT='pharmacy_stocktake_operating_provenance';
 END IF;
 IF pharmacy_stocktake_operating_scope(NEW.tenant_id,NEW.outlet_id,'count')THEN
 NEW.authority_kind:='operating';
 ELSIF NEW.authority_kind<>'legacy'THEN
 RAISE EXCEPTION 'reviewed stocktake needs held authority' USING ERRCODE='23514',CONSTRAINT='pharmacy_stocktake_operating_provenance';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_stocktake_operating_start_guard BEFORE INSERT ON pharmacy_stocktakes FOR EACH ROW EXECUTE FUNCTION pharmacy_stocktake_operating_start_guard();

CREATE FUNCTION pharmacy_stocktake_operating_command_guard()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE model text;reviewed boolean;
BEGIN
 SELECT authority_kind INTO STRICT model FROM pharmacy_stocktakes WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.stocktake_id;
 reviewed:=starts_with(COALESCE(current_setting('app.pharmacy_stock_permission',true),''),'pharmacy.stocktake.');
 IF model='operating'OR reviewed THEN
 IF model<>'operating'OR NOT pharmacy_stocktake_operating_scope(NEW.tenant_id,NEW.outlet_id,'count')OR NEW.kind NOT IN('count','submit')OR NOT EXISTS(
 SELECT 1 FROM pharmacy_stocktake_operating_evidence WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND stocktake_id=NEW.stocktake_id
 AND id=NEW.id AND command_id=NEW.id AND kind=NEW.kind AND actor_subject=NEW.actor_subject)THEN
 RAISE EXCEPTION 'reviewed count needs original source evidence' USING ERRCODE='23514',CONSTRAINT='pharmacy_stocktake_operating_provenance';
 END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_stocktake_operating_command_guard BEFORE INSERT ON pharmacy_stocktake_commands FOR EACH ROW EXECUTE FUNCTION pharmacy_stocktake_operating_command_guard();

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
