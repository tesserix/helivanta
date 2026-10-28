SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
ALTER TABLE practitioner_registration_evidence DROP CONSTRAINT practitioner_registration_evidence_status_check;
ALTER TABLE practitioner_registration_evidence ADD CONSTRAINT practitioner_registration_evidence_status_check CHECK(status IN ('upload_pending','scan_pending'));
ALTER TABLE practitioner_registration_evidence ADD COLUMN uploaded_at timestamptz;
ALTER TABLE practitioner_registration_evidence ADD COLUMN source_generation bigint CHECK(source_generation>0);
ALTER TABLE practitioner_registration_evidence ADD CONSTRAINT practitioner_evidence_quarantine_state CHECK((status='upload_pending' AND source_generation IS NULL) OR (status='scan_pending' AND source_generation IS NOT NULL AND uploaded_at IS NOT NULL));
ALTER TABLE practitioner_registration_evidence ADD CONSTRAINT practitioner_evidence_upload_chronology CHECK(uploaded_at IS NULL OR (uploaded_at>=created_at AND uploaded_at<expires_at));
ALTER POLICY tenant_isolation ON practitioner_registration_evidence WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.practitioner_evidence_manage',true)='true');
CREATE OR REPLACE FUNCTION practitioner_evidence_intent_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE evidence_count bigint;
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'practitioner evidence intent is immutable' USING ERRCODE='23514'; END IF;
 IF TG_OP='UPDATE' THEN
  IF (NEW.tenant_id,NEW.practitioner_id,NEW.registration_id,NEW.revision_id,NEW.id,NEW.content_type,NEW.size,NEW.sha256,NEW.created_by,NEW.created_at,NEW.expires_at) IS DISTINCT FROM (OLD.tenant_id,OLD.practitioner_id,OLD.registration_id,OLD.revision_id,OLD.id,OLD.content_type,OLD.size,OLD.sha256,OLD.created_by,OLD.created_at,OLD.expires_at)
   OR (OLD.uploaded_at IS NOT NULL AND NEW.uploaded_at IS DISTINCT FROM OLD.uploaded_at)
   OR (OLD.source_generation IS NOT NULL AND NEW.source_generation IS DISTINCT FROM OLD.source_generation) THEN RAISE EXCEPTION 'practitioner evidence intent is immutable' USING ERRCODE='23514'; END IF;
  IF current_setting('app.practitioner_evidence_manage',true) IS DISTINCT FROM 'true' OR OLD.status<>'upload_pending' OR OLD.expires_at<=practitioner_registry_now() OR NEW.status NOT IN ('upload_pending','scan_pending')
   OR (NEW.uploaded_at IS DISTINCT FROM OLD.uploaded_at AND (NEW.uploaded_at IS NULL OR NEW.uploaded_at<NEW.created_at OR NEW.uploaded_at>=NEW.expires_at))
   OR (NEW.status='scan_pending' AND (NEW.uploaded_at IS NULL OR NEW.source_generation IS NULL)) THEN RAISE EXCEPTION 'invalid practitioner evidence upload transition' USING ERRCODE='23514',CONSTRAINT='practitioner_evidence_upload'; END IF;
  NEW.updated_at:=practitioner_registry_now(); RETURN NEW;
 END IF;
 IF current_setting('app.practitioner_evidence_manage',true) IS DISTINCT FROM 'true' OR NEW.created_by IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'') OR NEW.status<>'upload_pending' OR NEW.uploaded_at IS NOT NULL OR NEW.source_generation IS NOT NULL THEN RAISE EXCEPTION 'evidence intake authority required' USING ERRCODE='42501'; END IF;
 PERFORM 1 FROM practitioner_registration_revisions WHERE tenant_id=NEW.tenant_id AND practitioner_id=NEW.practitioner_id AND registration_id=NEW.registration_id AND id=NEW.revision_id FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'evidence revision unavailable' USING ERRCODE='23503'; END IF;
 IF NOT EXISTS(SELECT 1 FROM practitioner_registration_evidence WHERE tenant_id=NEW.tenant_id AND id=NEW.id) THEN
  SELECT count(*) INTO evidence_count FROM practitioner_registration_evidence WHERE tenant_id=NEW.tenant_id AND practitioner_id=NEW.practitioner_id AND registration_id=NEW.registration_id AND revision_id=NEW.revision_id;
  IF evidence_count>=10 THEN RAISE EXCEPTION 'registration revision evidence limit' USING ERRCODE='23514',CONSTRAINT='practitioner_evidence_limit'; END IF;
 END IF;
 NEW.created_at:=practitioner_registry_now(); NEW.updated_at:=NEW.created_at; NEW.expires_at:=NEW.created_at+interval '1 hour';
 RETURN NEW;
END $$;

CREATE TRIGGER practitioner_evidence_upload_audit AFTER UPDATE ON practitioner_registration_evidence FOR EACH ROW EXECUTE FUNCTION audit_record_change();
