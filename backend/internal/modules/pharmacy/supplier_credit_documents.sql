SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
DO $$BEGIN
 IF NOT pharmacy_operating_choice_integrity() THEN RAISE EXCEPTION 'existing pharmacy purpose policies changed' USING ERRCODE='23514'; END IF;
END $$;

CREATE TABLE pharmacy_supplier_credit_documents(
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),tenant_id uuid NOT NULL,outlet_id uuid NOT NULL,return_id uuid NOT NULL,
 document_reference text NOT NULL CHECK(length(btrim(document_reference)) BETWEEN 1 AND 100 AND document_reference=upper(btrim(document_reference))),
 issued_on date NOT NULL CHECK(issued_on>='1970-01-01'),amount_minor bigint NOT NULL CHECK(amount_minor BETWEEN 1 AND 1000000000000),currency text NOT NULL CHECK(currency='INR'),
 reason text NOT NULL CHECK(length(btrim(reason)) BETWEEN 1 AND 500),correction_of uuid,
 return_number bigint NOT NULL CHECK(return_number BETWEEN 1 AND 1000000000000),
 payee_reference text NOT NULL CHECK(length(payee_reference) BETWEEN 1 AND 100),payee_name text NOT NULL CHECK(length(payee_name) BETWEEN 1 AND 200),
 original_bill_reference text NOT NULL CHECK(length(original_bill_reference) BETWEEN 1 AND 100),
 intent_sha256 text NOT NULL CHECK(intent_sha256~'^[a-f0-9]{64}$'),actor_subject text NOT NULL CHECK(length(btrim(actor_subject)) BETWEEN 1 AND 200),created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,outlet_id,id),UNIQUE(tenant_id,outlet_id,correction_of),
 FOREIGN KEY(tenant_id,outlet_id,return_id)REFERENCES pharmacy_supplier_returns(tenant_id,outlet_id,id)ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,outlet_id,correction_of)REFERENCES pharmacy_supplier_credit_documents(tenant_id,outlet_id,id)ON DELETE RESTRICT
);
CREATE INDEX pharmacy_supplier_credit_document_page ON pharmacy_supplier_credit_documents(tenant_id,outlet_id,created_at DESC,id DESC);
CREATE INDEX pharmacy_supplier_credit_reference ON pharmacy_supplier_credit_documents(tenant_id,outlet_id,payee_reference,document_reference);

CREATE TABLE pharmacy_supplier_credit_positions(
 id uuid NOT NULL,tenant_id uuid NOT NULL,outlet_id uuid NOT NULL,
 version bigint NOT NULL CHECK(version BETWEEN 1 AND 1000000000000),state text NOT NULL CHECK(state IN('active','voided','corrected')),
 matched_minor bigint NOT NULL CHECK(matched_minor BETWEEN 0 AND 1000000000000),allocated_minor bigint NOT NULL CHECK(allocated_minor BETWEEN 0 AND 1000000000000),
 actor_subject text NOT NULL CHECK(length(btrim(actor_subject)) BETWEEN 1 AND 200),
 PRIMARY KEY(tenant_id,outlet_id,id),FOREIGN KEY(tenant_id,outlet_id,id)REFERENCES pharmacy_supplier_credit_documents(tenant_id,outlet_id,id)ON DELETE RESTRICT,
 CHECK(state='active' OR(matched_minor=0 AND allocated_minor=0))
);
CREATE TABLE pharmacy_supplier_credit_voids(
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),tenant_id uuid NOT NULL,outlet_id uuid NOT NULL,credit_id uuid NOT NULL,
 expected_version bigint NOT NULL CHECK(expected_version BETWEEN 1 AND 999999999999),reason text NOT NULL CHECK(length(btrim(reason)) BETWEEN 1 AND 500),
 intent_sha256 text NOT NULL CHECK(intent_sha256~'^[a-f0-9]{64}$'),actor_subject text NOT NULL CHECK(length(btrim(actor_subject)) BETWEEN 1 AND 200),created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,outlet_id,id),UNIQUE(tenant_id,outlet_id,credit_id),
 FOREIGN KEY(tenant_id,outlet_id,credit_id)REFERENCES pharmacy_supplier_credit_documents(tenant_id,outlet_id,id)ON DELETE RESTRICT
);

