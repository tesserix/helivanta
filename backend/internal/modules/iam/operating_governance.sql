SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
DO $$ BEGIN IF NOT iam_assignment_governance_integrity()THEN RAISE EXCEPTION 'assignment governance prerequisite integrity is unavailable' USING ERRCODE='55000';END IF;END $$;

ALTER TABLE iam_operating_duties ADD COLUMN native_administration boolean NOT NULL DEFAULT false;
CREATE INDEX iam_operating_native_command_version ON iam_operating_commands(tenant_id,duty_id,expected_version);

CREATE TABLE iam_operating_governance_origins(
 tenant_id uuid NOT NULL,duty_id uuid NOT NULL,employer_kind text NOT NULL CHECK(employer_kind IN('pharmacy','supplier')),employer_id uuid NOT NULL,
 actor_subject text NOT NULL,authority jsonb NOT NULL CHECK(jsonb_typeof(authority)='object'),created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,duty_id),FOREIGN KEY(tenant_id,duty_id)REFERENCES iam_operating_duties(tenant_id,id)ON DELETE RESTRICT
);
CREATE TABLE iam_operating_governance_commands(
 tenant_id uuid NOT NULL,command_id uuid NOT NULL,duty_id uuid NOT NULL,employer_kind text NOT NULL CHECK(employer_kind IN('pharmacy','supplier')),employer_id uuid NOT NULL,
 actor_subject text NOT NULL,authority jsonb NOT NULL CHECK(jsonb_typeof(authority)='object'),created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,command_id),FOREIGN KEY(tenant_id,duty_id)REFERENCES iam_operating_governance_origins(tenant_id,duty_id)ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,command_id)REFERENCES iam_operating_commands(tenant_id,id)ON DELETE RESTRICT
);

CREATE FUNCTION iam_operating_native_selected(t uuid,k text,e uuid,d uuid)RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(NULLIF(current_setting('app.iam_operating_native_kind',true),'')IS NULL OR
 (t=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND k=current_setting('app.iam_operating_native_kind',true)
 AND e=NULLIF(current_setting('app.iam_operating_native_employer',true),'')::uuid
 AND(NULLIF(current_setting('app.iam_operating_native_duty',true),'')IS NULL OR d=NULLIF(current_setting('app.iam_operating_native_duty',true),'')::uuid)),false)
$$;
DO $$ DECLARE relation text;BEGIN
 FOREACH relation IN ARRAY ARRAY['iam_operating_governance_origins','iam_operating_governance_commands'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',relation);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',relation);
  EXECUTE format('CREATE POLICY tenant_isolation ON %I USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND current_setting(''app.iam_operating_access'',true)=''authorized'') WITH CHECK(tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND current_setting(''app.iam_operating_access'',true)=''authorized'' AND actor_subject=current_setting(''app.actor_subject'',true))',relation);
  EXECUTE format('CREATE POLICY native_selection ON %I AS RESTRICTIVE USING(iam_operating_native_selected(tenant_id,employer_kind,employer_id,duty_id)) WITH CHECK(iam_operating_native_selected(tenant_id,employer_kind,employer_id,duty_id))',relation);
  EXECUTE format('CREATE TRIGGER iam_operating_native_preserve BEFORE UPDATE OR DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION iam_assignment_proposal_preserve()',relation);
 END LOOP;
END $$;

CREATE FUNCTION iam_operating_native_visible(t uuid,k text,e uuid,d uuid)RETURNS boolean LANGUAGE plpgsql STABLE AS $$
BEGIN
 IF NULLIF(current_setting('app.iam_operating_native_kind',true),'')IS NULL THEN RETURN true;END IF;
 IF NOT iam_operating_native_selected(t,k,e,d)THEN RETURN false;END IF;
 IF current_setting('app.iam_operating_command',true)='prepare'AND d=NULLIF(current_setting('app.iam_operating_native_duty',true),'')::uuid THEN RETURN true;END IF;
 RETURN EXISTS(SELECT 1 FROM iam_operating_governance_origins o WHERE(o.tenant_id,o.duty_id,o.employer_kind,o.employer_id)=(t,d,k,e));
