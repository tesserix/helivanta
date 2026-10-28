SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
ALTER TABLE patient_sharing_notices ADD COLUMN authority_kind text NOT NULL DEFAULT '' CHECK(authority_kind IN ('','parental','appointed_guardian'));
CREATE FUNCTION patient_sharing_notice_capacity_guard() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.authority_kind IS DISTINCT FROM OLD.authority_kind THEN RAISE EXCEPTION 'notice capacity immutable' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER patient_sharing_notice_capacity_guard BEFORE UPDATE ON patient_sharing_notices FOR EACH ROW EXECUTE FUNCTION patient_sharing_notice_capacity_guard();
ALTER TABLE patient_clinical_consents ALTER COLUMN authority_id DROP NOT NULL;
ALTER TABLE patient_clinical_consents ADD COLUMN proxy_authority_id uuid;
ALTER TABLE patient_clinical_consents ADD CONSTRAINT patient_clinical_consent_proxy_fk FOREIGN KEY(tenant_id,proxy_authority_id) REFERENCES patient_proxy_authorities(tenant_id,id) ON DELETE RESTRICT NOT VALID;
ALTER TABLE patient_clinical_consents ADD CONSTRAINT patient_clinical_consent_one_authority CHECK((authority_id IS NULL)<>(proxy_authority_id IS NULL)) NOT VALID;
ALTER TABLE patient_clinical_consents VALIDATE CONSTRAINT patient_clinical_consent_proxy_fk;
ALTER TABLE patient_clinical_consents VALIDATE CONSTRAINT patient_clinical_consent_one_authority;
CREATE FUNCTION patient_clinical_proxy_allowed(row_tenant uuid,row_hospital uuid) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT row_tenant=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND row_hospital=NULLIF(current_setting('app.patient_hospital',true),'')::uuid
 AND EXISTS(SELECT 1 FROM patient_proxy_authorities a
 JOIN patient_link_reviews r ON r.tenant_id=a.tenant_id AND r.request_id=a.review_request_id
 JOIN patients p ON p.tenant_id=a.tenant_id AND p.hospital_id=a.hospital_id AND p.id=a.patient_id
 WHERE a.tenant_id=row_tenant AND a.hospital_id=row_hospital
 AND a.id=NULLIF(current_setting('app.patient_proxy_authority',true),'')::uuid
 AND patient_consent_subject_allowed(a.tenant_id,a.hospital_id,a.subject)
 AND a.status='accepted' AND a.version=2 AND a.valid_until>clock_timestamp()
 AND p.updated_at=a.registration_updated_at AND r.registration_updated_at=a.registration_updated_at
 AND r.status='approved' AND r.decision_id=a.decision_id AND r.requester_subject=a.subject AND r.hospital_id=a.hospital_id AND r.patient_id=a.patient_id
 AND ((a.kind='parental' AND r.relationship='child' AND NOT p.dob_estimated AND a.parental_end_at=((p.dob+interval '18 years')::date::timestamp AT TIME ZONE a.time_zone) AND a.parental_end_at>clock_timestamp()) OR (a.kind='appointed_guardian' AND r.relationship='ward')))
