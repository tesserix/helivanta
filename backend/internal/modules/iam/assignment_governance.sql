SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE TABLE iam_assignment_governance_requests (
 tenant_id uuid NOT NULL,request_id uuid NOT NULL,proposal_id uuid NOT NULL,hospital_id uuid NOT NULL,
 proposal_version bigint NOT NULL,kind text NOT NULL CHECK(kind IN('proposal','decision')),
 actor_subject text NOT NULL,authority jsonb NOT NULL CHECK(jsonb_typeof(authority)='object'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,request_id),UNIQUE(tenant_id,proposal_id,proposal_version),
 FOREIGN KEY(tenant_id,proposal_id,proposal_version) REFERENCES iam_assignment_proposal_history(tenant_id,id,version) ON DELETE RESTRICT
);
CREATE UNIQUE INDEX iam_assignment_governance_origin ON iam_assignment_governance_requests(tenant_id,proposal_id) WHERE kind='proposal';
CREATE TABLE iam_assignment_governance_verifications (
 tenant_id uuid NOT NULL,request_id uuid NOT NULL,proposal_id uuid NOT NULL,hospital_id uuid NOT NULL,
 eligible boolean NOT NULL,created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,request_id),
 FOREIGN KEY(tenant_id,request_id) REFERENCES iam_assignment_governance_requests(tenant_id,request_id) ON DELETE RESTRICT
);

CREATE FUNCTION iam_assignment_governance_selected(tenant uuid,hospital uuid,proposal uuid) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(NULLIF(current_setting('app.iam_assignment_native_hospital',true),'') IS NULL OR
 (tenant=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND hospital=NULLIF(current_setting('app.iam_assignment_native_hospital',true),'')::uuid
 AND (NULLIF(current_setting('app.iam_assignment_native_proposal',true),'') IS NULL
 OR proposal=NULLIF(current_setting('app.iam_assignment_native_proposal',true),'')::uuid)),false)
$$;

DO $$ DECLARE relation text; BEGIN
 FOREACH relation IN ARRAY ARRAY['iam_assignment_governance_requests','iam_assignment_governance_verifications'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',relation);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',relation);
  EXECUTE format('CREATE POLICY tenant_isolation ON %I USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND current_setting(''app.assignment_access'',true)=''authorized'') WITH CHECK(tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND current_setting(''app.assignment_access'',true)=''authorized'')',relation);
  EXECUTE format('CREATE POLICY native_selection ON %I AS RESTRICTIVE USING(iam_assignment_governance_selected(tenant_id,hospital_id,proposal_id)) WITH CHECK(iam_assignment_governance_selected(tenant_id,hospital_id,proposal_id))',relation);
  EXECUTE format('CREATE TRIGGER iam_assignment_governance_preserve BEFORE UPDATE OR DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION iam_assignment_proposal_preserve()',relation);
 END LOOP;
END $$;

CREATE POLICY native_selection ON iam_assignment_proposals AS RESTRICTIVE
 USING(iam_assignment_governance_selected(tenant_id,hospital_id,id)) WITH CHECK(iam_assignment_governance_selected(tenant_id,hospital_id,id));
CREATE POLICY native_selection ON iam_assignment_snapshots AS RESTRICTIVE
 USING(iam_assignment_governance_selected(tenant_id,hospital_id,id)) WITH CHECK(iam_assignment_governance_selected(tenant_id,hospital_id,id));
CREATE POLICY native_selection ON iam_assignment_scope_epochs AS RESTRICTIVE
 USING(NULLIF(current_setting('app.iam_assignment_native_hospital',true),'') IS NULL OR EXISTS(SELECT 1 FROM iam_assignment_proposals p WHERE p.tenant_id=iam_assignment_scope_epochs.tenant_id AND p.hospital_id=iam_assignment_scope_epochs.hospital_id AND p.subject=iam_assignment_scope_epochs.subject))
 WITH CHECK(NULLIF(current_setting('app.iam_assignment_native_hospital',true),'') IS NULL OR EXISTS(SELECT 1 FROM iam_assignment_proposals p WHERE p.tenant_id=iam_assignment_scope_epochs.tenant_id AND p.hospital_id=iam_assignment_scope_epochs.hospital_id AND p.subject=iam_assignment_scope_epochs.subject));
