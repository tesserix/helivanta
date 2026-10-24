SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE TABLE patient_link_reviews (
 tenant_id uuid NOT NULL,
 request_id uuid NOT NULL,
 id uuid GENERATED ALWAYS AS (request_id) STORED,
 account_id uuid NOT NULL,
 requester_subject text NOT NULL CHECK(length(btrim(requester_subject)) BETWEEN 1 AND 512),
 hospital_id uuid NOT NULL,
 patient_id uuid NOT NULL,
 relationship text NOT NULL CHECK(relationship IN ('self','child','parent','spouse','sibling','ward','other')),
 status text NOT NULL CHECK(status IN ('pending','approved','rejected','withdrawn')),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,request_id)
);
CREATE INDEX patient_link_reviews_page ON patient_link_reviews(tenant_id,hospital_id,created_at DESC,id DESC);
ALTER TABLE patient_link_reviews ENABLE ROW LEVEL SECURITY;
ALTER TABLE patient_link_reviews FORCE ROW LEVEL SECURITY;
CREATE FUNCTION patient_review_delivery_allowed(row_tenant uuid,row_hospital uuid,row_request uuid) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT row_tenant=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND row_hospital=NULLIF(current_setting('app.patient_review_hospital',true),'')::uuid
 AND row_request=NULLIF(current_setting('app.patient_review_request',true),'')::uuid
 AND current_setting('app.actor_subject',true) IN ('system:patient-registration-review','system:patient-registration-review-withdrawal')
$$;
CREATE POLICY tenant_isolation ON patient_link_reviews
 USING(hms_tenant_visible(tenant_id) AND (patient_hospital_allowed(tenant_id,hospital_id,true) OR patient_review_delivery_allowed(tenant_id,hospital_id,request_id)))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND (patient_hospital_allowed(tenant_id,hospital_id,true) OR patient_review_delivery_allowed(tenant_id,hospital_id,request_id)));
CREATE FUNCTION patient_link_review_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'review history is retained' USING ERRCODE='23514'; END IF;
 IF (NEW.tenant_id,NEW.request_id,NEW.account_id,NEW.requester_subject,NEW.hospital_id,NEW.patient_id,NEW.relationship,NEW.created_at)
 IS DISTINCT FROM (OLD.tenant_id,OLD.request_id,OLD.account_id,OLD.requester_subject,OLD.hospital_id,OLD.patient_id,OLD.relationship,OLD.created_at)
 OR (OLD.status='withdrawn' AND NEW.status<>'withdrawn') THEN
 RAISE EXCEPTION 'review claim is immutable' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER patient_link_review_guard BEFORE UPDATE OR DELETE ON patient_link_reviews FOR EACH ROW EXECUTE FUNCTION patient_link_review_guard();
CREATE TRIGGER patient_link_review_audit AFTER INSERT OR UPDATE ON patient_link_reviews FOR EACH ROW EXECUTE FUNCTION audit_record_change();
ALTER TABLE patient_link_reviews
 ADD COLUMN decision_id uuid CHECK(decision_id IS NULL OR substring(decision_id::text,15,1)='7'),
 ADD COLUMN decision_outcome text CHECK(decision_outcome IN ('approved','rejected')),
 ADD COLUMN decision_reason text,
 ADD COLUMN decision_fingerprint text,
 ADD COLUMN evidence_reference uuid,
 ADD COLUMN reviewer_subject text,
 ADD COLUMN reviewed_at timestamptz,
 ADD COLUMN registration_updated_at timestamptz,
 ADD COLUMN confirmed_patient_id uuid,
 ADD CONSTRAINT patient_link_review_decision_unique UNIQUE(tenant_id,decision_id),
 ADD CONSTRAINT patient_link_review_patient_fk FOREIGN KEY(tenant_id,confirmed_patient_id) REFERENCES patients(tenant_id,id),
 ADD CONSTRAINT patient_link_review_decision_complete CHECK(
 (decision_id IS NULL AND decision_outcome IS NULL AND decision_reason IS NULL AND decision_fingerprint IS NULL AND evidence_reference IS NULL AND reviewer_subject IS NULL AND reviewed_at IS NULL AND registration_updated_at IS NULL AND confirmed_patient_id IS NULL)
 OR (decision_id IS NOT NULL AND decision_outcome IS NOT NULL AND decision_reason IS NOT NULL AND decision_fingerprint IS NOT NULL AND length(decision_fingerprint)=64 AND evidence_reference IS NOT NULL AND substring(evidence_reference::text,15,1)='7' AND reviewer_subject IS NOT NULL AND length(btrim(reviewer_subject)) BETWEEN 1 AND 200 AND reviewer_subject<>requester_subject AND reviewed_at IS NOT NULL AND
 ((decision_outcome='approved' AND decision_reason='identity_confirmed' AND confirmed_patient_id=patient_id AND confirmed_patient_id IS NOT NULL AND registration_updated_at IS NOT NULL)
 OR (decision_outcome='rejected' AND decision_reason IN ('identity_mismatch','registration_missing','insufficient_evidence') AND confirmed_patient_id IS NULL AND registration_updated_at IS NULL)))),
 ADD CONSTRAINT patient_link_review_decision_status CHECK(
 (status='pending' AND decision_id IS NULL) OR (status='withdrawn') OR (status IN ('approved','rejected') AND decision_id IS NOT NULL AND status=decision_outcome));
CREATE INDEX patient_link_review_patient ON patient_link_reviews(tenant_id,confirmed_patient_id) WHERE confirmed_patient_id IS NOT NULL;
CREATE FUNCTION patient_link_decision_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' AND (NEW.decision_id IS NOT NULL OR NEW.status NOT IN ('pending','withdrawn')) THEN
 RAISE EXCEPTION 'review must begin undecided' USING ERRCODE='23514'; END IF;
 IF TG_OP='UPDATE' THEN
  IF (OLD.decision_id IS NOT NULL AND
  (NEW.decision_id,NEW.decision_outcome,NEW.decision_reason,NEW.decision_fingerprint,NEW.evidence_reference,NEW.reviewer_subject,NEW.reviewed_at,NEW.registration_updated_at,NEW.confirmed_patient_id)
  IS DISTINCT FROM (OLD.decision_id,OLD.decision_outcome,OLD.decision_reason,OLD.decision_fingerprint,OLD.evidence_reference,OLD.reviewer_subject,OLD.reviewed_at,OLD.registration_updated_at,OLD.confirmed_patient_id))
  OR (OLD.status<>'pending' AND NEW.status NOT IN (OLD.status,'withdrawn'))
  OR (OLD.status='withdrawn' AND OLD.decision_id IS NULL AND NEW.decision_id IS NOT NULL) THEN
   RAISE EXCEPTION 'review decision is immutable' USING ERRCODE='23514';
  END IF;
  IF OLD.decision_id IS NULL AND NEW.decision_id IS NOT NULL AND
   (NOT patient_hospital_allowed(NEW.tenant_id,NEW.hospital_id,true) OR NEW.reviewer_subject IS DISTINCT FROM current_setting('app.actor_subject',true)) THEN
   RAISE EXCEPTION 'registrar authority required' USING ERRCODE='23514';
  END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER patient_link_decision_guard BEFORE INSERT OR UPDATE ON patient_link_reviews FOR EACH ROW EXECUTE FUNCTION patient_link_decision_guard();
