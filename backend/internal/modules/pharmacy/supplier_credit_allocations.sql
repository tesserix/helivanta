SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
DO $$BEGIN
 IF NOT pharmacy_operating_choice_integrity() THEN RAISE EXCEPTION 'existing pharmacy purpose policies changed' USING ERRCODE='23514'; END IF;
END $$;

CREATE TABLE pharmacy_supplier_credit_allocations(
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),tenant_id uuid NOT NULL,outlet_id uuid NOT NULL,credit_id uuid NOT NULL,payable_id uuid NOT NULL,
 expected_version bigint NOT NULL CHECK(expected_version BETWEEN 1 AND 999999999999),expected_available_minor bigint NOT NULL CHECK(expected_available_minor BETWEEN 0 AND 1000000000000),
 expected_payable_version bigint NOT NULL CHECK(expected_payable_version BETWEEN 1 AND 999999999999),expected_outstanding_minor bigint NOT NULL CHECK(expected_outstanding_minor BETWEEN 0 AND 1000000000000),
 amount_minor bigint NOT NULL CHECK(amount_minor BETWEEN 1 AND 1000000000000),occurred_on date NOT NULL CHECK(occurred_on>='1970-01-01'),reason text NOT NULL CHECK(length(btrim(reason)) BETWEEN 1 AND 500),
 payable_reference text NOT NULL CHECK(length(payable_reference) BETWEEN 1 AND 100),credit_document_reference text NOT NULL CHECK(length(credit_document_reference) BETWEEN 1 AND 100),payee_name text NOT NULL CHECK(length(payee_name) BETWEEN 1 AND 200),
 intent_sha256 text NOT NULL CHECK(intent_sha256~'^[a-f0-9]{64}$'),actor_subject text NOT NULL CHECK(length(btrim(actor_subject)) BETWEEN 1 AND 200),created_at timestamptz NOT NULL DEFAULT now(),
 available_minor bigint GENERATED ALWAYS AS(expected_available_minor-amount_minor)STORED CHECK(available_minor BETWEEN 0 AND 1000000000000),
 outstanding_minor bigint GENERATED ALWAYS AS(expected_outstanding_minor-amount_minor)STORED CHECK(outstanding_minor BETWEEN 0 AND 1000000000000),
 PRIMARY KEY(tenant_id,outlet_id,id),
 FOREIGN KEY(tenant_id,outlet_id,credit_id)REFERENCES pharmacy_supplier_credit_documents(tenant_id,outlet_id,id)ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,outlet_id,payable_id)REFERENCES pharmacy_payables(tenant_id,outlet_id,id)ON DELETE RESTRICT,
 UNIQUE(tenant_id,outlet_id,credit_id,id)
);
CREATE INDEX pharmacy_supplier_credit_allocations_credit ON pharmacy_supplier_credit_allocations(tenant_id,outlet_id,credit_id,created_at DESC,id DESC);
CREATE INDEX pharmacy_supplier_credit_allocations_bill ON pharmacy_supplier_credit_allocations(tenant_id,outlet_id,payable_id);

