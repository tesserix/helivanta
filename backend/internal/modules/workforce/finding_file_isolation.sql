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