END $$;
CREATE POLICY native_selection ON iam_operating_duties AS RESTRICTIVE
 USING(iam_operating_native_visible(tenant_id,employer_kind,employer_id,id))WITH CHECK(iam_operating_native_visible(tenant_id,employer_kind,employer_id,id));
CREATE FUNCTION iam_operating_native_duty_reference(t uuid,d uuid)RETURNS boolean LANGUAGE plpgsql STABLE AS $$
BEGIN
 IF NULLIF(current_setting('app.iam_operating_native_kind',true),'')IS NULL THEN RETURN true;END IF;
 RETURN EXISTS(SELECT 1 FROM iam_operating_duties duty WHERE duty.tenant_id=t AND duty.id=d);
END $$;
DO $$ DECLARE relation text;BEGIN
 FOREACH relation IN ARRAY ARRAY['iam_operating_duty_history','iam_operating_commands','iam_operating_snapshots','iam_operating_projection_acks']LOOP
  EXECUTE format('CREATE POLICY native_selection ON %I AS RESTRICTIVE USING(iam_operating_native_duty_reference(tenant_id,duty_id))WITH CHECK(iam_operating_native_duty_reference(tenant_id,duty_id))',relation);
 END LOOP;
END $$;

CREATE FUNCTION iam_operating_native_authority_current(proof jsonb,t uuid,k text,e uuid,actor text,capability text)RETURNS boolean LANGUAGE sql VOLATILE AS $$
 SELECT COALESCE(proof->>'tenant_id'=t::text AND proof->>'employer_kind'=k AND proof->>'employer_id'=e::text AND proof->>'actor_subject'=actor AND proof->>'capability'=capability
 AND k IN('pharmacy','supplier') AND capability IN('iam.governance.operating.manage','iam.governance.operating.review')
 AND(proof-ARRAY['tenant_id','employer_kind','employer_id','actor_subject','capability','authority_kind','membership_epoch','owner_membership_id','source_grant_id','source_grant_version','authenticated_at','idle_deadline','expires_at'])='{}'::jsonb
 AND(proof->>'membership_epoch')::bigint BETWEEN 1 AND 9007199254740991
 AND(proof->>'authenticated_at')::timestamptz<=clock_timestamp() AND(proof->>'idle_deadline')::timestamptz>clock_timestamp() AND(proof->>'expires_at')::timestamptz>clock_timestamp()
 AND NOT EXISTS(SELECT 1 FROM iam_credential_revocations v WHERE v.subject=actor AND v.revoked_at>=(proof->>'authenticated_at')::timestamptz)
 AND EXISTS(SELECT 1 FROM iam_subject_membership_epochs m WHERE m.tenant_id=t AND m.subject=actor AND m.member_count>0 AND m.epoch=(proof->>'membership_epoch')::bigint)
 AND((proof->>'authority_kind'='owner' AND proof->>'source_grant_id'IS NULL AND proof->>'source_grant_version'IS NULL
 AND EXISTS(SELECT 1 FROM iam_members m WHERE m.tenant_id=t AND m.subject=actor AND m.id=(proof->>'owner_membership_id')::uuid AND m.role_key='tenant_admin'))
 OR(proof->>'authority_kind'='grant' AND proof->>'owner_membership_id'IS NULL AND EXISTS(
 SELECT 1 FROM iam_governance_grants g WHERE g.tenant_id=t AND g.id=(proof->>'source_grant_id')::uuid AND g.subject=actor AND g.version=(proof->>'source_grant_version')::bigint
 AND g.membership_epoch=(proof->>'membership_epoch')::bigint AND g.template_key='iam.operating_access_reviewer.v1' AND g.scope_kind=k AND g.scope_id=e
 AND g.status IN('active','provisional_active')AND g.valid_from<=clock_timestamp()AND g.expires_at=(proof->>'expires_at')::timestamptz))),false)
