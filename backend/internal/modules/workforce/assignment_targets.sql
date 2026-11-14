SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

DO $prerequisites$
BEGIN
 IF to_regprocedure('public.workforce_operating_selection_integrity()')IS NULL THEN
  RAISE EXCEPTION 'assignment target selection requires the complete prior authority schema';
 END IF;
 IF workforce_operating_selection_integrity()IS DISTINCT FROM true THEN
  RAISE EXCEPTION 'assignment target selection requires unchanged prior authority definitions';
 END IF;
END $prerequisites$;

CREATE FUNCTION workforce_assignment_selection_context(t uuid,h uuid)RETURNS boolean LANGUAGE plpgsql STABLE SECURITY INVOKER AS $context$
DECLARE raw text:=NULLIF(current_setting('app.workforce_assignment_selection',true),'');p jsonb;
BEGIN
 IF raw IS NULL THEN RETURN false;END IF;
 p:=raw::jsonb;
 RETURN COALESCE(jsonb_typeof(p)='object'AND hms_tenant_visible(t)
 AND t=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND h=NULLIF(current_setting('app.workforce_hospital',true),'')::uuid
 AND p->>'purpose'='assignment_target_choices'AND p->>'capability'='iam.governance.assignment.manage'
 AND p->>'tenant_id'=t::text AND p->>'hospital_id'=h::text
 AND p->>'actor_subject'=NULLIF(current_setting('app.actor_subject',true),'')
 AND p->>'actor_subject'=NULLIF(current_setting('app.workforce_subject',true),'')
 AND(p->>'membership_epoch')::bigint BETWEEN 1 AND 9007199254740991
 AND(p->>'owner_count')::bigint BETWEEN 1 AND 1000
 AND(p->>'authenticated_at')::timestamptz<=clock_timestamp()
 AND(p->>'idle_deadline')::timestamptz>clock_timestamp()
 AND(p->>'source_expires_at')::timestamptz>clock_timestamp()
 AND(p->>'read_expires_at')::timestamptz>clock_timestamp()
 AND(p->>'read_expires_at')::timestamptz<=LEAST((p->>'idle_deadline')::timestamptz,(p->>'source_expires_at')::timestamptz)
 AND(p->>'read_expires_at')::timestamptz<=clock_timestamp()+interval '5 seconds'
 AND((p->>'authority_kind'='owner'AND(p->>'owner_membership_id')::uuid<>'00000000-0000-0000-0000-000000000000'::uuid
 AND p->>'source_grant_id'IS NULL AND p->>'source_grant_version'IS NULL AND p->>'template_key'IS NULL)
 OR(p->>'authority_kind'='grant'AND(p->>'source_grant_id')::uuid<>'00000000-0000-0000-0000-000000000000'::uuid
 AND(p->>'source_grant_version')::bigint BETWEEN 1 AND 9007199254740991
 AND p->>'template_key'='iam.scoped_access_reviewer.v1'AND p->>'owner_membership_id'IS NULL)),false);
EXCEPTION WHEN invalid_text_representation OR invalid_datetime_format OR datetime_field_overflow OR numeric_value_out_of_range THEN RETURN false;
END $context$;

CREATE FUNCTION workforce_assignment_selection_review(t uuid,h uuid)RETURNS uuid LANGUAGE sql STABLE SECURITY INVOKER AS $$
 SELECT review_id FROM workforce_employment_generations
 WHERE tenant_id=t AND hospital_id=h
 AND staff_id=NULLIF(current_setting('app.workforce_assignment_target_staff',true),'')::uuid
 AND id=NULLIF(current_setting('app.workforce_assignment_target_generation',true),'')::uuid
$$;
CREATE FUNCTION workforce_assignment_selection_person(t uuid,h uuid)RETURNS uuid LANGUAGE sql STABLE SECURITY INVOKER AS $$
 SELECT person_review_id FROM workforce_employment_reviews
 WHERE tenant_id=t AND hospital_id=h AND id=workforce_assignment_selection_review(t,h)
 AND staff_id=NULLIF(current_setting('app.workforce_assignment_target_staff',true),'')::uuid
