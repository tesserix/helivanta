CREATE FUNCTION iam_manual_identity_uuid(value uuid) RETURNS boolean LANGUAGE sql IMMUTABLE AS $$
 SELECT substring(value::text,15,1)='7' AND substring(value::text,20,1) IN ('8','9','a','b')
$$;
CREATE FUNCTION iam_manual_identity_text(value text,maximum bigint) RETURNS boolean LANGUAGE sql IMMUTABLE AS $$
 SELECT char_length(value) BETWEEN 1 AND maximum AND octet_length(value)<=4*maximum
 AND btrim(value,E'\t\n\v\f\r \u00a0\u1680\u2000\u2001\u2002\u2003\u2004\u2005\u2006\u2007\u2008\u2009\u200a\u2028\u2029\u202f\u205f\u3000\ufeff')<>''
 AND char_length(value)+(SELECT count(*) FROM regexp_split_to_table(value,'') c WHERE ascii(c)>65535)<=maximum
 AND NOT EXISTS(SELECT 1 FROM regexp_split_to_table(value,'') c WHERE ascii(c)<32 OR ascii(c) BETWEEN 127 AND 159)
$$;
CREATE TABLE iam_manual_identity_profiles (
 tenant_id uuid NOT NULL CHECK(tenant_id<>'00000000-0000-0000-0000-000000000000'),
 id uuid NOT NULL CHECK(iam_manual_identity_uuid(id)),
 policy_reference uuid NOT NULL CHECK(iam_manual_identity_uuid(policy_reference)),
 review_reference uuid NOT NULL CHECK(iam_manual_identity_uuid(review_reference)),
 witness_reference uuid NOT NULL CHECK(iam_manual_identity_uuid(witness_reference)),
 independent_root_review_confirmed boolean NOT NULL CHECK(independent_root_review_confirmed),
 display_name text NOT NULL CHECK(iam_manual_identity_text(display_name,iam_manual_identity_limit('display_name'))),
 observed_at timestamptz NOT NULL CHECK(iam_manual_identity_instant(observed_at)),
 valid_from timestamptz NOT NULL CHECK(iam_manual_identity_instant(valid_from)),
 valid_until timestamptz NOT NULL CHECK(iam_manual_identity_instant(valid_until) AND valid_until>valid_from AND valid_until>observed_at),
 reason text NOT NULL CHECK(iam_manual_identity_text(reason,iam_manual_identity_limit('reason'))),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,id)
);
CREATE TABLE iam_manual_identity_people (
 tenant_id uuid NOT NULL,
 source_profile_id uuid NOT NULL,
 id uuid NOT NULL CHECK(iam_manual_identity_uuid(id)),
 source_person_reference uuid NOT NULL CHECK(iam_manual_identity_uuid(source_person_reference)),
 finding_reference uuid NOT NULL CHECK(iam_manual_identity_uuid(finding_reference)),
 version bigint NOT NULL DEFAULT 1 CHECK(version=1),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,source_profile_id,id),
 UNIQUE(tenant_id,source_profile_id,source_person_reference),
 FOREIGN KEY(tenant_id,source_profile_id) REFERENCES iam_manual_identity_profiles(tenant_id,id) ON DELETE RESTRICT
);
CREATE TABLE iam_manual_identity_bindings (
 tenant_id uuid NOT NULL,
 source_profile_id uuid NOT NULL,
 person_id uuid NOT NULL,
 subject text NOT NULL CHECK(subject ~ '^[A-Za-z0-9_-]{1,200}$'),
 membership_epoch bigint NOT NULL CHECK(membership_epoch BETWEEN 1 AND iam_manual_identity_limit('max_version')),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,source_profile_id,subject,membership_epoch),
 FOREIGN KEY(tenant_id,source_profile_id,person_id) REFERENCES iam_manual_identity_people(tenant_id,source_profile_id,id) ON DELETE RESTRICT
);
CREATE INDEX ON iam_manual_identity_bindings(tenant_id,source_profile_id,person_id);
CREATE TABLE iam_manual_identity_source_heads (
 tenant_id uuid PRIMARY KEY,
 source_profile_id uuid NOT NULL,
 version bigint NOT NULL DEFAULT 1 CHECK(version=1),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 FOREIGN KEY(tenant_id,source_profile_id) REFERENCES iam_manual_identity_profiles(tenant_id,id) ON DELETE RESTRICT
);
CREATE TABLE iam_manual_identity_imports (
 tenant_id uuid NOT NULL,
 source_profile_id uuid NOT NULL,
 intent_digest text NOT NULL CHECK(intent_digest ~ '^[0-9a-f]{64}$'),
 actor_role text NOT NULL CHECK(length(actor_role)>0),
 people integer NOT NULL CHECK(people BETWEEN iam_manual_identity_limit('root_minimum') AND iam_manual_identity_limit('root_maximum')),
 accounts integer NOT NULL CHECK(accounts BETWEEN people AND people*iam_manual_identity_limit('account_maximum')),
 imported_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,source_profile_id),
 FOREIGN KEY(tenant_id,source_profile_id) REFERENCES iam_manual_identity_profiles(tenant_id,id) ON DELETE RESTRICT
);

