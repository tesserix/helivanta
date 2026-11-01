SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

ALTER TABLE workforce_document_contributors ADD COLUMN creation_captured boolean NOT NULL DEFAULT false;

CREATE OR REPLACE FUNCTION workforce_document_contributor_record() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE actor text:=NULLIF(current_setting('app.workforce_subject',true),'');
BEGIN
 IF NEW.record_kind<>'enrollment' OR NEW.category NOT IN ('identity_evidence','employment_evidence') THEN RETURN NULL; END IF;
 IF workforce_document_worker_allowed(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.id) IS TRUE THEN RETURN NULL; END IF;
 IF actor IS NULL THEN RAISE EXCEPTION 'document contributor requires attributable owner command' USING ERRCODE='42501'; END IF;
 INSERT INTO workforce_document_contributors(tenant_id,hospital_id,staff_id,document_id,subject,creation_captured)
 VALUES(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.id,actor,TG_OP='INSERT') ON CONFLICT DO NOTHING;
 RETURN NULL;
END $$;

CREATE OR REPLACE FUNCTION workforce_person_review_complete() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE total bigint;
BEGIN
 SELECT count(*) INTO total FROM workforce_person_review_evidence e WHERE (e.tenant_id,e.hospital_id,e.review_id)=(NEW.tenant_id,NEW.hospital_id,NEW.id);
 IF total NOT BETWEEN 1 AND 10 OR NOT EXISTS(SELECT 1 FROM workforce_person_review_contributors c WHERE (c.tenant_id,c.hospital_id,c.review_id,c.subject)=(NEW.tenant_id,NEW.hospital_id,NEW.id,NEW.proposed_by_subject))
  OR EXISTS(SELECT 1 FROM workforce_person_review_evidence e WHERE (e.tenant_id,e.hospital_id,e.review_id)=(NEW.tenant_id,NEW.hospital_id,NEW.id)
   AND NOT EXISTS(SELECT 1 FROM workforce_document_contributors c WHERE (c.tenant_id,c.hospital_id,c.staff_id,c.document_id)=(e.tenant_id,e.hospital_id,e.staff_id,e.document_id) AND c.creation_captured)) THEN
  RAISE EXCEPTION 'person review requires frozen evidence and complete contributor provenance' USING ERRCODE='23514';
 END IF;
 RETURN NULL;
END $$;

CREATE CONSTRAINT TRIGGER workforce_person_review_approval_provenance AFTER UPDATE ON workforce_person_reviews
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW WHEN (NEW.status='approved') EXECUTE FUNCTION workforce_person_review_complete();