$$;
CREATE FUNCTION workforce_assignment_selection_file(t uuid,h uuid,f uuid)RETURNS boolean LANGUAGE sql STABLE SECURITY INVOKER AS $$
 SELECT EXISTS(SELECT 1 FROM workforce_employment_review_evidence e WHERE e.tenant_id=t AND e.hospital_id=h
 AND e.staff_id=NULLIF(current_setting('app.workforce_assignment_target_staff',true),'')::uuid
 AND e.review_id=workforce_assignment_selection_review(t,h)AND e.document_id=f)
 OR EXISTS(SELECT 1 FROM workforce_person_review_evidence e WHERE e.tenant_id=t AND e.hospital_id=h
 AND e.staff_id=NULLIF(current_setting('app.workforce_assignment_target_staff',true),'')::uuid
 AND e.review_id=workforce_assignment_selection_person(t,h)AND e.document_id=f)
$$;

CREATE FUNCTION workforce_assignment_selection_read(t uuid,h uuid,relation text,row_data jsonb)RETURNS boolean LANGUAGE plpgsql STABLE SECURITY INVOKER AS $read$
DECLARE staff uuid:=NULLIF(current_setting('app.workforce_assignment_target_staff',true),'')::uuid;
 generation uuid:=NULLIF(current_setting('app.workforce_assignment_target_generation',true),'')::uuid;
BEGIN
 IF workforce_assignment_selection_context(t,h)IS NOT TRUE THEN RETURN false;END IF;
 IF relation='workforce_hospitals'THEN RETURN(row_data->>'id')::uuid=h AND row_data->>'establishment_kind'='hospital';END IF;
 IF relation='workforce_staff'THEN
  IF staff IS NULL OR(row_data->>'id')::uuid=staff THEN RETURN true;END IF;
  RETURN EXISTS(SELECT 1 FROM workforce_employment_reviews r JOIN workforce_employment_period_history p
   ON(p.tenant_id,p.hospital_id,p.staff_id,p.id,p.version,p.revision_id)=(r.tenant_id,r.hospital_id,r.staff_id,r.period_id,r.period_version,r.period_revision_id)
   WHERE r.tenant_id=t AND r.hospital_id=h AND r.staff_id=staff AND r.id=workforce_assignment_selection_review(t,h)
   AND p.sponsor_id=(row_data->>'id')::uuid AND row_data->>'employment_status'='active');
 END IF;
 IF staff IS NULL THEN RETURN false;END IF;
 CASE relation
 WHEN 'workforce_login_links'THEN
  RETURN(row_data->>'staff_id')::uuid=staff AND row_data->>'status'='active'AND row_data->>'employment_generation_id'IS NOT NULL
   AND(generation IS NULL OR(row_data->>'employment_generation_id')::uuid=generation);
 WHEN 'workforce_enrollment_profile','workforce_identity_epochs'THEN RETURN(row_data->>'staff_id')::uuid=staff;
 WHEN 'workforce_employment_generations'THEN RETURN(row_data->>'staff_id')::uuid=staff AND(row_data->>'id')::uuid=generation;
 WHEN 'workforce_employment_reviews'THEN RETURN(row_data->>'staff_id')::uuid=staff AND(row_data->>'id')::uuid=workforce_assignment_selection_review(t,h);
 WHEN 'workforce_employment_review_evidence','workforce_employment_review_contributors'THEN
  RETURN(row_data->>'staff_id')::uuid=staff AND(row_data->>'review_id')::uuid=workforce_assignment_selection_review(t,h);
 WHEN 'workforce_employment_period_history'THEN
  RETURN(row_data->>'staff_id')::uuid=staff AND EXISTS(SELECT 1 FROM workforce_employment_reviews r
   WHERE r.tenant_id=t AND r.hospital_id=h AND r.staff_id=staff AND r.id=workforce_assignment_selection_review(t,h)
   AND(r.period_id,r.period_version,r.period_revision_id)=((row_data->>'id')::uuid,(row_data->>'version')::bigint,(row_data->>'revision_id')::uuid));
 WHEN 'workforce_employment_evidence_drafts','workforce_employment_evidence_history'THEN
  RETURN(row_data->>'staff_id')::uuid=staff AND EXISTS(SELECT 1 FROM workforce_employment_review_evidence e
   WHERE e.tenant_id=t AND e.hospital_id=h AND e.staff_id=staff AND e.review_id=workforce_assignment_selection_review(t,h)
   AND e.binding_id=(row_data->>'id')::uuid);
 WHEN 'workforce_person_reviews'THEN RETURN(row_data->>'staff_id')::uuid=staff AND(row_data->>'id')::uuid=workforce_assignment_selection_person(t,h);
 WHEN 'workforce_person_review_evidence','workforce_person_review_contributors'THEN
  RETURN(row_data->>'staff_id')::uuid=staff AND(row_data->>'review_id')::uuid=workforce_assignment_selection_person(t,h);
 WHEN 'workforce_documents','workforce_document_classifications'THEN RETURN workforce_assignment_selection_file(t,h,(row_data->>'id')::uuid);
 WHEN 'workforce_audit'THEN
  RETURN(row_data->>'action'='workforce.staff.insert'AND(row_data->>'resource_id')::uuid=staff)
   OR(row_data->>'action'='workforce.document.created'AND workforce_assignment_selection_file(t,h,(row_data->>'resource_id')::uuid));
 ELSE RETURN false;
 END CASE;