CREATE TABLE pharmacy_supplier_credit_allocation_reversals(
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),tenant_id uuid NOT NULL,outlet_id uuid NOT NULL,credit_id uuid NOT NULL,payable_id uuid NOT NULL,
 expected_version bigint NOT NULL CHECK(expected_version BETWEEN 1 AND 999999999999),expected_available_minor bigint NOT NULL CHECK(expected_available_minor BETWEEN 0 AND 1000000000000),
 expected_payable_version bigint NOT NULL CHECK(expected_payable_version BETWEEN 1 AND 999999999999),expected_outstanding_minor bigint NOT NULL CHECK(expected_outstanding_minor BETWEEN 0 AND 1000000000000),
 amount_minor bigint NOT NULL CHECK(amount_minor BETWEEN 1 AND 1000000000000),occurred_on date NOT NULL CHECK(occurred_on>='1970-01-01'),reason text NOT NULL CHECK(length(btrim(reason)) BETWEEN 1 AND 500),
 payable_reference text NOT NULL CHECK(length(payable_reference) BETWEEN 1 AND 100),credit_document_reference text NOT NULL CHECK(length(credit_document_reference) BETWEEN 1 AND 100),payee_name text NOT NULL CHECK(length(payee_name) BETWEEN 1 AND 200),
 intent_sha256 text NOT NULL CHECK(intent_sha256~'^[a-f0-9]{64}$'),actor_subject text NOT NULL CHECK(length(btrim(actor_subject)) BETWEEN 1 AND 200),created_at timestamptz NOT NULL DEFAULT now(),
 allocation_id uuid NOT NULL,
 available_minor bigint GENERATED ALWAYS AS(expected_available_minor+amount_minor)STORED CHECK(available_minor BETWEEN 0 AND 1000000000000),
 outstanding_minor bigint GENERATED ALWAYS AS(expected_outstanding_minor+amount_minor)STORED CHECK(outstanding_minor BETWEEN 0 AND 1000000000000),
 PRIMARY KEY(tenant_id,outlet_id,id),
 FOREIGN KEY(tenant_id,outlet_id,credit_id)REFERENCES pharmacy_supplier_credit_documents(tenant_id,outlet_id,id)ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,outlet_id,payable_id)REFERENCES pharmacy_payables(tenant_id,outlet_id,id)ON DELETE RESTRICT,
 UNIQUE(tenant_id,outlet_id,allocation_id),
 FOREIGN KEY(tenant_id,outlet_id,credit_id,allocation_id)REFERENCES pharmacy_supplier_credit_allocations(tenant_id,outlet_id,credit_id,id)ON DELETE RESTRICT
);
CREATE INDEX pharmacy_supplier_credit_allocation_reversals_credit ON pharmacy_supplier_credit_allocation_reversals(tenant_id,outlet_id,credit_id,created_at DESC,id DESC);
CREATE INDEX pharmacy_supplier_credit_allocation_reversals_bill ON pharmacy_supplier_credit_allocation_reversals(tenant_id,outlet_id,payable_id);

DO $policies$DECLARE target text;BEGIN
 FOREACH target IN ARRAY ARRAY['pharmacy_supplier_credit_allocations','pharmacy_supplier_credit_allocation_reversals'] LOOP
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
  EXECUTE format('CREATE TRIGGER %I BEFORE UPDATE OR DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable()',target||'_immutable',target);
  EXECUTE format('CREATE TRIGGER %I AFTER INSERT ON %I FOR EACH ROW EXECUTE FUNCTION audit_record_change()',target||'_audit',target);
 END LOOP;
END $policies$;


CREATE FUNCTION pharmacy_supplier_credit_payable_eligible(t uuid,o uuid,p uuid)RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT EXISTS(SELECT 1 FROM pharmacy_payable_lines l WHERE l.tenant_id=t AND l.outlet_id=o AND l.payable_id=p AND l.category='inventory')
 AND NOT EXISTS(SELECT 1 FROM pharmacy_payable_lines l LEFT JOIN pharmacy_stock_movements m ON m.tenant_id=l.tenant_id AND m.outlet_id=l.outlet_id AND m.id=l.receipt_id
 WHERE l.tenant_id=t AND l.outlet_id=o AND l.payable_id=p AND l.category='inventory' AND(m.id IS NULL OR m.kind<>'receipt' OR starts_with(m.source_reference,'supplier-receipt:')))
$$;
CREATE FUNCTION pharmacy_supplier_payable_allocated(t uuid,o uuid,p uuid)RETURNS numeric LANGUAGE sql STABLE AS $$
 SELECT COALESCE(sum(a.amount_minor),0)FROM pharmacy_supplier_credit_allocations a WHERE a.tenant_id=t AND a.outlet_id=o AND a.payable_id=p
 AND NOT EXISTS(SELECT 1 FROM pharmacy_supplier_credit_allocation_reversals r WHERE r.tenant_id=a.tenant_id AND r.outlet_id=a.outlet_id AND r.allocation_id=a.id)
$$;
CREATE FUNCTION pharmacy_supplier_payable_version(t uuid,o uuid,p uuid)RETURNS bigint LANGUAGE sql STABLE AS $$
 SELECT 1+(SELECT count(*)FROM pharmacy_settlements WHERE tenant_id=t AND outlet_id=o AND payable_id=p)
 +(SELECT count(*)FROM pharmacy_supplier_credit_allocations WHERE tenant_id=t AND outlet_id=o AND payable_id=p)
 +(SELECT count(*)FROM pharmacy_supplier_credit_allocation_reversals WHERE tenant_id=t AND outlet_id=o AND payable_id=p)
