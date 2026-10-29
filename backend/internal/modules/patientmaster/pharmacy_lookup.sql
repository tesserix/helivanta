SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- A pharmacy's link with a hospital is active once both sides agree it is
-- verified (#1416b): written when pharmacy confirms the acceptance, deleted
-- with a revocation or a withdrawal.
CREATE TABLE patient_pharmacy_links (
 tenant_id uuid NOT NULL,
 hospital_id uuid NOT NULL,
 pharmacy_id uuid NOT NULL,
 request_id uuid NOT NULL,
 -- The audit trail names a link by its round.
 id uuid GENERATED ALWAYS AS (request_id) STORED,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY (tenant_id,hospital_id,pharmacy_id),
 UNIQUE (tenant_id,request_id),
 FOREIGN KEY (tenant_id,hospital_id,request_id) REFERENCES patient_pharmacy_affiliation_reviews (tenant_id,hospital_id,request_id)
);
ALTER TABLE patient_pharmacy_links ENABLE ROW LEVEL SECURITY;
ALTER TABLE patient_pharmacy_links FORCE ROW LEVEL SECURITY;
-- That a link exists is not patient data; only its writers are narrow,
-- through restrictive policies on top of the tenant policy.
CREATE POLICY tenant_isolation ON patient_pharmacy_links
 USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid);
CREATE POLICY pharmacy_link_confirmed ON patient_pharmacy_links AS RESTRICTIVE FOR INSERT
 WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
  AND current_setting('app.actor_subject',true)='system:patient-pharmacy-affiliation-decision-receipt'
  AND EXISTS (SELECT 1 FROM patient_pharmacy_affiliation_reviews r WHERE r.tenant_id=patient_pharmacy_links.tenant_id
   AND r.request_id=patient_pharmacy_links.request_id AND r.pharmacy_id=patient_pharmacy_links.pharmacy_id AND r.status='accepted'));
CREATE POLICY pharmacy_link_ended ON patient_pharmacy_links AS RESTRICTIVE FOR DELETE
 USING (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
  AND (patient_hospital_allowed(tenant_id,hospital_id,true) OR current_setting('app.actor_subject',true)='system:patient-pharmacy-affiliation-withdrawal'));
-- Rows are only added or removed. A trigger, not an UPDATE policy: Postgres
-- applies UPDATE policies to SELECT ... FOR SHARE, which the lookup needs.
CREATE TRIGGER patient_pharmacy_link_fixed BEFORE UPDATE ON patient_pharmacy_links FOR EACH STATEMENT EXECUTE FUNCTION patient_pharmacy_affiliation_history();
CREATE TRIGGER patient_pharmacy_link_audit AFTER INSERT OR DELETE ON patient_pharmacy_links FOR EACH ROW EXECUTE FUNCTION audit_record_change();

-- Named pharmacists the hospital lets look up its patients for one pharmacy.
-- Never read by patient_hospital_allowed, so it is no registry access. Tied
-- to the link's round: a re-verified link starts with no grants.
CREATE TABLE patient_pharmacy_lookup_access (
 id uuid NOT NULL DEFAULT gen_random_uuid() UNIQUE,
 tenant_id uuid NOT NULL,
 hospital_id uuid NOT NULL,
 pharmacy_id uuid NOT NULL,
 request_id uuid NOT NULL,
 subject text NOT NULL CHECK (length(btrim(subject)) BETWEEN 1 AND 200),
 granted_by text NOT NULL CHECK (length(btrim(granted_by)) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY (tenant_id,hospital_id,pharmacy_id,request_id,subject),
 FOREIGN KEY (tenant_id,hospital_id,request_id) REFERENCES patient_pharmacy_affiliation_reviews (tenant_id,hospital_id,request_id)
);
CREATE INDEX patient_pharmacy_lookup_access_page ON patient_pharmacy_lookup_access (tenant_id,hospital_id,pharmacy_id,created_at DESC,subject);
ALTER TABLE patient_pharmacy_lookup_access ENABLE ROW LEVEL SECURITY;
ALTER TABLE patient_pharmacy_lookup_access FORCE ROW LEVEL SECURITY;
-- Readable within the tenant like patient_hospital_access, so the lookup
-- branch below can see the caller's own grant; written only by a registrar.
CREATE POLICY tenant_isolation ON patient_pharmacy_lookup_access
 USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid);