$$;

CREATE FUNCTION iam_operating_native_mutation_guard()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE proof jsonb:=NULLIF(current_setting('app.iam_operating_native_authority',true),'')::jsonb;
 command text:=current_setting('app.iam_operating_command',true);capability text;has_origin boolean;
BEGIN
 IF TG_OP='INSERT'THEN NEW.native_administration:=(proof IS NOT NULL);ELSE NEW.native_administration:=OLD.native_administration;END IF;
 IF proof IS NULL AND NOT NEW.native_administration THEN RETURN NEW;END IF;
 SELECT EXISTS(SELECT 1 FROM iam_operating_governance_origins WHERE tenant_id=NEW.tenant_id AND duty_id=NEW.id)INTO has_origin;
 IF proof IS NULL THEN
  IF NEW.native_administration AND(NOT has_origin OR NOT COALESCE(current_setting('app.iam_operating_native_projector',true)='authorized'AND NEW.actor_subject='service:iam-operating-projector'AND command IN('project','revoke'),false))THEN RAISE EXCEPTION 'native duty requires native administration' USING ERRCODE='42501';END IF;
  RETURN NEW;
 END IF;
 IF TG_OP='UPDATE'AND(NOT OLD.native_administration OR NOT has_origin)THEN RAISE EXCEPTION 'native administration cannot adopt legacy duty' USING ERRCODE='42501';END IF;
 capability:=CASE WHEN command='review' THEN 'iam.governance.operating.review' WHEN command IN('prepare','withdraw','revoke')THEN 'iam.governance.operating.manage' ELSE NULL END;
 IF NOT iam_operating_native_selected(NEW.tenant_id,NEW.employer_kind,NEW.employer_id,NEW.id)
 OR capability IS NULL OR NOT iam_operating_native_authority_current(proof,NEW.tenant_id,NEW.employer_kind,NEW.employer_id,NEW.actor_subject,capability)THEN
  RAISE EXCEPTION 'native duty requires exact current governance source' USING ERRCODE='42501';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER iam_operating_native_mutation_guard BEFORE INSERT OR UPDATE ON iam_operating_duties FOR EACH ROW EXECUTE FUNCTION iam_operating_native_mutation_guard();

CREATE FUNCTION iam_operating_native_record()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE proof jsonb:=NULLIF(current_setting('app.iam_operating_native_authority',true),'')::jsonb;d iam_operating_duties%ROWTYPE;
BEGIN
 IF proof IS NULL THEN RETURN NULL;END IF;
 IF TG_TABLE_NAME='iam_operating_duties' THEN
  INSERT INTO iam_operating_governance_origins(tenant_id,duty_id,employer_kind,employer_id,actor_subject,authority)VALUES(NEW.tenant_id,NEW.id,NEW.employer_kind,NEW.employer_id,NEW.actor_subject,proof);
 ELSE
  SELECT * INTO STRICT d FROM iam_operating_duties WHERE tenant_id=NEW.tenant_id AND id=NEW.duty_id;
  INSERT INTO iam_operating_governance_commands(tenant_id,command_id,duty_id,employer_kind,employer_id,actor_subject,authority)VALUES(NEW.tenant_id,NEW.id,NEW.duty_id,d.employer_kind,d.employer_id,NEW.actor_subject,proof);
 END IF;
 RETURN NULL;
END $$;
CREATE TRIGGER iam_operating_native_record AFTER INSERT ON iam_operating_duties FOR EACH ROW EXECUTE FUNCTION iam_operating_native_record();
CREATE TRIGGER iam_operating_native_record AFTER INSERT ON iam_operating_commands FOR EACH ROW EXECUTE FUNCTION iam_operating_native_record();

