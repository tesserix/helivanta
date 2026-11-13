SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE FUNCTION workforce_governance_draft_source(t uuid,h uuid,s uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(hms_tenant_visible(t)
 AND t=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND h=NULLIF(current_setting('app.workforce_hospital',true),'')::uuid
 AND p->>'purpose'='employment_preparation' AND p->>'tenant_id'=t::text AND p->>'hospital_id'=h::text
 AND p->>'actor_subject'=NULLIF(current_setting('app.workforce_subject',true),'')
 AND p->>'actor_subject'=NULLIF(current_setting('app.actor_subject',true),'')
 AND p->>'staff_id'=s::text AND s=NULLIF(current_setting('app.workforce_identity_staff',true),'')::uuid

 AND ((p->>'operation' IN('source','list') AND p->>'period_id' IS NULL AND p->>'command_id' IS NULL
 AND p->>'expected_version' IS NULL AND p->>'staff_version' IS NULL AND p->>'employment_epoch' IS NULL AND p->>'input_hash' IS NULL AND p->>'sponsor_id' IS NULL)
 OR (p->>'operation' IN('read','history') AND substring(p->>'period_id',15,1)='7' AND (p->>'period_id')::uuid IS NOT NULL
 AND p->>'command_id' IS NULL AND p->>'expected_version' IS NULL AND p->>'staff_version' IS NULL AND p->>'employment_epoch' IS NULL AND p->>'input_hash' IS NULL AND p->>'sponsor_id' IS NULL)
 OR (p->>'operation'='save' AND substring(p->>'period_id',15,1)='7' AND (p->>'period_id')::uuid IS NOT NULL
 AND substring(p->>'command_id',15,1)='7' AND (p->>'command_id')::uuid IS NOT NULL
 AND (p->>'expected_version')::bigint BETWEEN 0 AND 9007199254740989
 AND (p->>'staff_version')::bigint BETWEEN 1 AND 9007199254740990 AND (p->>'employment_epoch')::bigint BETWEEN 1 AND 9007199254740990
 AND p->>'input_hash' ~ '^[a-f0-9]{64}$'
 AND (p->>'sponsor_id' IS NULL OR (p->>'sponsor_id')::uuid NOT IN(s,'00000000-0000-0000-0000-000000000000'::uuid))))
 AND p->>'review_id' IS NULL AND p->>'actor_staff_id' IS NULL AND p->>'actor_link_id' IS NULL
 AND (p->>'membership_epoch')::bigint BETWEEN 1 AND 9007199254740991
 AND (p->>'owner_count')::integer BETWEEN 1 AND 1000
 AND (p->>'source_expires_at')::timestamptz>clock_timestamp()
 AND (p->>'idle_deadline')::timestamptz>clock_timestamp()
 AND (p->>'authenticated_at')::timestamptz<=clock_timestamp() AND p->>'authenticated_at' IS NOT NULL
 AND p->>'capability'='workforce.employment.prepare'
 AND ((p->>'authority_kind'='owner' AND (p->>'owner_membership_id')::uuid<>'00000000-0000-0000-0000-000000000000'::uuid
 AND p->>'source_grant_id' IS NULL AND p->>'source_grant_version' IS NULL AND p->>'template_key' IS NULL)
 OR (p->>'authority_kind'='grant' AND p->>'owner_membership_id' IS NULL
 AND (p->>'source_grant_id')::uuid<>'00000000-0000-0000-0000-000000000000'::uuid
 AND (p->>'source_grant_version')::bigint BETWEEN 1 AND 9007199254740991
 AND p->>'template_key'='workforce.employment_preparer.v1')),false)
 FROM(SELECT NULLIF(current_setting('app.workforce_governance_draft_authority',true),'')::jsonb AS p) proof
$$;

CREATE FUNCTION workforce_governance_draft_write(t uuid,h uuid,s uuid,period uuid,revision uuid,v bigint,sv bigint,epoch bigint,state text,actor text,digest text)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT workforce_governance_draft_source(t,h,s) AND p->>'operation'='save'
 AND (p->>'period_id')::uuid=period AND (p->>'command_id')::uuid=revision
 AND v=(p->>'expected_version')::bigint+1 AND sv=(p->>'staff_version')::bigint AND epoch=(p->>'employment_epoch')::bigint
 AND state='draft' AND actor=p->>'actor_subject' AND digest=p->>'input_hash'
 FROM(SELECT NULLIF(current_setting('app.workforce_governance_draft_authority',true),'')::jsonb AS p) proof
$$;

DO $guard$
DECLARE retained text; needle text; replacement text;
BEGIN
 retained:=pg_get_functiondef('public.workforce_employment_period_guard()'::regprocedure);
 needle:='workforce_enrollment_allowed(NEW.tenant_id,NEW.hospital_id) IS NOT TRUE';
 replacement:='(workforce_enrollment_allowed(NEW.tenant_id,NEW.hospital_id) OR workforce_governance_draft_write(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.id,NEW.revision_id,NEW.version,NEW.staff_version,NEW.employment_epoch,NEW.status,NEW.actor_subject,NEW.input_hash)) IS NOT TRUE';
 IF (length(retained)-length(replace(retained,needle,'')))/length(needle)<>1
 OR strpos(retained,'withdrawn employment draft is terminal')=0
 OR strpos(retained,'workforce_employment_period_sponsor')=0 THEN RAISE EXCEPTION 'governance preparation requires the exact retained period guard';END IF;
 EXECUTE replace(retained,needle,replacement);
END $guard$;

CREATE FUNCTION workforce_governance_draft_provenance() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF COALESCE(current_setting('app.workforce_governance_draft_authority',true),'')='' THEN RETURN NULL;END IF;
 IF NOT workforce_governance_draft_write(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.id,NEW.revision_id,NEW.version,NEW.staff_version,NEW.employment_epoch,NEW.status,NEW.actor_subject,NEW.input_hash)
 OR pg_trigger_depth()<>2 THEN RAISE EXCEPTION 'invalid private employment revision source' USING ERRCODE='42501';END IF;
 INSERT INTO workforce_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes)
 VALUES(NEW.tenant_id,NEW.hospital_id,NEW.actor_subject,'workforce.employment_period.governance_revision',NEW.revision_id,
 jsonb_build_object('authority',NULLIF(current_setting('app.workforce_governance_draft_authority',true),'')::jsonb,'revision_id',NEW.revision_id,'version',NEW.version));
 RETURN NULL;