CREATE POLICY pharmacy_lookup_granted ON patient_pharmacy_lookup_access AS RESTRICTIVE FOR INSERT
 WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND patient_hospital_allowed(tenant_id,hospital_id,true)
  AND granted_by=current_setting('app.actor_subject',true));
CREATE POLICY pharmacy_lookup_revoked ON patient_pharmacy_lookup_access AS RESTRICTIVE FOR DELETE
 USING (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND patient_hospital_allowed(tenant_id,hospital_id,true));
CREATE TRIGGER patient_pharmacy_lookup_access_fixed BEFORE UPDATE ON patient_pharmacy_lookup_access FOR EACH STATEMENT EXECUTE FUNCTION patient_pharmacy_affiliation_history();
CREATE TRIGGER patient_pharmacy_lookup_access_audit AFTER INSERT OR DELETE ON patient_pharmacy_lookup_access FOR EACH ROW EXECUTE FUNCTION audit_record_change();

-- A lookup session sees one hospital's patients only through both its own
-- grant and the active link for the pharmacy it names, in the current round.
CREATE FUNCTION patient_pharmacy_lookup_allowed(row_tenant uuid,row_hospital uuid,row_mrn text) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT row_tenant=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND row_hospital=NULLIF(current_setting('app.pharmacy_lookup_hospital',true),'')::uuid
 AND row_mrn=NULLIF(current_setting('app.pharmacy_lookup_mrn',true),'')
 AND EXISTS (SELECT 1 FROM patient_pharmacy_links l JOIN patient_pharmacy_lookup_access a
  ON a.tenant_id=l.tenant_id AND a.hospital_id=l.hospital_id AND a.pharmacy_id=l.pharmacy_id AND a.request_id=l.request_id
  WHERE l.tenant_id=row_tenant AND l.hospital_id=row_hospital
   AND l.pharmacy_id=NULLIF(current_setting('app.pharmacy_lookup_pharmacy',true),'')::uuid
   AND a.subject=NULLIF(current_setting('app.pharmacy_lookup_subject',true),''))
$$;
ALTER POLICY tenant_isolation ON patients
 USING(hms_tenant_visible(tenant_id) AND (patient_hospital_allowed(tenant_id,hospital_id,false) OR patient_clinical_self_registration_allowed(tenant_id,hospital_id,id,updated_at) OR patient_clinical_recipient_resource_allowed(tenant_id,hospital_id,'patient',id) OR patient_proxy_registration_allowed(tenant_id,hospital_id,id) OR patient_pharmacy_lookup_allowed(tenant_id,hospital_id,mrn)));
-- A lookup session sees the one patients row and nothing hanging off it:
-- these tables admit a row whenever its patient is visible, so they refuse
-- every row while lookup settings are present.
CREATE FUNCTION patient_not_pharmacy_lookup() RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT NULLIF(current_setting('app.pharmacy_lookup_hospital',true),'') IS NULL
$$;
CREATE POLICY pharmacy_lookup_excluded ON patient_consents AS RESTRICTIVE USING (patient_not_pharmacy_lookup());
CREATE POLICY pharmacy_lookup_excluded ON patient_identifiers AS RESTRICTIVE USING (patient_not_pharmacy_lookup());
CREATE POLICY pharmacy_lookup_excluded ON patient_duplicate_overrides AS RESTRICTIVE USING (patient_not_pharmacy_lookup());
CREATE POLICY pharmacy_lookup_excluded ON patient_reported_blood_groups AS RESTRICTIVE USING (patient_not_pharmacy_lookup());

-- Lookups and lookup grants are audited with the pharmacy they serve.
ALTER TABLE patient_access_audit ADD COLUMN pharmacy_id uuid;
ALTER TABLE patient_access_audit DROP CONSTRAINT patient_access_audit_action_check;
ALTER TABLE patient_access_audit ADD CONSTRAINT patient_access_audit_action_check
 CHECK (action IN ('read', 'list', 'match', 'register', 'grant', 'revoke', 'replay', 'pharmacy_lookup')) NOT VALID;
ALTER TABLE patient_access_audit VALIDATE CONSTRAINT patient_access_audit_action_check;
