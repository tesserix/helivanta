SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE TABLE patient_proxy_authorities (
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 tenant_id uuid NOT NULL,
 hospital_id uuid NOT NULL,
 patient_id uuid NOT NULL,
 review_request_id uuid NOT NULL,
 decision_id uuid NOT NULL,
 subject text NOT NULL CHECK(length(btrim(subject)) BETWEEN 1 AND 512),
 reviewer_subject text NOT NULL CHECK(length(btrim(reviewer_subject)) BETWEEN 1 AND 200 AND reviewer_subject<>subject),
 kind text NOT NULL CHECK(kind IN ('parental','appointed_guardian')),
 country_code text NOT NULL CHECK(country_code='IN'),
 time_zone text NOT NULL CHECK(time_zone='Asia/Kolkata'),
 policy_reference uuid NOT NULL CHECK(substring(policy_reference::text,15,1)='7'),
 identity_age_reference uuid NOT NULL CHECK(substring(identity_age_reference::text,15,1)='7'),
 authority_evidence_reference uuid NOT NULL CHECK(substring(authority_evidence_reference::text,15,1)='7'),
 appointment_reference uuid CHECK(appointment_reference IS NULL OR substring(appointment_reference::text,15,1)='7'),
 identity_age_confirmed boolean NOT NULL CHECK(identity_age_confirmed),
 authority_confirmed boolean NOT NULL CHECK(authority_confirmed),
 registration_updated_at timestamptz NOT NULL,
 fingerprint text NOT NULL CHECK(length(fingerprint)=64),
 status text NOT NULL DEFAULT 'proposed' CHECK(status IN ('proposed','accepted','revoked')),
 version bigint NOT NULL DEFAULT 1 CHECK(version>0),
 valid_until timestamptz NOT NULL,
 parental_end_at timestamptz,
 accepted_at timestamptz,
 revoked_at timestamptz,
 revoked_by text,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,id),
 FOREIGN KEY(tenant_id,patient_id) REFERENCES patients(tenant_id,id),
 FOREIGN KEY(tenant_id,review_request_id) REFERENCES patient_link_reviews(tenant_id,request_id),
 FOREIGN KEY(tenant_id,decision_id) REFERENCES patient_link_reviews(tenant_id,decision_id),
 CHECK((kind='parental' AND parental_end_at IS NOT NULL AND valid_until<=parental_end_at AND appointment_reference IS NULL) OR (kind='appointed_guardian' AND parental_end_at IS NULL AND appointment_reference IS NOT NULL)),
 CHECK(valid_until>created_at AND updated_at>=created_at),
 CHECK(accepted_at IS NULL OR accepted_at>=created_at),
 CHECK(revoked_at IS NULL OR (revoked_at>=created_at AND (accepted_at IS NULL OR revoked_at>=accepted_at))),
 CHECK((status='proposed' AND accepted_at IS NULL AND revoked_at IS NULL AND revoked_by IS NULL AND version=1)
 OR (status='accepted' AND accepted_at IS NOT NULL AND revoked_at IS NULL AND revoked_by IS NULL AND version=2)
 OR (status='revoked' AND revoked_at IS NOT NULL AND revoked_by IS NOT NULL AND version=CASE WHEN accepted_at IS NULL THEN 2 ELSE 3 END))
);
CREATE INDEX patient_proxy_authority_scope ON patient_proxy_authorities(tenant_id,hospital_id,id);
CREATE INDEX patient_proxy_authority_page ON patient_proxy_authorities(tenant_id,hospital_id,subject,created_at DESC,id DESC);
CREATE INDEX patient_proxy_authority_patient ON patient_proxy_authorities(tenant_id,patient_id);
CREATE INDEX patient_proxy_authority_review ON patient_proxy_authorities(tenant_id,review_request_id);
CREATE INDEX patient_proxy_authority_decision ON patient_proxy_authorities(tenant_id,decision_id);
ALTER TABLE patient_proxy_authorities ENABLE ROW LEVEL SECURITY;
ALTER TABLE patient_proxy_authorities FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON patient_proxy_authorities
 USING(hms_tenant_visible(tenant_id) AND (patient_hospital_allowed(tenant_id,hospital_id,true) OR patient_consent_subject_allowed(tenant_id,hospital_id,subject)))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND (patient_hospital_allowed(tenant_id,hospital_id,true) OR patient_consent_subject_allowed(tenant_id,hospital_id,subject)));
CREATE FUNCTION patient_proxy_registration_allowed(row_tenant uuid,row_hospital uuid,row_patient uuid) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT row_tenant=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND row_hospital=NULLIF(current_setting('app.patient_hospital',true),'')::uuid
 AND EXISTS(SELECT 1 FROM patient_proxy_authorities a JOIN patient_link_reviews r ON r.tenant_id=a.tenant_id AND r.request_id=a.review_request_id
 WHERE a.tenant_id=row_tenant AND a.hospital_id=row_hospital AND a.patient_id=row_patient
 AND a.id=NULLIF(current_setting('app.patient_proxy_authority',true),'')::uuid
 AND patient_consent_subject_allowed(a.tenant_id,a.hospital_id,a.subject)
 AND a.status IN ('proposed','accepted') AND a.valid_until>clock_timestamp()
 AND r.hospital_id=a.hospital_id AND r.patient_id=a.patient_id AND r.requester_subject=a.subject
 AND r.status='approved' AND r.decision_id=a.decision_id AND r.registration_updated_at=a.registration_updated_at
 AND ((a.kind='parental' AND r.relationship='child') OR (a.kind='appointed_guardian' AND r.relationship='ward')))
