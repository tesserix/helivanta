SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE workforce_credential_reviews (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL, staff_id uuid NOT NULL, credential_id uuid NOT NULL,
 base_version bigint NOT NULL CHECK (base_version >= 0),
 status text NOT NULL CHECK (status IN ('pending','approved','rejected','withdrawn')), approved_version bigint,
 "type" text NOT NULL CHECK (length(btrim("type")) BETWEEN 1 AND 80),
 "number" text NOT NULL CHECK (length(btrim("number")) BETWEEN 1 AND 100),
 issuer text NOT NULL CHECK (length(btrim(issuer)) BETWEEN 1 AND 150),
 valid_from date NOT NULL CHECK (valid_from BETWEEN DATE '1900-01-01' AND DATE '2200-12-31'),
 valid_until date NOT NULL CHECK (valid_until BETWEEN DATE '1900-01-01' AND DATE '2200-12-31'),
 evidence_document_ids uuid[] NOT NULL CHECK (cardinality(evidence_document_ids) BETWEEN 1 AND 5 AND array_position(evidence_document_ids, NULL) IS NULL),
 reason text NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 500),
 proposed_by_subject text NOT NULL CHECK (length(proposed_by_subject) BETWEEN 1 AND 200),
 decided_by_subject text CHECK (length(decided_by_subject) BETWEEN 1 AND 200), decided_at timestamptz,
 decision_note text NOT NULL CHECK (length(decision_note) <= 500),
 created_at timestamptz NOT NULL DEFAULT now(),
 CONSTRAINT workforce_credential_review_period CHECK (valid_until >= valid_from),
 CHECK ((status = 'pending') = (decided_by_subject IS NULL) AND (decided_by_subject IS NULL) = (decided_at IS NULL)),
 CHECK ((status = 'approved') = (approved_version IS NOT NULL) AND (approved_version IS NULL OR approved_version = base_version + 1)),
 CONSTRAINT workforce_credential_review_separation CHECK (status NOT IN ('approved','rejected') OR decided_by_subject <> proposed_by_subject),
 CONSTRAINT workforce_credential_review_withdrawn_by_proposer CHECK (status <> 'withdrawn' OR decided_by_subject = proposed_by_subject),
 CONSTRAINT workforce_credential_review_rejection_note CHECK (status <> 'rejected' OR length(btrim(decision_note)) > 0),
 PRIMARY KEY(tenant_id,hospital_id,id),
 UNIQUE(tenant_id,hospital_id,staff_id,credential_id,approved_version),
 UNIQUE(tenant_id,hospital_id,staff_id,credential_id,approved_version,id),
 FOREIGN KEY(tenant_id,hospital_id,staff_id) REFERENCES workforce_staff(tenant_id,hospital_id,id) ON DELETE RESTRICT
);
CREATE UNIQUE INDEX workforce_credential_review_one_pending ON workforce_credential_reviews(tenant_id,hospital_id,staff_id,credential_id) WHERE status = 'pending';
CREATE INDEX workforce_credential_review_history ON workforce_credential_reviews(tenant_id,hospital_id,staff_id,credential_id,created_at DESC,id DESC);

-- The enrollment foreign key is deferred so enrollment can rewrite its rows, yet still cannot drop a reviewed credential.
CREATE TABLE workforce_credential_validity (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, staff_id uuid NOT NULL, credential_id uuid NOT NULL,
 "type" text NOT NULL, "number" text NOT NULL, issuer text NOT NULL,
 valid_from date NOT NULL, valid_until date NOT NULL CHECK (valid_until >= valid_from),
 version bigint NOT NULL CHECK (version > 0), review_id uuid NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id,staff_id,credential_id),
 CONSTRAINT workforce_credential_validity_enrolled FOREIGN KEY(tenant_id,hospital_id,staff_id,credential_id)
  REFERENCES workforce_enrollment_credentials(tenant_id,hospital_id,staff_id,id) DEFERRABLE INITIALLY DEFERRED,
 CONSTRAINT workforce_credential_validity_review FOREIGN KEY(tenant_id,hospital_id,staff_id,credential_id,version,review_id)
  REFERENCES workforce_credential_reviews(tenant_id,hospital_id,staff_id,credential_id,approved_version,id) DEFERRABLE INITIALLY DEFERRED
);
CREATE INDEX workforce_credential_register ON workforce_credential_validity(tenant_id,hospital_id,valid_until,review_id);

CREATE FUNCTION workforce_credential_review_evidence_ok(r workforce_credential_reviews, released boolean) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT count(DISTINCT d.id) = cardinality(r.evidence_document_ids) FROM workforce_documents d
 WHERE (d.tenant_id,d.hospital_id,d.staff_id)=(r.tenant_id,r.hospital_id,r.staff_id) AND d.id = ANY(r.evidence_document_ids)
  AND d.record_kind='credential' AND d.record_id=r.credential_id AND d.category='credential_evidence'
  AND d.status IN ('available', CASE WHEN released THEN 'available' ELSE 'scan_pending' END)
$$;