CREATE FUNCTION iam_manual_identity_bootstrap(t uuid) RETURNS boolean LANGUAGE sql STABLE SECURITY INVOKER AS $$
 SELECT COALESCE(t=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND current_setting('app.iam_manual_identity_import',true)='authorized'
 AND current_user=(SELECT pg_get_userbyid(relowner) FROM pg_class WHERE oid='iam_manual_identity_profiles'::regclass),false)
$$;
CREATE FUNCTION iam_manual_identity_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN RAISE EXCEPTION 'manual identity import evidence is immutable' USING ERRCODE='23514'; END $$;
DO $tables$
DECLARE relation text;
BEGIN
 FOREACH relation IN ARRAY ARRAY['iam_manual_identity_profiles','iam_manual_identity_people','iam_manual_identity_bindings','iam_manual_identity_source_heads','iam_manual_identity_imports'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',relation);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',relation);
  EXECUTE format('CREATE POLICY manual_identity_operator ON %I USING(iam_manual_identity_bootstrap(tenant_id)) WITH CHECK(iam_manual_identity_bootstrap(tenant_id))',relation);
  EXECUTE format('CREATE POLICY assignment_target_purpose ON %I AS RESTRICTIVE FOR ALL USING(NULLIF(current_setting(''app.iam_staff_selection'',true),'''') IS NULL) WITH CHECK(NULLIF(current_setting(''app.iam_staff_selection'',true),'''') IS NULL)',relation);
  EXECUTE format('CREATE POLICY assignment_target_delete ON %I AS RESTRICTIVE FOR DELETE USING(false)',relation);
  EXECUTE format('CREATE POLICY operating_native_read ON %I AS RESTRICTIVE FOR SELECT USING(iam_operating_native_allows(%L,false))',relation,relation);
  EXECUTE format('CREATE POLICY operating_native_insert ON %I AS RESTRICTIVE FOR INSERT WITH CHECK(iam_operating_native_allows(%L,true))',relation,relation);
  EXECUTE format('CREATE POLICY operating_native_update ON %I AS RESTRICTIVE FOR UPDATE USING(iam_operating_native_allows(%L,false)) WITH CHECK(iam_operating_native_allows(%L,true))',relation,relation,relation);
  EXECUTE format('CREATE POLICY operating_native_delete ON %I AS RESTRICTIVE FOR DELETE USING(iam_operating_native_allows(%L,true))',relation,relation);
  EXECUTE format('CREATE TRIGGER manual_identity_preserve BEFORE UPDATE OR DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION iam_manual_identity_immutable()',relation);
 END LOOP;
END $tables$;