DO $policies$DECLARE target text;BEGIN
 FOREACH target IN ARRAY ARRAY['pharmacy_supplier_credit_documents','pharmacy_supplier_credit_positions','pharmacy_supplier_credit_voids'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',target);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',target);
  EXECUTE format($policy$CREATE POLICY tenant_isolation ON %I
   USING(hms_tenant_visible(tenant_id)AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
   WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND pharmacy_outlet_allowed(tenant_id,outlet_id,true)AND actor_subject=NULLIF(current_setting('app.pharmacy_subject',true),''))$policy$,target);
  EXECUTE format($policy$CREATE POLICY operating_choice_purpose ON %I AS RESTRICTIVE
   USING(hms_tenant_visible(tenant_id)AND COALESCE(current_setting('app.pharmacy_operating_choices',true),'')='')
   WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND COALESCE(current_setting('app.pharmacy_operating_choices',true),'')='')$policy$,target);
  EXECUTE format($policy$CREATE POLICY operating_choice_delete ON %I AS RESTRICTIVE FOR DELETE
   USING(hms_tenant_visible(tenant_id)AND COALESCE(current_setting('app.pharmacy_operating_choices',true),'')='')$policy$,target);
 END LOOP;
END $policies$;
CREATE TRIGGER pharmacy_supplier_credit_document_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_supplier_credit_documents FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();
CREATE TRIGGER pharmacy_supplier_credit_void_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_supplier_credit_voids FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();
CREATE TRIGGER pharmacy_supplier_credit_position_no_delete BEFORE DELETE OR TRUNCATE ON pharmacy_supplier_credit_positions FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();
CREATE TRIGGER pharmacy_supplier_credit_document_audit AFTER INSERT ON pharmacy_supplier_credit_documents FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_supplier_credit_void_audit AFTER INSERT ON pharmacy_supplier_credit_voids FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_supplier_credit_position_audit AFTER INSERT OR UPDATE ON pharmacy_supplier_credit_positions FOR EACH ROW EXECUTE FUNCTION audit_record_change();

CREATE FUNCTION pharmacy_supplier_credit_lock(t uuid,o uuid,c uuid)RETURNS void LANGUAGE sql VOLATILE AS $$
 SELECT pg_advisory_xact_lock(hashtextextended(t::text||':'||o::text||':supplier-credit:'||c::text,0))
