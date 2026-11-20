SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

DO $$BEGIN
 IF NOT pharmacy_operating_choice_integrity() THEN
  RAISE EXCEPTION 'existing pharmacy purpose policies changed' USING ERRCODE='23514';
 END IF;
END $$;

CREATE TABLE pharmacy_reorder_levels (
 tenant_id uuid NOT NULL, outlet_id uuid NOT NULL, product_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 minimum_units bigint CHECK(minimum_units BETWEEN 0 AND 1000000000),
 expected_version bigint NOT NULL CHECK(expected_version BETWEEN 0 AND 9007199254740990),
 product_version bigint NOT NULL CHECK(product_version BETWEEN 1 AND 9007199254740991),
 version bigint NOT NULL CHECK(version=expected_version+1),
 unit_of_issue text NOT NULL CHECK(length(btrim(unit_of_issue)) BETWEEN 1 AND 40),
 actor_subject text NOT NULL CHECK(length(btrim(actor_subject)) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,outlet_id,product_id,version),
 UNIQUE(tenant_id,outlet_id,id),
 FOREIGN KEY(tenant_id,outlet_id,product_id) REFERENCES pharmacy_catalogue_products(tenant_id,outlet_id,id) ON DELETE RESTRICT
);
ALTER TABLE pharmacy_reorder_levels ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_reorder_levels FORCE ROW LEVEL SECURITY;
CREATE POLICY operating_choice_purpose ON pharmacy_reorder_levels AS RESTRICTIVE
 USING(hms_tenant_visible(tenant_id) AND COALESCE(current_setting('app.pharmacy_operating_choices',true),'')='')
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND COALESCE(current_setting('app.pharmacy_operating_choices',true),'')='');
CREATE POLICY operating_choice_delete ON pharmacy_reorder_levels AS RESTRICTIVE FOR DELETE
 USING(hms_tenant_visible(tenant_id) AND COALESCE(current_setting('app.pharmacy_operating_choices',true),'')='');
CREATE POLICY tenant_isolation ON pharmacy_reorder_levels
 USING(hms_tenant_visible(tenant_id) AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK(pharmacy_outlet_allowed(tenant_id,outlet_id,true));
CREATE TRIGGER pharmacy_reorder_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_reorder_levels
 FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();
CREATE TRIGGER pharmacy_reorder_audit AFTER INSERT ON pharmacy_reorder_levels
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();

CREATE FUNCTION pharmacy_reorder_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE p pharmacy_catalogue_products;current_version bigint;
BEGIN
 SELECT * INTO p FROM pharmacy_catalogue_products WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.product_id FOR UPDATE;
 IF p.id IS NULL OR p.status<>'active' OR p.version<>NEW.product_version OR p.unit_of_issue<>NEW.unit_of_issue
 OR NEW.actor_subject IS DISTINCT FROM NULLIF(current_setting('app.pharmacy_subject',true),'') THEN
  RAISE EXCEPTION 'reorder product changed' USING ERRCODE='23514',CONSTRAINT='pharmacy_reorder_product';
 END IF;
 SELECT COALESCE(MAX(version),0) INTO current_version FROM pharmacy_reorder_levels
 WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND product_id=NEW.product_id;
 IF NEW.expected_version<>current_version THEN
  RAISE EXCEPTION 'reorder version changed' USING ERRCODE='23514',CONSTRAINT='pharmacy_reorder_version';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_reorder_guard BEFORE INSERT ON pharmacy_reorder_levels
 FOR EACH ROW EXECUTE FUNCTION pharmacy_reorder_guard();

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