DO $$ DECLARE relation text;column_name text;BEGIN
 FOREACH relation IN ARRAY ARRAY['iam_assignment_proposal_history','iam_assignment_decisions','iam_assignment_department_verifications'] LOOP
  column_name:=CASE WHEN relation='iam_assignment_proposal_history' THEN 'id' ELSE 'proposal_id' END;
  EXECUTE format('CREATE POLICY native_selection ON %I AS RESTRICTIVE USING(NULLIF(current_setting(''app.iam_assignment_native_hospital'',true),'''') IS NULL OR EXISTS(SELECT 1 FROM iam_assignment_proposals p WHERE p.tenant_id=%I.tenant_id AND p.id=%I.%I)) WITH CHECK(NULLIF(current_setting(''app.iam_assignment_native_hospital'',true),'''') IS NULL OR EXISTS(SELECT 1 FROM iam_assignment_proposals p WHERE p.tenant_id=%I.tenant_id AND p.id=%I.%I))',relation,relation,relation,column_name,relation,relation,column_name);
 END LOOP;
END $$;

CREATE FUNCTION iam_assignment_governance_authority_current(proof jsonb,checked_tenant uuid,checked_hospital uuid,checked_actor text,checked_capability text) RETURNS boolean LANGUAGE sql VOLATILE AS $$
 SELECT COALESCE(proof->>'tenant_id'=checked_tenant::text AND proof->>'hospital_id'=checked_hospital::text
 AND proof->>'actor_subject'=checked_actor AND proof->>'capability'=checked_capability
 AND (proof-ARRAY['tenant_id','hospital_id','actor_subject','capability','authority_kind','owner_membership_id','source_grant_id','source_grant_version','membership_epoch','authenticated_at','idle_deadline','expires_at'])='{}'::jsonb
 AND (proof->>'membership_epoch')::bigint BETWEEN 1 AND 9007199254740991
 AND (proof->>'authenticated_at')::timestamptz<=clock_timestamp()
 AND (proof->>'idle_deadline')::timestamptz>clock_timestamp() AND (proof->>'expires_at')::timestamptz>clock_timestamp()
 AND NOT EXISTS(SELECT 1 FROM iam_credential_revocations v WHERE v.subject=checked_actor AND v.revoked_at>=(proof->>'authenticated_at')::timestamptz)
 AND EXISTS(SELECT 1 FROM iam_subject_membership_epochs e WHERE e.tenant_id=checked_tenant AND e.subject=checked_actor AND e.member_count>0 AND e.epoch=(proof->>'membership_epoch')::bigint)
 AND ((proof->>'authority_kind'='owner' AND proof->>'source_grant_id' IS NULL AND proof->>'source_grant_version' IS NULL
 AND EXISTS(SELECT 1 FROM iam_members m WHERE m.tenant_id=checked_tenant AND m.id=(proof->>'owner_membership_id')::uuid AND m.subject=checked_actor AND m.role_key='tenant_admin'))
 OR(proof->>'authority_kind'='grant' AND proof->>'owner_membership_id' IS NULL
 AND EXISTS(SELECT 1 FROM iam_governance_grants g WHERE g.tenant_id=checked_tenant AND g.id=(proof->>'source_grant_id')::uuid AND g.subject=checked_actor AND g.version=(proof->>'source_grant_version')::bigint AND g.membership_epoch=(proof->>'membership_epoch')::bigint AND g.template_key='iam.scoped_access_reviewer.v1' AND g.scope_kind='hospital' AND g.scope_id=checked_hospital AND g.status IN('active','provisional_active') AND g.valid_from<=clock_timestamp() AND g.expires_at=(proof->>'expires_at')::timestamptz))),false)
$$;

CREATE FUNCTION iam_assignment_governance_request_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE proof jsonb:=NULLIF(current_setting('app.iam_assignment_native_authority',true),'')::jsonb;
 p iam_assignment_proposals%ROWTYPE;d iam_assignment_decisions%ROWTYPE;expected_capability text;
