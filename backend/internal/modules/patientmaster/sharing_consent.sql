SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE TABLE patient_sharing_recipients (
 tenant_id uuid NOT NULL,
 hospital_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 organization_reference text NOT NULL CHECK(length(btrim(organization_reference)) BETWEEN 1 AND 200),
 display_name text NOT NULL CHECK(length(btrim(display_name)) BETWEEN 1 AND 200),
 grantee_subject text NOT NULL CHECK(length(btrim(grantee_subject)) BETWEEN 1 AND 512),
 policy_reference uuid NOT NULL CHECK(substring(policy_reference::text,15,1)='7'),
 identity_evidence_reference uuid NOT NULL CHECK(substring(identity_evidence_reference::text,15,1)='7'),
 valid_until timestamptz NOT NULL CHECK(valid_until<>'infinity'::timestamptz),
 content_hash text NOT NULL CHECK(content_hash ~ '^[0-9a-f]{64}$'),
 creator_subject text NOT NULL CHECK(length(btrim(creator_subject)) BETWEEN 1 AND 512),
 reviewer_subject text,
 review_reference uuid CHECK(substring(review_reference::text,15,1)='7'),
 revoker_subject text,
 revocation_reference uuid CHECK(substring(revocation_reference::text,15,1)='7'),
 status text NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','approved','revoked')),
 version bigint NOT NULL DEFAULT 1,
 approved_at timestamptz,
 revoked_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,id),
 CHECK(valid_until>created_at),
 CHECK(updated_at>=created_at),
 CHECK(approved_at IS NULL OR approved_at>=created_at),
 CHECK(revoked_at IS NULL OR (revoked_at>=created_at AND (approved_at IS NULL OR revoked_at>=approved_at))),
 CHECK((status='draft' AND version=1 AND approved_at IS NULL AND revoked_at IS NULL)
 OR (status='approved' AND version=2 AND approved_at IS NOT NULL AND revoked_at IS NULL)
 OR (status='revoked' AND version=CASE WHEN approved_at IS NULL THEN 2 ELSE 3 END AND revoked_at IS NOT NULL)),
 CHECK((approved_at IS NULL AND reviewer_subject IS NULL AND review_reference IS NULL)
 OR (approved_at IS NOT NULL AND reviewer_subject IS NOT NULL AND length(btrim(reviewer_subject)) BETWEEN 1 AND 512 AND reviewer_subject<>creator_subject AND reviewer_subject<>grantee_subject AND review_reference IS NOT NULL)),
 CHECK((revoked_at IS NULL AND revoker_subject IS NULL AND revocation_reference IS NULL)
 OR (revoked_at IS NOT NULL AND revoker_subject IS NOT NULL AND length(btrim(revoker_subject)) BETWEEN 1 AND 512 AND revocation_reference IS NOT NULL))
);
CREATE UNIQUE INDEX patient_sharing_recipient_current_binding ON patient_sharing_recipients(tenant_id,hospital_id,organization_reference,grantee_subject) WHERE status IN ('draft','approved');
CREATE INDEX patient_sharing_recipient_page ON patient_sharing_recipients(tenant_id,hospital_id,created_at DESC,id DESC);
CREATE INDEX patient_sharing_recipient_approved_page ON patient_sharing_recipients(tenant_id,hospital_id,created_at DESC,id DESC) WHERE status='approved';
ALTER TABLE patient_sharing_recipients ENABLE ROW LEVEL SECURITY;
ALTER TABLE patient_sharing_recipients FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON patient_sharing_recipients
 USING(hms_tenant_visible(tenant_id) AND patient_hospital_allowed(tenant_id,hospital_id,true))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND patient_hospital_allowed(tenant_id,hospital_id,true));