$$;
CREATE OR REPLACE VIEW pharmacy_payable_balances WITH(security_invoker=true)AS
 SELECT p.id,p.tenant_id,p.outlet_id,p.payee_reference,p.payee_name,p.document_reference,p.issued_on,p.due_on,p.currency,p.line_count,p.total_minor,p.actor_subject,p.created_at,
 COALESCE(s.paid,0)::bigint AS paid_minor,(COALESCE(s.credit,0)+a.allocated)::bigint AS credit_minor,(p.total_minor-COALESCE(s.paid,0)-COALESCE(s.credit,0)-a.allocated)::bigint AS outstanding_minor
 FROM pharmacy_payables p LEFT JOIN LATERAL(
 SELECT sum(CASE WHEN kind='payment' THEN amount_minor WHEN kind='payment_reversal' THEN -amount_minor ELSE 0 END)AS paid,
 sum(CASE WHEN kind='credit' THEN amount_minor ELSE 0 END)AS credit FROM pharmacy_settlements WHERE tenant_id=p.tenant_id AND outlet_id=p.outlet_id AND payable_id=p.id
 )s ON true CROSS JOIN LATERAL(SELECT pharmacy_supplier_payable_allocated(p.tenant_id,p.outlet_id,p.id)AS allocated)a;

CREATE OR REPLACE FUNCTION pharmacy_settlement_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE doc pharmacy_payables; original pharmacy_settlements; settled numeric;
BEGIN
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id::text||':'||NEW.outlet_id::text||':payable:'||NEW.payable_id::text,0));
 SELECT * INTO doc FROM pharmacy_payables WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.payable_id;
 IF doc.id IS NULL OR doc.currency<>NEW.currency OR NEW.occurred_on<doc.issued_on THEN RAISE EXCEPTION 'invalid settlement document, currency or date' USING ERRCODE='23514',CONSTRAINT='pharmacy_settlement_document'; END IF;
 IF NEW.kind='payment_reversal' THEN
  SELECT * INTO original FROM pharmacy_settlements WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.reversal_of;
  IF original.id IS NULL OR original.kind<>'payment' OR original.payable_id<>NEW.payable_id OR original.amount_minor<>NEW.amount_minor OR NEW.occurred_on<original.occurred_on THEN
   RAISE EXCEPTION 'reversal must match original payment' USING ERRCODE='23514',CONSTRAINT='pharmacy_settlement_reversal';
  END IF;
 END IF;
 SELECT COALESCE(sum(CASE WHEN kind='payment_reversal' THEN -amount_minor ELSE amount_minor END),0) INTO settled FROM pharmacy_settlements WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND payable_id=NEW.payable_id;
 settled := settled + pharmacy_supplier_payable_allocated(NEW.tenant_id,NEW.outlet_id,NEW.payable_id);
 settled := settled + CASE WHEN NEW.kind='payment_reversal' THEN -NEW.amount_minor ELSE NEW.amount_minor END;
 IF settled<0 OR settled>doc.total_minor THEN RAISE EXCEPTION 'settlement exceeds outstanding amount' USING ERRCODE='23514',CONSTRAINT='pharmacy_settlement_balance'; END IF;
 RETURN NEW;
END $$;