BEGIN
 SELECT * INTO p FROM iam_assignment_proposals WHERE tenant_id=NEW.tenant_id AND id=NEW.proposal_id;
 IF NOT FOUND OR (p.request_id,p.hospital_id,p.version) IS DISTINCT FROM (NEW.request_id,NEW.hospital_id,NEW.proposal_version)
 OR NOT iam_assignment_governance_selected(NEW.tenant_id,NEW.hospital_id,NEW.proposal_id)
 OR NULLIF(current_setting('app.iam_assignment_native_hospital',true),'') IS NULL
 OR NEW.authority IS DISTINCT FROM proof OR NEW.actor_subject IS DISTINCT FROM current_setting('app.actor_subject',true) THEN
  RAISE EXCEPTION 'native assignment request must match canonical command' USING ERRCODE='42501';
 END IF;
 IF NEW.kind='proposal' THEN
  IF p.version<>1 OR p.status<>'pending_scope' OR p.proposed_by<>NEW.actor_subject THEN
   RAISE EXCEPTION 'native assignment origin required' USING ERRCODE='23514';
  END IF;
  expected_capability:='iam.assignment.manage';
 ELSE
  SELECT * INTO d FROM iam_assignment_decisions WHERE tenant_id=NEW.tenant_id AND proposal_id=NEW.proposal_id AND request_id=NEW.request_id AND result_version=NEW.proposal_version;
  IF NOT FOUND OR d.actor_subject<>NEW.actor_subject THEN
   RAISE EXCEPTION 'native assignment decision required' USING ERRCODE='23514';
  END IF;
  expected_capability:=CASE WHEN d.decision='withdraw' THEN 'iam.assignment.manage' ELSE 'iam.assignment.review' END;
 END IF;
 IF NOT iam_assignment_governance_authority_current(proof,NEW.tenant_id,NEW.hospital_id,NEW.actor_subject,expected_capability) THEN
  RAISE EXCEPTION 'native assignment requires exact current governance source' USING ERRCODE='42501';
 END IF;
 INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id) VALUES(NEW.tenant_id,NEW.actor_subject,'iam.assignment.governance.requested','iam_assignment_proposals',NEW.proposal_id);
 RETURN NEW;
END $$;
CREATE TRIGGER iam_assignment_governance_request_guard BEFORE INSERT ON iam_assignment_governance_requests FOR EACH ROW EXECUTE FUNCTION iam_assignment_governance_request_guard();

CREATE FUNCTION iam_assignment_governance_verification_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF current_setting('app.iam_assignment_native_verifier',true) IS DISTINCT FROM NEW.request_id::text OR NOT EXISTS(
 SELECT 1 FROM iam_assignment_governance_requests r JOIN iam_assignment_proposals p ON p.tenant_id=r.tenant_id AND p.id=r.proposal_id
 WHERE (r.tenant_id,r.request_id,r.proposal_id,r.hospital_id)=(NEW.tenant_id,NEW.request_id,NEW.proposal_id,NEW.hospital_id)
 AND p.request_id=r.request_id AND p.status IN('pending_scope','pending_decision_scope')) THEN
  RAISE EXCEPTION 'native assignment response requires current request' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER iam_assignment_governance_verification_guard BEFORE INSERT ON iam_assignment_governance_verifications FOR EACH ROW EXECUTE FUNCTION iam_assignment_governance_verification_guard();

CREATE FUNCTION iam_assignment_governance_complete() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE request uuid;version bigint;proposal uuid;BEGIN
 IF NULLIF(current_setting('app.iam_assignment_native_authority',true),'') IS NULL THEN RETURN NULL;END IF;
 request:=NEW.request_id;
 IF TG_TABLE_NAME='iam_assignment_proposals' THEN
  IF TG_OP='UPDATE' AND NEW.request_id=OLD.request_id THEN RETURN NULL;END IF;
  version:=NEW.version;proposal:=NEW.id;
 ELSE version:=NEW.result_version;proposal:=NEW.proposal_id;END IF;
 IF NOT EXISTS(SELECT 1 FROM iam_assignment_governance_requests r WHERE r.tenant_id=NEW.tenant_id AND r.request_id=request AND r.proposal_id=proposal AND r.proposal_version=version) THEN
  RAISE EXCEPTION 'native assignment command requires retained source' USING ERRCODE='23514';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER iam_assignment_governance_complete AFTER INSERT OR UPDATE ON iam_assignment_proposals DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION iam_assignment_governance_complete();
