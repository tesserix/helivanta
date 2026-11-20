SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
DO $$BEGIN
 IF NOT pharmacy_operating_choice_integrity() THEN
  RAISE EXCEPTION 'existing pharmacy purpose policies changed' USING ERRCODE='23514';
 END IF;
END $$;

CREATE TABLE pharmacy_stocktakes (
 tenant_id uuid NOT NULL,outlet_id uuid NOT NULL,id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 number bigint NOT NULL CHECK(number BETWEEN 1 AND 9007199254740991),
 selection text NOT NULL CHECK(selection IN('all','selected')),
 product_ids uuid[] NOT NULL,
 quarantine_location_id uuid NOT NULL,
 reason text NOT NULL CHECK(length(btrim(reason))BETWEEN 1 AND 500),
 actor_subject text NOT NULL CHECK(length(btrim(actor_subject))BETWEEN 1 AND 200),
 intent_sha256 text NOT NULL CHECK(intent_sha256~'^[0-9a-f]{64}$'),
 snapshot_rows integer NOT NULL CHECK(snapshot_rows BETWEEN 0 AND 10000),
 sealed boolean NOT NULL DEFAULT false,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,outlet_id,id),UNIQUE(tenant_id,outlet_id,number),
 FOREIGN KEY(tenant_id,outlet_id)REFERENCES pharmacy_outlets(tenant_id,id)ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,outlet_id,quarantine_location_id)REFERENCES pharmacy_stock_locations(tenant_id,outlet_id,id)ON DELETE RESTRICT,
 CHECK((selection='all'AND cardinality(product_ids)=0)OR(selection='selected'AND cardinality(product_ids)BETWEEN 1 AND 100))
);
CREATE INDEX pharmacy_stocktake_page ON pharmacy_stocktakes(tenant_id,outlet_id,created_at DESC,id DESC);
CREATE INDEX pharmacy_stocktake_quarantine ON pharmacy_stocktakes(tenant_id,outlet_id,quarantine_location_id);

CREATE TABLE pharmacy_stocktake_lines (
 tenant_id uuid NOT NULL,outlet_id uuid NOT NULL,id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 stocktake_id uuid NOT NULL,product_id uuid NOT NULL,product_version bigint NOT NULL,
 sku text NOT NULL CHECK(length(sku)BETWEEN 1 AND 40),brand_name text NOT NULL CHECK(length(brand_name)BETWEEN 1 AND 200),
 unit_of_issue text NOT NULL CHECK(length(unit_of_issue)BETWEEN 1 AND 40),
 batch_id uuid NOT NULL,lot_number text NOT NULL CHECK(length(lot_number)BETWEEN 1 AND 100),expires_on date NOT NULL,
 location_id uuid NOT NULL,location_code text NOT NULL CHECK(length(location_code)BETWEEN 1 AND 40),
 location_name text NOT NULL CHECK(length(location_name)BETWEEN 1 AND 200),stock_status text NOT NULL CHECK(stock_status IN('quarantine','sellable')),
 book_units bigint NOT NULL CHECK(book_units BETWEEN 0 AND 1000000000000),
 movement_count bigint NOT NULL CHECK(movement_count BETWEEN 0 AND 9007199254740991),
 regulatory_class text CHECK(regulatory_class IN('none','G','H','H1','X')),
 ndps_status text NOT NULL CHECK(ndps_status IN('unknown','no','yes')),schedule_declaration_id uuid,
 created_at timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,outlet_id,id),UNIQUE(tenant_id,outlet_id,stocktake_id,location_id,batch_id),
 FOREIGN KEY(tenant_id,outlet_id,stocktake_id)REFERENCES pharmacy_stocktakes(tenant_id,outlet_id,id)ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,outlet_id,product_id,product_version)REFERENCES pharmacy_catalogue_revisions(tenant_id,outlet_id,product_id,version)ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,outlet_id,batch_id)REFERENCES pharmacy_stock_batches(tenant_id,outlet_id,id)ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,outlet_id,location_id)REFERENCES pharmacy_stock_locations(tenant_id,outlet_id,id)ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,outlet_id,schedule_declaration_id)REFERENCES pharmacy_product_schedule_declarations(tenant_id,outlet_id,id)ON DELETE RESTRICT,
 CHECK((regulatory_class IS NULL AND schedule_declaration_id IS NULL AND ndps_status='unknown')OR(regulatory_class IS NOT NULL AND schedule_declaration_id IS NOT NULL AND ndps_status IN('no','yes')))
);
CREATE INDEX pharmacy_stocktake_line_page ON pharmacy_stocktake_lines(tenant_id,outlet_id,stocktake_id,created_at DESC,id DESC);
CREATE INDEX pharmacy_stocktake_line_product ON pharmacy_stocktake_lines(tenant_id,outlet_id,product_id,product_version);
CREATE INDEX pharmacy_stocktake_line_batch ON pharmacy_stocktake_lines(tenant_id,outlet_id,batch_id);
CREATE INDEX pharmacy_stocktake_line_location ON pharmacy_stocktake_lines(tenant_id,outlet_id,location_id);
CREATE INDEX pharmacy_stocktake_line_schedule ON pharmacy_stocktake_lines(tenant_id,outlet_id,schedule_declaration_id);

