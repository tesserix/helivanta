SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
DO $$BEGIN
 IF NOT pharmacy_operating_choice_integrity() THEN RAISE EXCEPTION 'existing pharmacy purpose policies changed' USING ERRCODE='23514'; END IF;
END $$;

CREATE TABLE pharmacy_purchase_tax_evidence(
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),tenant_id uuid NOT NULL,outlet_id uuid NOT NULL,payable_id uuid NOT NULL,
 replaces uuid CHECK(replaces<>id),supplier_gstin text CHECK(supplier_gstin~'^[0-9]{2}[A-Z]{5}[0-9]{4}[A-Z][1-9A-Z]Z[0-9A-Z]$' AND left(supplier_gstin,2)=supplier_state),
 supplier_state text NOT NULL CHECK(supplier_state~'^[0-9]{2}$'),place_of_supply text NOT NULL CHECK(place_of_supply~'^[0-9]{2}$'),
 reason text NOT NULL CHECK(length(btrim(reason)) BETWEEN 1 AND 500),line_count integer NOT NULL CHECK(line_count BETWEEN 1 AND 100),
 intent_sha256 text NOT NULL CHECK(intent_sha256~'^[a-f0-9]{64}$'),actor_subject text NOT NULL CHECK(length(btrim(actor_subject))BETWEEN 1 AND 200),created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,outlet_id,id),UNIQUE(tenant_id,outlet_id,payable_id,id),
 CONSTRAINT pharmacy_purchase_tax_successor UNIQUE NULLS NOT DISTINCT(tenant_id,outlet_id,payable_id,replaces),
 FOREIGN KEY(tenant_id,outlet_id,payable_id)REFERENCES pharmacy_payables(tenant_id,outlet_id,id)ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,outlet_id,payable_id,replaces)REFERENCES pharmacy_purchase_tax_evidence(tenant_id,outlet_id,payable_id,id)ON DELETE RESTRICT
);
ALTER TABLE pharmacy_purchase_tax_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_purchase_tax_evidence FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_purchase_tax_evidence
 USING(hms_tenant_visible(tenant_id)AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND pharmacy_outlet_allowed(tenant_id,outlet_id,true)AND actor_subject=NULLIF(current_setting('app.pharmacy_subject',true),''));
CREATE TRIGGER pharmacy_purchase_tax_audit AFTER INSERT ON pharmacy_purchase_tax_evidence FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_purchase_tax_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_purchase_tax_evidence FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

CREATE TABLE pharmacy_purchase_tax_lines(
 tenant_id uuid NOT NULL,outlet_id uuid NOT NULL,payable_id uuid NOT NULL,evidence_id uuid NOT NULL,position integer NOT NULL CHECK(position BETWEEN 1 AND 100),
 supply_nature text NOT NULL CHECK(supply_nature IN('taxable','nil_rated','exempt','outside_gst')),rate_ppm bigint NOT NULL CHECK(rate_ppm BETWEEN 0 AND 1000000 AND CASE WHEN supply_nature='taxable' THEN rate_ppm>=100 AND rate_ppm%100=0 ELSE rate_ppm=0 END),
 cgst_minor bigint NOT NULL CHECK(cgst_minor BETWEEN 0 AND 1000000000000),sgst_minor bigint NOT NULL CHECK(sgst_minor BETWEEN 0 AND 1000000000000),
 igst_minor bigint NOT NULL CHECK(igst_minor BETWEEN 0 AND 1000000000000),utgst_minor bigint NOT NULL CHECK(utgst_minor BETWEEN 0 AND 1000000000000),
 cgst_recoverable_minor bigint NOT NULL CHECK(cgst_recoverable_minor BETWEEN 0 AND cgst_minor),sgst_recoverable_minor bigint NOT NULL CHECK(sgst_recoverable_minor BETWEEN 0 AND sgst_minor),
 igst_recoverable_minor bigint NOT NULL CHECK(igst_recoverable_minor BETWEEN 0 AND igst_minor),utgst_recoverable_minor bigint NOT NULL CHECK(utgst_recoverable_minor BETWEEN 0 AND utgst_minor),
 created_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(tenant_id,outlet_id,evidence_id,position),
 FOREIGN KEY(tenant_id,outlet_id,payable_id,evidence_id)REFERENCES pharmacy_purchase_tax_evidence(tenant_id,outlet_id,payable_id,id)ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,outlet_id,payable_id,position)REFERENCES pharmacy_payable_lines(tenant_id,outlet_id,payable_id,position)ON DELETE RESTRICT
);
ALTER TABLE pharmacy_purchase_tax_lines ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_purchase_tax_lines FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_purchase_tax_lines
 USING(hms_tenant_visible(tenant_id)AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND pharmacy_outlet_allowed(tenant_id,outlet_id,true)
 AND EXISTS(SELECT 1 FROM pharmacy_purchase_tax_evidence e WHERE e.tenant_id=pharmacy_purchase_tax_lines.tenant_id AND e.outlet_id=pharmacy_purchase_tax_lines.outlet_id AND e.id=pharmacy_purchase_tax_lines.evidence_id AND e.actor_subject=NULLIF(current_setting('app.pharmacy_subject',true),'')));