CREATE FUNCTION pharmacy_supplier_credit_allocation_guard()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE position pharmacy_supplier_credit_positions;document pharmacy_supplier_credit_documents;bill record;zone text;
BEGIN
 PERFORM pharmacy_supplier_credit_lock(NEW.tenant_id,NEW.outlet_id,NEW.credit_id);
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id::text||':'||NEW.outlet_id::text||':payable:'||NEW.payable_id::text,0));
 SELECT * INTO position FROM pharmacy_supplier_credit_positions WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.credit_id FOR UPDATE;
 SELECT * INTO document FROM pharmacy_supplier_credit_documents WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.credit_id;
 SELECT id,payee_reference,currency,issued_on,document_reference,total_minor,outstanding_minor INTO bill FROM pharmacy_payable_balances WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.payable_id;
 SELECT timezone INTO zone FROM pharmacy_outlets WHERE tenant_id=NEW.tenant_id AND id=NEW.outlet_id;
 IF position.id IS NULL OR position.state<>'active' OR position.version<>NEW.expected_version OR
 document.amount_minor-position.allocated_minor<>NEW.expected_available_minor OR bill.id IS NULL OR bill.payee_reference<>document.payee_reference OR bill.currency<>document.currency OR
 NOT pharmacy_supplier_credit_payable_eligible(NEW.tenant_id,NEW.outlet_id,NEW.payable_id) OR
 pharmacy_supplier_payable_version(NEW.tenant_id,NEW.outlet_id,NEW.payable_id)<>NEW.expected_payable_version OR bill.outstanding_minor<>NEW.expected_outstanding_minor OR
 NEW.amount_minor>NEW.expected_available_minor OR NEW.amount_minor>bill.outstanding_minor OR
 NEW.occurred_on<document.issued_on OR NEW.occurred_on<bill.issued_on OR NEW.occurred_on>(clock_timestamp()AT TIME ZONE zone)::date THEN
 RAISE EXCEPTION 'allocation requires current credit, eligible supplier bill and conserved observed balances' USING ERRCODE='23514',CONSTRAINT='pharmacy_supplier_credit_allocation_state';
 END IF;
 NEW.payable_reference:=bill.document_reference;NEW.credit_document_reference:=document.document_reference;NEW.payee_name:=document.payee_name;
 UPDATE pharmacy_supplier_credit_positions SET allocated_minor=allocated_minor+NEW.amount_minor,version=version+1,actor_subject=NEW.actor_subject WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.credit_id;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_supplier_credit_allocation_guard BEFORE INSERT ON pharmacy_supplier_credit_allocations FOR EACH ROW EXECUTE FUNCTION pharmacy_supplier_credit_allocation_guard();

CREATE FUNCTION pharmacy_supplier_credit_allocation_reversal_guard()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE position pharmacy_supplier_credit_positions;document pharmacy_supplier_credit_documents;original pharmacy_supplier_credit_allocations;bill record;zone text;
BEGIN
 PERFORM pharmacy_supplier_credit_lock(NEW.tenant_id,NEW.outlet_id,NEW.credit_id);
 SELECT * INTO original FROM pharmacy_supplier_credit_allocations WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND credit_id=NEW.credit_id AND id=NEW.allocation_id;
 IF original.id IS NULL OR original.payable_id<>NEW.payable_id OR original.amount_minor<>NEW.amount_minor THEN
 RAISE EXCEPTION 'allocation reversal requires the exact original entry' USING ERRCODE='23514',CONSTRAINT='pharmacy_supplier_credit_allocation_reversal_source';END IF;
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id::text||':'||NEW.outlet_id::text||':payable:'||original.payable_id::text,0));
 SELECT * INTO position FROM pharmacy_supplier_credit_positions WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.credit_id FOR UPDATE;
 SELECT * INTO document FROM pharmacy_supplier_credit_documents WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.credit_id;
 SELECT id,total_minor,outstanding_minor INTO bill FROM pharmacy_payable_balances WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=original.payable_id;
 SELECT timezone INTO zone FROM pharmacy_outlets WHERE tenant_id=NEW.tenant_id AND id=NEW.outlet_id;
 IF position.id IS NULL OR position.state<>'active' OR position.version<>NEW.expected_version OR
 document.amount_minor-position.allocated_minor<>NEW.expected_available_minor OR position.allocated_minor<original.amount_minor OR bill.id IS NULL OR
 pharmacy_supplier_payable_version(NEW.tenant_id,NEW.outlet_id,original.payable_id)<>NEW.expected_payable_version OR bill.outstanding_minor<>NEW.expected_outstanding_minor OR
 NEW.expected_outstanding_minor+original.amount_minor>bill.total_minor OR NEW.occurred_on<original.occurred_on OR NEW.occurred_on>(clock_timestamp()AT TIME ZONE zone)::date OR
 EXISTS(SELECT 1 FROM pharmacy_supplier_credit_allocation_reversals WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND allocation_id=original.id) THEN
 RAISE EXCEPTION 'reversal requires current unreversed allocation and observed balances' USING ERRCODE='23514',CONSTRAINT='pharmacy_supplier_credit_allocation_reversal_state';END IF;
 NEW.payable_reference:=original.payable_reference;NEW.credit_document_reference:=original.credit_document_reference;NEW.payee_name:=original.payee_name;
 UPDATE pharmacy_supplier_credit_positions SET allocated_minor=allocated_minor-original.amount_minor,version=version+1,actor_subject=NEW.actor_subject WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.credit_id;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_supplier_credit_allocation_reversal_guard BEFORE INSERT ON pharmacy_supplier_credit_allocation_reversals FOR EACH ROW EXECUTE FUNCTION pharmacy_supplier_credit_allocation_reversal_guard();

