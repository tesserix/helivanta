SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE FUNCTION mediconnect_claim_check_allowed(row_tenant uuid,row_id uuid,row_subject text,row_hospital uuid,row_patient uuid) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT row_tenant=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND current_setting('app.actor_subject',true)='system:mediconnect-current-claim'
 AND row_id=NULLIF(current_setting('app.mediconnect_claim_id',true),'')::uuid
 AND row_subject=NULLIF(current_setting('app.mediconnect_claim_subject',true),'')
 AND row_hospital=NULLIF(current_setting('app.mediconnect_claim_hospital',true),'')::uuid
 AND row_patient=NULLIF(current_setting('app.mediconnect_claim_patient',true),'')::uuid
$$;
ALTER POLICY tenant_isolation ON mediconnect_link_requests
 USING(hms_tenant_visible(tenant_id)
 AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND (subject=NULLIF(current_setting('app.actor_subject',true),'') OR
 (id=NULLIF(current_setting('app.mediconnect_review_request',true),'')::uuid
 AND current_setting('app.actor_subject',true)='system:mediconnect-registration-reviewed') OR
 mediconnect_claim_check_allowed(tenant_id,id,subject,hospital_id,patient_id)));