CREATE TRIGGER pharmacy_purchase_tax_line_audit AFTER INSERT ON pharmacy_purchase_tax_lines FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_purchase_tax_line_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_purchase_tax_lines FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

CREATE FUNCTION pharmacy_purchase_tax_line_guard()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE original pharmacy_payable_lines; evidence pharmacy_purchase_tax_evidence; total bigint; recoverable bigint;
BEGIN
 SELECT * INTO original FROM pharmacy_payable_lines WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND payable_id=NEW.payable_id AND position=NEW.position;
 SELECT * INTO evidence FROM pharmacy_purchase_tax_evidence WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND payable_id=NEW.payable_id AND id=NEW.evidence_id;
 total:=NEW.cgst_minor+NEW.sgst_minor+NEW.igst_minor+NEW.utgst_minor;
 recoverable:=NEW.cgst_recoverable_minor+NEW.sgst_recoverable_minor+NEW.igst_recoverable_minor+NEW.utgst_recoverable_minor;
 IF original.id IS NULL OR evidence.id IS NULL OR total<>original.tax_minor OR recoverable<>original.recoverable_tax_minor
 OR(total>0 AND(evidence.supplier_gstin IS NULL OR NEW.supply_nature<>'taxable'OR NEW.rate_ppm=0))
 OR(evidence.supplier_state<>evidence.place_of_supply AND NEW.cgst_minor+NEW.sgst_minor+NEW.utgst_minor<>0)
 OR(evidence.supplier_state=evidence.place_of_supply AND(NEW.igst_minor<>0 OR abs(NEW.cgst_minor-NEW.sgst_minor-NEW.utgst_minor)>1
  OR CASE WHEN evidence.supplier_state IN('04','26','31','35','38','97')THEN NEW.sgst_minor<>0 ELSE NEW.utgst_minor<>0 END))THEN
  RAISE EXCEPTION 'purchase tax components must match the source bill and jurisdiction' USING ERRCODE='23514',CONSTRAINT='pharmacy_purchase_tax_source';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_purchase_tax_line_guard BEFORE INSERT ON pharmacy_purchase_tax_lines FOR EACH ROW EXECUTE FUNCTION pharmacy_purchase_tax_line_guard();

CREATE FUNCTION pharmacy_purchase_tax_complete()RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM pharmacy_payables p WHERE p.tenant_id=NEW.tenant_id AND p.outlet_id=NEW.outlet_id AND p.id=NEW.payable_id AND p.currency='INR'AND p.line_count=NEW.line_count)
 OR NEW.line_count<>(SELECT count(*)FROM pharmacy_purchase_tax_lines l WHERE l.tenant_id=NEW.tenant_id AND l.outlet_id=NEW.outlet_id AND l.evidence_id=NEW.id)THEN
  RAISE EXCEPTION 'purchase tax evidence requires every original line' USING ERRCODE='23514',CONSTRAINT='pharmacy_purchase_tax_complete';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER pharmacy_purchase_tax_complete AFTER INSERT ON pharmacy_purchase_tax_evidence DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION pharmacy_purchase_tax_complete();

