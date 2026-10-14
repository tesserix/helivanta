SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

ALTER TABLE workforce_documents ADD COLUMN scan_attempt bigint NOT NULL DEFAULT 1 CHECK(scan_attempt>0);
ALTER TABLE workforce_documents DROP CONSTRAINT workforce_documents_failure_code_check;
ALTER TABLE workforce_documents ADD CONSTRAINT workforce_documents_failure_code_check
 CHECK(failure_code IN('unsupported_content','malware_detected','scanner_unavailable','storage_unavailable','integrity_mismatch'));

CREATE FUNCTION workforce_document_worker_allowed(tenant uuid,hospital uuid,staff uuid,file uuid)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT current_user='helivanta_document_worker'
  AND tenant=NULLIF(current_setting('app.tenant_id',true),'')::uuid
  AND hospital=NULLIF(current_setting('app.workforce_hospital',true),'')::uuid
  AND staff=NULLIF(current_setting('app.workforce_document_staff',true),'')::uuid
  AND file=NULLIF(current_setting('app.workforce_document_file',true),'')::uuid;
$$;

CREATE OR REPLACE FUNCTION workforce_document_identity() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
 processing boolean := workforce_document_worker_allowed(OLD.tenant_id,OLD.hospital_id,OLD.staff_id,OLD.id);
BEGIN
 IF (NEW.tenant_id,NEW.hospital_id,NEW.id,NEW.staff_id,NEW.record_id,NEW.record_kind,NEW.record_version,NEW.category,NEW.filename,NEW.content_type,NEW.size,NEW.sha256,NEW.created_at,NEW.expires_at)
  IS DISTINCT FROM(OLD.tenant_id,OLD.hospital_id,OLD.id,OLD.staff_id,OLD.record_id,OLD.record_kind,OLD.record_version,OLD.category,OLD.filename,OLD.content_type,OLD.size,OLD.sha256,OLD.created_at,OLD.expires_at)
  OR(OLD.uploaded_at IS NOT NULL AND NEW.uploaded_at IS DISTINCT FROM OLD.uploaded_at)
  OR(OLD.source_generation IS NOT NULL AND NEW.source_generation IS DISTINCT FROM OLD.source_generation)
  OR(OLD.released_generation IS NOT NULL AND NEW.released_generation IS DISTINCT FROM OLD.released_generation) THEN
  RAISE EXCEPTION 'document ownership and storage provenance are immutable' USING ERRCODE='23514';
 END IF;
 IF OLD.status<>NEW.status AND NOT((OLD.status='upload_pending' AND NEW.status='scan_pending')
   OR(OLD.status='scan_pending' AND NEW.status IN('available','rejected','scan_error'))
   OR(OLD.status='scan_error' AND NEW.status='scan_pending') OR(OLD.status='available' AND NEW.status='revoked')) THEN
  RAISE EXCEPTION 'invalid document state transition' USING ERRCODE='23514';
 END IF;
 IF NEW.scan_attempt<>OLD.scan_attempt AND NOT(OLD.status='scan_error' AND NEW.status='scan_pending' AND NEW.scan_attempt=OLD.scan_attempt+1 AND NOT processing) THEN
  RAISE EXCEPTION 'invalid document processing attempt' USING ERRCODE='23514';
 END IF;
 IF OLD.status='scan_error' AND NEW.status='scan_pending' AND NOT processing AND NEW.scan_attempt=OLD.scan_attempt THEN
  RAISE EXCEPTION 'manual retry requires a new attempt' USING ERRCODE='23514';
 END IF;
 IF (NEW.released_generation,NEW.engine_version,NEW.definition_version,NEW.definitions_updated_at,NEW.scanned_at,NEW.failure_code)
   IS DISTINCT FROM(OLD.released_generation,OLD.engine_version,OLD.definition_version,OLD.definitions_updated_at,OLD.scanned_at,OLD.failure_code) THEN
  IF NOT(processing AND OLD.status IN('scan_pending','scan_error')) AND NOT(
    OLD.status='scan_error' AND NEW.status='scan_pending' AND NEW.scan_attempt=OLD.scan_attempt+1
    AND NEW.failure_code IS NULL AND (NEW.released_generation,NEW.engine_version,NEW.definition_version,NEW.definitions_updated_at,NEW.scanned_at)
     IS NOT DISTINCT FROM(OLD.released_generation,OLD.engine_version,OLD.definition_version,OLD.definitions_updated_at,OLD.scanned_at)) THEN
   RAISE EXCEPTION 'document verdict requires the scoped processing identity' USING ERRCODE='42501';
  END IF;
 END IF;
 IF OLD.status IN('scan_pending','scan_error') AND NEW.status IN('available','rejected','scan_error') AND NOT processing THEN
  RAISE EXCEPTION 'processing transition requires the scoped processing identity' USING ERRCODE='42501';
 END IF;
 IF NEW.status='available' AND (NEW.scanned_at<NEW.uploaded_at-interval '5 minutes' OR NEW.scanned_at>clock_timestamp()+interval '5 minutes'
   OR NEW.definitions_updated_at<NEW.scanned_at-interval '24 hours' OR NEW.definitions_updated_at>NEW.scanned_at+interval '5 minutes') THEN
  RAISE EXCEPTION 'invalid scanner provenance' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;