CREATE FUNCTION patient_sharing_recipient_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'recipient history retained' USING ERRCODE='23514'; END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.status<>'draft' OR NEW.creator_subject IS DISTINCT FROM current_setting('app.actor_subject',true) THEN
   RAISE EXCEPTION 'attributable recipient draft required' USING ERRCODE='23514';
  END IF;
 ELSE
  IF (NEW.tenant_id,NEW.hospital_id,NEW.id,NEW.organization_reference,NEW.display_name,NEW.grantee_subject,NEW.policy_reference,NEW.identity_evidence_reference,NEW.valid_until,NEW.content_hash,NEW.creator_subject,NEW.created_at)
  IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.id,OLD.organization_reference,OLD.display_name,OLD.grantee_subject,OLD.policy_reference,OLD.identity_evidence_reference,OLD.valid_until,OLD.content_hash,OLD.creator_subject,OLD.created_at)
  OR NEW.version<>OLD.version+1 OR OLD.status='revoked' OR NEW.updated_at<OLD.updated_at THEN
   RAISE EXCEPTION 'recipient lineage immutable' USING ERRCODE='23514';
  END IF;
  IF NEW.status='approved' THEN
   IF OLD.status<>'draft' OR NEW.valid_until<=clock_timestamp() OR NEW.reviewer_subject IS DISTINCT FROM current_setting('app.actor_subject',true) OR NEW.reviewer_subject=OLD.creator_subject OR NEW.reviewer_subject=OLD.grantee_subject THEN
    RAISE EXCEPTION 'independent recipient review required' USING ERRCODE='23514';
   END IF;
  ELSIF NEW.status='revoked' THEN
   IF NEW.revoker_subject IS DISTINCT FROM current_setting('app.actor_subject',true) OR (NEW.reviewer_subject,NEW.review_reference,NEW.approved_at) IS DISTINCT FROM (OLD.reviewer_subject,OLD.review_reference,OLD.approved_at) THEN
    RAISE EXCEPTION 'recipient review history retained' USING ERRCODE='23514';
   END IF;
  ELSE RAISE EXCEPTION 'invalid recipient transition' USING ERRCODE='23514';
  END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER patient_sharing_recipient_guard BEFORE INSERT OR UPDATE OR DELETE ON patient_sharing_recipients FOR EACH ROW EXECUTE FUNCTION patient_sharing_recipient_guard();
CREATE TRIGGER patient_sharing_recipient_audit AFTER INSERT OR UPDATE ON patient_sharing_recipients FOR EACH ROW EXECUTE FUNCTION audit_record_change();

CREATE FUNCTION patient_clinical_self_registration_allowed(row_tenant uuid,row_hospital uuid,row_patient uuid,row_updated timestamptz) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT row_tenant=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND row_hospital=NULLIF(current_setting('app.patient_hospital',true),'')::uuid
 AND EXISTS(SELECT 1 FROM patient_consent_authorities a JOIN patient_link_reviews r ON r.tenant_id=a.tenant_id AND r.request_id=a.review_request_id
 WHERE a.tenant_id=row_tenant AND a.hospital_id=row_hospital AND a.patient_id=row_patient
 AND a.id=NULLIF(current_setting('app.patient_clinical_authority',true),'')::uuid
 AND patient_consent_subject_allowed(a.tenant_id,a.hospital_id,a.subject)
 AND a.status='accepted' AND a.version=2 AND a.valid_until>clock_timestamp()
 AND a.registration_updated_at=row_updated AND r.status='approved' AND r.decision_id=a.decision_id
 AND r.requester_subject=a.subject AND r.relationship='self' AND r.confirmed_patient_id=a.patient_id AND r.registration_updated_at=a.registration_updated_at)
$$;
ALTER POLICY tenant_isolation ON patients
 USING(hms_tenant_visible(tenant_id) AND (patient_hospital_allowed(tenant_id,hospital_id,false) OR patient_clinical_self_registration_allowed(tenant_id,hospital_id,id,updated_at)));
CREATE FUNCTION patient_clinical_self_allowed(row_tenant uuid,row_hospital uuid) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT row_tenant=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND row_hospital=NULLIF(current_setting('app.patient_hospital',true),'')::uuid
 AND EXISTS(SELECT 1 FROM patient_consent_authorities a JOIN patients p ON p.tenant_id=a.tenant_id AND p.id=a.patient_id AND p.hospital_id=a.hospital_id AND p.updated_at=a.registration_updated_at
 WHERE a.tenant_id=row_tenant AND a.hospital_id=row_hospital AND a.id=NULLIF(current_setting('app.patient_clinical_authority',true),'')::uuid
 AND patient_consent_subject_allowed(a.tenant_id,a.hospital_id,a.subject) AND a.status='accepted' AND a.version=2 AND a.valid_until>clock_timestamp())
$$;
ALTER POLICY tenant_isolation ON patient_sharing_notices
 USING(hms_tenant_visible(tenant_id) AND (patient_hospital_allowed(tenant_id,hospital_id,true) OR (status='approved' AND patient_clinical_self_allowed(tenant_id,hospital_id))));
ALTER POLICY tenant_isolation ON patient_sharing_recipients
 USING(hms_tenant_visible(tenant_id) AND (patient_hospital_allowed(tenant_id,hospital_id,true)
 OR (status='approved' AND valid_until>clock_timestamp() AND patient_clinical_self_allowed(tenant_id,hospital_id))
 OR (hospital_id=NULLIF(current_setting('app.patient_hospital',true),'')::uuid AND grantee_subject=NULLIF(current_setting('app.actor_subject',true),'') AND grantee_subject=NULLIF(current_setting('app.patient_clinical_recipient',true),''))));