$$;
CREATE FUNCTION pharmacy_supplier_credit_document_guard()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE source pharmacy_supplier_returns;bill_date date;zone text;prior pharmacy_supplier_credit_documents;prior_state text;
BEGIN
 PERFORM pharmacy_supplier_credit_lock(NEW.tenant_id,NEW.outlet_id,NEW.id);
 SELECT r.* INTO source FROM pharmacy_supplier_returns r JOIN pharmacy_supplier_return_dispatches d ON d.tenant_id=r.tenant_id AND d.outlet_id=r.outlet_id AND d.return_id=r.id
 WHERE r.tenant_id=NEW.tenant_id AND r.outlet_id=NEW.outlet_id AND r.id=NEW.return_id
 AND r.line_count=(SELECT count(*)FROM pharmacy_supplier_return_dispatch_lines l JOIN pharmacy_stock_movements m ON m.tenant_id=l.tenant_id AND m.outlet_id=l.outlet_id AND m.id=l.movement_id
  WHERE l.tenant_id=d.tenant_id AND l.outlet_id=d.outlet_id AND l.dispatch_id=d.id AND pharmacy_supplier_return_movement_matches(m));
 SELECT p.issued_on,o.timezone INTO bill_date,zone FROM pharmacy_payables p JOIN pharmacy_outlets o ON o.tenant_id=p.tenant_id AND o.id=p.outlet_id
 WHERE p.tenant_id=NEW.tenant_id AND p.outlet_id=NEW.outlet_id AND p.id=source.payable_id;
 IF source.id IS NULL OR bill_date IS NULL OR NEW.currency<>source.currency OR NEW.return_number<>source.number OR
  NEW.payee_reference<>source.payee_reference OR NEW.payee_name<>source.payee_name OR NEW.original_bill_reference<>source.document_reference OR
  NEW.issued_on<bill_date OR NEW.issued_on>(clock_timestamp()AT TIME ZONE zone)::date THEN
  RAISE EXCEPTION 'credit requires a dispatched return, original supplier/currency and actual document date' USING ERRCODE='23514',CONSTRAINT='pharmacy_supplier_credit_source';
 END IF;
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id::text||':'||NEW.outlet_id::text||':supplier-credit-reference:'||length(NEW.payee_reference)::text||':'||NEW.payee_reference||':'||NEW.document_reference,0));
 IF NEW.correction_of IS NOT NULL THEN
  PERFORM pharmacy_supplier_credit_lock(NEW.tenant_id,NEW.outlet_id,NEW.correction_of);
  SELECT * INTO prior FROM pharmacy_supplier_credit_documents WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.correction_of;
  SELECT state INTO prior_state FROM pharmacy_supplier_credit_positions WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.correction_of;
  IF prior.id IS NULL OR prior_state IS DISTINCT FROM 'voided' OR prior.payee_reference<>NEW.payee_reference OR prior.currency<>NEW.currency THEN
   RAISE EXCEPTION 'correction requires the unused voided credit of this supplier' USING ERRCODE='23514',CONSTRAINT='pharmacy_supplier_credit_correction';
  END IF;
 END IF;
 IF EXISTS(SELECT 1 FROM pharmacy_supplier_credit_documents d JOIN pharmacy_supplier_credit_positions p ON p.tenant_id=d.tenant_id AND p.outlet_id=d.outlet_id AND p.id=d.id
  WHERE d.tenant_id=NEW.tenant_id AND d.outlet_id=NEW.outlet_id AND d.payee_reference=NEW.payee_reference AND d.document_reference=NEW.document_reference
  AND(p.state='active' OR NEW.correction_of IS NULL)) OR
  (EXISTS(SELECT 1 FROM pharmacy_supplier_credit_documents d WHERE d.tenant_id=NEW.tenant_id AND d.outlet_id=NEW.outlet_id AND d.payee_reference=NEW.payee_reference AND d.document_reference=NEW.document_reference)
   AND(prior.id IS NULL OR prior.document_reference<>NEW.document_reference)) THEN
  RAISE EXCEPTION 'supplier document already recorded; explicit correction required' USING ERRCODE='23514',CONSTRAINT='pharmacy_supplier_credit_reference';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_supplier_credit_document_guard BEFORE INSERT ON pharmacy_supplier_credit_documents FOR EACH ROW EXECUTE FUNCTION pharmacy_supplier_credit_document_guard();

CREATE FUNCTION pharmacy_supplier_credit_position_guard()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE amount bigint;
BEGIN
 IF pg_trigger_depth()<2 OR NEW.actor_subject IS DISTINCT FROM NULLIF(current_setting('app.pharmacy_subject',true),'') THEN
  RAISE EXCEPTION 'credit position changes require a ledger entry' USING ERRCODE='23514',CONSTRAINT='pharmacy_supplier_credit_position_guard';
 END IF;
 SELECT amount_minor INTO STRICT amount FROM pharmacy_supplier_credit_documents WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.id;
 IF NEW.matched_minor>amount OR NEW.allocated_minor>amount THEN
  RAISE EXCEPTION 'credit position exceeds the document' USING ERRCODE='23514',CONSTRAINT='pharmacy_supplier_credit_position_guard';
 END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.version<>1 OR NEW.state<>'active' OR NEW.matched_minor<>0 OR NEW.allocated_minor<>0 THEN
   RAISE EXCEPTION 'new credit position must be unused' USING ERRCODE='23514',CONSTRAINT='pharmacy_supplier_credit_position_guard';
  END IF;
 ELSIF NEW.id<>OLD.id OR NEW.tenant_id<>OLD.tenant_id OR NEW.outlet_id<>OLD.outlet_id OR NEW.version<>OLD.version+1 OR
  NOT((OLD.state='active' AND NEW.state IN('active','voided'))OR(OLD.state='voided'AND NEW.state='corrected')) THEN
  RAISE EXCEPTION 'invalid credit position transition' USING ERRCODE='23514',CONSTRAINT='pharmacy_supplier_credit_position_guard';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_supplier_credit_position_guard BEFORE INSERT OR UPDATE ON pharmacy_supplier_credit_positions FOR EACH ROW EXECUTE FUNCTION pharmacy_supplier_credit_position_guard();