END $read$;

CREATE FUNCTION workforce_assignment_selection_audit(t uuid,h uuid,actor text,action text,resource uuid,changes jsonb)RETURNS boolean LANGUAGE plpgsql STABLE SECURITY INVOKER AS $audit$
DECLARE p jsonb:=NULLIF(current_setting('app.workforce_assignment_selection',true),'')::jsonb;
 staff uuid:=NULLIF(current_setting('app.workforce_assignment_target_staff',true),'')::uuid;
BEGIN
 IF workforce_assignment_selection_context(t,h)IS NOT TRUE THEN RETURN false;END IF;
 RETURN COALESCE(actor=p->>'actor_subject'AND action='workforce.assignment_target.qualification.read'AND resource=staff
 AND changes=jsonb_build_object('authority',p,'staff_version',(changes->>'staff_version')::bigint,'eligible',(changes->>'eligible')::boolean,'link_id',(changes->>'link_id')::uuid,'generation_id',(changes->>'generation_id')::uuid)
 AND EXISTS(SELECT 1 FROM workforce_staff s WHERE s.tenant_id=t AND s.hospital_id=h AND s.id=staff AND s.version=(changes->>'staff_version')::bigint)
 AND(((changes->>'eligible')::boolean=false AND changes->>'link_id'IS NULL AND changes->>'generation_id'IS NULL)
 OR((changes->>'eligible')::boolean=true AND(changes->>'generation_id')::uuid=NULLIF(current_setting('app.workforce_assignment_target_generation',true),'')::uuid
 AND EXISTS(SELECT 1 FROM workforce_login_links l JOIN workforce_employment_generations g
 ON(g.tenant_id,g.hospital_id,g.staff_id,g.id)=(l.tenant_id,l.hospital_id,l.staff_id,l.employment_generation_id)
 WHERE l.tenant_id=t AND l.hospital_id=h AND l.staff_id=staff AND l.id=(changes->>'link_id')::uuid
 AND l.status='active'AND l.version>=2 AND g.access_intent='digital'AND g.declared_subject=l.subject))),false);
END $audit$;