ALTER POLICY document_isolation ON workforce_documents
 USING(hms_tenant_visible(tenant_id) AND CASE WHEN current_user='helivanta_document_worker'
  THEN workforce_document_worker_allowed(tenant_id,hospital_id,staff_id,id)
  ELSE workforce_enrollment_allowed(tenant_id,hospital_id) END)
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND CASE WHEN current_user='helivanta_document_worker'
  THEN workforce_document_worker_allowed(tenant_id,hospital_id,staff_id,id)
  ELSE workforce_enrollment_allowed(tenant_id,hospital_id) END);

GRANT USAGE ON SCHEMA public TO helivanta_document_worker;
GRANT SELECT(tenant_id,hospital_id,id,staff_id,record_id,record_kind,record_version,content_type,size,sha256,status,scan_attempt,
 uploaded_at,source_generation,released_generation,engine_version,definition_version,definitions_updated_at,scanned_at,failure_code,expires_at,created_at,updated_at)
 ON workforce_documents TO helivanta_document_worker;
GRANT UPDATE(status,released_generation,engine_version,definition_version,definitions_updated_at,scanned_at,failure_code,updated_at)
 ON workforce_documents TO helivanta_document_worker;
GRANT INSERT ON audit_events,outbox_events TO helivanta_document_worker;
GRANT SELECT(id) ON outbox_events TO helivanta_document_worker;

ALTER POLICY tenant_isolation ON audit_events
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
  AND CASE WHEN current_user='helivanta_document_worker' THEN
   actor_subject='service:workforce-document-worker' AND resource_type='workforce_documents'
   AND resource_id=NULLIF(current_setting('app.workforce_document_file',true),'')::uuid
   AND action IN('read','update')
  ELSE true END);

ALTER POLICY tenant_isolation ON outbox_events
 WITH CHECK((tenant_id IS NULL OR tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid)
  AND CASE WHEN current_user='helivanta_document_worker' THEN
   tenant_id IS NOT NULL AND subject='helivanta.in.workforce.document_changed.v1'
   AND payload->>'tenant_id'=tenant_id::text AND payload->>'type'=subject
   AND payload->'data'->>'hospital_id'=current_setting('app.workforce_hospital',true)
   AND payload->'data'->>'staff_id'=current_setting('app.workforce_document_staff',true)
   AND payload->'data'->>'file_id'=current_setting('app.workforce_document_file',true)
   AND EXISTS(SELECT 1 FROM workforce_documents d WHERE d.tenant_id=outbox_events.tenant_id
    AND workforce_document_worker_allowed(d.tenant_id,d.hospital_id,d.staff_id,d.id)
    AND d.status=payload->'data'->>'status' AND d.scan_attempt::text=payload->'data'->>'scan_attempt')
  ELSE true END);
