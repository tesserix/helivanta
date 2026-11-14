SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE FUNCTION workforce_governance_document_source(t uuid,h uuid,s uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(hms_tenant_visible(t) AND t=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND h=NULLIF(current_setting('app.workforce_hospital',true),'')::uuid
 AND jsonb_typeof(p)='object' AND p-ARRAY['purpose','tenant_id','hospital_id','staff_id','staff_version','employment_epoch','operation','stage','phase','file_id','actor_subject','capability','authority_kind','membership_epoch','owner_count','source_expires_at','authenticated_at','idle_deadline','owner_membership_id','source_grant_id','source_grant_version','template_key','intent_hash','scan_attempt','source_generation','released_generation']='{}'::jsonb
 AND p->>'purpose'='employment_document_preparation' AND p->>'tenant_id'=t::text AND p->>'hospital_id'=h::text AND p->>'staff_id'=s::text
 AND p->>'actor_subject'=NULLIF(current_setting('app.workforce_subject',true),'')
 AND p->>'actor_subject'=NULLIF(current_setting('app.actor_subject',true),'')
 AND (p->>'staff_version')::bigint BETWEEN 1 AND 9007199254740990 AND (p->>'employment_epoch')::bigint BETWEEN 1 AND 9007199254740990
 AND p->>'review_id' IS NULL AND p->>'period_id' IS NULL AND p->>'command_id' IS NULL AND p->>'actor_staff_id' IS NULL AND p->>'actor_link_id' IS NULL
 AND ((p->>'operation'='list' AND p->>'file_id' IS NULL AND (p->>'phase')::bigint=1 AND p->>'stage'='read')
 OR(p->>'operation'<>'list' AND (p->>'file_id')::uuid<>'00000000-0000-0000-0000-000000000000'::uuid
 AND ((p->>'operation'='register' AND (p->>'phase')::bigint=1 AND p->>'stage' IN('read','intent'))
 OR(p->>'operation'='upload' AND ((p->>'phase')::bigint=1 AND p->>'stage'='read' OR (p->>'phase')::bigint=2 AND p->>'stage' IN('read','stamp') OR (p->>'phase')::bigint=3 AND p->>'stage' IN('read','queue')))
 OR(p->>'operation'='download' AND (p->>'phase')::bigint BETWEEN 1 AND 2 AND p->>'stage'='read')
 OR(p->>'operation' IN('metadata','revoke','retry') AND (p->>'phase')::bigint=1 AND (p->>'stage'='read' OR p->>'stage'=p->>'operation')))))
 AND ((p->>'stage'='read' AND p->>'intent_hash' IS NULL AND p->>'scan_attempt' IS NULL AND p->>'source_generation' IS NULL AND p->>'released_generation' IS NULL)
 OR(p->>'stage'<>'read' AND p->>'intent_hash' ~ '^[a-f0-9]{64}$' AND (p->>'scan_attempt')::bigint BETWEEN 1 AND 9007199254740990
 AND((p->>'stage' IN('intent','stamp') AND (p->>'scan_attempt')::bigint=1 AND p->>'source_generation' IS NULL AND p->>'released_generation' IS NULL)
 OR(p->>'stage'='queue' AND (p->>'scan_attempt')::bigint=1 AND (p->>'source_generation')::bigint>0 AND p->>'released_generation' IS NULL)
 OR(p->>'stage'='revoke' AND (p->>'source_generation')::bigint>0 AND (p->>'released_generation')::bigint>0)
 OR(p->>'stage'='retry' AND (p->>'scan_attempt')::bigint>=2 AND (p->>'source_generation')::bigint>0 AND p->>'released_generation' IS NULL))))
 AND (p->>'membership_epoch')::bigint BETWEEN 1 AND 9007199254740991 AND (p->>'owner_count')::integer BETWEEN 1 AND 1000
 AND (p->>'source_expires_at')::timestamptz>clock_timestamp() AND (p->>'idle_deadline')::timestamptz>clock_timestamp()
 AND (p->>'authenticated_at')::timestamptz<=clock_timestamp() AND p->>'authenticated_at' IS NOT NULL
 AND p->>'capability'='workforce.employment.prepare'
 AND ((p->>'authority_kind'='owner' AND (p->>'owner_membership_id')::uuid<>'00000000-0000-0000-0000-000000000000'::uuid
 AND p->>'source_grant_id' IS NULL AND p->>'source_grant_version' IS NULL AND p->>'template_key' IS NULL)
 OR(p->>'authority_kind'='grant' AND p->>'owner_membership_id' IS NULL AND (p->>'source_grant_id')::uuid<>'00000000-0000-0000-0000-000000000000'::uuid
 AND (p->>'source_grant_version')::bigint BETWEEN 1 AND 9007199254740991 AND p->>'template_key'='workforce.employment_preparer.v1')),false)
 FROM(SELECT NULLIF(current_setting('app.workforce_governance_document_authority',true),'')::jsonb AS p) proof
$$;

CREATE FUNCTION workforce_governance_document_read(t uuid,h uuid,s uuid,file uuid,kind text,record uuid,v bigint,category text)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT workforce_governance_document_source(t,h,s) AND kind='enrollment' AND record=s AND category='employment_evidence'
 AND v=(p->>'staff_version')::bigint AND (p->>'operation'='list' OR file=(p->>'file_id')::uuid)
 FROM(SELECT NULLIF(current_setting('app.workforce_governance_document_authority',true),'')::jsonb AS p) proof
$$;
CREATE FUNCTION workforce_governance_document_hash(kind text,record uuid,category text,name text,media text,bytes bigint,digest text)
RETURNS text LANGUAGE sql IMMUTABLE AS $$
 SELECT encode(sha256(convert_to(jsonb_build_object('record_kind',kind,'record_id',record,'category',category,'filename',name,'content_type',media,'size',bytes,'sha256',digest)::text,'UTF8')),'hex')
$$;
CREATE FUNCTION workforce_governance_document_write(d workforce_documents)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(workforce_governance_document_read((d).tenant_id,(d).hospital_id,(d).staff_id,(d).id,(d).record_kind,(d).record_id,(d).record_version,(d).category)
 AND p->>'stage'<>'read' AND NOT (d).frozen
 AND (d).scan_attempt=(p->>'scan_attempt')::bigint
 AND p->>'intent_hash'=workforce_governance_document_hash((d).record_kind,(d).record_id,(d).category,(d).filename,(d).content_type,(d).size,(d).sha256)
 AND ((p->>'stage'='intent' AND (d).status='upload_pending' AND (d).uploaded_at IS NULL AND (d).source_generation IS NULL AND (d).released_generation IS NULL)
 OR(p->>'stage'='stamp' AND (d).status='upload_pending' AND (d).uploaded_at IS NOT NULL AND (d).source_generation IS NULL AND (d).released_generation IS NULL)
 OR(p->>'stage' IN('queue','retry') AND (d).status='scan_pending' AND (d).source_generation=(p->>'source_generation')::bigint AND (d).released_generation IS NULL AND (d).failure_code IS NULL)
 OR(p->>'stage'='revoke' AND (d).status='revoked' AND (d).source_generation=(p->>'source_generation')::bigint AND (d).released_generation=(p->>'released_generation')::bigint)),false)
 FROM(SELECT NULLIF(current_setting('app.workforce_governance_document_authority',true),'')::jsonb AS p) proof
$$;

CREATE FUNCTION workforce_governance_document_audit(t uuid,h uuid,actor text,action text,resource uuid,changes jsonb)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(workforce_governance_document_source(t,h,(p->>'staff_id')::uuid)
 AND actor=p->>'actor_subject' AND changes=jsonb_build_object('authority',p,'version',(p->>'staff_version')::bigint)
 AND ((action='workforce.employment_document.list' AND p->>'operation'='list' AND resource=(p->>'staff_id')::uuid)
 OR(action IN('workforce.employment_document.phase','workforce.employment_document.downloaded') AND resource=(p->>'file_id')::uuid
 AND(action<>'workforce.employment_document.downloaded' OR p->>'operation'='download'))
 OR(action='workforce.employment_document.file.read' AND p->>'operation'='list'
 AND EXISTS(SELECT 1 FROM workforce_documents d WHERE d.tenant_id=t AND d.hospital_id=h AND d.id=resource AND workforce_governance_document_read(d.tenant_id,d.hospital_id,d.staff_id,d.id,d.record_kind,d.record_id,d.record_version,d.category)))
 OR(action='workforce.employment_document.mutation' AND resource=(p->>'file_id')::uuid AND p->>'stage'<>'read' AND pg_trigger_depth()=1)),false)
 FROM(SELECT NULLIF(current_setting('app.workforce_governance_document_authority',true),'')::jsonb AS p) proof
$$;
CREATE FUNCTION workforce_governance_document_provenance() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF COALESCE(current_setting('app.workforce_governance_document_authority',true),'')='' THEN RETURN NULL;END IF;
 IF NOT workforce_governance_document_write(NEW) OR pg_trigger_depth()<>1 THEN RAISE EXCEPTION 'invalid employment document mutation source' USING ERRCODE='42501';END IF;
 INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes)
 VALUES(NEW.tenant_id,NEW.hospital_id,current_setting('app.actor_subject',true),'workforce.employment_document.mutation',NEW.id,
 jsonb_build_object('authority',NULLIF(current_setting('app.workforce_governance_document_authority',true),'')::jsonb,'version',NEW.record_version));
 RETURN NULL;
