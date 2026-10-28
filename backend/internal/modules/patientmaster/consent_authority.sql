SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE TABLE patient_consent_authorities (
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 tenant_id uuid NOT NULL,
 hospital_id uuid NOT NULL,
 patient_id uuid NOT NULL,
 review_request_id uuid NOT NULL,
 decision_id uuid NOT NULL,
 subject text NOT NULL CHECK(length(btrim(subject)) BETWEEN 1 AND 512),
 reviewer_subject text NOT NULL CHECK(length(btrim(reviewer_subject)) BETWEEN 1 AND 200 AND reviewer_subject<>subject),
 policy_reference uuid NOT NULL CHECK(substring(policy_reference::text,15,1)='7'),
 evidence_reference uuid NOT NULL CHECK(substring(evidence_reference::text,15,1)='7'),
 registration_updated_at timestamptz NOT NULL,
 fingerprint text NOT NULL CHECK(length(fingerprint)=64),
 status text NOT NULL DEFAULT 'proposed' CHECK(status IN ('proposed','accepted','revoked')),
 version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 valid_until timestamptz NOT NULL,
 accepted_at timestamptz,
 revoked_at timestamptz,
 revoked_by text,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,id),
 FOREIGN KEY(tenant_id,patient_id) REFERENCES patients(tenant_id,id),
 FOREIGN KEY(tenant_id,review_request_id) REFERENCES patient_link_reviews(tenant_id,request_id),
 CHECK(valid_until>created_at),
 CHECK(updated_at>=created_at),
 CHECK(accepted_at IS NULL OR accepted_at>=created_at),
 CHECK(revoked_at IS NULL OR (revoked_at>=created_at AND (accepted_at IS NULL OR revoked_at>=accepted_at))),
 CHECK((status='proposed' AND accepted_at IS NULL AND revoked_at IS NULL AND revoked_by IS NULL AND version=1)
 OR (status='accepted' AND accepted_at IS NOT NULL AND revoked_at IS NULL AND revoked_by IS NULL AND version=2)
 OR (status='revoked' AND revoked_at IS NOT NULL AND revoked_by IS NOT NULL AND version=CASE WHEN accepted_at IS NULL THEN 2 ELSE 3 END))
);
CREATE INDEX patient_consent_authority_page ON patient_consent_authorities(tenant_id,hospital_id,subject,created_at DESC,id DESC);
CREATE INDEX patient_consent_authority_patient ON patient_consent_authorities(tenant_id,patient_id);
CREATE INDEX patient_consent_authority_review ON patient_consent_authorities(tenant_id,review_request_id);
ALTER TABLE patient_consent_authorities ENABLE ROW LEVEL SECURITY;
ALTER TABLE patient_consent_authorities FORCE ROW LEVEL SECURITY;
CREATE FUNCTION patient_consent_subject_allowed(row_tenant uuid,row_hospital uuid,row_subject text) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT row_tenant=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND row_hospital=NULLIF(current_setting('app.patient_hospital',true),'')::uuid
 AND row_subject=NULLIF(current_setting('app.patient_consent_subject',true),'')
 AND row_subject=NULLIF(current_setting('app.actor_subject',true),'')
$$;
CREATE POLICY tenant_isolation ON patient_consent_authorities
 USING(hms_tenant_visible(tenant_id) AND (patient_hospital_allowed(tenant_id,hospital_id,true) OR patient_consent_subject_allowed(tenant_id,hospital_id,subject)))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND (patient_hospital_allowed(tenant_id,hospital_id,true) OR patient_consent_subject_allowed(tenant_id,hospital_id,subject)));
ALTER POLICY tenant_isolation ON patient_link_reviews
 USING(hms_tenant_visible(tenant_id) AND (patient_hospital_allowed(tenant_id,hospital_id,true) OR patient_review_delivery_allowed(tenant_id,hospital_id,request_id) OR patient_consent_subject_allowed(tenant_id,hospital_id,requester_subject)));
CREATE FUNCTION patient_consent_authority_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'consent authority history retained' USING ERRCODE='23514'; END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.status<>'proposed' OR NEW.version<>1 OR NEW.reviewer_subject IS DISTINCT FROM current_setting('app.actor_subject',true)
  OR NOT patient_hospital_allowed(NEW.tenant_id,NEW.hospital_id,true) OR NOT EXISTS(
   SELECT 1 FROM patient_link_reviews r JOIN patients p ON p.tenant_id=r.tenant_id AND p.id=r.patient_id AND p.hospital_id=r.hospital_id
   WHERE r.tenant_id=NEW.tenant_id AND r.request_id=NEW.review_request_id AND r.hospital_id=NEW.hospital_id
   AND r.patient_id=NEW.patient_id AND r.requester_subject=NEW.subject AND r.relationship='self'
   AND r.status='approved' AND r.decision_id=NEW.decision_id AND r.registration_updated_at=NEW.registration_updated_at
   AND p.updated_at=NEW.registration_updated_at) THEN
   RAISE EXCEPTION 'independent current consent authority review required' USING ERRCODE='23514';
  END IF;
 ELSE
  IF (NEW.id,NEW.tenant_id,NEW.hospital_id,NEW.patient_id,NEW.review_request_id,NEW.decision_id,NEW.subject,NEW.reviewer_subject,NEW.policy_reference,NEW.evidence_reference,NEW.registration_updated_at,NEW.fingerprint,NEW.valid_until,NEW.created_at)
  IS DISTINCT FROM (OLD.id,OLD.tenant_id,OLD.hospital_id,OLD.patient_id,OLD.review_request_id,OLD.decision_id,OLD.subject,OLD.reviewer_subject,OLD.policy_reference,OLD.evidence_reference,OLD.registration_updated_at,OLD.fingerprint,OLD.valid_until,OLD.created_at)
  OR NEW.version<>OLD.version+1 OR OLD.status='revoked' THEN
   RAISE EXCEPTION 'consent authority lineage immutable' USING ERRCODE='23514';
  END IF;
  IF NEW.status='accepted' THEN
   IF OLD.status<>'proposed' OR NOT patient_consent_subject_allowed(NEW.tenant_id,NEW.hospital_id,NEW.subject)
   OR NEW.valid_until<=clock_timestamp() OR NEW.accepted_at IS NULL OR NEW.revoked_at IS NOT NULL OR NEW.revoked_by IS NOT NULL
   OR NOT EXISTS(SELECT 1 FROM patient_link_reviews r WHERE r.tenant_id=NEW.tenant_id AND r.request_id=NEW.review_request_id AND r.status='approved' AND r.decision_id=NEW.decision_id) THEN
    RAISE EXCEPTION 'current subject acceptance required' USING ERRCODE='23514';
   END IF;
  ELSIF NEW.status='revoked' THEN
   IF NEW.accepted_at IS DISTINCT FROM OLD.accepted_at OR NEW.revoked_at IS NULL
   OR NEW.revoked_by IS DISTINCT FROM current_setting('app.actor_subject',true) THEN
    RAISE EXCEPTION 'attributable revocation required' USING ERRCODE='23514';
   END IF;
  ELSE RAISE EXCEPTION 'invalid consent authority transition' USING ERRCODE='23514';
  END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER patient_consent_authority_guard BEFORE INSERT OR UPDATE OR DELETE ON patient_consent_authorities FOR EACH ROW EXECUTE FUNCTION patient_consent_authority_guard();
CREATE TRIGGER patient_consent_authority_audit AFTER INSERT OR UPDATE ON patient_consent_authorities FOR EACH ROW EXECUTE FUNCTION audit_record_change();