CREATE TABLE patient_clinical_consents (
 tenant_id uuid NOT NULL,
 hospital_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 patient_id uuid NOT NULL,
 subject text NOT NULL CHECK(length(btrim(subject)) BETWEEN 1 AND 512),
 authority_id uuid NOT NULL,
 review_request_id uuid NOT NULL,
 notice_id uuid NOT NULL,
 recipient_id uuid NOT NULL,
 purpose text NOT NULL CHECK(purpose='care_coordination'),
 fingerprint text NOT NULL CHECK(fingerprint ~ '^[0-9a-f]{64}$'),
 body text NOT NULL CHECK(octet_length(body) BETWEEN 2 AND 16384 AND jsonb_typeof(body::jsonb)='object'),
 hmac text NOT NULL CHECK(hmac ~ '^vault:v1:[A-Za-z0-9+/]{43}=$'),
 status text NOT NULL DEFAULT 'granted' CHECK(status IN ('granted','withdrawn','quarantined')),
 version bigint NOT NULL DEFAULT 1 CHECK(version BETWEEN 1 AND 3),
 valid_until timestamptz NOT NULL CHECK(valid_until<>'infinity'::timestamptz),
 captured_at timestamptz NOT NULL,
 withdrawn_at timestamptz,
 withdrawn_by text,
 quarantined_at timestamptz,
 quarantined_by text,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,id),
 FOREIGN KEY(tenant_id,patient_id) REFERENCES patients(tenant_id,id),
 FOREIGN KEY(tenant_id,authority_id) REFERENCES patient_consent_authorities(tenant_id,id),
 FOREIGN KEY(tenant_id,review_request_id) REFERENCES patient_link_reviews(tenant_id,request_id),
 FOREIGN KEY(tenant_id,notice_id) REFERENCES patient_sharing_notices(tenant_id,id),
 FOREIGN KEY(tenant_id,recipient_id) REFERENCES patient_sharing_recipients(tenant_id,id),
 CHECK(valid_until>created_at AND captured_at<=created_at AND updated_at>=created_at),
 CHECK((withdrawn_at IS NULL AND withdrawn_by IS NULL) OR (withdrawn_at>=created_at AND withdrawn_by=subject)),
 CHECK((quarantined_at IS NULL AND quarantined_by IS NULL) OR (quarantined_at>=created_at AND length(btrim(quarantined_by)) BETWEEN 1 AND 512)),
 CHECK((status='granted' AND version=1 AND withdrawn_at IS NULL AND quarantined_at IS NULL)
 OR (status='withdrawn' AND version=2 AND withdrawn_at IS NOT NULL AND quarantined_at IS NULL)
 OR (status='quarantined' AND quarantined_at IS NOT NULL AND version=CASE WHEN withdrawn_at IS NULL THEN 2 ELSE 3 END))
);
CREATE UNIQUE INDEX patient_clinical_consent_current_scope ON patient_clinical_consents(tenant_id,hospital_id,patient_id,subject,recipient_id,purpose) WHERE status='granted';
CREATE INDEX patient_clinical_consent_subject_page ON patient_clinical_consents(tenant_id,hospital_id,subject,created_at DESC,id DESC);
CREATE INDEX patient_clinical_consent_recipient_page ON patient_clinical_consents(tenant_id,hospital_id,recipient_id,created_at DESC,id DESC);
ALTER TABLE patient_clinical_consents ENABLE ROW LEVEL SECURITY;
ALTER TABLE patient_clinical_consents FORCE ROW LEVEL SECURITY;
CREATE FUNCTION patient_clinical_recipient_allowed(row_tenant uuid,row_hospital uuid,row_recipient uuid) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT row_tenant=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND row_hospital=NULLIF(current_setting('app.patient_hospital',true),'')::uuid
 AND current_setting('app.actor_subject',true)=NULLIF(current_setting('app.patient_clinical_recipient',true),'')
 AND EXISTS(SELECT 1 FROM patient_sharing_recipients g WHERE g.tenant_id=row_tenant AND g.hospital_id=row_hospital AND g.id=row_recipient AND g.grantee_subject=current_setting('app.actor_subject',true))
$$;
CREATE POLICY tenant_isolation ON patient_clinical_consents
 USING(hms_tenant_visible(tenant_id) AND (patient_consent_subject_allowed(tenant_id,hospital_id,subject) OR patient_clinical_recipient_allowed(tenant_id,hospital_id,recipient_id)))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND (patient_consent_subject_allowed(tenant_id,hospital_id,subject) OR patient_clinical_recipient_allowed(tenant_id,hospital_id,recipient_id)));
