SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- The lookup may name the patient by id instead of MRN (#1387): a stay the
-- pharmacy resolved from a bed carries the patient id, never the MRN. The
-- session names exactly one of the two; the same grant and link apply.
CREATE FUNCTION patient_pharmacy_lookup_allowed(row_tenant uuid,row_hospital uuid,row_mrn text,row_id uuid) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT ((row_mrn=NULLIF(current_setting('app.pharmacy_lookup_mrn',true),'')
          AND NULLIF(current_setting('app.pharmacy_lookup_patient',true),'') IS NULL)
      OR (row_id=NULLIF(current_setting('app.pharmacy_lookup_patient',true),'')::uuid
          AND NULLIF(current_setting('app.pharmacy_lookup_mrn',true),'') IS NULL))
 AND row_tenant=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND row_hospital=NULLIF(current_setting('app.pharmacy_lookup_hospital',true),'')::uuid
 AND EXISTS (SELECT 1 FROM patient_pharmacy_links l JOIN patient_pharmacy_lookup_access a
  ON a.tenant_id=l.tenant_id AND a.hospital_id=l.hospital_id AND a.pharmacy_id=l.pharmacy_id AND a.request_id=l.request_id
  WHERE l.tenant_id=row_tenant AND l.hospital_id=row_hospital
   AND l.pharmacy_id=NULLIF(current_setting('app.pharmacy_lookup_pharmacy',true),'')::uuid
   AND a.subject=NULLIF(current_setting('app.pharmacy_lookup_subject',true),''))
$$;
ALTER POLICY tenant_isolation ON patients
 USING(hms_tenant_visible(tenant_id) AND (patient_hospital_allowed(tenant_id,hospital_id,false) OR patient_clinical_self_registration_allowed(tenant_id,hospital_id,id,updated_at) OR patient_clinical_recipient_resource_allowed(tenant_id,hospital_id,'patient',id) OR patient_proxy_registration_allowed(tenant_id,hospital_id,id) OR patient_pharmacy_lookup_allowed(tenant_id,hospital_id,mrn,id)));
DROP FUNCTION patient_pharmacy_lookup_allowed(uuid,uuid,text);
