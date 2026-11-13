SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

ALTER TABLE workforce_employment_generations ADD COLUMN governance_authority jsonb;

CREATE FUNCTION workforce_activation_governance_source(t uuid,h uuid,s uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(hms_tenant_visible(t)
 AND t=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND h=NULLIF(current_setting('app.workforce_hospital',true),'')::uuid
 AND s=NULLIF(current_setting('app.workforce_identity_staff',true),'')::uuid
 AND COALESCE(current_setting('app.workforce_employment_authority',true),'')=''
 AND p->>'purpose'='employment_activation' AND p->>'operation' IN('source','activate')
 AND p->>'tenant_id'=t::text AND p->>'hospital_id'=h::text AND p->>'staff_id'=s::text
 AND p->>'review_id'=NULLIF(current_setting('app.workforce_employment_review_id',true),'')
 AND (p->>'review_id')::uuid<>'00000000-0000-0000-0000-000000000000'::uuid
 AND p->>'actor_subject'=NULLIF(current_setting('app.workforce_subject',true),'')
 AND p->>'actor_subject'=NULLIF(current_setting('app.actor_subject',true),'')
 AND p->>'capability'='workforce.employment.activate'
 AND (p->>'membership_epoch')::bigint BETWEEN 1 AND 9007199254740991
 AND (p->>'owner_count')::integer BETWEEN 1 AND 1000
 AND (p->>'source_expires_at')::timestamptz>clock_timestamp()
 AND (p->>'idle_deadline')::timestamptz>clock_timestamp()
 AND (p->>'authenticated_at')::timestamptz<=clock_timestamp() AND p->>'authenticated_at' IS NOT NULL
 AND ((p->>'authority_kind'='owner' AND (p->>'owner_membership_id')::uuid<>'00000000-0000-0000-0000-000000000000'::uuid
 AND p->>'source_grant_id' IS NULL AND p->>'source_grant_version' IS NULL AND p->>'template_key' IS NULL)
 OR (p->>'authority_kind'='grant' AND p->>'owner_membership_id' IS NULL
 AND (p->>'source_grant_id')::uuid<>'00000000-0000-0000-0000-000000000000'::uuid
 AND (p->>'source_grant_version')::bigint BETWEEN 1 AND 9007199254740991
 AND p->>'template_key'='workforce.employment_activator.v1')),false)
 FROM(SELECT NULLIF(current_setting('app.workforce_activation_authority',true),'')::jsonb AS p) proof
$$;

CREATE FUNCTION workforce_activation_governance_review(t uuid,h uuid,s uuid,r uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT workforce_activation_governance_source(t,h,s)
 AND COALESCE(r::text=NULLIF(current_setting('app.workforce_activation_authority',true),'')::jsonb->>'review_id',false)
$$;

CREATE FUNCTION workforce_activation_governance_write(t uuid,h uuid,s uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(workforce_activation_governance_source(t,h,s)
 AND p->>'operation'='activate'
 AND (p->>'generation_id')::uuid<>'00000000-0000-0000-0000-000000000000'::uuid
 AND (p->>'command_id')::uuid<>'00000000-0000-0000-0000-000000000000'::uuid
 AND (p->>'expected_staff_version')::bigint BETWEEN 1 AND 9007199254740989
 AND (p->>'expected_employment_epoch')::bigint BETWEEN 1 AND 9007199254740989,false)
 FROM(SELECT NULLIF(current_setting('app.workforce_activation_authority',true),'')::jsonb AS p) proof
$$;

CREATE OR REPLACE FUNCTION workforce_employment_review_context(row_tenant uuid,row_hospital uuid,row_staff uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT workforce_identity_source_context(row_tenant,row_hospital,row_staff)
 AND NULLIF(current_setting('app.workforce_employment_review_id',true),'') IS NOT NULL
 AND (workforce_employment_governance_source(row_tenant,row_hospital,row_staff)
 OR workforce_activation_governance_source(row_tenant,row_hospital,row_staff)
 OR (COALESCE(current_setting('app.workforce_employment_authority',true),'')=''
 AND COALESCE(current_setting('app.workforce_activation_authority',true),'')=''
 AND ((workforce_hr_intake_context(row_tenant,row_hospital) AND current_setting('app.workforce_employment_review',true)='manage')
 OR (current_setting('app.workforce_employment_review',true)='review'
 AND NULLIF(current_setting('app.workforce_employment_review_actor',true),'') IS NOT NULL
 AND NULLIF(current_setting('app.workforce_employment_review_link',true),'') IS NOT NULL))))
$$;

CREATE FUNCTION workforce_activation_governance_file(t uuid,h uuid,f uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT EXISTS(SELECT 1 FROM workforce_employment_reviews r
 WHERE r.tenant_id=t AND r.hospital_id=h AND workforce_activation_governance_review(r.tenant_id,r.hospital_id,r.staff_id,r.id)
 AND (EXISTS(SELECT 1 FROM workforce_employment_review_evidence e WHERE (e.tenant_id,e.hospital_id,e.staff_id,e.review_id,e.document_id)=(r.tenant_id,r.hospital_id,r.staff_id,r.id,f))
 OR EXISTS(SELECT 1 FROM workforce_person_review_evidence e WHERE (e.tenant_id,e.hospital_id,e.staff_id,e.review_id,e.document_id)=(r.tenant_id,r.hospital_id,r.staff_id,r.person_review_id,f))))
$$;

CREATE FUNCTION workforce_governance_staff_activation() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE proof jsonb:=NULLIF(current_setting('app.workforce_activation_authority',true),'')::jsonb;
BEGIN
 IF proof IS NULL THEN RETURN NEW; END IF;
 IF NOT workforce_activation_governance_write(NEW.tenant_id,NEW.hospital_id,NEW.id)
 OR OLD.employment_status<>'inactive' OR NEW.employment_status<>'active'
 OR OLD.version<>(proof->>'expected_staff_version')::bigint OR OLD.employment_epoch<>(proof->>'expected_employment_epoch')::bigint
 OR NEW.version<>OLD.version+1 OR NEW.employment_epoch<>OLD.employment_epoch+1
 OR (to_jsonb(NEW)-ARRAY['employment_status','version','employment_epoch','updated_at']) IS DISTINCT FROM (to_jsonb(OLD)-ARRAY['employment_status','version','employment_epoch','updated_at'])
 OR NOT EXISTS(SELECT 1 FROM workforce_employment_reviews r WHERE (r.tenant_id,r.hospital_id,r.staff_id,r.staff_version,r.employment_epoch,r.status)=(OLD.tenant_id,OLD.hospital_id,OLD.id,OLD.version,OLD.employment_epoch,'approved')
 AND workforce_activation_governance_review(r.tenant_id,r.hospital_id,r.staff_id,r.id)) THEN
  RAISE EXCEPTION 'governance activation permits only the exact approved employment transition' USING ERRCODE='42501',CONSTRAINT='workforce_governance_activation_transition';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER zz_workforce_governance_activation BEFORE UPDATE ON workforce_staff
 FOR EACH ROW EXECUTE FUNCTION workforce_governance_staff_activation();

CREATE FUNCTION workforce_employment_generation_governance_record() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE proof jsonb:=NULLIF(current_setting('app.workforce_activation_authority',true),'')::jsonb;
BEGIN
 IF proof IS NOT NULL AND (NOT workforce_activation_governance_write(NEW.tenant_id,NEW.hospital_id,NEW.staff_id)
 OR NEW.id::text IS DISTINCT FROM proof->>'generation_id' OR NEW.revision_id::text IS DISTINCT FROM proof->>'command_id'
 OR NEW.review_id::text IS DISTINCT FROM proof->>'review_id' OR NEW.activated_by_subject IS DISTINCT FROM proof->>'actor_subject') THEN
  RAISE EXCEPTION 'employment generation requires its exact current governance source' USING ERRCODE='42501';
 END IF;
 NEW.governance_authority:=proof;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_employment_generation_zz_authority BEFORE INSERT ON workforce_employment_generations
 FOR EACH ROW EXECUTE FUNCTION workforce_employment_generation_governance_record();

DO $migration$
DECLARE item record; prior record; reading text; writing text; target text;
BEGIN
 target:='NULLIF(current_setting(''app.workforce_identity_staff'',true),'''')::uuid';
 FOR item IN SELECT c.relname AS table_name,c.relrowsecurity,c.relforcerowsecurity FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname='public' AND c.relkind IN('r','p') AND starts_with(c.relname,'workforce_')
 AND EXISTS(SELECT 1 FROM pg_attribute a WHERE a.attrelid=c.oid AND a.attname='tenant_id' AND NOT a.attisdropped) ORDER BY c.relname LOOP
  IF NOT item.relrowsecurity OR NOT item.relforcerowsecurity THEN RAISE EXCEPTION 'activation purpose requires forced tenant RLS on %',item.table_name; END IF;
  reading:='false'; writing:='false';
  CASE item.table_name
   WHEN 'workforce_hospitals' THEN reading:=format('workforce_activation_governance_source(tenant_id,id,%s)',target);
   WHEN 'workforce_staff' THEN
    reading:=$read$workforce_activation_governance_source(tenant_id,hospital_id,id)
     OR EXISTS(SELECT 1 FROM workforce_employment_reviews r JOIN workforce_employment_period_history p ON (p.tenant_id,p.hospital_id,p.staff_id,p.id,p.version,p.revision_id)=(r.tenant_id,r.hospital_id,r.staff_id,r.period_id,r.period_version,r.period_revision_id)
     WHERE (r.tenant_id,r.hospital_id,p.sponsor_id)=(workforce_staff.tenant_id,workforce_staff.hospital_id,workforce_staff.id) AND workforce_activation_governance_review(r.tenant_id,r.hospital_id,r.staff_id,r.id))$read$;
    writing:='workforce_activation_governance_write(tenant_id,hospital_id,id)';
   WHEN 'workforce_enrollment_profile','workforce_login_links','workforce_identity_epochs','workforce_employment_review_cohorts' THEN
    reading:='workforce_activation_governance_source(tenant_id,hospital_id,staff_id)';
   WHEN 'workforce_employment_generations' THEN
    reading:='workforce_activation_governance_review(tenant_id,hospital_id,staff_id,review_id)';
    writing:='workforce_activation_governance_write(tenant_id,hospital_id,staff_id) AND id::text=NULLIF(current_setting(''app.workforce_activation_authority'',true),'''')::jsonb->>''generation_id''';
   WHEN 'workforce_employment_reviews','workforce_employment_review_history' THEN
    reading:='workforce_activation_governance_review(tenant_id,hospital_id,staff_id,id)';
   WHEN 'workforce_employment_review_evidence','workforce_employment_review_contributors' THEN
    reading:='workforce_activation_governance_review(tenant_id,hospital_id,staff_id,review_id)';
   WHEN 'workforce_employment_period_drafts','workforce_employment_period_history' THEN
    reading:=format($read$EXISTS(SELECT 1 FROM workforce_employment_reviews r WHERE (r.tenant_id,r.hospital_id,r.staff_id,r.period_id)=(%1$I.tenant_id,%1$I.hospital_id,%1$I.staff_id,%1$I.id) AND workforce_activation_governance_review(r.tenant_id,r.hospital_id,r.staff_id,r.id) %2$s)$read$,item.table_name,CASE WHEN item.table_name='workforce_employment_period_history' THEN format('AND %I.version<=r.period_version',item.table_name) ELSE '' END);
   WHEN 'workforce_employment_evidence_bindings','workforce_employment_evidence_history' THEN
    reading:=format($read$EXISTS(SELECT 1 FROM workforce_employment_review_evidence e WHERE (e.tenant_id,e.hospital_id,e.staff_id,e.binding_id)=(%1$I.tenant_id,%1$I.hospital_id,%1$I.staff_id,%1$I.id) AND workforce_activation_governance_review(e.tenant_id,e.hospital_id,e.staff_id,e.review_id) %2$s)$read$,item.table_name,CASE WHEN item.table_name='workforce_employment_evidence_history' THEN format('AND %I.version<=e.binding_version',item.table_name) ELSE '' END);
   WHEN 'workforce_person_reviews','workforce_person_review_history' THEN reading:='workforce_employment_person_context(tenant_id,hospital_id,staff_id,id)';
   WHEN 'workforce_person_review_evidence','workforce_person_review_contributors' THEN reading:='workforce_employment_person_context(tenant_id,hospital_id,staff_id,review_id)';
   WHEN 'workforce_documents' THEN reading:='workforce_activation_governance_source(tenant_id,hospital_id,staff_id) AND workforce_activation_governance_file(tenant_id,hospital_id,id)';
   WHEN 'workforce_document_classifications' THEN reading:='workforce_activation_governance_file(tenant_id,hospital_id,id)';
   WHEN 'workforce_document_classification_revisions' THEN reading:='workforce_activation_governance_file(tenant_id,hospital_id,file_id)';
   WHEN 'workforce_document_contributors' THEN reading:='workforce_activation_governance_file(tenant_id,hospital_id,document_id)';
   WHEN 'workforce_audit' THEN
    reading:=format('workforce_activation_governance_source(tenant_id,hospital_id,%s) AND resource_id IN(%s,NULLIF(current_setting(''app.workforce_employment_review_id'',true),'''')::uuid)',target,target);
    writing:=format($write$workforce_activation_governance_source(tenant_id,hospital_id,%s)
     AND actor_subject=NULLIF(current_setting('app.actor_subject',true),'')
     AND ((changes @> NULLIF(current_setting('app.workforce_activation_authority',true),'')::jsonb
      AND ((action='workforce.employment_activation.source.read' AND resource_id=NULLIF(current_setting('app.workforce_employment_review_id',true),'')::uuid)
      OR (action IN('workforce.employment.activated','workforce.employment_activation.receipt.read') AND resource_id::text=NULLIF(current_setting('app.workforce_activation_authority',true),'')::jsonb->>'generation_id')))
     OR (workforce_activation_governance_write(tenant_id,hospital_id,resource_id)
      AND action='workforce_staff.update' AND resource_id=%s
      AND changes=jsonb_build_object('old_level',NULL,'new_level',NULL,'old_status','inactive','new_status','active')))$write$,target,target);
   ELSE NULL;
  END CASE;
  IF reading<>'false' THEN
   FOR prior IN SELECT p.polname,p.polcmd,pg_get_expr(p.polqual,p.polrelid) AS reading,pg_get_expr(p.polwithcheck,p.polrelid) AS writing FROM pg_policy p
    WHERE p.polrelid=('public.'||quote_ident(item.table_name))::regclass AND p.polpermissive AND p.polcmd IN('*','r') LOOP
    IF prior.polcmd='*' AND prior.writing IS NULL THEN RAISE EXCEPTION 'activation requires explicit unchanged write policy on %',item.table_name; END IF;
    IF writing<>'false' AND prior.polcmd='*' THEN
     EXECUTE format('ALTER POLICY %I ON %I USING ((%s) OR (%s)) WITH CHECK ((%s) OR (%s))',prior.polname,item.table_name,prior.reading,reading,prior.writing,writing);
    ELSE EXECUTE format('ALTER POLICY %I ON %I USING ((%s) OR (%s))',prior.polname,item.table_name,prior.reading,reading); END IF;
   END LOOP;
  END IF;
  IF item.table_name='workforce_staff' THEN
   SELECT pg_get_expr(p.polqual,p.polrelid) AS reading,pg_get_expr(p.polwithcheck,p.polrelid) AS writing INTO STRICT prior
    FROM pg_policy p WHERE p.polrelid='public.workforce_staff'::regclass AND p.polname='partner_candidate_update_guard' AND NOT p.polpermissive AND p.polcmd='w';
   IF prior.reading IS NULL OR prior.writing IS NULL THEN RAISE EXCEPTION 'activation requires the retained explicit Staff update guard'; END IF;
   EXECUTE format('ALTER POLICY partner_candidate_update_guard ON workforce_staff USING ((%s) OR (%s)) WITH CHECK ((%s) OR (%s))',prior.reading,reading,prior.writing,writing);
  END IF;
  EXECUTE format($policy$CREATE POLICY activation_governance_purpose ON %I AS RESTRICTIVE FOR ALL
   USING(hms_tenant_visible(tenant_id) AND (COALESCE(current_setting('app.workforce_activation_authority',true),'')='' OR (%s)))
   WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND (COALESCE(current_setting('app.workforce_activation_authority',true),'')='' OR (%s)))$policy$,item.table_name,reading,writing);
  EXECUTE format($policy$CREATE POLICY activation_governance_delete ON %I AS RESTRICTIVE FOR DELETE
   USING(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND COALESCE(current_setting('app.workforce_activation_authority',true),'')='')$policy$,item.table_name);
 END LOOP;
END $migration$;