END $$;
CREATE TRIGGER workforce_governance_draft_provenance AFTER INSERT ON workforce_employment_period_history
 FOR EACH ROW EXECUTE FUNCTION workforce_governance_draft_provenance();

DO $policies$
DECLARE item record;prior record;reading text;writing text;selected_staff text;
BEGIN
 selected_staff:='NULLIF(current_setting(''app.workforce_identity_staff'',true),'''')::uuid';
 FOR item IN SELECT c.relname AS table_name,c.relrowsecurity,c.relforcerowsecurity FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname='public' AND c.relkind IN('r','p') AND starts_with(c.relname,'workforce_')
 AND EXISTS(SELECT 1 FROM pg_attribute a WHERE a.attrelid=c.oid AND a.attname='tenant_id' AND NOT a.attisdropped) ORDER BY c.relname LOOP
  IF NOT item.relrowsecurity OR NOT item.relforcerowsecurity THEN RAISE EXCEPTION 'governance draft requires forced tenant RLS on %',item.table_name;END IF;
  reading:='false';writing:='false';
  CASE item.table_name
   WHEN 'workforce_hospitals' THEN reading:=format('workforce_governance_draft_source(tenant_id,id,%s)',selected_staff);
   WHEN 'workforce_staff' THEN reading:=format($read$workforce_governance_draft_source(tenant_id,hospital_id,id)
    OR (employment_status='active' AND id=(NULLIF(current_setting('app.workforce_governance_draft_authority',true),'')::jsonb->>'sponsor_id')::uuid
    AND workforce_governance_draft_source(tenant_id,hospital_id,%s))$read$,selected_staff);
   WHEN 'workforce_employment_period_drafts','workforce_employment_period_history' THEN
    reading:=$read$workforce_governance_draft_source(tenant_id,hospital_id,staff_id)
     AND (((NULLIF(current_setting('app.workforce_governance_draft_authority',true),'')::jsonb)->>'operation'='list' AND status='draft')
     OR ((NULLIF(current_setting('app.workforce_governance_draft_authority',true),'')::jsonb)->>'operation' IN('read','history','save')
     AND id=((NULLIF(current_setting('app.workforce_governance_draft_authority',true),'')::jsonb)->>'period_id')::uuid))$read$;
    IF item.table_name='workforce_employment_period_history' THEN
     reading:=reading||$read$ AND ((NULLIF(current_setting('app.workforce_governance_draft_authority',true),'')::jsonb)->>'operation'<>'save'
      OR revision_id=((NULLIF(current_setting('app.workforce_governance_draft_authority',true),'')::jsonb)->>'command_id')::uuid)$read$;
    END IF;
    writing:='workforce_governance_draft_write(tenant_id,hospital_id,staff_id,id,revision_id,version,staff_version,employment_epoch,status,actor_subject,input_hash)';
   WHEN 'workforce_audit' THEN
    reading:=format($read$workforce_governance_draft_source(tenant_id,hospital_id,%s)
     AND (NULLIF(current_setting('app.workforce_governance_draft_authority',true),'')::jsonb)->>'operation'='save'
     AND action='workforce.employment_period.governance_revision'
     AND resource_id=((NULLIF(current_setting('app.workforce_governance_draft_authority',true),'')::jsonb)->>'command_id')::uuid
     AND changes->'authority'->>'staff_id'=%s::text
     AND changes->'authority'->>'period_id'=(NULLIF(current_setting('app.workforce_governance_draft_authority',true),'')::jsonb)->>'period_id'$read$,selected_staff,selected_staff);
    writing:=format($write$workforce_governance_draft_source(tenant_id,hospital_id,%s)
     AND actor_subject=NULLIF(current_setting('app.actor_subject',true),'')
     AND changes->'authority'=NULLIF(current_setting('app.workforce_governance_draft_authority',true),'')::jsonb
     AND((action='workforce.employment_period.governance_revision' AND pg_trigger_depth()=2
     AND changes->'authority'->>'operation'='save' AND resource_id=(changes->'authority'->>'command_id')::uuid
     AND changes=jsonb_build_object('authority',changes->'authority','revision_id',resource_id,'version',(changes->'authority'->>'expected_version')::bigint+1))
     OR(changes=jsonb_build_object('authority',changes->'authority','version',(changes->>'version')::bigint)
     AND((changes->'authority'->>'operation'='source' AND action='workforce.employment_source.read' AND resource_id=%s AND (changes->>'version')::bigint BETWEEN 1 AND 9007199254740990)
     OR(changes->'authority'->>'operation'='list' AND action='workforce.employment_period.list' AND resource_id=%s AND (changes->>'version')::bigint=0)
     OR(changes->'authority'->>'operation' IN('read','history') AND action=CASE changes->'authority'->>'operation' WHEN 'read' THEN 'workforce.employment_period.read' ELSE 'workforce.employment_period.history.read' END AND resource_id=(changes->'authority'->>'period_id')::uuid AND (changes->>'version')::bigint BETWEEN 1 AND 9007199254740990)
     OR(changes->'authority'->>'operation'='save' AND action IN('workforce.employment_period.draft.saved','workforce.employment_period.receipt.read') AND resource_id=(changes->'authority'->>'period_id')::uuid AND (changes->>'version')::bigint=(changes->'authority'->>'expected_version')::bigint+1))))$write$,selected_staff,selected_staff,selected_staff);
   ELSE NULL;
  END CASE;
  IF reading<>'false' OR writing<>'false' THEN
   SELECT p.polname,p.polcmd,pg_get_expr(p.polqual,p.polrelid) AS reading,pg_get_expr(p.polwithcheck,p.polrelid) AS writing
    INTO STRICT prior FROM pg_policy p WHERE p.polrelid=to_regclass('public.'||item.table_name) AND p.polpermissive;
   IF prior.polcmd<>'*' OR prior.reading IS NULL OR prior.writing IS NULL THEN RAISE EXCEPTION 'governance draft requires one explicit tenant policy on %',item.table_name;END IF;
   IF reading<>'false' THEN EXECUTE format('ALTER POLICY %I ON %I USING ((%s) OR (%s))',prior.polname,item.table_name,prior.reading,reading);END IF;
   IF writing<>'false' THEN EXECUTE format('ALTER POLICY %I ON %I WITH CHECK ((%s) OR (%s))',prior.polname,item.table_name,prior.writing,writing);END IF;
  END IF;
  EXECUTE format($policy$CREATE POLICY governance_draft_purpose ON %I AS RESTRICTIVE FOR ALL
   USING(hms_tenant_visible(tenant_id) AND (COALESCE(current_setting('app.workforce_governance_draft_authority',true),'')='' OR (%s)))
   WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND (COALESCE(current_setting('app.workforce_governance_draft_authority',true),'')='' OR (%s)))$policy$,item.table_name,reading,writing);
  EXECUTE format($policy$CREATE POLICY governance_draft_delete ON %I AS RESTRICTIVE FOR DELETE
   USING(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND COALESCE(current_setting('app.workforce_governance_draft_authority',true),'')='')$policy$,item.table_name);
 END LOOP;
END $policies$;