CREATE CONSTRAINT TRIGGER iam_assignment_governance_complete AFTER INSERT ON iam_assignment_decisions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION iam_assignment_governance_complete();

CREATE FUNCTION iam_assignment_governance_settlement_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE original jsonb;proof jsonb;BEGIN
 IF NEW.request_id=OLD.request_id AND OLD.status IN('pending_scope','pending_decision_scope')
 AND EXISTS(SELECT 1 FROM iam_assignment_governance_requests r WHERE r.tenant_id=OLD.tenant_id AND r.request_id=OLD.request_id) THEN
  IF COALESCE(current_setting('app.iam_assignment_native_settlement',true),'') NOT IN('authorized','denied')
  OR current_setting('app.iam_assignment_native_settlement',true)='denied' AND NEW.status NOT IN('pending_review','scope_rejected') THEN
   RAISE EXCEPTION 'native assignment requires guarded settlement' USING ERRCODE='42501';
  END IF;
  IF current_setting('app.iam_assignment_native_settlement',true)='authorized' THEN
   SELECT r.authority INTO original FROM iam_assignment_governance_requests r WHERE r.tenant_id=OLD.tenant_id AND r.request_id=OLD.request_id;
   proof:=NULLIF(current_setting('app.iam_assignment_native_authority',true),'')::jsonb;
   IF proof IS DISTINCT FROM original OR NOT iam_assignment_governance_authority_current(proof,OLD.tenant_id,OLD.hospital_id,proof->>'actor_subject',proof->>'capability') THEN
    RAISE EXCEPTION 'native assignment settlement requires original current source' USING ERRCODE='42501';
   END IF;
  END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER iam_assignment_governance_settlement_guard BEFORE UPDATE ON iam_assignment_proposals FOR EACH ROW EXECUTE FUNCTION iam_assignment_governance_settlement_guard();

