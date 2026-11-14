SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

DO $prerequisites$
BEGIN
 IF to_regprocedure('public.iam_assignment_governance_integrity()')IS NULL
 OR to_regprocedure('public.iam_operating_governance_integrity()')IS NULL THEN
  RAISE EXCEPTION 'Staff selection requires the complete prior governance schema';
 END IF;
 IF iam_assignment_governance_integrity()IS DISTINCT FROM true
 OR iam_operating_governance_integrity()IS DISTINCT FROM true THEN
  RAISE EXCEPTION 'Staff selection requires unchanged prior governance authority';
 END IF;
END $prerequisites$;

CREATE FUNCTION iam_staff_selection_context(t uuid)RETURNS boolean LANGUAGE plpgsql STABLE SECURITY INVOKER AS $context$
DECLARE raw text:=NULLIF(current_setting('app.iam_staff_selection',true),'');p jsonb;subjects jsonb;
BEGIN
 IF raw IS NULL THEN RETURN false;END IF;
 p:=raw::jsonb;subjects:=NULLIF(current_setting('app.iam_staff_selection_subjects',true),'')::jsonb;
 RETURN COALESCE(hms_tenant_visible(t)AND t=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND jsonb_typeof(p)='object'AND p->>'purpose'='assignment_target_choices'
 AND p->>'tenant_id'=t::text AND(p->>'hospital_id')::uuid<>'00000000-0000-0000-0000-000000000000'::uuid
 AND p->>'actor_subject'=NULLIF(current_setting('app.actor_subject',true),'')
 AND p->>'capability'='iam.governance.assignment.manage'
 AND(p->>'membership_epoch')::bigint BETWEEN 1 AND 9007199254740991
 AND(p->>'owner_count')::bigint BETWEEN 1 AND 1000
 AND(p->>'authenticated_at')::timestamptz<=clock_timestamp()
 AND(p->>'idle_deadline')::timestamptz>clock_timestamp()AND(p->>'source_expires_at')::timestamptz>clock_timestamp()
 AND(p->>'read_expires_at')::timestamptz>clock_timestamp()
 AND(p->>'read_expires_at')::timestamptz<=LEAST((p->>'idle_deadline')::timestamptz,(p->>'source_expires_at')::timestamptz)
 AND(p->>'read_expires_at')::timestamptz<=clock_timestamp()+interval '5 seconds'
 AND jsonb_typeof(subjects)='array'AND jsonb_array_length(subjects)<=21
 AND NOT EXISTS(SELECT 1 FROM jsonb_array_elements(subjects)value WHERE jsonb_typeof(value)<>'string')
 AND((p->>'authority_kind'='owner'AND(p->>'owner_membership_id')::uuid<>'00000000-0000-0000-0000-000000000000'::uuid
 AND p->>'source_grant_id'IS NULL AND p->>'source_grant_version'IS NULL AND p->>'template_key'IS NULL)
 OR(p->>'authority_kind'='grant'AND(p->>'source_grant_id')::uuid<>'00000000-0000-0000-0000-000000000000'::uuid
 AND(p->>'source_grant_version')::bigint BETWEEN 1 AND 9007199254740991
 AND p->>'template_key'='iam.scoped_access_reviewer.v1'AND p->>'owner_membership_id'IS NULL)),false);
EXCEPTION WHEN invalid_text_representation OR invalid_datetime_format OR datetime_field_overflow OR numeric_value_out_of_range THEN RETURN false;
END $context$;

CREATE FUNCTION iam_staff_selection_read(t uuid,relation text,row_data jsonb)RETURNS boolean LANGUAGE plpgsql STABLE SECURITY INVOKER AS $read$
DECLARE p jsonb:=NULLIF(current_setting('app.iam_staff_selection',true),'')::jsonb;
 subjects jsonb:=NULLIF(current_setting('app.iam_staff_selection_subjects',true),'')::jsonb;
BEGIN
 IF iam_staff_selection_context(t)IS NOT TRUE THEN RETURN false;END IF;
 IF relation IN('iam_members','iam_subject_membership_epochs')THEN RETURN subjects? (row_data->>'subject');END IF;
 IF relation='iam_employment_bases'THEN RETURN subjects? (row_data->>'subject')AND row_data->>'hospital_id'=p->>'hospital_id';END IF;
 RETURN false;
END $read$;

