SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
DO $$BEGIN
 IF NOT pharmacy_operating_choice_integrity() THEN
  RAISE EXCEPTION 'existing pharmacy purpose policies changed' USING ERRCODE='23514';
 END IF;
END $$;

CREATE TABLE pharmacy_supplier_returns(
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),tenant_id uuid NOT NULL,outlet_id uuid NOT NULL,
 number bigint NOT NULL CHECK(number BETWEEN 1 AND 1000000000000),payable_id uuid NOT NULL,
 payee_reference text NOT NULL CHECK(length(payee_reference)BETWEEN 1 AND 100),payee_name text NOT NULL CHECK(length(payee_name)BETWEEN 1 AND 200),
 document_reference text NOT NULL CHECK(length(document_reference)BETWEEN 1 AND 100),currency text NOT NULL CHECK(currency~'^[A-Z]{3}$'),
 line_count integer NOT NULL CHECK(line_count BETWEEN 1 AND 100),expected_credit_minor bigint NOT NULL CHECK(expected_credit_minor BETWEEN 0 AND 1000000000000),
 reason text NOT NULL CHECK(length(btrim(reason))BETWEEN 1 AND 500),intent_sha256 text NOT NULL CHECK(intent_sha256~'^[a-f0-9]{64}$'),
 actor_subject text NOT NULL CHECK(length(btrim(actor_subject))BETWEEN 1 AND 200),created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,outlet_id,id),UNIQUE(tenant_id,outlet_id,number),
 FOREIGN KEY(tenant_id,outlet_id,payable_id)REFERENCES pharmacy_payables(tenant_id,outlet_id,id)ON DELETE RESTRICT
);
ALTER TABLE pharmacy_supplier_returns ENABLE ROW LEVEL SECURITY;ALTER TABLE pharmacy_supplier_returns FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_supplier_returns
 USING(hms_tenant_visible(tenant_id)AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND pharmacy_outlet_allowed(tenant_id,outlet_id,true)AND actor_subject=NULLIF(current_setting('app.pharmacy_subject',true),''));
CREATE INDEX pharmacy_supplier_return_bill ON pharmacy_supplier_returns(tenant_id,outlet_id,payable_id);
CREATE INDEX pharmacy_supplier_return_page ON pharmacy_supplier_returns(tenant_id,outlet_id,created_at DESC,id DESC);
CREATE TRIGGER pharmacy_supplier_return_audit AFTER INSERT ON pharmacy_supplier_returns FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_supplier_return_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_supplier_returns FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

CREATE TABLE pharmacy_supplier_return_lines(
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),tenant_id uuid NOT NULL,outlet_id uuid NOT NULL,return_id uuid NOT NULL,
 position integer NOT NULL CHECK(position BETWEEN 1 AND 100),receipt_id uuid NOT NULL,quarantine_location_id uuid NOT NULL,
 quantity_units bigint NOT NULL CHECK(quantity_units BETWEEN 1 AND 1000000000),expected_credit_minor bigint NOT NULL CHECK(expected_credit_minor BETWEEN 0 AND 1000000000000),
 batch_id uuid NOT NULL,product_id uuid NOT NULL,product_version bigint NOT NULL CHECK(product_version>0),
 product_name text NOT NULL CHECK(length(product_name)BETWEEN 1 AND 200),generic_name text NOT NULL CHECK(length(generic_name)BETWEEN 1 AND 200),unit_code text NOT NULL CHECK(length(unit_code)BETWEEN 1 AND 40),
 lot_number text NOT NULL CHECK(length(lot_number)BETWEEN 1 AND 100),expires_on date NOT NULL,receipt_units bigint NOT NULL CHECK(receipt_units BETWEEN 1 AND 1000000000),
 source_reference text NOT NULL CHECK(length(source_reference)BETWEEN 1 AND 100),source_created_at timestamptz NOT NULL,created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,outlet_id,id),UNIQUE(tenant_id,outlet_id,return_id,position),UNIQUE(tenant_id,outlet_id,return_id,receipt_id,quarantine_location_id),
 FOREIGN KEY(tenant_id,outlet_id,return_id)REFERENCES pharmacy_supplier_returns(tenant_id,outlet_id,id)ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,outlet_id,receipt_id)REFERENCES pharmacy_stock_movements(tenant_id,outlet_id,id)ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,outlet_id,quarantine_location_id)REFERENCES pharmacy_stock_locations(tenant_id,outlet_id,id)ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,outlet_id,batch_id)REFERENCES pharmacy_stock_batches(tenant_id,outlet_id,id)ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,outlet_id,product_id,product_version)REFERENCES pharmacy_catalogue_revisions(tenant_id,outlet_id,product_id,version)ON DELETE RESTRICT
);
ALTER TABLE pharmacy_supplier_return_lines ENABLE ROW LEVEL SECURITY;ALTER TABLE pharmacy_supplier_return_lines FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_supplier_return_lines
 USING(hms_tenant_visible(tenant_id)AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND pharmacy_outlet_allowed(tenant_id,outlet_id,true)
 AND EXISTS(SELECT 1 FROM pharmacy_supplier_returns h WHERE h.tenant_id=pharmacy_supplier_return_lines.tenant_id AND h.outlet_id=pharmacy_supplier_return_lines.outlet_id AND h.id=pharmacy_supplier_return_lines.return_id AND h.actor_subject=NULLIF(current_setting('app.pharmacy_subject',true),'')));