ALTER TABLE pharmacy_purchase_tax_evidence ADD CONSTRAINT pharmacy_purchase_tax_states CHECK(supplier_state IN('10','11','12','13','14','15','16','17','18','19','20','21','22','23','24','26','27','29','30','31','32','33','34','35','36','37','38','97','01','02','03','04','05','06','07','08','09')AND place_of_supply IN('10','11','12','13','14','15','16','17','18','19','20','21','22','23','24','26','27','29','30','31','32','33','34','35','36','37','38','97','01','02','03','04','05','06','07','08','09'));

ALTER TABLE pharmacy_tally_ledger_map_entries DROP CONSTRAINT pharmacy_tally_ledger_map_head;
ALTER TABLE pharmacy_tally_ledger_map_entries ADD CONSTRAINT pharmacy_tally_ledger_map_head CHECK(head~'^(voucher_type:(sale|credit_note|supplier_payment|supplier_payment_reversal|supplier_purchase)|tender:(cash|upi|upi_verified|card|admission_account)|sales_(goods|service):(taxable:(0|[1-9][0-9]{0,5}|1000000)|(nil_rated|exempt):0)|purchases:(inventory|utilities|rent|salary|transport|maintenance|professional|other):(taxable:(0|[1-9][0-9]{0,5}|1000000)|(nil_rated|exempt|outside_gst):0)|((input_|nonrecoverable_)?(cgst|sgst|igst|utgst)):(0|[1-9][0-9]{0,5}|1000000)|payment_method:(cash|bank|card|upi|other)|supplier:.+)$'
 AND(head!~'^supplier:'OR(length(head)BETWEEN 10 AND 109 AND substr(head,10)=upper(btrim(substr(head,10)))))AND head!~'[\x01-\x1f\x7f-\x9f￾￿]');

CREATE TABLE pharmacy_tally_export_receipts(
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),tenant_id uuid NOT NULL,outlet_id uuid NOT NULL,
 from_date date NOT NULL,to_date date NOT NULL CHECK(to_date>=from_date AND to_date-from_date<366),ledger_map_id uuid,
 sha256 text NOT NULL CHECK(sha256~'^[a-f0-9]{64}$'),source_sha256 text NOT NULL CHECK(source_sha256~'^[a-f0-9]{64}$'),
 voucher_count integer NOT NULL CHECK(voucher_count BETWEEN 0 AND 5000),purchase_bills boolean NOT NULL,actor_subject text NOT NULL CHECK(length(btrim(actor_subject))BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT now(),PRIMARY KEY(tenant_id,outlet_id,id),
 FOREIGN KEY(tenant_id,outlet_id)REFERENCES pharmacy_outlets(tenant_id,id)ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,outlet_id,ledger_map_id)REFERENCES pharmacy_tally_ledger_maps(tenant_id,outlet_id,id)ON DELETE RESTRICT
);
ALTER TABLE pharmacy_tally_export_receipts ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_tally_export_receipts FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_tally_export_receipts
 USING(hms_tenant_visible(tenant_id)AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND pharmacy_outlet_allowed(tenant_id,outlet_id,false)AND actor_subject=NULLIF(current_setting('app.pharmacy_subject',true),''));
CREATE TRIGGER pharmacy_tally_export_receipt_audit AFTER INSERT ON pharmacy_tally_export_receipts FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_tally_export_receipt_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_tally_export_receipts FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

DO $purpose$
DECLARE target text;
BEGIN
 FOREACH target IN ARRAY ARRAY['pharmacy_purchase_tax_evidence','pharmacy_purchase_tax_lines','pharmacy_tally_export_receipts'] LOOP
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