CREATE TABLE pharmacy_stocktake_commands (
 tenant_id uuid NOT NULL,outlet_id uuid NOT NULL,id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 stocktake_id uuid NOT NULL,kind text NOT NULL CHECK(kind='count'),
 expected_version bigint NOT NULL CHECK(expected_version BETWEEN 0 AND 9007199254740990),
 version bigint NOT NULL CHECK(version=expected_version+1),
 state text NOT NULL CHECK(state='counting'),
 item_count integer NOT NULL CHECK(item_count BETWEEN 1 AND 100),
 reason text NOT NULL CHECK(length(btrim(reason))BETWEEN 1 AND 500),
 actor_subject text NOT NULL CHECK(length(btrim(actor_subject))BETWEEN 1 AND 200),
 intent_sha256 text NOT NULL CHECK(intent_sha256~'^[0-9a-f]{64}$'),
 posted_xact xid8 NOT NULL DEFAULT pg_current_xact_id(),created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,outlet_id,id),UNIQUE(tenant_id,outlet_id,stocktake_id,id),
 UNIQUE(tenant_id,outlet_id,stocktake_id,version),
 FOREIGN KEY(tenant_id,outlet_id,stocktake_id)REFERENCES pharmacy_stocktakes(tenant_id,outlet_id,id)ON DELETE RESTRICT
);
CREATE TABLE pharmacy_stocktake_counts (
 tenant_id uuid NOT NULL,outlet_id uuid NOT NULL,id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 stocktake_id uuid NOT NULL,command_id uuid NOT NULL,line_id uuid NOT NULL,
 command_version bigint NOT NULL CHECK(command_version BETWEEN 1 AND 9007199254740991),
 counted_units bigint NOT NULL CHECK(counted_units BETWEEN 0 AND 1000000000000),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,outlet_id,id),UNIQUE(tenant_id,outlet_id,command_id,line_id),
 FOREIGN KEY(tenant_id,outlet_id,stocktake_id,command_id)REFERENCES pharmacy_stocktake_commands(tenant_id,outlet_id,stocktake_id,id)ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,outlet_id,line_id)REFERENCES pharmacy_stocktake_lines(tenant_id,outlet_id,id)ON DELETE RESTRICT
);
CREATE INDEX pharmacy_stocktake_count_latest ON pharmacy_stocktake_counts(tenant_id,outlet_id,stocktake_id,line_id,command_version DESC);
CREATE INDEX pharmacy_stocktake_count_line ON pharmacy_stocktake_counts(tenant_id,outlet_id,line_id);

