SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE OR REPLACE FUNCTION workforce_governance_finding_file(t uuid,h uuid,s uuid,f uuid)
RETURNS boolean LANGUAGE plpgsql STABLE SECURITY INVOKER SET search_path=pg_catalog,public AS $$
BEGIN
 IF public.workforce_governance_finding_source(t,h,s) IS NOT TRUE THEN
  RETURN false;
 END IF;
 RETURN EXISTS(SELECT 1 FROM public.workforce_person_review_evidence e
 WHERE (e.tenant_id,e.hospital_id,e.staff_id,e.document_id)=(t,h,s,f)
 AND e.review_id=NULLIF(current_setting('app.workforce_employment_person_review',true),'')::uuid);
END $$;

CREATE OR REPLACE FUNCTION workforce_activation_governance_file(t uuid,h uuid,f uuid)
RETURNS boolean LANGUAGE plpgsql STABLE SECURITY INVOKER SET search_path=pg_catalog,public AS $$
BEGIN
 IF public.workforce_activation_governance_source(t,h,NULLIF(current_setting('app.workforce_identity_staff',true),'')::uuid) IS NOT TRUE THEN
  RETURN false;
 END IF;
 RETURN EXISTS(SELECT 1 FROM public.workforce_employment_reviews r
 WHERE r.tenant_id=t AND r.hospital_id=h AND public.workforce_activation_governance_review(r.tenant_id,r.hospital_id,r.staff_id,r.id)
 AND (EXISTS(SELECT 1 FROM public.workforce_employment_review_evidence e WHERE (e.tenant_id,e.hospital_id,e.staff_id,e.review_id,e.document_id)=(r.tenant_id,r.hospital_id,r.staff_id,r.id,f))
 OR EXISTS(SELECT 1 FROM public.workforce_person_review_evidence e WHERE (e.tenant_id,e.hospital_id,e.staff_id,e.review_id,e.document_id)=(r.tenant_id,r.hospital_id,r.staff_id,r.person_review_id,f))));
END $$;

CREATE OR REPLACE FUNCTION workforce_employment_governance_file(t uuid,h uuid,f uuid)
RETURNS boolean LANGUAGE plpgsql STABLE SECURITY INVOKER SET search_path=pg_catalog,public AS $$
BEGIN
 IF public.workforce_employment_governance_source(t,h,NULLIF(current_setting('app.workforce_identity_staff',true),'')::uuid) IS NOT TRUE THEN
  RETURN false;
 END IF;
 RETURN (EXISTS(SELECT 1 FROM public.workforce_documents d WHERE (d.tenant_id,d.hospital_id,d.id)=(t,h,f)
 AND public.workforce_employment_governance_prepare(d.tenant_id,d.hospital_id,d.staff_id)
 AND d.record_kind='enrollment' AND d.record_id=d.staff_id AND d.category IN('identity_evidence','employment_evidence'))
 OR (current_setting('app.workforce_employment_review',true)='review'
 AND public.workforce_employment_review_file_context(t,h,f)));
END $$;
