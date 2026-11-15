SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

ALTER TABLE workforce_employment_reviews ADD COLUMN governance_authority jsonb;
ALTER TABLE workforce_employment_review_history ADD COLUMN governance_authority jsonb;

CREATE FUNCTION workforce_employment_governance_source(t uuid,h uuid,s uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(hms_tenant_visible(t)
 AND t=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND h=NULLIF(current_setting('app.workforce_hospital',true),'')::uuid
 AND s=NULLIF(current_setting('app.workforce_identity_staff',true),'')::uuid
 AND p->>'purpose'='employment_review'
 AND p->>'tenant_id'=t::text AND p->>'hospital_id'=h::text AND p->>'staff_id'=s::text
 AND p->>'review_id'=NULLIF(current_setting('app.workforce_employment_review_id',true),'')
 AND (p->>'review_id')::uuid<>'00000000-0000-0000-0000-000000000000'::uuid
 AND p->>'actor_subject'=NULLIF(current_setting('app.workforce_subject',true),'')
 AND p->>'actor_subject'=NULLIF(current_setting('app.actor_subject',true),'')
 AND (p->>'membership_epoch')::bigint BETWEEN 1 AND 9007199254740991
 AND (p->>'owner_count')::integer BETWEEN 1 AND 1000
 AND (p->>'source_expires_at')::timestamptz>clock_timestamp()
 AND (p->>'idle_deadline')::timestamptz>clock_timestamp()
 AND (p->>'authenticated_at')::timestamptz<=clock_timestamp()
 AND p->>'authenticated_at' IS NOT NULL
 AND ((p->>'capability'='workforce.employment.prepare' AND current_setting('app.workforce_employment_review',true)='manage')
 OR (p->>'capability'='workforce.employment.review' AND current_setting('app.workforce_employment_review',true)='review'))
 AND ((p->>'authority_kind'='owner' AND p->>'owner_membership_id' IS NOT NULL
 AND (p->>'owner_membership_id')::uuid<>'00000000-0000-0000-0000-000000000000'::uuid
 AND p->>'source_grant_id' IS NULL AND p->>'source_grant_version' IS NULL AND p->>'template_key' IS NULL)
 OR (p->>'authority_kind'='grant' AND p->>'owner_membership_id' IS NULL
 AND (p->>'source_grant_id')::uuid<>'00000000-0000-0000-0000-000000000000'::uuid
 AND (p->>'source_grant_version')::bigint BETWEEN 1 AND 9007199254740991
 AND p->>'template_key'=CASE p->>'capability' WHEN 'workforce.employment.prepare' THEN 'workforce.employment_preparer.v1' WHEN 'workforce.employment.review' THEN 'workforce.employment_reviewer.v1' END)),false)
 FROM(SELECT NULLIF(current_setting('app.workforce_employment_authority',true),'')::jsonb AS p) proof
$$;

CREATE FUNCTION workforce_employment_governance_authority(t uuid,h uuid,s uuid,r uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT workforce_employment_governance_source(t,h,s)
 AND COALESCE(r::text=NULLIF(current_setting('app.workforce_employment_authority',true),'')::jsonb->>'review_id',false)
$$;

CREATE FUNCTION workforce_employment_governance_prepare(t uuid,h uuid,s uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT workforce_employment_governance_source(t,h,s)
 AND COALESCE(NULLIF(current_setting('app.workforce_employment_authority',true),'')::jsonb->>'capability'='workforce.employment.prepare',false)
$$;

CREATE OR REPLACE FUNCTION workforce_employment_review_context(row_tenant uuid,row_hospital uuid,row_staff uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT workforce_identity_source_context(row_tenant,row_hospital,row_staff)
 AND NULLIF(current_setting('app.workforce_employment_review_id',true),'') IS NOT NULL
 AND (workforce_employment_governance_source(row_tenant,row_hospital,row_staff)
 OR (COALESCE(current_setting('app.workforce_employment_authority',true),'')=''
 AND ((workforce_hr_intake_context(row_tenant,row_hospital) AND current_setting('app.workforce_employment_review',true)='manage')
 OR (current_setting('app.workforce_employment_review',true)='review'
 AND NULLIF(current_setting('app.workforce_employment_review_actor',true),'') IS NOT NULL
 AND NULLIF(current_setting('app.workforce_employment_review_link',true),'') IS NOT NULL))))
$$;

CREATE FUNCTION workforce_employment_governance_file(t uuid,h uuid,f uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT (EXISTS(SELECT 1 FROM workforce_documents d WHERE (d.tenant_id,d.hospital_id,d.id)=(t,h,f)
 AND workforce_employment_governance_prepare(d.tenant_id,d.hospital_id,d.staff_id)
 AND d.record_kind='enrollment' AND d.record_id=d.staff_id AND d.category IN('identity_evidence','employment_evidence'))
 OR (workforce_employment_governance_source(t,h,NULLIF(current_setting('app.workforce_identity_staff',true),'')::uuid)
 AND current_setting('app.workforce_employment_review',true)='review'
 AND workforce_employment_review_file_context(t,h,f)))
$$;

CREATE FUNCTION workforce_employment_review_governance_record() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE proof jsonb:=NULLIF(current_setting('app.workforce_employment_authority',true),'')::jsonb;
BEGIN
 IF proof IS NOT NULL AND NOT workforce_employment_governance_authority(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.id) THEN
  RAISE EXCEPTION 'employment review requires exact current nonclinical authority' USING ERRCODE='42501';
 END IF;
 -- The immutable-source guard runs first; this owner-maintained proof follows it and precedes the history snapshot.
 NEW.governance_authority:=proof;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_employment_review_zz_authority BEFORE INSERT OR UPDATE ON workforce_employment_reviews
 FOR EACH ROW EXECUTE FUNCTION workforce_employment_review_governance_record();

DO $migration$
DECLARE item record; prior record; reading text; writing text; selected_staff text;
BEGIN
 selected_staff:='NULLIF(current_setting(''app.workforce_identity_staff'',true),'''')::uuid';
 FOR item IN SELECT c.relname AS table_name,c.relrowsecurity,c.relforcerowsecurity FROM pg_class c
 JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname='public' AND c.relkind IN('r','p')
 AND starts_with(c.relname,'workforce_')
 AND EXISTS(SELECT 1 FROM pg_attribute a WHERE a.attrelid=c.oid AND a.attname='tenant_id' AND NOT a.attisdropped)
 ORDER BY c.relname LOOP
  IF NOT item.relrowsecurity OR NOT item.relforcerowsecurity THEN RAISE EXCEPTION 'employment purpose requires forced tenant RLS on %',item.table_name; END IF;
  reading:='false'; writing:='false';
  CASE item.table_name
   WHEN 'workforce_hospitals' THEN
    reading:=format('workforce_employment_governance_source(tenant_id,id,%s)',selected_staff);
   WHEN 'workforce_staff' THEN
    reading:=$read$workforce_employment_governance_source(tenant_id,hospital_id,id)
     OR EXISTS(SELECT 1 FROM workforce_employment_period_drafts p WHERE (p.tenant_id,p.hospital_id,p.sponsor_id)=(workforce_staff.tenant_id,workforce_staff.hospital_id,workforce_staff.id) AND workforce_employment_governance_prepare(p.tenant_id,p.hospital_id,p.staff_id))
     OR EXISTS(SELECT 1 FROM workforce_employment_reviews r JOIN workforce_employment_period_history p ON (p.tenant_id,p.hospital_id,p.staff_id,p.id,p.version,p.revision_id)=(r.tenant_id,r.hospital_id,r.staff_id,r.period_id,r.period_version,r.period_revision_id)
     WHERE (r.tenant_id,r.hospital_id,p.sponsor_id)=(workforce_staff.tenant_id,workforce_staff.hospital_id,workforce_staff.id) AND workforce_employment_governance_authority(r.tenant_id,r.hospital_id,r.staff_id,r.id))$read$;
   WHEN 'workforce_enrollment_profile','workforce_login_links','workforce_identity_epochs','workforce_employment_review_cohorts' THEN
    reading:='workforce_employment_governance_source(tenant_id,hospital_id,staff_id)';
    IF item.table_name='workforce_employment_review_cohorts' THEN
     writing:='workforce_employment_governance_prepare(tenant_id,hospital_id,staff_id) AND current_setting(''app.workforce_generation'',true)=''cohort'' AND pg_trigger_depth()>0';
    END IF;
   WHEN 'workforce_employment_reviews','workforce_employment_review_history' THEN
    reading:='workforce_employment_governance_authority(tenant_id,hospital_id,staff_id,id)';
    writing:=reading;
   WHEN 'workforce_employment_review_evidence','workforce_employment_review_contributors' THEN
    reading:='workforce_employment_governance_authority(tenant_id,hospital_id,staff_id,review_id)';
    writing:=reading;
   WHEN 'workforce_employment_period_drafts','workforce_employment_period_history' THEN
    reading:=format($read$workforce_employment_governance_prepare(tenant_id,hospital_id,staff_id)
     OR (workforce_employment_governance_source(tenant_id,hospital_id,staff_id) AND EXISTS(SELECT 1 FROM workforce_employment_reviews r
     WHERE (r.tenant_id,r.hospital_id,r.staff_id,r.period_id)=(%1$I.tenant_id,%1$I.hospital_id,%1$I.staff_id,%1$I.id) %2$s))$read$,
     item.table_name,CASE WHEN item.table_name='workforce_employment_period_history' THEN format('AND %I.version<=r.period_version',item.table_name) ELSE '' END);
   WHEN 'workforce_employment_evidence_bindings','workforce_employment_evidence_history' THEN
    reading:=format($read$workforce_employment_governance_prepare(tenant_id,hospital_id,staff_id)
     OR (workforce_employment_governance_source(tenant_id,hospital_id,staff_id) AND EXISTS(SELECT 1 FROM workforce_employment_review_evidence e
     WHERE (e.tenant_id,e.hospital_id,e.staff_id,e.binding_id)=(%1$I.tenant_id,%1$I.hospital_id,%1$I.staff_id,%1$I.id) %2$s))$read$,
     item.table_name,CASE WHEN item.table_name='workforce_employment_evidence_history' THEN format('AND %I.version<=e.binding_version',item.table_name) ELSE '' END);
   WHEN 'workforce_person_reviews','workforce_person_review_history' THEN
    reading:='workforce_employment_person_context(tenant_id,hospital_id,staff_id,id)';
   WHEN 'workforce_person_review_evidence','workforce_person_review_contributors' THEN
    reading:='workforce_employment_person_context(tenant_id,hospital_id,staff_id,review_id)';
   WHEN 'workforce_documents' THEN
    reading:=$read$record_kind='enrollment' AND record_id=staff_id AND category IN('identity_evidence','employment_evidence')
     AND (workforce_employment_governance_prepare(tenant_id,hospital_id,staff_id)
     OR (workforce_employment_governance_source(tenant_id,hospital_id,staff_id) AND workforce_employment_review_file_context(tenant_id,hospital_id,id)))$read$;
   WHEN 'workforce_document_classifications' THEN
    reading:='workforce_employment_governance_file(tenant_id,hospital_id,id)';
   WHEN 'workforce_document_classification_revisions' THEN
    reading:='workforce_employment_governance_file(tenant_id,hospital_id,file_id)';
   WHEN 'workforce_document_contributors' THEN
    reading:='workforce_employment_governance_file(tenant_id,hospital_id,document_id)';
   WHEN 'workforce_audit' THEN
    reading:=format($read$workforce_employment_governance_source(tenant_id,hospital_id,%s)
     AND ((resource_id=%s AND action IN('workforce_staff.insert','workforce_staff.update','workforce.enrollment.changed'))
     OR (resource_id=NULLIF(current_setting('app.workforce_employment_review_id',true),'')::uuid
     AND action IN('workforce.employment_review.submitted','workforce.employment_review.receipt.read','workforce.employment_review.read','workforce.employment_review.history.read','workforce.employment_review.decided','workforce.employment_review.withdrawn','workforce.employment_review.revoked','workforce.employment_review.evidence.authorized','workforce.employment_review.evidence.downloaded')))$read$,selected_staff,selected_staff);
    writing:=format($write$workforce_employment_governance_source(tenant_id,hospital_id,%s)
     AND actor_subject=NULLIF(current_setting('app.actor_subject',true),'')
     AND resource_id=NULLIF(current_setting('app.workforce_employment_review_id',true),'')::uuid
     AND action IN('workforce.employment_review.submitted','workforce.employment_review.receipt.read','workforce.employment_review.read','workforce.employment_review.history.read','workforce.employment_review.decided','workforce.employment_review.withdrawn','workforce.employment_review.revoked','workforce.employment_review.evidence.authorized','workforce.employment_review.evidence.downloaded')
     AND changes @> NULLIF(current_setting('app.workforce_employment_authority',true),'')::jsonb$write$,selected_staff);
   ELSE NULL;
  END CASE;
  IF reading<>'false' THEN
   FOR prior IN SELECT p.polname,p.polcmd,pg_get_expr(p.polqual,p.polrelid) AS reading,pg_get_expr(p.polwithcheck,p.polrelid) AS writing
    FROM pg_policy p WHERE p.polrelid=('public.'||quote_ident(item.table_name))::regclass AND p.polpermissive AND p.polcmd IN('*','r') LOOP
    IF prior.polcmd='*' AND prior.writing IS NULL THEN RAISE EXCEPTION 'employment authority requires explicit unchanged write policies on %',item.table_name; END IF;
    EXECUTE format('ALTER POLICY %I ON %I USING ((%s) OR (%s))',prior.polname,item.table_name,prior.reading,reading);
   END LOOP;
  END IF;
  EXECUTE format($policy$CREATE POLICY employment_governance_purpose ON %I AS RESTRICTIVE FOR ALL
   USING(hms_tenant_visible(tenant_id) AND (COALESCE(current_setting('app.workforce_employment_authority',true),'')='' OR (%s)))
   WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND (COALESCE(current_setting('app.workforce_employment_authority',true),'')='' OR (%s)))$policy$,item.table_name,reading,writing);
  EXECUTE format($policy$CREATE POLICY employment_governance_delete ON %I AS RESTRICTIVE FOR DELETE
   USING(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND COALESCE(current_setting('app.workforce_employment_authority',true),'')='')$policy$,item.table_name);
 END LOOP;
END $migration$;