CREATE INDEX pharmacy_supplier_return_line_receipt ON pharmacy_supplier_return_lines(tenant_id,outlet_id,receipt_id);
CREATE INDEX pharmacy_supplier_return_line_bucket ON pharmacy_supplier_return_lines(tenant_id,outlet_id,quarantine_location_id,batch_id);
CREATE INDEX pharmacy_supplier_return_line_batch ON pharmacy_supplier_return_lines(tenant_id,outlet_id,batch_id);
CREATE INDEX pharmacy_supplier_return_line_product ON pharmacy_supplier_return_lines(tenant_id,outlet_id,product_id,product_version);
CREATE TRIGGER pharmacy_supplier_return_line_audit AFTER INSERT ON pharmacy_supplier_return_lines FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_supplier_return_line_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_supplier_return_lines FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

CREATE FUNCTION pharmacy_supplier_return_source_guard()RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_TABLE_NAME='pharmacy_supplier_returns'THEN
  IF NOT EXISTS(SELECT 1 FROM pharmacy_payables p WHERE p.tenant_id=NEW.tenant_id AND p.outlet_id=NEW.outlet_id AND p.id=NEW.payable_id
   AND p.payee_reference=NEW.payee_reference AND p.payee_name=NEW.payee_name AND p.document_reference=NEW.document_reference AND p.currency=NEW.currency)THEN
   RAISE EXCEPTION 'return requires its original supplier bill' USING ERRCODE='23514',CONSTRAINT='pharmacy_supplier_return_bill';
  END IF;
 ELSE
  IF NOT EXISTS(SELECT 1 FROM pharmacy_supplier_returns h
   JOIN pharmacy_payable_lines pl ON pl.tenant_id=h.tenant_id AND pl.outlet_id=h.outlet_id AND pl.payable_id=h.payable_id AND pl.receipt_id=NEW.receipt_id AND pl.category='inventory'
   JOIN pharmacy_stock_movements m ON m.tenant_id=pl.tenant_id AND m.outlet_id=pl.outlet_id AND m.id=pl.receipt_id AND m.kind='receipt'
   JOIN pharmacy_stock_batches b ON b.tenant_id=m.tenant_id AND b.outlet_id=m.outlet_id AND b.id=m.batch_id
   JOIN pharmacy_catalogue_revisions r ON r.tenant_id=b.tenant_id AND r.outlet_id=b.outlet_id AND r.product_id=b.product_id AND r.version=b.product_version
   JOIN pharmacy_stock_locations q ON q.tenant_id=h.tenant_id AND q.outlet_id=h.outlet_id AND q.id=NEW.quarantine_location_id AND q.stock_status='quarantine'
   WHERE h.tenant_id=NEW.tenant_id AND h.outlet_id=NEW.outlet_id AND h.id=NEW.return_id
   AND m.quantity_units=NEW.receipt_units AND NEW.quantity_units<=m.quantity_units AND m.batch_id=NEW.batch_id
   AND b.product_id=NEW.product_id AND b.product_version=NEW.product_version AND b.lot_number=NEW.lot_number AND b.expires_on=NEW.expires_on
   AND COALESCE(NULLIF(r.snapshot->>'brand_name',''),r.snapshot->>'generic_name')=NEW.product_name AND r.snapshot->>'generic_name'=NEW.generic_name AND r.snapshot->>'unit_of_issue'=NEW.unit_code AND r.snapshot->>'currency'=h.currency
   AND m.source_reference=NEW.source_reference AND m.created_at=NEW.source_created_at AND m.source_reference NOT LIKE 'supplier-receipt:%'
   AND NOT EXISTS(SELECT 1 FROM pharmacy_supplier_receipts sr WHERE sr.tenant_id=m.tenant_id AND sr.outlet_id=m.outlet_id AND sr.id=m.id))THEN
   RAISE EXCEPTION 'return line requires exact external receipt evidence' USING ERRCODE='23514',CONSTRAINT='pharmacy_supplier_return_line_source';
  END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_supplier_return_source_guard BEFORE INSERT ON pharmacy_supplier_returns FOR EACH ROW EXECUTE FUNCTION pharmacy_supplier_return_source_guard();