CREATE FUNCTION workforce_credential_review_proposal() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.status<>'pending' OR NEW.decided_by_subject IS NOT NULL OR NEW.approved_version IS NOT NULL
   OR NEW.base_version IS DISTINCT FROM COALESCE((SELECT v.version FROM workforce_credential_validity v WHERE (v.tenant_id,v.hospital_id,v.staff_id,v.credential_id)=(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.credential_id)),0) THEN
  RAISE EXCEPTION 'a credential review starts pending on the current reviewed version' USING ERRCODE='23514';
 END IF;
 IF NOT EXISTS(SELECT 1 FROM workforce_enrollment_credentials c WHERE (c.tenant_id,c.hospital_id,c.staff_id,c.id,c.type,c.issuer)=(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.credential_id,NEW.type,NEW.issuer)) THEN
  RAISE EXCEPTION 'a credential review names an enrolled credential of the same staff member' USING ERRCODE='23503';
 END IF;
 IF array_length(NEW.evidence_document_ids,1) IS DISTINCT FROM (SELECT count(DISTINCT e) FROM unnest(NEW.evidence_document_ids) e)::int OR NOT workforce_credential_review_evidence_ok(NEW,false) THEN
  RAISE EXCEPTION 'credential review evidence must be this credential''s scanned or scanning files' USING ERRCODE='23514', CONSTRAINT='workforce_credential_review_evidence';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_credential_review_proposal BEFORE INSERT ON workforce_credential_reviews FOR EACH ROW EXECUTE FUNCTION workforce_credential_review_proposal();

CREATE FUNCTION workforce_credential_review_decision() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE allowed text[] := ARRAY['status','approved_version','decided_by_subject','decided_at','decision_note'];
BEGIN
 IF OLD.status<>'pending' OR NEW.status='pending' OR (to_jsonb(NEW) - allowed) IS DISTINCT FROM (to_jsonb(OLD) - allowed) THEN
  RAISE EXCEPTION 'a credential review is decided once and its content is immutable' USING ERRCODE='23514';
 END IF;
 IF NEW.status='approved' AND NOT workforce_credential_review_evidence_ok(NEW,true) THEN
  RAISE EXCEPTION 'every file a credential review cites must be scanned and released before approval' USING ERRCODE='23514', CONSTRAINT='workforce_credential_review_evidence_unavailable';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_credential_review_decision BEFORE UPDATE ON workforce_credential_reviews FOR EACH ROW EXECUTE FUNCTION workforce_credential_review_decision();

-- Only an approval writes reviewed validity, and it copies exactly what the approved review proposed.
CREATE FUNCTION workforce_credential_validity_write() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF COALESCE(current_setting('app.workforce_credential_review',true),'')<>'authorized'
   OR (TG_OP='INSERT' AND NEW.version<>1)
   OR (TG_OP='UPDATE' AND ((NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.credential_id,NEW.created_at) IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.staff_id,OLD.credential_id,OLD.created_at) OR NEW.version<>OLD.version+1))
   OR NOT EXISTS(SELECT 1 FROM workforce_credential_reviews r WHERE (r.tenant_id,r.hospital_id,r.staff_id,r.credential_id,r.id,r.type,r.number,r.issuer,r.valid_from,r.valid_until)
     =(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.credential_id,NEW.review_id,NEW.type,NEW.number,NEW.issuer,NEW.valid_from,NEW.valid_until)) THEN
  RAISE EXCEPTION 'reviewed credential validity changes only by approving a review, one version at a time' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_credential_validity_write BEFORE INSERT OR UPDATE ON workforce_credential_validity FOR EACH ROW EXECUTE FUNCTION workforce_credential_validity_write();

-- Enrollment rows always mirror the reviewed record, so the enrollment form cannot change what a review settled.
CREATE FUNCTION workforce_credential_reviewed_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM workforce_credential_validity v WHERE (v.tenant_id,v.hospital_id,v.staff_id,v.credential_id)=(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.id)
   AND (v.type,v.number,v.issuer,v.valid_from,v.valid_until) IS DISTINCT FROM (NEW.type,NEW.number,NEW.issuer,NEW.valid_from,NEW.valid_until)) THEN
  RAISE EXCEPTION 'a reviewed credential changes only through a credential review' USING ERRCODE='23514', CONSTRAINT='workforce_credential_reviewed';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_credential_reviewed_guard BEFORE INSERT OR UPDATE ON workforce_enrollment_credentials FOR EACH ROW EXECUTE FUNCTION workforce_credential_reviewed_guard();

DO $$DECLARE tbl text;BEGIN
 FOREACH tbl IN ARRAY ARRAY['workforce_credential_reviews','workforce_credential_validity'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',tbl);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',tbl);
  EXECUTE format('CREATE POLICY enrollment_isolation ON %I USING (hms_tenant_visible(tenant_id) AND workforce_enrollment_allowed(tenant_id,hospital_id)) WITH CHECK(tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND workforce_enrollment_allowed(tenant_id,hospital_id))',tbl);
  EXECUTE format('CREATE TRIGGER workforce_credential_no_delete BEFORE DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal()',tbl);
 END LOOP;
END $$;