DO $migration$
DECLARE policies text;functions text;triggers text;policy_count bigint;trigger_count bigint;
BEGIN
 SELECT string_agg(format('(%L::regclass,%L,%L)',p.polrelid::regclass::text,p.polname,md5(p.polcmd::text||p.polpermissive::text||p.polroles::text||COALESCE(pg_get_expr(p.polqual,p.polrelid),'')||COALESCE(pg_get_expr(p.polwithcheck,p.polrelid),''))),','),count(*) INTO policies,policy_count
 FROM pg_policy p WHERE p.polrelid IN('iam_assignment_proposals'::regclass,'iam_assignment_proposal_history'::regclass,'iam_assignment_decisions'::regclass,'iam_assignment_snapshots'::regclass,'iam_assignment_scope_epochs'::regclass,'iam_assignment_department_verifications'::regclass,'iam_assignment_governance_requests'::regclass,'iam_assignment_governance_verifications'::regclass);
 SELECT string_agg(format('(%L,%L)',p.oid::regprocedure::text,md5(pg_get_functiondef(p.oid))),',') INTO functions FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
 WHERE n.nspname='public' AND p.proname IN('iam_assignment_governance_selected','iam_assignment_governance_authority_current','iam_assignment_governance_request_guard','iam_assignment_governance_verification_guard','iam_assignment_governance_complete','iam_assignment_governance_settlement_guard','iam_assignment_proposal_guard','iam_assignment_proposal_record','iam_assignment_proposal_preserve','iam_assignment_proposal_history_guard','iam_assignment_decision_guard','iam_assignment_snapshot_guard','iam_assignment_snapshot_record','iam_assignment_epoch_guard','iam_assignment_department_verification_guard','iam_proposal_membership_bind','iam_snapshot_membership_bind','iam_assignment_establishment_immutable') OR n.nspname='public' AND (p.proname LIKE 'iam_assignment_%' OR p.proname LIKE 'iam_proposal_%' OR p.proname LIKE 'iam_snapshot_%') AND p.proname NOT IN('iam_assignment_governance_integrity','iam_assignment_governance_support');
 SELECT string_agg(format('(%L::regclass,%L,%L)',t.tgrelid::regclass::text,t.tgname,md5(pg_get_triggerdef(t.oid,true))),','),count(*) INTO triggers,trigger_count FROM pg_trigger t
 WHERE NOT t.tgisinternal AND t.tgrelid IN('iam_assignment_proposals'::regclass,'iam_assignment_proposal_history'::regclass,'iam_assignment_decisions'::regclass,'iam_assignment_snapshots'::regclass,'iam_assignment_scope_epochs'::regclass,'iam_assignment_department_verifications'::regclass,'iam_assignment_governance_requests'::regclass,'iam_assignment_governance_verifications'::regclass);
 EXECUTE format($definition$CREATE FUNCTION iam_assignment_governance_integrity() RETURNS boolean LANGUAGE sql STABLE AS $body$
 SELECT NOT EXISTS(SELECT 1 FROM (VALUES %s) expected(relation,name,digest) LEFT JOIN pg_policy p ON p.polrelid=expected.relation AND p.polname=expected.name WHERE p.oid IS NULL OR md5(p.polcmd::text||p.polpermissive::text||p.polroles::text||COALESCE(pg_get_expr(p.polqual,p.polrelid),'')||COALESCE(pg_get_expr(p.polwithcheck,p.polrelid),''))<>expected.digest)
 AND (SELECT count(*) FROM pg_policy p WHERE p.polrelid IN('iam_assignment_proposals'::regclass,'iam_assignment_proposal_history'::regclass,'iam_assignment_decisions'::regclass,'iam_assignment_snapshots'::regclass,'iam_assignment_scope_epochs'::regclass,'iam_assignment_department_verifications'::regclass,'iam_assignment_governance_requests'::regclass,'iam_assignment_governance_verifications'::regclass))=%s
 AND NOT EXISTS(SELECT 1 FROM (VALUES %s) expected(name,digest) LEFT JOIN pg_proc p ON p.oid=to_regprocedure(expected.name) WHERE p.oid IS NULL OR md5(pg_get_functiondef(p.oid))<>expected.digest)
 AND NOT EXISTS(SELECT 1 FROM (VALUES %s) expected(relation,name,digest) LEFT JOIN pg_trigger t ON t.tgrelid=expected.relation AND t.tgname=expected.name WHERE t.oid IS NULL OR t.tgenabled NOT IN('O','A') OR md5(pg_get_triggerdef(t.oid,true))<>expected.digest)
 AND (SELECT count(*) FROM pg_trigger t WHERE NOT t.tgisinternal AND t.tgrelid IN('iam_assignment_proposals'::regclass,'iam_assignment_proposal_history'::regclass,'iam_assignment_decisions'::regclass,'iam_assignment_snapshots'::regclass,'iam_assignment_scope_epochs'::regclass,'iam_assignment_department_verifications'::regclass,'iam_assignment_governance_requests'::regclass,'iam_assignment_governance_verifications'::regclass))=%s
 $body$ $definition$,policies,policy_count,functions,triggers,trigger_count);
END $migration$;