CREATE FUNCTION patient_clinical_consent_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'clinical consent history retained' USING ERRCODE='23514'; END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.status<>'granted' OR NEW.subject IS DISTINCT FROM current_setting('app.actor_subject',true)
  OR (NEW.body::jsonb->>'schema_version',NEW.body::jsonb->>'id',NEW.body::jsonb->>'tenant_id',NEW.body::jsonb->>'hospital_id',NEW.body::jsonb->>'patient_id',NEW.body::jsonb->>'subject',NEW.body::jsonb->>'authority_id',NEW.body::jsonb->>'review_request_id',NEW.body::jsonb->>'notice_id',NEW.body::jsonb->>'recipient_id',NEW.body::jsonb->>'purpose',NEW.body::jsonb->>'fingerprint')
  IS DISTINCT FROM ('1',NEW.id::text,NEW.tenant_id::text,NEW.hospital_id::text,NEW.patient_id::text,NEW.subject,NEW.authority_id::text,NEW.review_request_id::text,NEW.notice_id::text,NEW.recipient_id::text,NEW.purpose,NEW.fingerprint)
  THEN RAISE EXCEPTION 'attributable clinical declaration required' USING ERRCODE='23514'; END IF;
 ELSE
  IF (NEW.tenant_id,NEW.hospital_id,NEW.id,NEW.patient_id,NEW.subject,NEW.authority_id,NEW.review_request_id,NEW.notice_id,NEW.recipient_id,NEW.purpose,NEW.fingerprint,NEW.body,NEW.hmac,NEW.valid_until,NEW.captured_at,NEW.created_at)
  IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.id,OLD.patient_id,OLD.subject,OLD.authority_id,OLD.review_request_id,OLD.notice_id,OLD.recipient_id,OLD.purpose,OLD.fingerprint,OLD.body,OLD.hmac,OLD.valid_until,OLD.captured_at,OLD.created_at)
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
CREATE TRIGGER patient_clinical_consent_guard BEFORE INSERT OR UPDATE OR DELETE ON patient_clinical_consents FOR EACH ROW EXECUTE FUNCTION patient_clinical_consent_guard();
CREATE TRIGGER patient_clinical_consent_audit AFTER INSERT OR UPDATE ON patient_clinical_consents FOR EACH ROW EXECUTE FUNCTION audit_record_change();

CREATE FUNCTION patient_clinical_recipient_resource_allowed(row_tenant uuid,row_hospital uuid,kind text,resource uuid) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT row_tenant=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND row_hospital=NULLIF(current_setting('app.patient_hospital',true),'')::uuid
 AND EXISTS(SELECT 1 FROM patient_clinical_consents c WHERE c.tenant_id=row_tenant AND c.hospital_id=row_hospital
 AND c.id=NULLIF(current_setting('app.patient_clinical_consent',true),'')::uuid AND c.status='granted'
 AND patient_clinical_recipient_allowed(c.tenant_id,c.hospital_id,c.recipient_id)
 AND ((kind='authority' AND c.authority_id=resource) OR (kind='review' AND c.review_request_id=resource) OR (kind='patient' AND c.patient_id=resource) OR (kind='notice' AND c.notice_id=resource)))
$$;
ALTER POLICY tenant_isolation ON patient_consent_authorities
 USING(hms_tenant_visible(tenant_id) AND (patient_hospital_allowed(tenant_id,hospital_id,true) OR patient_consent_subject_allowed(tenant_id,hospital_id,subject) OR patient_clinical_recipient_resource_allowed(tenant_id,hospital_id,'authority',id)));
ALTER POLICY tenant_isolation ON patient_link_reviews
 USING(hms_tenant_visible(tenant_id) AND (patient_hospital_allowed(tenant_id,hospital_id,true) OR patient_review_delivery_allowed(tenant_id,hospital_id,request_id) OR patient_consent_subject_allowed(tenant_id,hospital_id,requester_subject) OR patient_clinical_recipient_resource_allowed(tenant_id,hospital_id,'review',request_id)));
ALTER POLICY tenant_isolation ON patients
 USING(hms_tenant_visible(tenant_id) AND (patient_hospital_allowed(tenant_id,hospital_id,false) OR patient_clinical_self_registration_allowed(tenant_id,hospital_id,id,updated_at) OR patient_clinical_recipient_resource_allowed(tenant_id,hospital_id,'patient',id)));