CREATE FUNCTION iam_operating_native_provenance_guard()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE proof jsonb:=NULLIF(current_setting('app.iam_operating_native_authority',true),'')::jsonb;d iam_operating_duties%ROWTYPE;c iam_operating_commands%ROWTYPE;capability text;
BEGIN
 IF pg_trigger_depth()<>2 OR NEW.authority IS DISTINCT FROM proof OR NEW.actor_subject IS DISTINCT FROM current_setting('app.actor_subject',true)THEN
  RAISE EXCEPTION 'native provenance must be emitted by canonical command' USING ERRCODE='42501';
 END IF;
 SELECT * INTO STRICT d FROM iam_operating_duties WHERE tenant_id=NEW.tenant_id AND id=NEW.duty_id;
 IF NOT d.native_administration THEN RAISE EXCEPTION 'native origin cannot adopt legacy administration' USING ERRCODE='42501';END IF;
 IF(d.employer_kind,d.employer_id,d.actor_subject)IS DISTINCT FROM(NEW.employer_kind,NEW.employer_id,NEW.actor_subject)THEN RAISE EXCEPTION 'native source scope mismatch' USING ERRCODE='23514';END IF;
 IF TG_TABLE_NAME='iam_operating_governance_origins'THEN
  IF d.version<>1 OR d.status<>'pending_review'OR d.prepared_by<>NEW.actor_subject OR current_setting('app.iam_operating_command',true)<>'prepare'THEN RAISE EXCEPTION 'native origin requires canonical preparation' USING ERRCODE='23514';END IF;
  capability:='iam.governance.operating.manage';
 ELSE
  SELECT * INTO STRICT c FROM iam_operating_commands WHERE tenant_id=NEW.tenant_id AND id=NEW.command_id AND duty_id=NEW.duty_id;
  IF c.actor_subject<>NEW.actor_subject OR c.expected_version+1<>d.version THEN RAISE EXCEPTION 'native decision source requires canonical receipt' USING ERRCODE='23514';END IF;
  capability:=CASE WHEN c.input->>'decision'IN('approve','reject')THEN 'iam.governance.operating.review' ELSE 'iam.governance.operating.manage' END;
 END IF;
 IF NOT iam_operating_native_authority_current(proof,NEW.tenant_id,NEW.employer_kind,NEW.employer_id,NEW.actor_subject,capability)THEN RAISE EXCEPTION 'native provenance requires current source' USING ERRCODE='42501';END IF;
 INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id)VALUES(NEW.tenant_id,NEW.actor_subject,'iam.operating.governance.recorded','iam_operating_duties',NEW.duty_id);
 RETURN NEW;
END $$;
CREATE TRIGGER iam_operating_native_provenance_guard BEFORE INSERT ON iam_operating_governance_origins FOR EACH ROW EXECUTE FUNCTION iam_operating_native_provenance_guard();
CREATE TRIGGER iam_operating_native_provenance_guard BEFORE INSERT ON iam_operating_governance_commands FOR EACH ROW EXECUTE FUNCTION iam_operating_native_provenance_guard();

CREATE FUNCTION iam_operating_native_allows(relation text,writing boolean)RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT NULLIF(current_setting('app.iam_operating_native_authority',true),'')IS NULL OR relation IN('iam_operating_duties','iam_operating_duty_history','iam_operating_snapshots','iam_operating_commands','iam_operating_projection_acks','iam_operating_governance_origins','iam_operating_governance_commands')
 OR(NOT writing AND relation IN('iam_members','iam_subject_membership_epochs','iam_credential_revocations','iam_governance_grants'))
$$;
DO $$ DECLARE relation text;BEGIN
 FOR relation IN SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema()AND c.relkind='r'AND c.relrowsecurity AND c.relname LIKE 'iam\_%'ESCAPE '\' LOOP
  EXECUTE format('CREATE POLICY operating_native_read ON %I AS RESTRICTIVE FOR SELECT USING(iam_operating_native_allows(%L,false))',relation,relation);
  EXECUTE format('CREATE POLICY operating_native_insert ON %I AS RESTRICTIVE FOR INSERT WITH CHECK(iam_operating_native_allows(%L,true))',relation,relation);
  EXECUTE format('CREATE POLICY operating_native_update ON %I AS RESTRICTIVE FOR UPDATE USING(iam_operating_native_allows(%L,false))WITH CHECK(iam_operating_native_allows(%L,true))',relation,relation,relation);
  EXECUTE format('CREATE POLICY operating_native_delete ON %I AS RESTRICTIVE FOR DELETE USING(iam_operating_native_allows(%L,true))',relation,relation);
 END LOOP;