CREATE OR REPLACE FUNCTION pharmacy_supplier_credit_projection_complete()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE credit uuid;position pharmacy_supplier_credit_positions;voids bigint;corrections bigint;matches bigint;reversals bigint;matched numeric;allocations bigint;allocation_reversals bigint;allocated numeric;expected_actor text;expected_state text;
BEGIN
 IF TG_TABLE_NAME IN('pharmacy_supplier_credit_voids','pharmacy_supplier_credit_matches','pharmacy_supplier_credit_match_reversals','pharmacy_supplier_credit_allocations','pharmacy_supplier_credit_allocation_reversals')THEN credit:=NEW.credit_id;ELSE credit:=NEW.id;END IF;
 SELECT * INTO STRICT position FROM pharmacy_supplier_credit_positions WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=credit;
 SELECT count(*)INTO voids FROM pharmacy_supplier_credit_voids WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND credit_id=credit;
 SELECT count(*)INTO corrections FROM pharmacy_supplier_credit_documents WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND correction_of=credit;
 SELECT count(*),COALESCE(sum(amount_minor),0)INTO matches,matched FROM pharmacy_supplier_credit_matches WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND credit_id=credit;
 SELECT count(*),matched-COALESCE(sum(amount_minor),0)INTO reversals,matched FROM pharmacy_supplier_credit_match_reversals WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND credit_id=credit;
 SELECT count(*),COALESCE(sum(amount_minor),0)INTO allocations,allocated FROM pharmacy_supplier_credit_allocations WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND credit_id=credit;
 SELECT count(*),allocated-COALESCE(sum(amount_minor),0)INTO allocation_reversals,allocated FROM pharmacy_supplier_credit_allocation_reversals WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND credit_id=credit;
 IF corrections>0 THEN
  expected_state:='corrected';SELECT actor_subject INTO expected_actor FROM pharmacy_supplier_credit_documents WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND correction_of=credit;
 ELSIF voids>0 THEN
  expected_state:='voided';SELECT actor_subject INTO expected_actor FROM pharmacy_supplier_credit_voids WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND credit_id=credit;
 ELSE
  expected_state:='active';
  SELECT actor_subject INTO expected_actor FROM(
   SELECT actor_subject,expected_version FROM pharmacy_supplier_credit_matches WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND credit_id=credit
   UNION ALL SELECT actor_subject,expected_version FROM pharmacy_supplier_credit_match_reversals WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND credit_id=credit
   UNION ALL SELECT actor_subject,expected_version FROM pharmacy_supplier_credit_allocations WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND credit_id=credit
   UNION ALL SELECT actor_subject,expected_version FROM pharmacy_supplier_credit_allocation_reversals WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND credit_id=credit
   UNION ALL SELECT actor_subject,0 FROM pharmacy_supplier_credit_documents WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=credit
  )latest ORDER BY expected_version DESC LIMIT 1;
 END IF;
 IF position.version<>1+voids+corrections+matches+reversals+allocations+allocation_reversals OR position.state<>expected_state OR position.actor_subject<>expected_actor OR position.matched_minor<>matched OR position.allocated_minor<>allocated THEN
  RAISE EXCEPTION 'credit position requires complete immutable evidence' USING ERRCODE='23514',CONSTRAINT='pharmacy_supplier_credit_projection_complete';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER pharmacy_supplier_credit_allocations_complete AFTER INSERT ON pharmacy_supplier_credit_allocations DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION pharmacy_supplier_credit_projection_complete();
CREATE CONSTRAINT TRIGGER pharmacy_supplier_credit_allocation_reversals_complete AFTER INSERT ON pharmacy_supplier_credit_allocation_reversals DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION pharmacy_supplier_credit_projection_complete();

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