ALTER POLICY tenant_isolation ON patient_sharing_notices
 USING(hms_tenant_visible(tenant_id) AND (patient_hospital_allowed(tenant_id,hospital_id,true) OR (status='approved' AND patient_clinical_self_allowed(tenant_id,hospital_id)) OR patient_clinical_recipient_resource_allowed(tenant_id,hospital_id,'notice',id)));
CREATE TABLE patient_clinical_consent_decisions (
 tenant_id uuid NOT NULL,
 hospital_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 consent_id uuid NOT NULL,
 consent_version bigint NOT NULL CHECK(consent_version>0),
 actor_subject text NOT NULL CHECK(length(btrim(actor_subject)) BETWEEN 1 AND 512),
 purpose text NOT NULL CHECK(length(btrim(purpose)) BETWEEN 1 AND 64),
 resource_type text NOT NULL CHECK(length(btrim(resource_type)) BETWEEN 1 AND 64),
 decision text NOT NULL CHECK(decision IN ('allow','deny')),
 reason text NOT NULL CHECK(reason IN ('eligible','inactive','scope_mismatch','owner_state_changed','claim_inactive','integrity_failed')),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,id),
 FOREIGN KEY(tenant_id,consent_id) REFERENCES patient_clinical_consents(tenant_id,id)
);
CREATE INDEX patient_clinical_decision_consent ON patient_clinical_consent_decisions(tenant_id,consent_id,created_at DESC,id DESC);
ALTER TABLE patient_clinical_consent_decisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE patient_clinical_consent_decisions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON patient_clinical_consent_decisions
 USING(hms_tenant_visible(tenant_id) AND EXISTS(SELECT 1 FROM patient_clinical_consents c WHERE c.tenant_id=patient_clinical_consent_decisions.tenant_id AND c.id=patient_clinical_consent_decisions.consent_id AND c.hospital_id=patient_clinical_consent_decisions.hospital_id))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND actor_subject=current_setting('app.actor_subject',true) AND EXISTS(SELECT 1 FROM patient_clinical_consents c WHERE c.tenant_id=patient_clinical_consent_decisions.tenant_id AND c.id=patient_clinical_consent_decisions.consent_id AND c.hospital_id=patient_clinical_consent_decisions.hospital_id));
CREATE FUNCTION patient_clinical_decision_history_guard() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'clinical decision history immutable' USING ERRCODE='23514'; END $$;
CREATE TRIGGER patient_clinical_decision_history_guard BEFORE UPDATE OR DELETE ON patient_clinical_consent_decisions FOR EACH ROW EXECUTE FUNCTION patient_clinical_decision_history_guard();
CREATE TRIGGER patient_clinical_decision_audit AFTER INSERT ON patient_clinical_consent_decisions FOR EACH ROW EXECUTE FUNCTION audit_record_change();

CREATE TABLE patient_clinical_integrity_incidents (
 tenant_id uuid NOT NULL,
 hospital_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 consent_id uuid NOT NULL,
 observed_version bigint NOT NULL CHECK(observed_version>0),
 actor_subject text NOT NULL CHECK(length(btrim(actor_subject)) BETWEEN 1 AND 512),
 observed_hash text NOT NULL CHECK(observed_hash ~ '^[0-9a-f]{64}$'),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,id),
 UNIQUE(tenant_id,consent_id),
 FOREIGN KEY(tenant_id,consent_id) REFERENCES patient_clinical_consents(tenant_id,id)
);
ALTER TABLE patient_clinical_integrity_incidents ENABLE ROW LEVEL SECURITY;
ALTER TABLE patient_clinical_integrity_incidents FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON patient_clinical_integrity_incidents
 USING(hms_tenant_visible(tenant_id) AND EXISTS(SELECT 1 FROM patient_clinical_consents c WHERE c.tenant_id=patient_clinical_integrity_incidents.tenant_id AND c.id=patient_clinical_integrity_incidents.consent_id AND c.hospital_id=patient_clinical_integrity_incidents.hospital_id))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND actor_subject=current_setting('app.actor_subject',true) AND EXISTS(SELECT 1 FROM patient_clinical_consents c WHERE c.tenant_id=patient_clinical_integrity_incidents.tenant_id AND c.id=patient_clinical_integrity_incidents.consent_id AND c.hospital_id=patient_clinical_integrity_incidents.hospital_id));
CREATE TRIGGER patient_clinical_incident_history_guard BEFORE UPDATE OR DELETE ON patient_clinical_integrity_incidents FOR EACH ROW EXECUTE FUNCTION patient_clinical_decision_history_guard();
CREATE TRIGGER patient_clinical_incident_audit AFTER INSERT ON patient_clinical_integrity_incidents FOR EACH ROW EXECUTE FUNCTION audit_record_change();