DO $policies$
DECLARE target record;prior record;reading text;writing text;active text:=$expression$NULLIF(current_setting('app.workforce_assignment_selection',true),'')IS NOT NULL$expression$;
BEGIN
 FOR target IN SELECT c.oid,c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname=current_schema()AND c.relkind IN('r','p')AND starts_with(c.relname,'workforce_')
 AND EXISTS(SELECT 1 FROM pg_attribute a WHERE a.attrelid=c.oid AND a.attname='tenant_id'AND NOT a.attisdropped)LOOP
  IF(SELECT count(*)FROM pg_policy p WHERE p.polrelid=target.oid AND p.polpermissive)<>1 THEN RAISE EXCEPTION 'assignment selection requires one canonical permissive policy';END IF;
  SELECT p.polname,pg_get_expr(p.polqual,p.polrelid)AS reading,pg_get_expr(p.polwithcheck,p.polrelid)AS writing INTO STRICT prior FROM pg_policy p WHERE p.polrelid=target.oid AND p.polpermissive;
  reading:='false';writing:='false';
  IF target.relname='workforce_hospitals'THEN reading:='workforce_assignment_selection_read(tenant_id,id,''workforce_hospitals'',to_jsonb(workforce_hospitals))';
  ELSIF target.relname IN('workforce_staff','workforce_login_links','workforce_enrollment_profile','workforce_identity_epochs','workforce_employment_generations','workforce_employment_reviews','workforce_employment_review_evidence','workforce_employment_review_contributors','workforce_employment_period_history','workforce_employment_evidence_drafts','workforce_employment_evidence_history','workforce_person_reviews','workforce_person_review_evidence','workforce_person_review_contributors','workforce_documents','workforce_document_classifications','workforce_audit')THEN
   reading:=format('workforce_assignment_selection_read(tenant_id,hospital_id,%L,to_jsonb(%I))',target.relname,target.relname);
  END IF;
  IF target.relname='workforce_audit'THEN writing:='workforce_assignment_selection_audit(tenant_id,hospital_id,actor_subject,action,resource_id,changes)';END IF;
  EXECUTE format('ALTER POLICY %I ON %I USING(CASE WHEN %s THEN(%s)ELSE(%s)END)WITH CHECK(CASE WHEN %s THEN(%s)ELSE(%s)END)',prior.polname,target.relname,active,reading,COALESCE(prior.reading,'true'),active,writing,COALESCE(prior.writing,prior.reading,'true'));
  EXECUTE format('CREATE POLICY assignment_selection_purpose ON %I AS RESTRICTIVE FOR ALL USING(CASE WHEN %s THEN(%s)ELSE true END)WITH CHECK(CASE WHEN %s THEN(%s)ELSE true END)',target.relname,active,reading,active,writing);
  EXECUTE format('CREATE POLICY assignment_selection_delete ON %I AS RESTRICTIVE FOR DELETE USING(NOT(%s))',target.relname,active);
 END LOOP;
END $policies$;

DO $integrity$
DECLARE policies text;functions text;policy_count bigint;
BEGIN
 SELECT string_agg(format('(%L::regclass,%L,%L)',p.polrelid::regclass::text,p.polname,md5(p.polcmd::text||p.polpermissive::text||p.polroles::text||COALESCE(pg_get_expr(p.polqual,p.polrelid),'')||COALESCE(pg_get_expr(p.polwithcheck,p.polrelid),''))),','),count(*)INTO policies,policy_count
 FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname=current_schema()AND starts_with(c.relname,'workforce_')AND EXISTS(SELECT 1 FROM pg_attribute a WHERE a.attrelid=c.oid AND a.attname='tenant_id'AND NOT a.attisdropped);
 SELECT string_agg(format('(%L,%L)',p.oid::regprocedure::text,md5(pg_get_functiondef(p.oid))),',')INTO functions FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
 WHERE n.nspname=current_schema()AND p.proname IN('workforce_assignment_selection_context','workforce_assignment_selection_review','workforce_assignment_selection_person','workforce_assignment_selection_file','workforce_assignment_selection_read','workforce_assignment_selection_audit');
 EXECUTE format($definition$CREATE FUNCTION workforce_assignment_selection_integrity()RETURNS boolean LANGUAGE sql STABLE AS $body$
 SELECT NOT EXISTS(SELECT 1 FROM(VALUES %s)expected(relation,name,digest)LEFT JOIN pg_policy p ON p.polrelid=expected.relation AND p.polname=expected.name
 WHERE p.oid IS NULL OR md5(p.polcmd::text||p.polpermissive::text||p.polroles::text||COALESCE(pg_get_expr(p.polqual,p.polrelid),'')||COALESCE(pg_get_expr(p.polwithcheck,p.polrelid),''))<>expected.digest)
 AND(SELECT count(*)FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema()AND starts_with(c.relname,'workforce_')AND EXISTS(SELECT 1 FROM pg_attribute a WHERE a.attrelid=c.oid AND a.attname='tenant_id'AND NOT a.attisdropped))=%s
 AND NOT EXISTS(SELECT 1 FROM(VALUES %s)expected(name,digest)LEFT JOIN pg_proc p ON p.oid=to_regprocedure(expected.name)WHERE p.oid IS NULL OR md5(pg_get_functiondef(p.oid))<>expected.digest)
 $body$ $definition$,policies,policy_count,functions);