$$;
ALTER POLICY tenant_isolation ON patients
 USING(hms_tenant_visible(tenant_id) AND (patient_hospital_allowed(tenant_id,hospital_id,false) OR patient_clinical_self_registration_allowed(tenant_id,hospital_id,id,updated_at) OR patient_clinical_recipient_resource_allowed(tenant_id,hospital_id,'patient',id) OR patient_proxy_registration_allowed(tenant_id,hospital_id,id)));
CREATE FUNCTION patient_proxy_authority_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'proxy authority history retained' USING ERRCODE='23514'; END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.status<>'proposed' OR NEW.version<>1 OR NEW.valid_until<=clock_timestamp()
  OR NEW.reviewer_subject IS DISTINCT FROM current_setting('app.actor_subject',true)
  OR NOT patient_hospital_allowed(NEW.tenant_id,NEW.hospital_id,true) OR NOT EXISTS(
   SELECT 1 FROM patient_link_reviews r JOIN patients p ON p.tenant_id=r.tenant_id AND p.id=r.patient_id AND p.hospital_id=r.hospital_id
   WHERE r.tenant_id=NEW.tenant_id AND r.request_id=NEW.review_request_id AND r.hospital_id=NEW.hospital_id
   AND r.patient_id=NEW.patient_id AND r.requester_subject=NEW.subject
   AND ((NEW.kind='parental' AND r.relationship='child' AND NOT p.dob_estimated AND NEW.parental_end_at=((p.dob+interval '18 years')::date::timestamp AT TIME ZONE NEW.time_zone)) OR (NEW.kind='appointed_guardian' AND r.relationship='ward'))
   AND r.status='approved' AND r.decision_id=NEW.decision_id AND r.registration_updated_at=NEW.registration_updated_at
   AND p.updated_at=NEW.registration_updated_at) THEN
   RAISE EXCEPTION 'independent current guardian review required' USING ERRCODE='23514';
  END IF;
 ELSE
  IF (NEW.id,NEW.tenant_id,NEW.hospital_id,NEW.patient_id,NEW.review_request_id,NEW.decision_id,NEW.subject,NEW.reviewer_subject,NEW.kind,NEW.country_code,NEW.time_zone,NEW.policy_reference,NEW.identity_age_reference,NEW.authority_evidence_reference,NEW.appointment_reference,NEW.identity_age_confirmed,NEW.authority_confirmed,NEW.registration_updated_at,NEW.fingerprint,NEW.valid_until,NEW.parental_end_at,NEW.created_at)
  IS DISTINCT FROM (OLD.id,OLD.tenant_id,OLD.hospital_id,OLD.patient_id,OLD.review_request_id,OLD.decision_id,OLD.subject,OLD.reviewer_subject,OLD.kind,OLD.country_code,OLD.time_zone,OLD.policy_reference,OLD.identity_age_reference,OLD.authority_evidence_reference,OLD.appointment_reference,OLD.identity_age_confirmed,OLD.authority_confirmed,OLD.registration_updated_at,OLD.fingerprint,OLD.valid_until,OLD.parental_end_at,OLD.created_at)
  OR NEW.version<>OLD.version+1 OR OLD.status='revoked' THEN
   RAISE EXCEPTION 'proxy authority lineage immutable' USING ERRCODE='23514';
  END IF;
  IF NEW.status='accepted' THEN
   IF OLD.status<>'proposed' OR NOT patient_consent_subject_allowed(NEW.tenant_id,NEW.hospital_id,NEW.subject)
   OR NEW.valid_until<=clock_timestamp() OR NEW.accepted_at IS NULL OR NEW.revoked_at IS NOT NULL OR NEW.revoked_by IS NOT NULL
   OR NOT EXISTS(SELECT 1 FROM patient_link_reviews r JOIN patients p ON p.tenant_id=r.tenant_id AND p.hospital_id=r.hospital_id AND p.id=r.patient_id
    WHERE r.tenant_id=NEW.tenant_id AND r.request_id=NEW.review_request_id AND r.hospital_id=NEW.hospital_id AND r.patient_id=NEW.patient_id AND r.status='approved' AND r.decision_id=NEW.decision_id AND r.requester_subject=NEW.subject AND r.registration_updated_at=NEW.registration_updated_at AND p.updated_at=NEW.registration_updated_at AND (NEW.kind<>'parental' OR (NOT p.dob_estimated AND NEW.parental_end_at=((p.dob+interval '18 years')::date::timestamp AT TIME ZONE NEW.time_zone)))) THEN
    RAISE EXCEPTION 'current proxy acceptance required' USING ERRCODE='23514';
   END IF;
  ELSIF NEW.status='revoked' THEN
   IF NEW.accepted_at IS DISTINCT FROM OLD.accepted_at OR NEW.revoked_at IS NULL
   OR NEW.revoked_by IS DISTINCT FROM current_setting('app.actor_subject',true) THEN
    RAISE EXCEPTION 'attributable proxy revocation required' USING ERRCODE='23514';
   END IF;
  ELSE RAISE EXCEPTION 'invalid proxy authority transition' USING ERRCODE='23514';
  END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER patient_proxy_authority_guard BEFORE INSERT OR UPDATE OR DELETE ON patient_proxy_authorities FOR EACH ROW EXECUTE FUNCTION patient_proxy_authority_guard();
CREATE TRIGGER patient_proxy_authority_audit AFTER INSERT OR UPDATE ON patient_proxy_authorities FOR EACH ROW EXECUTE FUNCTION audit_record_change();
