SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE TABLE mediconnect_review_submissions (
 tenant_id uuid NOT NULL,
 request_id uuid NOT NULL,
 id uuid GENERATED ALWAYS AS (request_id) STORED,
 account_id uuid NOT NULL,
 subject text NOT NULL,
 hospital_id uuid NOT NULL,
 patient_id uuid NOT NULL,
 relationship text NOT NULL,
 status text NOT NULL DEFAULT 'submitted' CHECK (status IN ('submitted','identity_verified','rejected','withdrawn')),
 decision_id uuid CHECK (decision_id IS NULL OR substring(decision_id::text,15,1)='7'),
 decision_outcome text CHECK (decision_outcome IN ('approved','rejected')),
 reviewed_at timestamptz,
 withdrawn_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,request_id),
 FOREIGN KEY (tenant_id,request_id) REFERENCES mediconnect_link_requests(tenant_id,id),
 FOREIGN KEY (tenant_id,account_id,subject) REFERENCES mediconnect_accounts(tenant_id,id,subject),
 CHECK ((decision_id IS NULL AND decision_outcome IS NULL AND reviewed_at IS NULL) OR
        (decision_id IS NOT NULL AND decision_outcome IS NOT NULL AND reviewed_at IS NOT NULL)),
 CHECK ((status='submitted' AND decision_id IS NULL AND withdrawn_at IS NULL) OR
        (status='identity_verified' AND decision_outcome='approved' AND decision_id IS NOT NULL AND withdrawn_at IS NULL) OR
        (status='rejected' AND decision_outcome='rejected' AND decision_id IS NOT NULL AND withdrawn_at IS NULL) OR
        (status='withdrawn' AND withdrawn_at IS NOT NULL))
);
CREATE INDEX ON mediconnect_review_submissions(tenant_id,account_id,subject);
ALTER TABLE mediconnect_review_submissions ENABLE ROW LEVEL SECURITY;
ALTER TABLE mediconnect_review_submissions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON mediconnect_review_submissions
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND (subject=NULLIF(current_setting('app.actor_subject',true),'') OR
 (request_id=NULLIF(current_setting('app.mediconnect_review_request',true),'')::uuid
 AND current_setting('app.actor_subject',true)='system:mediconnect-registration-reviewed')))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND (subject=NULLIF(current_setting('app.actor_subject',true),'') OR
 (request_id=NULLIF(current_setting('app.mediconnect_review_request',true),'')::uuid
 AND current_setting('app.actor_subject',true)='system:mediconnect-registration-reviewed')));
CREATE FUNCTION mediconnect_review_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'review history is retained' USING ERRCODE='23514'; END IF;
 IF TG_OP='INSERT' THEN
  IF NOT EXISTS(SELECT 1 FROM mediconnect_link_requests r WHERE
   (r.tenant_id,r.id,r.account_id,r.subject,r.hospital_id,r.patient_id,r.relationship)=
   (NEW.tenant_id,NEW.request_id,NEW.account_id,NEW.subject,NEW.hospital_id,NEW.patient_id,NEW.relationship)
   AND r.status='pending_verification') OR NEW.status<>'submitted' THEN
   RAISE EXCEPTION 'review request lineage invalid' USING ERRCODE='23514';
  END IF;
 ELSE
  IF (NEW.tenant_id,NEW.request_id,NEW.account_id,NEW.subject,NEW.hospital_id,NEW.patient_id,NEW.relationship,NEW.created_at)
   IS DISTINCT FROM (OLD.tenant_id,OLD.request_id,OLD.account_id,OLD.subject,OLD.hospital_id,OLD.patient_id,OLD.relationship,OLD.created_at)
   OR (OLD.status='withdrawn' AND NEW.status<>'withdrawn')
   OR (OLD.status<>'submitted' AND NEW.status NOT IN (OLD.status,'withdrawn'))
   OR (OLD.decision_id IS NOT NULL AND (NEW.decision_id,NEW.decision_outcome,NEW.reviewed_at) IS DISTINCT FROM (OLD.decision_id,OLD.decision_outcome,OLD.reviewed_at))
   OR (OLD.withdrawn_at IS NOT NULL AND NEW.withdrawn_at IS DISTINCT FROM OLD.withdrawn_at) THEN
   RAISE EXCEPTION 'review history is immutable' USING ERRCODE='23514';
  END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER mediconnect_review_guard BEFORE INSERT OR UPDATE OR DELETE ON mediconnect_review_submissions
 FOR EACH ROW EXECUTE FUNCTION mediconnect_review_guard();
CREATE TRIGGER mediconnect_review_audit AFTER INSERT OR UPDATE ON mediconnect_review_submissions
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();
