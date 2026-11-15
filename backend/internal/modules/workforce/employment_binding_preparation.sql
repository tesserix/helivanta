SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE FUNCTION workforce_governance_binding_source(t uuid,h uuid,s uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(hms_tenant_visible(t) AND t=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND h=NULLIF(current_setting('app.workforce_hospital',true),'')::uuid
 AND jsonb_typeof(p)='object' AND p-ARRAY['purpose','tenant_id','hospital_id','staff_id','staff_version','employment_epoch','period_id','period_version','binding_id','operation','phase','command_id','file_id','expected_version','input_hash','actor_subject','capability','authority_kind','membership_epoch','owner_count','source_expires_at','authenticated_at','idle_deadline','owner_membership_id','source_grant_id','source_grant_version','template_key']='{}'::jsonb
 AND p->>'purpose'='employment_binding_preparation' AND p->>'tenant_id'=t::text AND p->>'hospital_id'=h::text AND p->>'staff_id'=s::text AND s<>'00000000-0000-0000-0000-000000000000'::uuid
 AND p->>'actor_subject'=NULLIF(current_setting('app.workforce_subject',true),'') AND p->>'actor_subject'=NULLIF(current_setting('app.actor_subject',true),'')
 AND substring(p->>'period_id',15,1)='7' AND (p->>'period_id')::uuid IS NOT NULL
 AND (p->>'period_version')::bigint BETWEEN 1 AND 9007199254740990
 AND (p->>'staff_version')::bigint BETWEEN 1 AND 9007199254740990 AND (p->>'employment_epoch')::bigint BETWEEN 1 AND 9007199254740990
 AND ((p->>'operation'='list' AND p->>'binding_id' IS NULL) OR(p->>'operation' IN('read','history','download','attach','withdraw') AND substring(p->>'binding_id',15,1)='7' AND (p->>'binding_id')::uuid IS NOT NULL))
 AND ((p->>'operation'='download' AND (p->>'phase')::bigint BETWEEN 1 AND 3) OR(p->>'operation'<>'download' AND (p->>'phase')::bigint=1))
 AND ((p->>'operation' IN('list','read','history','download') AND p->>'command_id' IS NULL AND p->>'file_id' IS NULL AND p->>'expected_version' IS NULL AND p->>'input_hash' IS NULL)
 OR(p->>'operation' IN('attach','withdraw') AND substring(p->>'command_id',15,1)='7' AND (p->>'command_id')::uuid IS NOT NULL AND p->>'input_hash' ~ '^[a-f0-9]{64}$'
 AND((p->>'operation'='attach' AND (p->>'file_id')::uuid<>'00000000-0000-0000-0000-000000000000'::uuid AND (p->>'expected_version')::bigint=0)
 OR(p->>'operation'='withdraw' AND p->>'file_id' IS NULL AND (p->>'expected_version')::bigint=1))))
 AND (p->>'membership_epoch')::bigint BETWEEN 1 AND 9007199254740991 AND (p->>'owner_count')::integer BETWEEN 1 AND 1000
 AND (p->>'source_expires_at')::timestamptz>clock_timestamp() AND (p->>'idle_deadline')::timestamptz>clock_timestamp()
 AND (p->>'authenticated_at')::timestamptz<=clock_timestamp() AND p->>'authenticated_at' IS NOT NULL AND p->>'capability'='workforce.employment.prepare'
 AND((p->>'authority_kind'='owner' AND (p->>'owner_membership_id')::uuid<>'00000000-0000-0000-0000-000000000000'::uuid AND p->>'source_grant_id' IS NULL AND p->>'source_grant_version' IS NULL AND p->>'template_key' IS NULL)
 OR(p->>'authority_kind'='grant' AND p->>'owner_membership_id' IS NULL AND (p->>'source_grant_id')::uuid<>'00000000-0000-0000-0000-000000000000'::uuid AND (p->>'source_grant_version')::bigint BETWEEN 1 AND 9007199254740991 AND p->>'template_key'='workforce.employment_preparer.v1')),false)
 FROM(SELECT NULLIF(current_setting('app.workforce_governance_binding_authority',true),'')::jsonb AS p) proof
$$;
CREATE FUNCTION workforce_governance_binding_read(t uuid,h uuid,s uuid,period uuid,v bigint,binding uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT workforce_governance_binding_source(t,h,s) AND period=(p->>'period_id')::uuid AND v=(p->>'period_version')::bigint
 AND(p->>'operation'='list' OR binding=(p->>'binding_id')::uuid)
 FROM(SELECT NULLIF(current_setting('app.workforce_governance_binding_authority',true),'')::jsonb AS p) proof
$$;
CREATE FUNCTION workforce_governance_binding_write(b workforce_employment_evidence_bindings)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(workforce_governance_binding_read((b).tenant_id,(b).hospital_id,(b).staff_id,(b).period_id,(b).period_version,(b).id)
 AND (b).revision_id=(p->>'command_id')::uuid AND (b).input_hash=p->>'input_hash' AND (b).actor_subject=p->>'actor_subject'
 AND (b).version=(p->>'expected_version')::bigint+1
 AND((p->>'operation'='attach' AND (b).status='attached' AND (b).document_id=(p->>'file_id')::uuid AND (b).proposed_by_subject=p->>'actor_subject' AND (b).staff_version=(p->>'staff_version')::bigint AND (b).employment_epoch=(p->>'employment_epoch')::bigint)
 OR(p->>'operation'='withdraw' AND (b).status='withdrawn')),false)
 FROM(SELECT NULLIF(current_setting('app.workforce_governance_binding_authority',true),'')::jsonb AS p) proof
$$;
CREATE FUNCTION workforce_governance_binding_document(t uuid,h uuid,s uuid,file uuid,kind text,record uuid,category text)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT workforce_governance_binding_source(t,h,s) AND kind='enrollment' AND record=s AND category='employment_evidence'
 AND((p->>'operation'='attach' AND file=(p->>'file_id')::uuid)
 OR EXISTS(SELECT 1 FROM workforce_employment_evidence_bindings b WHERE (b.tenant_id,b.hospital_id,b.staff_id,b.document_id)=(t,h,s,file)
 AND workforce_governance_binding_read(b.tenant_id,b.hospital_id,b.staff_id,b.period_id,b.period_version,b.id)))
 FROM(SELECT NULLIF(current_setting('app.workforce_governance_binding_authority',true),'')::jsonb AS p) proof
$$;
CREATE FUNCTION workforce_governance_binding_audit(t uuid,h uuid,actor text,action text,resource uuid,changes jsonb)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(workforce_governance_binding_source(t,h,(p->>'staff_id')::uuid) AND actor=p->>'actor_subject'
 AND((action='workforce.employment_binding.phase' AND resource=(p->>'period_id')::uuid AND changes=jsonb_build_object('authority',p))
 OR(action='workforce.employment_binding.revision' AND resource=(p->>'command_id')::uuid AND pg_trigger_depth()=2 AND changes=jsonb_build_object('authority',p,'version',(p->>'expected_version')::bigint+1))
 OR(action IN('workforce.employment_evidence.attached','workforce.employment_evidence.withdrawn','workforce.employment_evidence.read','workforce.employment_evidence.list','workforce.employment_evidence.history.read','workforce.employment_evidence.receipt.read','workforce.employment_evidence.disclosure.authorized','workforce.employment_evidence.downloaded')
 AND resource=CASE WHEN p->>'operation'='list' THEN (p->>'period_id')::uuid ELSE (p->>'binding_id')::uuid END
 AND changes=jsonb_build_object('period_id',(p->>'period_id')::uuid,'period_version',(p->>'period_version')::bigint,'binding_version',(changes->>'binding_version')::bigint)
 AND (changes->>'binding_version')::bigint BETWEEN 0 AND 2)),false)
 FROM(SELECT NULLIF(current_setting('app.workforce_governance_binding_authority',true),'')::jsonb AS p) proof
$$;
DO $guard$
DECLARE retained text;needle text;replacement text;
BEGIN
 retained:=pg_get_functiondef('public.workforce_employment_evidence_guard()'::regprocedure);
 needle:='workforce_enrollment_allowed(NEW.tenant_id,NEW.hospital_id) IS NOT TRUE';
 replacement:='(workforce_enrollment_allowed(NEW.tenant_id,NEW.hospital_id) OR workforce_governance_binding_write(NEW)) IS NOT TRUE';
 IF(length(retained)-length(replace(retained,needle,'')))/length(needle)<>1 OR strpos(retained,'workforce_employment_evidence_release')=0 OR strpos(retained,'attributable withdrawal')=0 THEN RAISE EXCEPTION 'native binding requires retained canonical evidence guard';END IF;
 EXECUTE replace(retained,needle,replacement);
END $guard$;
CREATE FUNCTION workforce_governance_binding_provenance() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF COALESCE(current_setting('app.workforce_governance_binding_authority',true),'')='' THEN RETURN NULL;END IF;
 IF pg_trigger_depth()<>2 OR NOT workforce_governance_binding_write(jsonb_populate_record(NULL::workforce_employment_evidence_bindings,to_jsonb(NEW))) THEN RAISE EXCEPTION 'invalid native binding provenance' USING ERRCODE='42501';END IF;
 INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes) VALUES(NEW.tenant_id,NEW.hospital_id,NEW.actor_subject,'workforce.employment_binding.revision',NEW.revision_id,jsonb_build_object('authority',NULLIF(current_setting('app.workforce_governance_binding_authority',true),'')::jsonb,'version',NEW.version));
 RETURN NULL;
END $$;
CREATE TRIGGER workforce_governance_binding_provenance AFTER INSERT ON workforce_employment_evidence_history FOR EACH ROW EXECUTE FUNCTION workforce_governance_binding_provenance();
DO $locking$
DECLARE prior record;
BEGIN
 SELECT pg_get_expr(polqual,polrelid) AS reading,pg_get_expr(polwithcheck,polrelid) AS writing INTO STRICT prior FROM pg_policy WHERE polrelid='public.workforce_staff'::regclass AND polname='partner_candidate_update_guard' AND NOT polpermissive AND polcmd='w';
 IF prior.reading IS NULL OR prior.writing IS NULL THEN RAISE EXCEPTION 'native binding requires explicit retained Staff boundary';END IF;
 EXECUTE format('ALTER POLICY partner_candidate_update_guard ON workforce_staff USING((%s) OR workforce_governance_binding_source(tenant_id,hospital_id,id)) WITH CHECK(%s)',prior.reading,prior.writing);
END $locking$;
DO $policies$
DECLARE item record;prior record;reading text;writing text;selected_staff text;
BEGIN
 selected_staff:='(NULLIF(current_setting(''app.workforce_governance_binding_authority'',true),'''')::jsonb->>''staff_id'')::uuid';
 FOR item IN SELECT c.relname AS table_name,c.relrowsecurity,c.relforcerowsecurity FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND c.relkind IN('r','p') AND starts_with(c.relname,'workforce_') AND EXISTS(SELECT 1 FROM pg_attribute a WHERE a.attrelid=c.oid AND a.attname='tenant_id' AND NOT a.attisdropped) ORDER BY c.relname LOOP
 IF NOT item.relrowsecurity OR NOT item.relforcerowsecurity THEN RAISE EXCEPTION 'native binding requires forced RLS on %',item.table_name;END IF;
 reading:='false';writing:='false';
 CASE item.table_name
 WHEN 'workforce_hospitals' THEN reading:=format('workforce_governance_binding_source(tenant_id,id,%s)',selected_staff);
 WHEN 'workforce_staff' THEN reading:='workforce_governance_binding_source(tenant_id,hospital_id,id)';
 WHEN 'workforce_employment_period_drafts','workforce_employment_period_history' THEN reading:=$r$workforce_governance_binding_source(tenant_id,hospital_id,staff_id) AND id=(NULLIF(current_setting('app.workforce_governance_binding_authority',true),'')::jsonb->>'period_id')::uuid$r$;
 WHEN 'workforce_documents' THEN reading:='workforce_governance_binding_document(tenant_id,hospital_id,staff_id,id,record_kind,record_id,category)';
 WHEN 'workforce_employment_evidence_bindings','workforce_employment_evidence_history' THEN
 reading:='workforce_governance_binding_read(tenant_id,hospital_id,staff_id,period_id,period_version,id)';
 writing:=CASE WHEN item.table_name='workforce_employment_evidence_bindings' THEN 'workforce_governance_binding_write(workforce_employment_evidence_bindings)' ELSE 'workforce_governance_binding_write(jsonb_populate_record(NULL::workforce_employment_evidence_bindings,to_jsonb(workforce_employment_evidence_history)))' END;
 WHEN 'workforce_audit' THEN
 reading:=$r$workforce_governance_binding_source(tenant_id,hospital_id,(NULLIF(current_setting('app.workforce_governance_binding_authority',true),'')::jsonb->>'staff_id')::uuid) AND resource_id=(NULLIF(current_setting('app.workforce_governance_binding_authority',true),'')::jsonb->>'command_id')::uuid AND action='workforce.employment_binding.revision'$r$;
 writing:='workforce_governance_binding_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes)';
 ELSE NULL;
 END CASE;
 IF reading<>'false' OR writing<>'false' THEN
 SELECT p.polname,p.polcmd,pg_get_expr(p.polqual,p.polrelid) AS reading,pg_get_expr(p.polwithcheck,p.polrelid) AS writing INTO STRICT prior FROM pg_policy p WHERE p.polrelid=to_regclass('public.'||item.table_name) AND p.polpermissive;
 IF prior.polcmd<>'*' OR prior.reading IS NULL OR prior.writing IS NULL THEN RAISE EXCEPTION 'native binding requires one tenant policy on %',item.table_name;END IF;
 IF reading<>'false' THEN EXECUTE format('ALTER POLICY %I ON %I USING((%s) OR (%s))',prior.polname,item.table_name,prior.reading,reading);END IF;
 IF writing<>'false' THEN EXECUTE format('ALTER POLICY %I ON %I WITH CHECK((%s) OR (%s))',prior.polname,item.table_name,prior.writing,writing);END IF;
 END IF;
 EXECUTE format($p$CREATE POLICY governance_binding_purpose ON %I AS RESTRICTIVE FOR ALL USING(hms_tenant_visible(tenant_id) AND(COALESCE(current_setting('app.workforce_governance_binding_authority',true),'')='' OR (%s))) WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND(COALESCE(current_setting('app.workforce_governance_binding_authority',true),'')='' OR (%s)))$p$,item.table_name,reading,writing);
 EXECUTE format($p$CREATE POLICY governance_binding_delete ON %I AS RESTRICTIVE FOR DELETE USING(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND COALESCE(current_setting('app.workforce_governance_binding_authority',true),'')='')$p$,item.table_name);
 END LOOP;
END $policies$;