CREATE FUNCTION iam_staff_selection_audit(t uuid,actor text,action text,resource_type text,resource uuid)RETURNS boolean LANGUAGE plpgsql STABLE SECURITY INVOKER AS $audit$
DECLARE p jsonb:=NULLIF(current_setting('app.iam_staff_selection',true),'')::jsonb;
BEGIN
 IF iam_staff_selection_context(t)IS NOT TRUE THEN RETURN false;END IF;
 RETURN COALESCE(actor=p->>'actor_subject'AND action='iam.assignment.target_choices.read'
 AND resource_type='workforce_hospitals'AND resource=(p->>'hospital_id')::uuid,false);
END $audit$;

DO $policies$
DECLARE target record;prior record;reading text;writing text;active text:=$expression$NULLIF(current_setting('app.iam_staff_selection',true),'')IS NOT NULL$expression$;
BEGIN
 FOR target IN SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname=current_schema()AND c.relkind IN('r','p')AND(c.relname LIKE 'iam_%'OR c.relname='audit_events')
 AND EXISTS(SELECT 1 FROM pg_attribute a WHERE a.attrelid=c.oid AND a.attname='tenant_id'AND NOT a.attisdropped)LOOP
  reading:='false';writing:='false';
  IF target.relname IN('iam_members','iam_subject_membership_epochs','iam_employment_bases')THEN
   reading:=format('iam_staff_selection_read(tenant_id,%L,to_jsonb(%I))',target.relname,target.relname);
  END IF;
  IF target.relname='iam_employment_bases'THEN
   IF(SELECT count(*)FROM pg_policy p WHERE p.polrelid='iam_employment_bases'::regclass AND p.polpermissive)<>1 THEN RAISE EXCEPTION 'Staff selection requires one canonical permissive policy';END IF;
   SELECT p.polname,pg_get_expr(p.polqual,p.polrelid)AS reading,pg_get_expr(p.polwithcheck,p.polrelid)AS writing INTO STRICT prior FROM pg_policy p WHERE p.polrelid='iam_employment_bases'::regclass AND p.polpermissive;
   EXECUTE format('ALTER POLICY %I ON iam_employment_bases USING(CASE WHEN %s THEN(%s)ELSE(%s)END)WITH CHECK(CASE WHEN %s THEN false ELSE(%s)END)',prior.polname,active,reading,COALESCE(prior.reading,'true'),active,COALESCE(prior.writing,prior.reading,'true'));
  END IF;
  IF target.relname='audit_events'THEN writing:='iam_staff_selection_audit(tenant_id,actor_subject,action,resource_type,resource_id)';END IF;
  EXECUTE format('CREATE POLICY assignment_target_purpose ON %I AS RESTRICTIVE FOR ALL USING(CASE WHEN %s THEN(%s)ELSE true END)WITH CHECK(CASE WHEN %s THEN(%s)ELSE true END)',target.relname,active,reading,active,writing);
  EXECUTE format('CREATE POLICY assignment_target_delete ON %I AS RESTRICTIVE FOR DELETE USING(NOT(%s))',target.relname,active);
 END LOOP;
END $policies$;

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
 EXECUTE format($definition$CREATE OR REPLACE FUNCTION iam_operating_governance_integrity()RETURNS boolean LANGUAGE sql STABLE AS $body$
 SELECT NOT EXISTS(SELECT 1 FROM(VALUES %s)expected(relation,name,digest)LEFT JOIN pg_policy p ON p.polrelid=expected.relation AND p.polname=expected.name WHERE p.oid IS NULL OR md5(p.polcmd::text||p.polpermissive::text||p.polroles::text||COALESCE(pg_get_expr(p.polqual,p.polrelid),'')||COALESCE(pg_get_expr(p.polwithcheck,p.polrelid),''))<>expected.digest)
 AND(SELECT count(*)FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema()AND(c.relname LIKE 'iam_operating_%%'OR p.polname LIKE 'operating_native_%%'))=%s
 AND NOT EXISTS(SELECT 1 FROM(VALUES %s)expected(name,digest)LEFT JOIN pg_proc p ON p.oid=to_regprocedure(expected.name)WHERE p.oid IS NULL OR md5(pg_get_functiondef(p.oid))<>expected.digest)
 AND NOT EXISTS(SELECT 1 FROM(VALUES %s)expected(relation,name,digest)LEFT JOIN pg_trigger t ON t.tgrelid=expected.relation AND t.tgname=expected.name WHERE t.oid IS NULL OR t.tgenabled NOT IN('O','A')OR md5(pg_get_triggerdef(t.oid,true))<>expected.digest)
 AND(SELECT count(*)FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema()AND NOT t.tgisinternal AND(c.relname LIKE 'iam_operating_%%'OR t.tgname='iam_operating_native_global_mutation_guard'))=%s
 $body$ $definition$,policies,policy_count,functions,triggers,trigger_count);