DO $scope$DECLARE target text;BEGIN
 FOREACH target IN ARRAY ARRAY['pharmacy_stocktakes','pharmacy_stocktake_lines','pharmacy_stocktake_commands','pharmacy_stocktake_counts'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',target);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',target);
  EXECUTE format($policy$CREATE POLICY tenant_isolation ON %I USING(hms_tenant_visible(tenant_id)AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))WITH CHECK(pharmacy_outlet_allowed(tenant_id,outlet_id,true))$policy$,target);
  EXECUTE format($policy$CREATE POLICY operating_choice_purpose ON %I AS RESTRICTIVE USING(hms_tenant_visible(tenant_id)AND COALESCE(current_setting('app.pharmacy_operating_choices',true),'')='')WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND COALESCE(current_setting('app.pharmacy_operating_choices',true),'')='')$policy$,target);
  EXECUTE format($policy$CREATE POLICY operating_choice_delete ON %I AS RESTRICTIVE FOR DELETE USING(hms_tenant_visible(tenant_id)AND COALESCE(current_setting('app.pharmacy_operating_choices',true),'')='')$policy$,target);
  EXECUTE format('CREATE TRIGGER %I AFTER INSERT OR UPDATE ON %I FOR EACH ROW EXECUTE FUNCTION audit_record_change()',target||'_audit',target);
 END LOOP;
END $scope$;

CREATE FUNCTION pharmacy_stocktake_start_guard()RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.actor_subject IS DISTINCT FROM NULLIF(current_setting('app.pharmacy_subject',true),'') OR NEW.sealed THEN
  RAISE EXCEPTION 'stocktake actor or seal changed' USING ERRCODE='23514',CONSTRAINT='pharmacy_stocktake_identity';
 END IF;
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id::text||':'||NEW.outlet_id::text||':stocktake-start',0));
 IF EXISTS(SELECT 1 FROM pharmacy_stocktakes WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id)THEN
  RAISE EXCEPTION 'an outlet count is already open' USING ERRCODE='23514',CONSTRAINT='pharmacy_stocktake_open';
 END IF;
 IF NOT EXISTS(SELECT 1 FROM pharmacy_stock_locations WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.quarantine_location_id AND stock_status='quarantine')THEN
  RAISE EXCEPTION 'stocktake needs quarantine' USING ERRCODE='23514',CONSTRAINT='pharmacy_stocktake_quarantine';
 END IF;
 SELECT COALESCE(MAX(number),0)+1 INTO NEW.number FROM pharmacy_stocktakes WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_stocktake_start_guard BEFORE INSERT ON pharmacy_stocktakes FOR EACH ROW EXECUTE FUNCTION pharmacy_stocktake_start_guard();

CREATE FUNCTION pharmacy_stocktake_line_guard()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE parent pharmacy_stocktakes;
BEGIN
 SELECT *INTO parent FROM pharmacy_stocktakes WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.stocktake_id FOR UPDATE;
 IF parent.id IS NULL OR parent.sealed OR parent.actor_subject IS DISTINCT FROM NULLIF(current_setting('app.pharmacy_subject',true),'')OR NEW.created_at<>parent.created_at THEN
  RAISE EXCEPTION 'stocktake snapshot is sealed or unavailable' USING ERRCODE='23514',CONSTRAINT='pharmacy_stocktake_sealed';
 END IF;
 IF NOT EXISTS(SELECT 1 FROM pharmacy_stock_batches WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.batch_id AND product_id=NEW.product_id AND lot_number=NEW.lot_number AND expires_on=NEW.expires_on)THEN
  RAISE EXCEPTION 'stocktake batch identity changed' USING ERRCODE='23514',CONSTRAINT='pharmacy_stocktake_batch';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_stocktake_line_guard BEFORE INSERT ON pharmacy_stocktake_lines FOR EACH ROW EXECUTE FUNCTION pharmacy_stocktake_line_guard();
CREATE TRIGGER pharmacy_stocktake_line_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_stocktake_lines FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();
CREATE TRIGGER pharmacy_stocktake_immutable BEFORE DELETE OR TRUNCATE ON pharmacy_stocktakes FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

CREATE FUNCTION pharmacy_stocktake_seal_guard()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE actual bigint;
BEGIN
 IF OLD.sealed OR NOT NEW.sealed OR(to_jsonb(OLD)-'sealed')IS DISTINCT FROM(to_jsonb(NEW)-'sealed')OR NEW.actor_subject IS DISTINCT FROM NULLIF(current_setting('app.pharmacy_subject',true),'')THEN
  RAISE EXCEPTION 'stocktake snapshot is immutable' USING ERRCODE='23514',CONSTRAINT='pharmacy_stocktake_sealed';
 END IF;
 SELECT COUNT(*)INTO actual FROM pharmacy_stocktake_lines WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND stocktake_id=NEW.id;
 IF actual<>NEW.snapshot_rows THEN RAISE EXCEPTION 'stocktake snapshot is incomplete' USING ERRCODE='23514',CONSTRAINT='pharmacy_stocktake_snapshot';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_stocktake_seal_guard BEFORE UPDATE ON pharmacy_stocktakes FOR EACH ROW EXECUTE FUNCTION pharmacy_stocktake_seal_guard();
