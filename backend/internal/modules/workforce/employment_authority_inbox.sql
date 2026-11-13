SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE FUNCTION workforce_employment_inbox_source(t uuid,h uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(hms_tenant_visible(t)
 AND t=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND h=NULLIF(current_setting('app.workforce_hospital',true),'')::uuid
 AND p->>'purpose'='employment_inbox' AND p->>'tenant_id'=t::text AND p->>'hospital_id'=h::text
 AND p->>'actor_subject'=NULLIF(current_setting('app.workforce_subject',true),'')
 AND p->>'actor_subject'=NULLIF(current_setting('app.actor_subject',true),'')
 AND p->>'staff_id' IS NULL AND p->>'review_id' IS NULL AND p->>'actor_staff_id' IS NULL AND p->>'actor_link_id' IS NULL
 AND (p->>'membership_epoch')::bigint BETWEEN 1 AND 9007199254740991
 AND (p->>'owner_count')::integer BETWEEN 1 AND 1000
 AND (p->>'source_expires_at')::timestamptz>clock_timestamp()
 AND (p->>'idle_deadline')::timestamptz>clock_timestamp()
 AND (p->>'authenticated_at')::timestamptz<=clock_timestamp() AND p->>'authenticated_at' IS NOT NULL
 AND p->>'capability' IN('workforce.employment.prepare','workforce.employment.review')
 AND ((p->>'authority_kind'='owner' AND (p->>'owner_membership_id')::uuid<>'00000000-0000-0000-0000-000000000000'::uuid
 AND p->>'source_grant_id' IS NULL AND p->>'source_grant_version' IS NULL AND p->>'template_key' IS NULL)
 OR (p->>'authority_kind'='grant' AND p->>'owner_membership_id' IS NULL
 AND (p->>'source_grant_id')::uuid<>'00000000-0000-0000-0000-000000000000'::uuid
 AND (p->>'source_grant_version')::bigint BETWEEN 1 AND 9007199254740991
 AND p->>'template_key'=CASE p->>'capability' WHEN 'workforce.employment.prepare' THEN 'workforce.employment_preparer.v1' WHEN 'workforce.employment.review' THEN 'workforce.employment_reviewer.v1' END)),false)
 FROM(SELECT NULLIF(current_setting('app.workforce_employment_inbox_authority',true),'')::jsonb AS p) proof
$$;

DO $migration$
DECLARE item record; reading text; writing text;
BEGIN
 FOR item IN SELECT c.relname AS table_name,c.relrowsecurity,c.relforcerowsecurity FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
 WHERE n.nspname='public' AND c.relkind IN('r','p') AND starts_with(c.relname,'workforce_')
 AND EXISTS(SELECT 1 FROM pg_attribute a WHERE a.attrelid=c.oid AND a.attname='tenant_id' AND NOT a.attisdropped) ORDER BY c.relname LOOP
  IF NOT item.relrowsecurity OR NOT item.relforcerowsecurity THEN RAISE EXCEPTION 'employment inbox requires forced tenant RLS on %',item.table_name; END IF;
  reading:='false'; writing:='false';
  CASE item.table_name
   WHEN 'workforce_hospitals' THEN reading:='workforce_employment_inbox_source(tenant_id,id)';
   WHEN 'workforce_employment_reviews' THEN reading:='workforce_employment_inbox_source(tenant_id,hospital_id)';
   WHEN 'workforce_staff' THEN reading:=$read$workforce_employment_inbox_source(tenant_id,hospital_id)
    AND EXISTS(SELECT 1 FROM workforce_employment_reviews r WHERE (r.tenant_id,r.hospital_id,r.staff_id)=(workforce_staff.tenant_id,workforce_staff.hospital_id,workforce_staff.id))$read$;
   WHEN 'workforce_audit' THEN writing:=$write$workforce_employment_inbox_source(tenant_id,hospital_id)
    AND actor_subject=NULLIF(current_setting('app.actor_subject',true),'') AND resource_id=hospital_id
    AND action='workforce.employment_authority_inbox.read'
    AND changes->'authority'=NULLIF(current_setting('app.workforce_employment_inbox_authority',true),'')::jsonb
    AND (changes->>'rows')::integer BETWEEN 0 AND 50
    AND changes->>'status' IN('pending','approved','rejected','withdrawn','revoked')
    AND changes=jsonb_build_object('authority',changes->'authority','rows',(changes->>'rows')::integer,'status',changes->>'status')$write$;
   ELSE NULL;
  END CASE;
  IF reading<>'false' THEN
   EXECUTE format('CREATE POLICY employment_inbox_read ON %I FOR SELECT USING (%s)',item.table_name,reading);
  END IF;
  IF writing<>'false' THEN
   EXECUTE format('CREATE POLICY employment_inbox_audit ON %I FOR INSERT WITH CHECK (%s)',item.table_name,writing);
  END IF;
  EXECUTE format($policy$CREATE POLICY employment_inbox_purpose ON %I AS RESTRICTIVE FOR ALL
   USING(hms_tenant_visible(tenant_id) AND (COALESCE(current_setting('app.workforce_employment_inbox_authority',true),'')='' OR (%s)))
   WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND (COALESCE(current_setting('app.workforce_employment_inbox_authority',true),'')='' OR (%s)))$policy$,item.table_name,reading,writing);
  EXECUTE format($policy$CREATE POLICY employment_inbox_delete ON %I AS RESTRICTIVE FOR DELETE
   USING(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND COALESCE(current_setting('app.workforce_employment_inbox_authority',true),'')='')$policy$,item.table_name);
 END LOOP;
END $migration$;