END $migration$;

CREATE FUNCTION iam_staff_selection_capture_integrity()RETURNS void LANGUAGE plpgsql SECURITY INVOKER AS $integrity$
DECLARE policies text;functions text;policy_count bigint;
BEGIN
 IF iam_assignment_governance_integrity()IS DISTINCT FROM true OR iam_operating_governance_integrity()IS DISTINCT FROM true THEN
  RAISE EXCEPTION 'Staff selection capture requires unchanged prior governance authority';
 END IF;
 SELECT string_agg(format('(%L::regclass,%L,%L)',p.polrelid::regclass::text,p.polname,md5(p.polcmd::text||p.polpermissive::text||p.polroles::text||COALESCE(pg_get_expr(p.polqual,p.polrelid),'')||COALESCE(pg_get_expr(p.polwithcheck,p.polrelid),''))),','),count(*)INTO policies,policy_count
 FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname=current_schema()AND(p.polname IN('assignment_target_purpose','assignment_target_delete')OR c.relname IN('iam_members','iam_subject_membership_epochs','iam_employment_bases','audit_events'));
 SELECT string_agg(format('(%L,%L)',p.oid::regprocedure::text,md5(pg_get_functiondef(p.oid))),',')INTO functions FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
 WHERE n.nspname=current_schema()AND p.proname IN('iam_staff_selection_context','iam_staff_selection_read','iam_staff_selection_audit','iam_staff_selection_capture_integrity');
 EXECUTE format($definition$CREATE OR REPLACE FUNCTION iam_staff_selection_integrity()RETURNS boolean LANGUAGE sql STABLE AS $body$
 SELECT NOT EXISTS(SELECT 1 FROM(VALUES %s)expected(relation,name,digest)LEFT JOIN pg_policy p ON p.polrelid=expected.relation AND p.polname=expected.name
 WHERE p.oid IS NULL OR md5(p.polcmd::text||p.polpermissive::text||p.polroles::text||COALESCE(pg_get_expr(p.polqual,p.polrelid),'')||COALESCE(pg_get_expr(p.polwithcheck,p.polrelid),''))<>expected.digest)
 AND(SELECT count(*)FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema()AND(p.polname IN('assignment_target_purpose','assignment_target_delete')OR c.relname IN('iam_members','iam_subject_membership_epochs','iam_employment_bases','audit_events')))=%s
 AND NOT EXISTS(SELECT 1 FROM(VALUES %s)expected(name,digest)LEFT JOIN pg_proc p ON p.oid=to_regprocedure(expected.name)WHERE p.oid IS NULL OR md5(pg_get_functiondef(p.oid))<>expected.digest)
 $body$ $definition$,policies,policy_count,functions);
END $integrity$;
SELECT iam_staff_selection_capture_integrity();
REVOKE ALL ON FUNCTION iam_staff_selection_capture_integrity() FROM PUBLIC;

CREATE FUNCTION iam_staff_selection_support()RETURNS boolean LANGUAGE sql STABLE SECURITY INVOKER AS $$
 SELECT iam_staff_selection_integrity()AND iam_assignment_governance_integrity()AND iam_operating_governance_integrity()
 AND NOT has_function_privilege(current_user,'public.iam_staff_selection_capture_integrity()','EXECUTE')AND NOT EXISTS(
 SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema()AND c.relkind IN('r','p')AND(c.relname LIKE 'iam_%'OR c.relname='audit_events')
 AND EXISTS(SELECT 1 FROM pg_attribute a WHERE a.attrelid=c.oid AND a.attname='tenant_id'AND NOT a.attisdropped)
 AND(NOT c.relrowsecurity OR NOT c.relforcerowsecurity OR has_table_privilege(current_user,c.oid,'TRUNCATE')
 OR NOT EXISTS(SELECT 1 FROM pg_policy p WHERE p.polrelid=c.oid AND p.polname='assignment_target_purpose'AND NOT p.polpermissive AND p.polcmd='*'AND p.polqual IS NOT NULL AND p.polwithcheck IS NOT NULL)
 OR NOT EXISTS(SELECT 1 FROM pg_policy p WHERE p.polrelid=c.oid AND p.polname='assignment_target_delete'AND NOT p.polpermissive AND p.polcmd='d'AND p.polqual IS NOT NULL)))
$$;