END $$;
CREATE FUNCTION iam_operating_native_subject(subject text)RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT NULLIF(current_setting('app.iam_operating_native_authority',true),'')IS NULL OR subject=current_setting('app.actor_subject',true)OR subject=NULLIF(current_setting('app.iam_operating_native_target',true),'')
$$;
CREATE POLICY operating_native_subject ON iam_members AS RESTRICTIVE FOR SELECT USING(iam_operating_native_subject(subject));
CREATE POLICY operating_native_subject ON iam_subject_membership_epochs AS RESTRICTIVE FOR SELECT USING(iam_operating_native_subject(subject));
-- Global authentication tables deliberately remain independent of tenant selection.
CREATE FUNCTION iam_operating_native_global_mutation_guard()RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NULLIF(current_setting('app.iam_operating_native_authority',true),'')IS NOT NULL THEN RAISE EXCEPTION 'native duty source cannot mutate global identity' USING ERRCODE='42501';END IF;
 RETURN NULL;
END $$;
DO $$ DECLARE relation text;BEGIN
 FOR relation IN SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema()AND c.relkind='r'AND NOT c.relrowsecurity AND(c.relname LIKE 'iam\_%'ESCAPE '\'OR c.relname='login_attempt')LOOP
  EXECUTE format('CREATE TRIGGER iam_operating_native_global_mutation_guard BEFORE INSERT OR UPDATE OR DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION iam_operating_native_global_mutation_guard()',relation);
 END LOOP;
END $$;

CREATE FUNCTION iam_operating_native_complete()RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT NEW.native_administration THEN RETURN NULL;END IF;
 IF NOT EXISTS(SELECT 1 FROM iam_operating_governance_origins o WHERE(o.tenant_id,o.duty_id,o.employer_kind,o.employer_id)=(NEW.tenant_id,NEW.id,NEW.employer_kind,NEW.employer_id))THEN
  RAISE EXCEPTION 'native duty requires immutable preparation source' USING ERRCODE='23514',CONSTRAINT='iam_operating_native_completion';
 END IF;
 IF NEW.version>1 AND NOT COALESCE(NEW.actor_subject='service:iam-operating-projector'AND current_setting('app.iam_operating_native_projector',true)='authorized',false)AND NOT EXISTS(
 SELECT 1 FROM iam_operating_commands c JOIN iam_operating_governance_commands s ON(s.tenant_id,s.command_id,s.duty_id)=(c.tenant_id,c.id,c.duty_id)
 WHERE c.tenant_id=NEW.tenant_id AND c.duty_id=NEW.id AND c.expected_version+1=NEW.version AND c.actor_subject=NEW.actor_subject)THEN
  RAISE EXCEPTION 'native duty requires immutable decision source' USING ERRCODE='23514',CONSTRAINT='iam_operating_native_completion';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER iam_operating_native_completion AFTER INSERT OR UPDATE ON iam_operating_duties DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION iam_operating_native_complete();

-- Preserve the checked IAM0049 baseline while including the new purpose restrictions.
DO $migration$
DECLARE policies text;functions text;triggers text;policy_count bigint;trigger_count bigint;
BEGIN
 SELECT string_agg(format('(%L::regclass,%L,%L)',p.polrelid::regclass::text,p.polname,md5(p.polcmd::text||p.polpermissive::text||p.polroles::text||COALESCE(pg_get_expr(p.polqual,p.polrelid),'')||COALESCE(pg_get_expr(p.polwithcheck,p.polrelid),''))),','),count(*) INTO policies,policy_count
 FROM pg_policy p WHERE p.polrelid IN('iam_assignment_proposals'::regclass,'iam_assignment_proposal_history'::regclass,'iam_assignment_decisions'::regclass,'iam_assignment_snapshots'::regclass,'iam_assignment_scope_epochs'::regclass,'iam_assignment_department_verifications'::regclass,'iam_assignment_governance_requests'::regclass,'iam_assignment_governance_verifications'::regclass);
 SELECT string_agg(format('(%L,%L)',p.oid::regprocedure::text,md5(pg_get_functiondef(p.oid))),',') INTO functions FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
 WHERE n.nspname='public' AND p.proname IN('iam_assignment_governance_selected','iam_assignment_governance_authority_current','iam_assignment_governance_request_guard','iam_assignment_governance_verification_guard','iam_assignment_governance_complete','iam_assignment_governance_settlement_guard','iam_assignment_proposal_guard','iam_assignment_proposal_record','iam_assignment_proposal_preserve','iam_assignment_proposal_history_guard','iam_assignment_decision_guard','iam_assignment_snapshot_guard','iam_assignment_snapshot_record','iam_assignment_epoch_guard','iam_assignment_department_verification_guard','iam_proposal_membership_bind','iam_snapshot_membership_bind','iam_assignment_establishment_immutable') OR n.nspname='public' AND (p.proname LIKE 'iam_assignment_%' OR p.proname LIKE 'iam_proposal_%' OR p.proname LIKE 'iam_snapshot_%') AND p.proname NOT IN('iam_assignment_governance_integrity','iam_assignment_governance_support');
 SELECT string_agg(format('(%L::regclass,%L,%L)',t.tgrelid::regclass::text,t.tgname,md5(pg_get_triggerdef(t.oid,true))),','),count(*) INTO triggers,trigger_count FROM pg_trigger t
 WHERE NOT t.tgisinternal AND t.tgrelid IN('iam_assignment_proposals'::regclass,'iam_assignment_proposal_history'::regclass,'iam_assignment_decisions'::regclass,'iam_assignment_snapshots'::regclass,'iam_assignment_scope_epochs'::regclass,'iam_assignment_department_verifications'::regclass,'iam_assignment_governance_requests'::regclass,'iam_assignment_governance_verifications'::regclass);
 EXECUTE format($definition$CREATE OR REPLACE FUNCTION iam_assignment_governance_integrity() RETURNS boolean LANGUAGE sql STABLE AS $body$
 SELECT NOT EXISTS(SELECT 1 FROM (VALUES %s) expected(relation,name,digest) LEFT JOIN pg_policy p ON p.polrelid=expected.relation AND p.polname=expected.name WHERE p.oid IS NULL OR md5(p.polcmd::text||p.polpermissive::text||p.polroles::text||COALESCE(pg_get_expr(p.polqual,p.polrelid),'')||COALESCE(pg_get_expr(p.polwithcheck,p.polrelid),''))<>expected.digest)
 AND (SELECT count(*) FROM pg_policy p WHERE p.polrelid IN('iam_assignment_proposals'::regclass,'iam_assignment_proposal_history'::regclass,'iam_assignment_decisions'::regclass,'iam_assignment_snapshots'::regclass,'iam_assignment_scope_epochs'::regclass,'iam_assignment_department_verifications'::regclass,'iam_assignment_governance_requests'::regclass,'iam_assignment_governance_verifications'::regclass))=%s
 AND NOT EXISTS(SELECT 1 FROM (VALUES %s) expected(name,digest) LEFT JOIN pg_proc p ON p.oid=to_regprocedure(expected.name) WHERE p.oid IS NULL OR md5(pg_get_functiondef(p.oid))<>expected.digest)
 AND NOT EXISTS(SELECT 1 FROM (VALUES %s) expected(relation,name,digest) LEFT JOIN pg_trigger t ON t.tgrelid=expected.relation AND t.tgname=expected.name WHERE t.oid IS NULL OR t.tgenabled NOT IN('O','A') OR md5(pg_get_triggerdef(t.oid,true))<>expected.digest)
 AND (SELECT count(*) FROM pg_trigger t WHERE NOT t.tgisinternal AND t.tgrelid IN('iam_assignment_proposals'::regclass,'iam_assignment_proposal_history'::regclass,'iam_assignment_decisions'::regclass,'iam_assignment_snapshots'::regclass,'iam_assignment_scope_epochs'::regclass,'iam_assignment_department_verifications'::regclass,'iam_assignment_governance_requests'::regclass,'iam_assignment_governance_verifications'::regclass))=%s
 $body$ $definition$,policies,policy_count,functions,triggers,trigger_count);
END $migration$;

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
 EXECUTE format($definition$CREATE FUNCTION iam_operating_governance_integrity()RETURNS boolean LANGUAGE sql STABLE AS $body$
 SELECT NOT EXISTS(SELECT 1 FROM(VALUES %s)expected(relation,name,digest)LEFT JOIN pg_policy p ON p.polrelid=expected.relation AND p.polname=expected.name WHERE p.oid IS NULL OR md5(p.polcmd::text||p.polpermissive::text||p.polroles::text||COALESCE(pg_get_expr(p.polqual,p.polrelid),'')||COALESCE(pg_get_expr(p.polwithcheck,p.polrelid),''))<>expected.digest)
 AND(SELECT count(*)FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema()AND(c.relname LIKE 'iam_operating_%%'OR p.polname LIKE 'operating_native_%%'))=%s
 AND NOT EXISTS(SELECT 1 FROM(VALUES %s)expected(name,digest)LEFT JOIN pg_proc p ON p.oid=to_regprocedure(expected.name)WHERE p.oid IS NULL OR md5(pg_get_functiondef(p.oid))<>expected.digest)
 AND NOT EXISTS(SELECT 1 FROM(VALUES %s)expected(relation,name,digest)LEFT JOIN pg_trigger t ON t.tgrelid=expected.relation AND t.tgname=expected.name WHERE t.oid IS NULL OR t.tgenabled NOT IN('O','A')OR md5(pg_get_triggerdef(t.oid,true))<>expected.digest)
 AND(SELECT count(*)FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema()AND NOT t.tgisinternal AND(c.relname LIKE 'iam_operating_%%'OR t.tgname='iam_operating_native_global_mutation_guard'))=%s
 $body$ $definition$,policies,policy_count,functions,triggers,trigger_count);
END $migration$;
CREATE FUNCTION iam_operating_governance_support()RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT iam_operating_governance_integrity()AND iam_assignment_governance_integrity()
 AND NOT EXISTS(SELECT 1 FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema()AND p.polname='operating_native_read'AND(NOT c.relrowsecurity OR NOT c.relforcerowsecurity OR has_table_privilege(current_user,c.oid,'TRUNCATE')))AND NOT EXISTS(
 SELECT 1 FROM(VALUES('iam_operating_duties'),('iam_operating_duty_history'),('iam_operating_commands'),('iam_operating_snapshots'),('iam_operating_projection_acks'),('iam_operating_governance_origins'),('iam_operating_governance_commands'))expected(name)
 WHERE NOT EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema()AND c.relname=expected.name AND c.relrowsecurity AND c.relforcerowsecurity
 AND NOT has_table_privilege(current_user,c.oid,'TRUNCATE')
 AND EXISTS(SELECT 1 FROM pg_policy p WHERE p.polrelid=c.oid AND p.polname='tenant_isolation')AND EXISTS(SELECT 1 FROM pg_policy p WHERE p.polrelid=c.oid AND p.polname='native_selection'AND NOT p.polpermissive)))
 AND EXISTS(SELECT 1 FROM pg_trigger t WHERE t.tgrelid='iam_operating_duties'::regclass AND t.tgname='iam_operating_native_completion'AND t.tgdeferrable AND t.tginitdeferred)
$$;