END $$;
CREATE TRIGGER workforce_governance_document_provenance AFTER INSERT OR UPDATE ON workforce_documents FOR EACH ROW EXECUTE FUNCTION workforce_governance_document_provenance();

DO $locking_read$
DECLARE prior record;
BEGIN
 SELECT pg_get_expr(polqual,polrelid) AS reading,pg_get_expr(polwithcheck,polrelid) AS writing INTO STRICT prior
 FROM pg_policy WHERE polrelid='public.workforce_staff'::regclass AND polname='partner_candidate_update_guard' AND NOT polpermissive AND polcmd='w';
 IF prior.reading IS NULL OR prior.writing IS NULL THEN RAISE EXCEPTION 'employment documents require the retained Staff update boundary';END IF;
 EXECUTE format('ALTER POLICY partner_candidate_update_guard ON workforce_staff USING((%s) OR workforce_governance_document_source(tenant_id,hospital_id,id)) WITH CHECK(%s)',prior.reading,prior.writing);
END $locking_read$;

DO $policies$
DECLARE item record;prior record;reading text;writing text;selected_staff text;
BEGIN
 selected_staff:='(NULLIF(current_setting(''app.workforce_governance_document_authority'',true),'''')::jsonb->>''staff_id'')::uuid';
 FOR item IN SELECT c.relname AS table_name,c.relrowsecurity,c.relforcerowsecurity FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname='public' AND c.relkind IN('r','p') AND starts_with(c.relname,'workforce_')
 AND EXISTS(SELECT 1 FROM pg_attribute a WHERE a.attrelid=c.oid AND a.attname='tenant_id' AND NOT a.attisdropped) ORDER BY c.relname LOOP
 IF NOT item.relrowsecurity OR NOT item.relforcerowsecurity THEN RAISE EXCEPTION 'employment documents require forced tenant RLS on %',item.table_name;END IF;
 reading:='false';writing:='false';
 CASE item.table_name
 WHEN 'workforce_hospitals' THEN reading:=format('workforce_governance_document_source(tenant_id,id,%s)',selected_staff);
 WHEN 'workforce_staff' THEN reading:='workforce_governance_document_source(tenant_id,hospital_id,id)';
 WHEN 'workforce_documents' THEN
  reading:='workforce_governance_document_read(tenant_id,hospital_id,staff_id,id,record_kind,record_id,record_version,category)';
  writing:='workforce_governance_document_write(workforce_documents)';
 WHEN 'workforce_document_contributors' THEN
  reading:=$read$workforce_governance_document_source(tenant_id,hospital_id,staff_id)
   AND document_id=(NULLIF(current_setting('app.workforce_governance_document_authority',true),'')::jsonb->>'file_id')::uuid
   AND subject=NULLIF(current_setting('app.actor_subject',true),'')$read$;
  writing:=reading||$write$ AND pg_trigger_depth()=1
   AND (NULLIF(current_setting('app.workforce_governance_document_authority',true),'')::jsonb->>'stage') IN('intent','stamp','queue','revoke','retry')
   AND creation_captured=((NULLIF(current_setting('app.workforce_governance_document_authority',true),'')::jsonb->>'stage')='intent')$write$;
 WHEN 'workforce_audit' THEN writing:='workforce_governance_document_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes)';
 ELSE NULL;
 END CASE;
 IF reading<>'false' OR writing<>'false' THEN
  SELECT p.polname,p.polcmd,pg_get_expr(p.polqual,p.polrelid) AS reading,pg_get_expr(p.polwithcheck,p.polrelid) AS writing
  INTO STRICT prior FROM pg_policy p WHERE p.polrelid=to_regclass('public.'||item.table_name) AND p.polpermissive;
  IF prior.polcmd<>'*' OR prior.reading IS NULL OR prior.writing IS NULL THEN RAISE EXCEPTION 'employment documents require one explicit tenant policy on %',item.table_name;END IF;
  IF reading<>'false' THEN EXECUTE format('ALTER POLICY %I ON %I USING((%s) OR (%s))',prior.polname,item.table_name,prior.reading,reading);END IF;
  IF writing<>'false' THEN EXECUTE format('ALTER POLICY %I ON %I WITH CHECK((%s) OR (%s))',prior.polname,item.table_name,prior.writing,writing);END IF;
 END IF;
 EXECUTE format($policy$CREATE POLICY governance_document_purpose ON %I AS RESTRICTIVE FOR ALL
 USING(hms_tenant_visible(tenant_id) AND (COALESCE(current_setting('app.workforce_governance_document_authority',true),'')='' OR (%s)))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND (COALESCE(current_setting('app.workforce_governance_document_authority',true),'')='' OR (%s)))$policy$,item.table_name,reading,writing);
 EXECUTE format($policy$CREATE POLICY governance_document_delete ON %I AS RESTRICTIVE FOR DELETE
 USING(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND COALESCE(current_setting('app.workforce_governance_document_authority',true),'')='')$policy$,item.table_name);
 END LOOP;
END $policies$;