CREATE FUNCTION pharmacy_supplier_credit_initialize()RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO pharmacy_supplier_credit_positions(id,tenant_id,outlet_id,version,state,matched_minor,allocated_minor,actor_subject)VALUES(NEW.id,NEW.tenant_id,NEW.outlet_id,1,'active',0,0,NEW.actor_subject);
 IF NEW.correction_of IS NOT NULL THEN
  UPDATE pharmacy_supplier_credit_positions SET version=version+1,state='corrected',actor_subject=NEW.actor_subject WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.correction_of;
 END IF;
 RETURN NULL;
END $$;
CREATE TRIGGER pharmacy_supplier_credit_initialize AFTER INSERT ON pharmacy_supplier_credit_documents FOR EACH ROW EXECUTE FUNCTION pharmacy_supplier_credit_initialize();
CREATE FUNCTION pharmacy_supplier_credit_void_guard()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE position pharmacy_supplier_credit_positions;
BEGIN
 PERFORM pharmacy_supplier_credit_lock(NEW.tenant_id,NEW.outlet_id,NEW.credit_id);
 SELECT * INTO position FROM pharmacy_supplier_credit_positions WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.credit_id FOR UPDATE;
 IF position.id IS NULL OR position.state<>'active' OR position.version<>NEW.expected_version OR position.matched_minor<>0 OR position.allocated_minor<>0 THEN
  RAISE EXCEPTION 'void requires the current unused unmatched credit' USING ERRCODE='23514',CONSTRAINT='pharmacy_supplier_credit_void_state';
 END IF;
 UPDATE pharmacy_supplier_credit_positions SET state='voided',version=version+1,actor_subject=NEW.actor_subject WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.credit_id;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_supplier_credit_void_guard BEFORE INSERT ON pharmacy_supplier_credit_voids FOR EACH ROW EXECUTE FUNCTION pharmacy_supplier_credit_void_guard();

CREATE FUNCTION pharmacy_supplier_credit_projection_complete()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE credit uuid;position pharmacy_supplier_credit_positions;voids bigint;corrections bigint;expected_actor text;expected_state text;
BEGIN
 IF TG_TABLE_NAME='pharmacy_supplier_credit_voids' THEN credit:=NEW.credit_id;ELSE credit:=NEW.id;END IF;
 SELECT * INTO STRICT position FROM pharmacy_supplier_credit_positions WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=credit;
 SELECT count(*)INTO voids FROM pharmacy_supplier_credit_voids WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND credit_id=credit;
 SELECT count(*)INTO corrections FROM pharmacy_supplier_credit_documents WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND correction_of=credit;
 IF corrections>0 THEN
  expected_state:='corrected';SELECT actor_subject INTO expected_actor FROM pharmacy_supplier_credit_documents WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND correction_of=credit;
 ELSIF voids>0 THEN
  expected_state:='voided';SELECT actor_subject INTO expected_actor FROM pharmacy_supplier_credit_voids WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND credit_id=credit;
 ELSE
  expected_state:='active';SELECT actor_subject INTO expected_actor FROM pharmacy_supplier_credit_documents WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=credit;
 END IF;
 IF position.version<>1+voids+corrections OR position.state<>expected_state OR position.actor_subject<>expected_actor OR position.matched_minor<>0 OR position.allocated_minor<>0 THEN
  RAISE EXCEPTION 'credit position requires complete immutable evidence' USING ERRCODE='23514',CONSTRAINT='pharmacy_supplier_credit_projection_complete';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER pharmacy_supplier_credit_document_complete AFTER INSERT ON pharmacy_supplier_credit_documents DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION pharmacy_supplier_credit_projection_complete();
CREATE CONSTRAINT TRIGGER pharmacy_supplier_credit_position_complete AFTER INSERT OR UPDATE ON pharmacy_supplier_credit_positions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION pharmacy_supplier_credit_projection_complete();
CREATE CONSTRAINT TRIGGER pharmacy_supplier_credit_void_complete AFTER INSERT ON pharmacy_supplier_credit_voids DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION pharmacy_supplier_credit_projection_complete();
CREATE VIEW pharmacy_supplier_credit_balances WITH(security_invoker=true)AS
 SELECT d.*,p.version,p.state,p.matched_minor,p.allocated_minor,CASE WHEN p.state='active' THEN d.amount_minor-p.allocated_minor ELSE 0 END AS available_minor
 FROM pharmacy_supplier_credit_documents d JOIN pharmacy_supplier_credit_positions p ON p.tenant_id=d.tenant_id AND p.outlet_id=d.outlet_id AND p.id=d.id;

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