CREATE FUNCTION iam_assignment_governance_support() RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT iam_assignment_governance_integrity() AND NOT EXISTS(SELECT 1 FROM (VALUES
 ('iam_assignment_proposals'),('iam_assignment_proposal_history'),('iam_assignment_decisions'),('iam_assignment_snapshots'),
 ('iam_assignment_scope_epochs'),('iam_assignment_department_verifications'),('iam_assignment_governance_requests'),('iam_assignment_governance_verifications')) AS expected(name)
 WHERE NOT EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relname=expected.name AND c.relrowsecurity AND c.relforcerowsecurity
 AND NOT has_table_privilege(current_user,c.oid,'TRUNCATE')
 AND EXISTS(SELECT 1 FROM pg_policy p WHERE p.polrelid=c.oid AND p.polname='tenant_isolation')
 AND EXISTS(SELECT 1 FROM pg_policy p WHERE p.polrelid=c.oid AND p.polname='native_selection' AND NOT p.polpermissive)))
 AND NOT EXISTS(SELECT 1 FROM (VALUES
 ('iam_assignment_proposals','iam_assignment_establishment_immutable','iam_assignment_establishment_immutable()'),
 ('iam_assignment_decisions','iam_assignment_decision_guard','iam_assignment_decision_guard()'),
 ('iam_assignment_decisions','iam_assignment_decision_immutable','iam_assignment_proposal_preserve()'),
 ('iam_assignment_decisions','iam_assignment_decision_no_truncate','iam_assignment_proposal_preserve()'),
 ('iam_assignment_proposals','iam_assignment_proposal_guard','iam_assignment_proposal_guard()'),
 ('iam_assignment_proposals','iam_assignment_proposal_record','iam_assignment_proposal_record()'),
 ('iam_assignment_proposals','iam_assignment_proposal_no_delete','iam_assignment_proposal_preserve()'),
 ('iam_assignment_proposals','iam_assignment_proposal_no_truncate','iam_assignment_proposal_preserve()'),
 ('iam_assignment_proposal_history','iam_assignment_proposal_history_immutable','iam_assignment_proposal_preserve()'),
 ('iam_assignment_proposal_history','iam_assignment_proposal_history_no_truncate','iam_assignment_proposal_preserve()'),
 ('iam_assignment_proposal_history','iam_assignment_proposal_history_guard','iam_assignment_proposal_history_guard()'),
 ('iam_assignment_department_verifications','iam_assignment_department_verification_guard','iam_assignment_department_verification_guard()'),
 ('iam_assignment_department_verifications','iam_assignment_department_verification_immutable','iam_assignment_proposal_preserve()'),
 ('iam_assignment_department_verifications','iam_assignment_department_verification_no_truncate','iam_assignment_proposal_preserve()'),
 ('iam_assignment_proposals','iam_proposal_membership_bind','iam_proposal_membership_bind()'),
 ('iam_assignment_snapshots','iam_snapshot_membership_bind','iam_snapshot_membership_bind()'),
 ('iam_assignment_snapshots','iam_assignment_snapshot_guard','iam_assignment_snapshot_guard()'),
 ('iam_assignment_snapshots','iam_assignment_snapshot_immutable','iam_assignment_proposal_preserve()'),
 ('iam_assignment_snapshots','iam_assignment_snapshot_no_truncate','iam_assignment_proposal_preserve()'),
 ('iam_assignment_scope_epochs','iam_assignment_epoch_guard','iam_assignment_epoch_guard()'),
 ('iam_assignment_scope_epochs','iam_assignment_epoch_no_delete','iam_assignment_proposal_preserve()'),
 ('iam_assignment_scope_epochs','iam_assignment_epoch_no_truncate','iam_assignment_proposal_preserve()'),
 ('iam_assignment_proposals','iam_assignment_z_snapshot_record','iam_assignment_snapshot_record()'),
 ('iam_assignment_governance_requests','iam_assignment_governance_request_guard','iam_assignment_governance_request_guard()'),
 ('iam_assignment_governance_verifications','iam_assignment_governance_verification_guard','iam_assignment_governance_verification_guard()'),
 ('iam_assignment_governance_requests','iam_assignment_governance_preserve','iam_assignment_proposal_preserve()'),
 ('iam_assignment_governance_verifications','iam_assignment_governance_preserve','iam_assignment_proposal_preserve()'),
 ('iam_assignment_proposals','iam_assignment_governance_complete','iam_assignment_governance_complete()'),
 ('iam_assignment_decisions','iam_assignment_governance_complete','iam_assignment_governance_complete()'),
 ('iam_assignment_proposals','iam_assignment_governance_settlement_guard','iam_assignment_governance_settlement_guard()')) AS expected(relation,name,function_name)
 WHERE NOT EXISTS(SELECT 1 FROM pg_trigger t WHERE t.tgrelid=to_regclass('public.'||expected.relation) AND t.tgname=expected.name AND t.tgfoid=to_regprocedure('public.'||expected.function_name) AND NOT t.tgisinternal AND t.tgenabled IN('O','A')))
$$;
