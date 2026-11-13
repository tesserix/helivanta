SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

ALTER TABLE workforce_person_reviews ADD COLUMN governance_authority jsonb;
ALTER TABLE workforce_person_review_history ADD COLUMN governance_authority jsonb;

CREATE FUNCTION workforce_person_governance_authority(t uuid,h uuid,s uuid,r uuid)RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(hms_tenant_visible(t)
 AND t=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND h=NULLIF(current_setting('app.workforce_hospital',true),'')::uuid
 AND p->>'tenant_id'=t::text AND p->>'hospital_id'=h::text AND p->>'staff_id'=s::text
 AND p->>'actor_subject'=NULLIF(current_setting('app.workforce_subject',true),'')
 AND p->>'actor_subject'=NULLIF(current_setting('app.actor_subject',true),'')
 AND p->>'review_id'=r::text
 AND (p->>'membership_epoch')::bigint BETWEEN 1 AND 9007199254740991
 AND (p->>'owner_count')::bigint BETWEEN 1 AND 1000
 AND (p->>'source_expires_at')::timestamptz>clock_timestamp()
 AND (p->>'idle_deadline')::timestamptz>clock_timestamp()
 AND (p->>'authenticated_at')::timestamptz IS NOT NULL
 AND ((p->>'capability'='workforce.person.prepare' AND current_setting('app.workforce_person_review',true)='manage')
 OR (p->>'capability'='workforce.person.review' AND current_setting('app.workforce_person_review',true)='review'))
 AND ((p->>'authority_kind'='owner' AND (p->>'owner_membership_id')::uuid IS NOT NULL AND (p->>'owner_membership_id')::uuid<>'00000000-0000-0000-0000-000000000000'::uuid
 AND p->>'source_grant_id' IS NULL AND p->>'source_grant_version' IS NULL AND p->>'template_key' IS NULL)
 OR (p->>'authority_kind'='grant' AND p->>'owner_membership_id' IS NULL AND (p->>'source_grant_id')::uuid IS NOT NULL AND (p->>'source_grant_id')::uuid<>'00000000-0000-0000-0000-000000000000'::uuid
 AND (p->>'source_grant_version')::bigint BETWEEN 1 AND 9007199254740991
 AND p->>'template_key'=CASE p->>'capability' WHEN 'workforce.person.prepare' THEN 'workforce.person_preparer.v1' WHEN 'workforce.person.review' THEN 'workforce.person_reviewer.v1' END)),false)
 FROM(SELECT NULLIF(current_setting('app.workforce_person_authority',true),'')::jsonb AS p) proof
$$;

CREATE FUNCTION workforce_person_review_governance_record()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE proof jsonb:=NULLIF(current_setting('app.workforce_person_authority',true),'')::jsonb;
BEGIN
 IF proof IS NOT NULL AND NOT workforce_person_governance_authority(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.id) THEN
  RAISE EXCEPTION 'person review requires current exact nonclinical authority' USING ERRCODE='42501';
 END IF;
 NEW.governance_authority:=proof;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_person_review_zz_authority BEFORE INSERT OR UPDATE ON workforce_person_reviews
 FOR EACH ROW EXECUTE FUNCTION workforce_person_review_governance_record();

-- The nonclinical review purpose cannot write the masters it reads as evidence.
DO $migration$
DECLARE tab text;
BEGIN
 FOREACH tab IN ARRAY ARRAY['workforce_staff','workforce_enrollment_profile','workforce_documents','workforce_login_links'] LOOP
  EXECUTE format('CREATE POLICY person_governance_readonly ON %I AS RESTRICTIVE FOR ALL USING(hms_tenant_visible(tenant_id)) WITH CHECK(tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND COALESCE(current_setting(''app.workforce_person_authority'',true),'''')='''')',tab);
 END LOOP;
END $migration$;