CREATE FUNCTION iam_manual_identity_import_complete() RETURNS trigger LANGUAGE plpgsql SECURITY INVOKER AS $$
DECLARE person_count bigint; account_count bigint; receipt iam_manual_identity_imports;
BEGIN
 SELECT * INTO STRICT receipt FROM iam_manual_identity_imports WHERE tenant_id=NEW.tenant_id AND source_profile_id=NEW.id;
 SELECT count(*) INTO person_count FROM iam_manual_identity_people WHERE tenant_id=NEW.tenant_id AND source_profile_id=NEW.id;
 SELECT count(*) INTO account_count FROM iam_manual_identity_bindings WHERE tenant_id=NEW.tenant_id AND source_profile_id=NEW.id;
 IF NEW.observed_at>clock_timestamp() OR NEW.valid_until<=clock_timestamp()
 OR person_count<>receipt.people OR account_count<>receipt.accounts
 OR receipt.actor_role<>current_user
 OR NOT EXISTS(SELECT 1 FROM iam_manual_identity_source_heads WHERE tenant_id=NEW.tenant_id AND source_profile_id=NEW.id)
 OR EXISTS(SELECT 1 FROM iam_manual_identity_people p WHERE p.tenant_id=NEW.tenant_id AND p.source_profile_id=NEW.id AND (SELECT count(*) FROM iam_manual_identity_bindings b WHERE(b.tenant_id,b.source_profile_id,b.person_id)=(p.tenant_id,p.source_profile_id,p.id)) NOT BETWEEN 1 AND iam_manual_identity_limit('account_maximum'))
 OR EXISTS(SELECT 1 FROM iam_manual_identity_bindings b LEFT JOIN iam_subject_membership_epochs e ON(e.tenant_id,e.subject,e.epoch)=(b.tenant_id,b.subject,b.membership_epoch) WHERE b.tenant_id=NEW.tenant_id AND b.source_profile_id=NEW.id AND (e.member_count IS NULL OR e.member_count<1))
 OR NOT EXISTS(SELECT 1 FROM audit_events WHERE tenant_id=NEW.tenant_id AND resource_type='iam_manual_identity_imports' AND resource_id=NEW.id AND action='import' AND actor_subject='operator:'||current_user) THEN
  RAISE EXCEPTION 'manual identity source import is incomplete or no longer current' USING ERRCODE='23514';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER manual_identity_import_complete AFTER INSERT ON iam_manual_identity_profiles DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION iam_manual_identity_import_complete();

-- Prior seals were validated before extending discovery policies.
DO $migration$
DECLARE policies text;functions text;triggers text;policy_count bigint;trigger_count bigint;
BEGIN
 SELECT string_agg(format('(%L::regclass,%L,%L)',p.polrelid::regclass::text,p.polname,md5(p.polcmd::text||p.polpermissive::text||p.polroles::text||COALESCE(pg_get_expr(p.polqual,p.polrelid),'')||COALESCE(pg_get_expr(p.polwithcheck,p.polrelid),''))),','),count(*)INTO policies,policy_count
 FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname=current_schema()AND(c.relname LIKE 'iam_operating_%'OR p.polname LIKE 'operating_native_%');
 SELECT string_agg(format('(%L,%L)',p.oid::regprocedure::text,md5(pg_get_functiondef(p.oid))),',')INTO functions FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
 WHERE n.nspname=current_schema()AND p.proname LIKE 'iam_operating_%'AND p.proname NOT IN('iam_operating_governance_integrity','iam_operating_governance_support');
 SELECT string_agg(format('(%L::regclass,%L,%L)',t.tgrelid::regclass::text,t.tgname,md5(pg_get_triggerdef(t.oid,true))),','),count(*)INTO triggers,trigger_count FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname=current_schema()AND NOT t.tgisinternal AND(c.relname LIKE 'iam_operating_%'OR t.tgname='iam_operating_native_global_mutation_guard');
 EXECUTE format($definition$CREATE OR REPLACE FUNCTION iam_operating_governance_integrity()RETURNS boolean LANGUAGE sql STABLE AS $body$
 SELECT NOT EXISTS(SELECT 1 FROM(VALUES %s)expected(relation,name,digest)LEFT JOIN pg_policy p ON p.polrelid=expected.relation AND p.polname=expected.name WHERE p.oid IS NULL OR md5(p.polcmd::text||p.polpermissive::text||p.polroles::text||COALESCE(pg_get_expr(p.polqual,p.polrelid),'')||COALESCE(pg_get_expr(p.polwithcheck,p.polrelid),''))<>expected.digest)
 AND(SELECT count(*)FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema()AND(c.relname LIKE 'iam_operating_%%'OR p.polname LIKE 'operating_native_%%'))=%s
 AND NOT EXISTS(SELECT 1 FROM(VALUES %s)expected(name,digest)LEFT JOIN pg_proc p ON p.oid=to_regprocedure(expected.name)WHERE p.oid IS NULL OR md5(pg_get_functiondef(p.oid))<>expected.digest)
 AND NOT EXISTS(SELECT 1 FROM(VALUES %s)expected(relation,name,digest)LEFT JOIN pg_trigger t ON t.tgrelid=expected.relation AND t.tgname=expected.name WHERE t.oid IS NULL OR t.tgenabled NOT IN('O','A')OR md5(pg_get_triggerdef(t.oid,true))<>expected.digest)
 AND(SELECT count(*)FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema()AND NOT t.tgisinternal AND(c.relname LIKE 'iam_operating_%%'OR t.tgname='iam_operating_native_global_mutation_guard'))=%s
 $body$ $definition$,policies,policy_count,functions,triggers,trigger_count);
