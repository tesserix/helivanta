SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

DO $$DECLARE choice_definition text;choice_metadata_ok boolean;BEGIN
 IF pharmacy_operating_choice_integrity()IS DISTINCT FROM true OR pharmacy_stocktake_hospital_integrity()IS DISTINCT FROM true THEN
  RAISE EXCEPTION 'existing pharmacy operating/hospital authority metadata changed' USING ERRCODE='23514';
 END IF;
 -- The hospital seal above authenticates this definition. Its runtime support
 -- probe forbids TRUNCATE for app roles, so it cannot run as the schema owner.
 SELECT prosrc INTO choice_definition FROM pg_proc WHERE oid=to_regprocedure('pharmacy_stocktake_hospital_choice_integrity()');
 IF choice_definition IS NULL OR position('pharmacy_operating_choice_support()'IN choice_definition)=0 THEN
  RAISE EXCEPTION 'unexpected pharmacy choice integrity definition' USING ERRCODE='23514';
 END IF;
 EXECUTE replace(choice_definition,'pharmacy_operating_choice_support()','pharmacy_operating_choice_integrity()') INTO choice_metadata_ok;
 IF choice_metadata_ok IS DISTINCT FROM true THEN
  RAISE EXCEPTION 'existing pharmacy choice authority metadata changed' USING ERRCODE='23514';
 END IF;
END $$;

CREATE TABLE pharmacy_product_barcodes (
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 tenant_id uuid NOT NULL,
 outlet_id uuid NOT NULL,
 gtin text NOT NULL CHECK (gtin ~ '^[0-9]{14}$'),
 product_id uuid NOT NULL,
 actor_subject text NOT NULL CHECK (length(btrim(actor_subject)) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,outlet_id,gtin),
 UNIQUE (tenant_id,id),
 FOREIGN KEY (tenant_id,outlet_id,product_id) REFERENCES pharmacy_catalogue_products(tenant_id,outlet_id,id)
);
ALTER TABLE pharmacy_product_barcodes ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_product_barcodes FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_product_barcodes
 USING (hms_tenant_visible(tenant_id) AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK (pharmacy_outlet_allowed(tenant_id,outlet_id,true));
CREATE TRIGGER pharmacy_product_barcode_audit AFTER INSERT ON pharmacy_product_barcodes
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_product_barcode_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_product_barcodes
 FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

DO $purpose$
DECLARE target text;
BEGIN
 FOREACH target IN ARRAY ARRAY['pharmacy_product_barcodes'] LOOP
  EXECUTE format($policy$CREATE POLICY operating_choice_purpose ON %I AS RESTRICTIVE
   USING(hms_tenant_visible(tenant_id)AND COALESCE(current_setting('app.pharmacy_operating_choices',true),'')='')
   WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND COALESCE(current_setting('app.pharmacy_operating_choices',true),'')='')$policy$,target);
  EXECUTE format($policy$CREATE POLICY operating_choice_delete ON %I AS RESTRICTIVE FOR DELETE
   USING(hms_tenant_visible(tenant_id)AND COALESCE(current_setting('app.pharmacy_operating_choices',true),'')='')$policy$,target);
  EXECUTE format($policy$CREATE POLICY hospital_stocktake_choice_purpose ON %I AS RESTRICTIVE
   USING(hms_tenant_visible(tenant_id)AND COALESCE(current_setting('app.pharmacy_stocktake_choices',true),'')='')
   WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND COALESCE(current_setting('app.pharmacy_stocktake_choices',true),'')='')$policy$,target);
  EXECUTE format($policy$CREATE POLICY hospital_stocktake_choice_delete ON %I AS RESTRICTIVE FOR DELETE
   USING(hms_tenant_visible(tenant_id)AND COALESCE(current_setting('app.pharmacy_stocktake_choices',true),'')='')$policy$,target);
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

-- The old seals were checked before DDL. Refresh only after the reviewed owner metadata extension.
DO $choice_integrity$DECLARE reviewed jsonb;BEGIN
 reviewed:=pharmacy_stocktake_hospital_choice_state();
 EXECUTE format('CREATE OR REPLACE FUNCTION pharmacy_stocktake_hospital_choice_integrity()RETURNS boolean LANGUAGE sql STABLE AS %L',format('SELECT COALESCE(pharmacy_operating_choice_support()AND pharmacy_stocktake_hospital_choice_state()=%L::jsonb,false)',reviewed::text));
END $choice_integrity$;
DO $hospital_integrity$DECLARE reviewed jsonb;BEGIN
 reviewed:=pharmacy_stocktake_hospital_support_state();
 EXECUTE format('CREATE OR REPLACE FUNCTION pharmacy_stocktake_hospital_integrity()RETURNS boolean LANGUAGE sql STABLE AS %L',format('SELECT COALESCE(pharmacy_operating_choice_integrity()AND pharmacy_stocktake_hospital_support_state()=%L::jsonb,false)',reviewed::text));
END $hospital_integrity$;
