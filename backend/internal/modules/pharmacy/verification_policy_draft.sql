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


-- Administrative preparation only. No effective policy or clinical grant is created.
CREATE TABLE pharmacy_verification_policy_drafts (
 tenant_id uuid NOT NULL,
 outlet_id uuid NOT NULL,
 policy_id uuid NOT NULL CHECK (substring(policy_id::text,15,1)='7' AND substring(policy_id::text,20,1) IN ('8','9','a','b')),
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7' AND substring(id::text,20,1) IN ('8','9','a','b')),
 expected_version bigint NOT NULL,
 version bigint NOT NULL CHECK(version=expected_version+1),
 label text NOT NULL,
 second_check_required boolean NOT NULL,
 review_valid_for_seconds bigint NOT NULL,
 query_response_target_seconds bigint NOT NULL,
 reason text NOT NULL,
 intent_sha256 text NOT NULL CHECK(intent_sha256 ~ '^[a-f0-9]{64}$'),
 prepared_by text NOT NULL CHECK(length(prepared_by) BETWEEN 1 AND 512),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,outlet_id,id),
 UNIQUE(tenant_id,outlet_id,policy_id,version),
 FOREIGN KEY(tenant_id,outlet_id) REFERENCES pharmacy_outlets(tenant_id,id),
 CONSTRAINT pharmacy_verification_policy_draft_intent CHECK(pharmacy_verification_policy_draft_contract(expected_version,label,second_check_required,review_valid_for_seconds,query_response_target_seconds,reason))
);
CREATE INDEX pharmacy_verification_policy_draft_history ON pharmacy_verification_policy_drafts(tenant_id,outlet_id,policy_id,created_at DESC,id DESC);
CREATE INDEX pharmacy_verification_policy_draft_page ON pharmacy_verification_policy_drafts(tenant_id,outlet_id,created_at DESC,id DESC);
ALTER TABLE pharmacy_verification_policy_drafts ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_verification_policy_drafts FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_verification_policy_drafts
 USING(hms_tenant_visible(tenant_id) AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK(pharmacy_outlet_allowed(tenant_id,outlet_id,true) AND prepared_by=NULLIF(current_setting('app.pharmacy_subject',true),''));
CREATE FUNCTION pharmacy_verification_policy_draft_lock(tenant uuid,outlet uuid,policy uuid) RETURNS void
LANGUAGE sql AS $$
 SELECT pg_advisory_xact_lock(hashtextextended(tenant::text||':verification-policy-draft:'||outlet::text||':'||policy::text,0))
$$;
CREATE FUNCTION pharmacy_verification_policy_draft_version_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE previous bigint;
BEGIN
 PERFORM pharmacy_verification_policy_draft_lock(NEW.tenant_id,NEW.outlet_id,NEW.policy_id);
 SELECT COALESCE(max(version),0) INTO previous FROM pharmacy_verification_policy_drafts
 WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND policy_id=NEW.policy_id;
 IF previous<>NEW.expected_version THEN
  RAISE EXCEPTION 'policy draft version changed' USING ERRCODE='23514',CONSTRAINT='pharmacy_verification_policy_draft_version';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_verification_policy_draft_version_guard BEFORE INSERT ON pharmacy_verification_policy_drafts
FOR EACH ROW EXECUTE FUNCTION pharmacy_verification_policy_draft_version_guard();
CREATE TRIGGER pharmacy_verification_policy_draft_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_verification_policy_drafts
FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();
CREATE TRIGGER pharmacy_verification_policy_draft_audit AFTER INSERT ON pharmacy_verification_policy_drafts
FOR EACH ROW EXECUTE FUNCTION audit_record_change();

DO $purpose$
DECLARE target text;
BEGIN
 FOREACH target IN ARRAY ARRAY['pharmacy_verification_policy_drafts'] LOOP
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