CREATE TRIGGER pharmacy_supplier_return_line_source_guard BEFORE INSERT ON pharmacy_supplier_return_lines FOR EACH ROW EXECUTE FUNCTION pharmacy_supplier_return_source_guard();

CREATE FUNCTION pharmacy_supplier_return_total_guard()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE target uuid;header pharmacy_supplier_returns;line_total bigint;credit_total numeric;
BEGIN
 IF TG_TABLE_NAME='pharmacy_supplier_returns'THEN target:=NEW.id;ELSE target:=NEW.return_id;END IF;
 SELECT *INTO STRICT header FROM pharmacy_supplier_returns h WHERE h.tenant_id=NEW.tenant_id AND h.outlet_id=NEW.outlet_id AND h.id=target;
 SELECT COUNT(*),COALESCE(SUM(expected_credit_minor),0)INTO line_total,credit_total FROM pharmacy_supplier_return_lines l WHERE l.tenant_id=NEW.tenant_id AND l.outlet_id=NEW.outlet_id AND l.return_id=target;
 IF line_total<>header.line_count OR credit_total<>header.expected_credit_minor OR EXISTS(
  SELECT 1 FROM pharmacy_supplier_return_lines l WHERE l.tenant_id=NEW.tenant_id AND l.outlet_id=NEW.outlet_id AND l.return_id=target
  GROUP BY receipt_id HAVING SUM(quantity_units)>MIN(receipt_units)
 )THEN
  RAISE EXCEPTION 'return requires its complete bounded receipt lines and credit expectation' USING ERRCODE='23514',CONSTRAINT='pharmacy_supplier_return_total';
 END IF;
 RETURN NEW;
END $$;
CREATE CONSTRAINT TRIGGER pharmacy_supplier_return_total_guard AFTER INSERT ON pharmacy_supplier_returns DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION pharmacy_supplier_return_total_guard();
CREATE CONSTRAINT TRIGGER pharmacy_supplier_return_line_total_guard AFTER INSERT ON pharmacy_supplier_return_lines DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION pharmacy_supplier_return_total_guard();

CREATE TABLE pharmacy_supplier_return_cancellations(
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),tenant_id uuid NOT NULL,outlet_id uuid NOT NULL,return_id uuid NOT NULL,
 reason text NOT NULL CHECK(length(btrim(reason))BETWEEN 1 AND 500),intent_sha256 text NOT NULL CHECK(intent_sha256~'^[a-f0-9]{64}$'),
 actor_subject text NOT NULL CHECK(length(btrim(actor_subject))BETWEEN 1 AND 200),created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,outlet_id,id),UNIQUE(tenant_id,outlet_id,return_id),
 FOREIGN KEY(tenant_id,outlet_id,return_id)REFERENCES pharmacy_supplier_returns(tenant_id,outlet_id,id)ON DELETE RESTRICT
);
ALTER TABLE pharmacy_supplier_return_cancellations ENABLE ROW LEVEL SECURITY;ALTER TABLE pharmacy_supplier_return_cancellations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_supplier_return_cancellations
 USING(hms_tenant_visible(tenant_id)AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND pharmacy_outlet_allowed(tenant_id,outlet_id,true)AND actor_subject=NULLIF(current_setting('app.pharmacy_subject',true),''));
CREATE TRIGGER pharmacy_supplier_return_cancellation_audit AFTER INSERT ON pharmacy_supplier_return_cancellations FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_supplier_return_cancellation_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_supplier_return_cancellations FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

DO $purpose$
DECLARE target text;
BEGIN
 FOREACH target IN ARRAY ARRAY['pharmacy_supplier_returns','pharmacy_supplier_return_lines','pharmacy_supplier_return_cancellations'] LOOP
  EXECUTE format($policy$CREATE POLICY operating_choice_purpose ON %I AS RESTRICTIVE
   USING(hms_tenant_visible(tenant_id)AND COALESCE(current_setting('app.pharmacy_operating_choices',true),'')='')
   WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND COALESCE(current_setting('app.pharmacy_operating_choices',true),'')='')$policy$,target);
  EXECUTE format($policy$CREATE POLICY operating_choice_delete ON %I AS RESTRICTIVE FOR DELETE
   USING(hms_tenant_visible(tenant_id)AND COALESCE(current_setting('app.pharmacy_operating_choices',true),'')='')$policy$,target);
 END LOOP;
END $purpose$;

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