END $integrity$;

CREATE FUNCTION workforce_assignment_selection_support()RETURNS boolean LANGUAGE sql STABLE SECURITY INVOKER AS $$
 SELECT workforce_assignment_selection_integrity()AND workforce_operating_selection_integrity()AND NOT EXISTS(
 SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema()AND c.relkind IN('r','p')AND starts_with(c.relname,'workforce_')
 AND EXISTS(SELECT 1 FROM pg_attribute a WHERE a.attrelid=c.oid AND a.attname='tenant_id'AND NOT a.attisdropped)
 AND(NOT c.relrowsecurity OR NOT c.relforcerowsecurity OR has_table_privilege(current_user,c.oid,'TRUNCATE')
 OR NOT EXISTS(SELECT 1 FROM pg_policy p WHERE p.polrelid=c.oid AND p.polname='assignment_selection_purpose'AND NOT p.polpermissive AND p.polcmd='*'AND p.polqual IS NOT NULL AND p.polwithcheck IS NOT NULL)
 OR NOT EXISTS(SELECT 1 FROM pg_policy p WHERE p.polrelid=c.oid AND p.polname='assignment_selection_delete'AND NOT p.polpermissive AND p.polcmd='d'AND p.polqual IS NOT NULL)))
$$;

DO $integrity$
DECLARE policies text;functions text;policy_count bigint;
BEGIN
 SELECT string_agg(format('(%L::regclass,%L,%L)',p.polrelid::regclass::text,p.polname,md5(p.polcmd::text||p.polpermissive::text||p.polroles::text||COALESCE(pg_get_expr(p.polqual,p.polrelid),'')||COALESCE(pg_get_expr(p.polwithcheck,p.polrelid),''))),','),count(*)INTO policies,policy_count
 FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname=current_schema()AND(c.relname IN('workforce_hospitals','workforce_staff','workforce_login_links','workforce_audit')OR p.polname IN('operating_selection_purpose','operating_selection_delete'));
 SELECT string_agg(format('(%L,%L)',p.oid::regprocedure::text,md5(pg_get_functiondef(p.oid))),',')INTO functions FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
 WHERE n.nspname=current_schema()AND p.proname IN('workforce_operating_selection_context','workforce_operating_selection_staff','workforce_operating_selection_link','workforce_operating_selection_audit');
 EXECUTE format($definition$CREATE OR REPLACE FUNCTION workforce_operating_selection_integrity()RETURNS boolean LANGUAGE sql STABLE AS $body$
 SELECT NOT EXISTS(SELECT 1 FROM(VALUES %s)expected(relation,name,digest)LEFT JOIN pg_policy p ON p.polrelid=expected.relation AND p.polname=expected.name
 WHERE p.oid IS NULL OR md5(p.polcmd::text||p.polpermissive::text||p.polroles::text||COALESCE(pg_get_expr(p.polqual,p.polrelid),'')||COALESCE(pg_get_expr(p.polwithcheck,p.polrelid),''))<>expected.digest)
 AND(SELECT count(*)FROM pg_policy p JOIN pg_class c ON c.oid=p.polrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema()AND(c.relname IN('workforce_hospitals','workforce_staff','workforce_login_links','workforce_audit')OR p.polname IN('operating_selection_purpose','operating_selection_delete')))=%s
 AND NOT EXISTS(SELECT 1 FROM(VALUES %s)expected(name,digest)LEFT JOIN pg_proc p ON p.oid=to_regprocedure(expected.name)WHERE p.oid IS NULL OR md5(pg_get_functiondef(p.oid))<>expected.digest)
 $body$ $definition$,policies,policy_count,functions);
END $integrity$;
