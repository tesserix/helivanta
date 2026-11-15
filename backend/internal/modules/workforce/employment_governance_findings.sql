SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE FUNCTION workforce_governance_finding_source(t uuid,h uuid,s uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(hms_tenant_visible(t)
 AND t=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND h=NULLIF(current_setting('app.workforce_hospital',true),'')::uuid
 AND p->>'purpose'='employment_finding_selection' AND p->>'tenant_id'=t::text AND p->>'hospital_id'=h::text
 AND p->>'actor_subject'=NULLIF(current_setting('app.workforce_subject',true),'')
 AND p->>'actor_subject'=NULLIF(current_setting('app.actor_subject',true),'')
 AND p->>'staff_id'=s::text AND s=NULLIF(current_setting('app.workforce_identity_staff',true),'')::uuid
 AND (p->>'staff_version')::bigint BETWEEN 1 AND 9007199254740990 AND (p->>'employment_epoch')::bigint BETWEEN 1 AND 9007199254740990
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
 FROM(SELECT NULLIF(current_setting('app.workforce_governance_finding_authority',true),'')::jsonb AS p) proof
$$;

CREATE FUNCTION workforce_governance_finding_file(t uuid,h uuid,s uuid,f uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT workforce_governance_finding_source(t,h,s)
 AND EXISTS(SELECT 1 FROM workforce_person_review_evidence e WHERE (e.tenant_id,e.hospital_id,e.staff_id,e.document_id)=(t,h,s,f)
 AND e.review_id=NULLIF(current_setting('app.workforce_employment_person_review',true),'')::uuid)
$$;

DO $migration$
DECLARE item record;prior record;reading text;writing text;selected_staff text;
BEGIN
 selected_staff:='NULLIF(current_setting(''app.workforce_identity_staff'',true),'''')::uuid';
 FOR item IN SELECT c.relname AS table_name,c.relrowsecurity,c.relforcerowsecurity FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname='public' AND c.relkind IN('r','p') AND starts_with(c.relname,'workforce_')
 AND EXISTS(SELECT 1 FROM pg_attribute a WHERE a.attrelid=c.oid AND a.attname='tenant_id' AND NOT a.attisdropped) ORDER BY c.relname LOOP
  IF NOT item.relrowsecurity OR NOT item.relforcerowsecurity THEN RAISE EXCEPTION 'governance finding requires forced tenant RLS on %',item.table_name; END IF;
  reading:='false';writing:='false';
  CASE item.table_name
   WHEN 'workforce_staff' THEN reading:='workforce_governance_finding_source(tenant_id,hospital_id,id)';
   WHEN 'workforce_enrollment_profile' THEN reading:='workforce_governance_finding_source(tenant_id,hospital_id,staff_id)';
   WHEN 'workforce_person_reviews' THEN reading:=$read$workforce_governance_finding_source(tenant_id,hospital_id,staff_id)
    AND status='approved' AND valid_until>clock_timestamp()$read$;
   WHEN 'workforce_person_review_evidence' THEN reading:=$read$workforce_governance_finding_source(tenant_id,hospital_id,staff_id)
    AND review_id=NULLIF(current_setting('app.workforce_employment_person_review',true),'')::uuid$read$;
   WHEN 'workforce_documents' THEN reading:=$read$workforce_governance_finding_file(tenant_id,hospital_id,staff_id,id)
    AND record_kind='enrollment' AND record_id=staff_id AND category='identity_evidence'$read$;
   WHEN 'workforce_document_classifications' THEN reading:=format('workforce_governance_finding_file(tenant_id,hospital_id,%s,id)',selected_staff);
   WHEN 'workforce_document_contributors' THEN reading:='workforce_governance_finding_file(tenant_id,hospital_id,staff_id,document_id)';
   WHEN 'workforce_audit' THEN writing:=format($write$workforce_governance_finding_source(tenant_id,hospital_id,%s)
    AND actor_subject=NULLIF(current_setting('app.actor_subject',true),'') AND resource_id=%s
    AND action='workforce.employment_review.governance_person_selection.read'
    AND changes->'authority'=NULLIF(current_setting('app.workforce_governance_finding_authority',true),'')::jsonb
    AND (changes->>'rows')::integer BETWEEN 0 AND 20
    AND changes=jsonb_build_object('authority',changes->'authority','rows',(changes->>'rows')::integer)$write$,selected_staff,selected_staff);
   ELSE NULL;
  END CASE;
  IF reading<>'false' OR writing<>'false' THEN
   SELECT p.polname,p.polcmd,pg_get_expr(p.polqual,p.polrelid) AS reading,pg_get_expr(p.polwithcheck,p.polrelid) AS writing
    INTO STRICT prior FROM pg_policy p WHERE p.polrelid=to_regclass('public.'||item.table_name) AND p.polpermissive;
   IF prior.polcmd<>'*' OR prior.reading IS NULL OR prior.writing IS NULL THEN RAISE EXCEPTION 'governance finding requires one explicit tenant policy on %',item.table_name;END IF;
   IF reading<>'false' THEN EXECUTE format('ALTER POLICY %I ON %I USING ((%s) OR (%s))',prior.polname,item.table_name,prior.reading,reading);END IF;
   IF writing<>'false' THEN EXECUTE format('ALTER POLICY %I ON %I WITH CHECK ((%s) OR (%s))',prior.polname,item.table_name,prior.writing,writing);END IF;
  END IF;
  EXECUTE format($policy$CREATE POLICY governance_finding_purpose ON %I AS RESTRICTIVE FOR ALL
   USING(hms_tenant_visible(tenant_id) AND (COALESCE(current_setting('app.workforce_governance_finding_authority',true),'')='' OR (%s)))
   WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND (COALESCE(current_setting('app.workforce_governance_finding_authority',true),'')='' OR (%s)))$policy$,item.table_name,reading,writing);
  EXECUTE format($policy$CREATE POLICY governance_finding_delete ON %I AS RESTRICTIVE FOR DELETE
   USING(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND COALESCE(current_setting('app.workforce_governance_finding_authority',true),'')='')$policy$,item.table_name);
 END LOOP;
END $migration$;