CREATE FUNCTION pharmacy_stocktake_commit_guard()RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM pharmacy_stocktakes WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.id AND sealed)THEN
  RAISE EXCEPTION 'stocktake snapshot must be sealed' USING ERRCODE='23514',CONSTRAINT='pharmacy_stocktake_snapshot';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER pharmacy_stocktake_commit_guard AFTER INSERT ON pharmacy_stocktakes DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION pharmacy_stocktake_commit_guard();

CREATE FUNCTION pharmacy_stocktake_command_guard()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE parent pharmacy_stocktakes;prior_version bigint;
BEGIN
 SELECT *INTO parent FROM pharmacy_stocktakes WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.stocktake_id AND sealed FOR UPDATE;
 SELECT COALESCE(MAX(version),0)INTO prior_version FROM pharmacy_stocktake_commands WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND stocktake_id=NEW.stocktake_id;
 IF parent.id IS NULL OR NEW.actor_subject IS DISTINCT FROM NULLIF(current_setting('app.pharmacy_subject',true),'')OR NEW.expected_version<>prior_version OR NEW.posted_xact<>pg_current_xact_id()THEN
  RAISE EXCEPTION 'stocktake command or version changed' USING ERRCODE='23514',CONSTRAINT='pharmacy_stocktake_command';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_stocktake_command_guard BEFORE INSERT ON pharmacy_stocktake_commands FOR EACH ROW EXECUTE FUNCTION pharmacy_stocktake_command_guard();
CREATE TRIGGER pharmacy_stocktake_command_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_stocktake_commands FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();
CREATE TRIGGER pharmacy_stocktake_count_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_stocktake_counts FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

CREATE FUNCTION pharmacy_stocktake_count_guard()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE command pharmacy_stocktake_commands;line pharmacy_stocktake_lines;observed bigint;
BEGIN
 SELECT *INTO command FROM pharmacy_stocktake_commands WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.command_id AND stocktake_id=NEW.stocktake_id;
 SELECT *INTO line FROM pharmacy_stocktake_lines WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.line_id AND stocktake_id=NEW.stocktake_id;
 IF command.id IS NULL OR line.id IS NULL OR command.kind<>'count'OR command.posted_xact<>pg_current_xact_id()OR NEW.command_version<>command.version OR command.actor_subject IS DISTINCT FROM NULLIF(current_setting('app.pharmacy_subject',true),'')THEN
  RAISE EXCEPTION 'count is not part of its original command' USING ERRCODE='23514',CONSTRAINT='pharmacy_stocktake_count';
 END IF;
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id::text||':'||NEW.outlet_id::text||':'||line.location_id::text||':'||line.batch_id::text,0));
 SELECT COUNT(*)INTO observed FROM pharmacy_stock_movements WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND location_id=line.location_id AND batch_id=line.batch_id;
 IF observed<>line.movement_count THEN
  RAISE EXCEPTION 'stock moved since the book snapshot' USING ERRCODE='23514',CONSTRAINT='pharmacy_stocktake_count_stale';
 END IF;
 NEW.created_at:=command.created_at;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_stocktake_count_guard BEFORE INSERT ON pharmacy_stocktake_counts FOR EACH ROW EXECUTE FUNCTION pharmacy_stocktake_count_guard();

CREATE FUNCTION pharmacy_stocktake_command_commit_guard()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE actual bigint;
BEGIN
 SELECT COUNT(*)INTO actual FROM pharmacy_stocktake_counts WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND command_id=NEW.id;
 IF actual<>NEW.item_count THEN RAISE EXCEPTION 'count command is incomplete' USING ERRCODE='23514',CONSTRAINT='pharmacy_stocktake_count_incomplete';END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER pharmacy_stocktake_command_commit_guard AFTER INSERT ON pharmacy_stocktake_commands DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION pharmacy_stocktake_command_commit_guard();

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