END $migration$;
SELECT iam_staff_selection_capture_integrity();
DO $identity_integrity$
DECLARE policies text; functions text; triggers text; policy_count bigint; function_count bigint; trigger_count bigint;
BEGIN
 SELECT string_agg(format('(%L::regclass,%L,%L)',p.polrelid::regclass::text,p.polname,md5(p.polcmd::text||p.polpermissive::text||p.polroles::text||COALESCE(pg_get_expr(p.polqual,p.polrelid),'')||COALESCE(pg_get_expr(p.polwithcheck,p.polrelid),''))),','),count(*) INTO policies,policy_count
 FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid WHERE c.relname LIKE 'iam_manual_identity_%' AND c.relnamespace=current_schema()::regnamespace;
 SELECT string_agg(format('(%L,%L)',p.oid::regprocedure::text,md5(pg_get_functiondef(p.oid))),','),count(*) INTO functions,function_count
 FROM pg_proc p WHERE p.pronamespace=current_schema()::regnamespace AND p.proname LIKE 'iam_manual_identity_%' AND p.proname<>'iam_manual_identity_integrity';
 SELECT string_agg(format('(%L::regclass,%L,%L)',t.tgrelid::regclass::text,t.tgname,md5(pg_get_triggerdef(t.oid,true))),','),count(*) INTO triggers,trigger_count
 FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid WHERE NOT t.tgisinternal AND c.relnamespace=current_schema()::regnamespace AND c.relname LIKE 'iam_manual_identity_%';
 EXECUTE format($definition$CREATE FUNCTION iam_manual_identity_integrity() RETURNS boolean LANGUAGE sql STABLE AS $body$
 SELECT NOT EXISTS(SELECT 1 FROM(VALUES %s)expected(relation,name,digest)LEFT JOIN pg_policy p ON p.polrelid=expected.relation AND p.polname=expected.name WHERE p.oid IS NULL OR md5(p.polcmd::text||p.polpermissive::text||p.polroles::text||COALESCE(pg_get_expr(p.polqual,p.polrelid),'')||COALESCE(pg_get_expr(p.polwithcheck,p.polrelid),''))<>expected.digest)
 AND(SELECT count(*)FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid WHERE c.relnamespace=current_schema()::regnamespace AND c.relname LIKE 'iam_manual_identity_%%')=%s
 AND NOT EXISTS(SELECT 1 FROM(VALUES %s)expected(name,digest)LEFT JOIN pg_proc p ON p.oid=to_regprocedure(expected.name)WHERE p.oid IS NULL OR md5(pg_get_functiondef(p.oid))<>expected.digest)
 AND(SELECT count(*)FROM pg_proc p WHERE p.pronamespace=current_schema()::regnamespace AND p.proname LIKE 'iam_manual_identity_%%'AND p.proname<>'iam_manual_identity_integrity')=%s
 AND NOT EXISTS(SELECT 1 FROM(VALUES %s)expected(relation,name,digest)LEFT JOIN pg_trigger t ON t.tgrelid=expected.relation AND t.tgname=expected.name WHERE t.oid IS NULL OR t.tgenabled NOT IN('O','A')OR md5(pg_get_triggerdef(t.oid,true))<>expected.digest)
 AND(SELECT count(*)FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid WHERE NOT t.tgisinternal AND c.relnamespace=current_schema()::regnamespace AND c.relname LIKE 'iam_manual_identity_%%')=%s
 AND NOT EXISTS(SELECT 1 FROM pg_class c WHERE c.relnamespace=current_schema()::regnamespace AND c.relname LIKE 'iam_manual_identity_%%'AND c.relkind='r'AND(NOT c.relrowsecurity OR NOT c.relforcerowsecurity))
 $body$ $definition$,policies,policy_count,functions,function_count,triggers,trigger_count);
END $identity_integrity$;