$$;
ALTER POLICY tenant_isolation ON patient_sharing_notices USING(hms_tenant_visible(tenant_id) AND (patient_hospital_allowed(tenant_id,hospital_id,true) OR (status='approved' AND patient_clinical_self_allowed(tenant_id,hospital_id)) OR patient_clinical_recipient_resource_allowed(tenant_id,hospital_id,'notice',id) OR (status='approved' AND patient_clinical_proxy_allowed(tenant_id,hospital_id))));
ALTER POLICY tenant_isolation ON patient_sharing_recipients USING(hms_tenant_visible(tenant_id) AND (patient_hospital_allowed(tenant_id,hospital_id,true) OR (status='approved' AND valid_until>clock_timestamp() AND (patient_clinical_self_allowed(tenant_id,hospital_id) OR patient_clinical_proxy_allowed(tenant_id,hospital_id))) OR (hospital_id=NULLIF(current_setting('app.patient_hospital',true),'')::uuid AND grantee_subject=NULLIF(current_setting('app.actor_subject',true),'') AND grantee_subject=NULLIF(current_setting('app.patient_clinical_recipient',true),''))));
CREATE OR REPLACE FUNCTION patient_clinical_consent_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'clinical consent history retained' USING ERRCODE='23514'; END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.status<>'granted' OR NEW.subject IS DISTINCT FROM current_setting('app.actor_subject',true)
  OR (NEW.body::jsonb->>'schema_version',NEW.body::jsonb->>'id',NEW.body::jsonb->>'tenant_id',NEW.body::jsonb->>'hospital_id',NEW.body::jsonb->>'patient_id',NEW.body::jsonb->>'subject',NEW.body::jsonb->>'authority_id',NEW.body::jsonb->>'review_request_id',NEW.body::jsonb->>'notice_id',NEW.body::jsonb->>'recipient_id',NEW.body::jsonb->>'purpose',NEW.body::jsonb->>'fingerprint')
  IS DISTINCT FROM (CASE WHEN NEW.proxy_authority_id IS NULL THEN '1' ELSE '2' END,NEW.id::text,NEW.tenant_id::text,NEW.hospital_id::text,NEW.patient_id::text,NEW.subject,COALESCE(NEW.authority_id,NEW.proxy_authority_id)::text,NEW.review_request_id::text,NEW.notice_id::text,NEW.recipient_id::text,NEW.purpose,NEW.fingerprint)
  OR (NEW.proxy_authority_id IS NULL AND COALESCE(NEW.body::jsonb->>'authority_kind','')<>'')
  OR (NEW.proxy_authority_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM patient_proxy_authorities a JOIN patient_sharing_notices n ON n.tenant_id=a.tenant_id AND n.hospital_id=a.hospital_id AND n.id=NEW.notice_id WHERE a.tenant_id=NEW.tenant_id AND a.hospital_id=NEW.hospital_id AND a.id=NEW.proxy_authority_id AND a.patient_id=NEW.patient_id AND a.subject=NEW.subject AND a.review_request_id=NEW.review_request_id AND a.kind=NEW.body::jsonb->>'authority_kind' AND n.authority_kind=a.kind AND n.status='approved' AND a.status='accepted' AND a.version=2 AND NEW.valid_until<=a.valid_until AND patient_clinical_proxy_allowed(a.tenant_id,a.hospital_id)))
  THEN RAISE EXCEPTION 'attributable clinical declaration required' USING ERRCODE='23514'; END IF;
 ELSE
  IF (NEW.tenant_id,NEW.hospital_id,NEW.id,NEW.patient_id,NEW.subject,NEW.authority_id,NEW.proxy_authority_id,NEW.review_request_id,NEW.notice_id,NEW.recipient_id,NEW.purpose,NEW.fingerprint,NEW.body,NEW.hmac,NEW.valid_until,NEW.captured_at,NEW.created_at)
  IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.id,OLD.patient_id,OLD.subject,OLD.authority_id,OLD.proxy_authority_id,OLD.review_request_id,OLD.notice_id,OLD.recipient_id,OLD.purpose,OLD.fingerprint,OLD.body,OLD.hmac,OLD.valid_until,OLD.captured_at,OLD.created_at)
  OR NEW.version<>OLD.version+1 OR (OLD.status='quarantined' AND (OLD.withdrawn_at IS NOT NULL OR NEW.status<>'quarantined' OR NEW.withdrawn_at IS NULL)) OR NEW.updated_at<OLD.updated_at THEN
   RAISE EXCEPTION 'clinical declaration immutable' USING ERRCODE='23514';
  END IF;
  IF NEW.status='withdrawn' THEN
   IF OLD.status<>'granted' OR NEW.withdrawn_by IS DISTINCT FROM OLD.subject OR NEW.withdrawn_by IS DISTINCT FROM current_setting('app.actor_subject',true) THEN RAISE EXCEPTION 'subject withdrawal required' USING ERRCODE='23514'; END IF;
  ELSIF NEW.status='quarantined' THEN
   IF OLD.status='quarantined' THEN
    IF NEW.withdrawn_by IS DISTINCT FROM OLD.subject OR NEW.withdrawn_by IS DISTINCT FROM current_setting('app.actor_subject',true) OR (NEW.quarantined_at,NEW.quarantined_by) IS DISTINCT FROM (OLD.quarantined_at,OLD.quarantined_by) THEN RAISE EXCEPTION 'quarantined subject withdrawal required' USING ERRCODE='23514'; END IF;
   ELSE
    IF current_setting('app.patient_clinical_integrity',true) IS DISTINCT FROM OLD.id::text OR NEW.quarantined_by IS DISTINCT FROM current_setting('app.actor_subject',true) OR (NEW.withdrawn_at,NEW.withdrawn_by) IS DISTINCT FROM (OLD.withdrawn_at,OLD.withdrawn_by) THEN RAISE EXCEPTION 'attributable integrity incident required' USING ERRCODE='23514'; END IF;
   END IF;
  ELSE RAISE EXCEPTION 'invalid clinical consent transition' USING ERRCODE='23514'; END IF;
 END IF;
 RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION patient_clinical_recipient_resource_allowed(row_tenant uuid,row_hospital uuid,kind text,resource uuid) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT row_tenant=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND row_hospital=NULLIF(current_setting('app.patient_hospital',true),'')::uuid
 AND EXISTS(SELECT 1 FROM patient_clinical_consents c WHERE c.tenant_id=row_tenant AND c.hospital_id=row_hospital
 AND c.id=NULLIF(current_setting('app.patient_clinical_consent',true),'')::uuid AND c.status='granted'
 AND patient_clinical_recipient_allowed(c.tenant_id,c.hospital_id,c.recipient_id)
 AND ((kind='authority' AND c.authority_id=resource) OR (kind='proxy_authority' AND c.proxy_authority_id=resource) OR (kind='review' AND c.review_request_id=resource) OR (kind='patient' AND c.patient_id=resource) OR (kind='notice' AND c.notice_id=resource)))
$$;
ALTER POLICY tenant_isolation ON patient_proxy_authorities
 USING(hms_tenant_visible(tenant_id) AND (patient_hospital_allowed(tenant_id,hospital_id,true) OR patient_consent_subject_allowed(tenant_id,hospital_id,subject) OR patient_clinical_recipient_resource_allowed(tenant_id,hospital_id,'proxy_authority',id)));
