SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE OR REPLACE FUNCTION workforce_document_identity() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
 processing boolean := workforce_document_worker_allowed(OLD.tenant_id,OLD.hospital_id,OLD.staff_id,OLD.id);
 record_frozen boolean := NEW.frozen AND NOT OLD.frozen;
 retrying boolean := NOT processing AND OLD.status IN('scan_error','scan_pending') AND NEW.status='scan_pending'
  AND NEW.scan_attempt=OLD.scan_attempt+1
  AND (OLD.status='scan_error' OR OLD.updated_at<=clock_timestamp()-interval '45 minutes');
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
 IF NEW.scan_attempt<>OLD.scan_attempt AND NOT retrying THEN
  RAISE EXCEPTION 'invalid document processing attempt' USING ERRCODE='23514';
 END IF;
 IF OLD.status='scan_error' AND NEW.status='scan_pending' AND NOT processing AND NEW.scan_attempt=OLD.scan_attempt THEN
  RAISE EXCEPTION 'manual retry requires a new attempt' USING ERRCODE='23514';
 END IF;
 IF (NEW.released_generation,NEW.engine_version,NEW.definition_version,NEW.definitions_updated_at,NEW.scanned_at,NEW.failure_code)
   IS DISTINCT FROM(OLD.released_generation,OLD.engine_version,OLD.definition_version,OLD.definitions_updated_at,OLD.scanned_at,OLD.failure_code) THEN
  IF NOT(processing AND OLD.status IN('scan_pending','scan_error')) AND NOT(
    retrying AND NEW.failure_code IS NULL AND (NEW.released_generation,NEW.engine_version,NEW.definition_version,NEW.definitions_updated_at,NEW.scanned_at)
     IS NOT DISTINCT FROM(OLD.released_generation,OLD.engine_version,OLD.definition_version,OLD.definitions_updated_at,OLD.scanned_at)) THEN
   RAISE EXCEPTION 'document verdict requires the scoped processing identity' USING ERRCODE='42501';
  END IF;
 END IF;
 IF OLD.status IN('scan_pending','scan_error') AND NEW.status IN('available','rejected','scan_error') AND NOT processing
  AND NOT(record_frozen AND OLD.status=NEW.status) THEN
  RAISE EXCEPTION 'processing transition requires the scoped processing identity' USING ERRCODE='42501';
 END IF;
 IF NEW.status='available' AND (NEW.scanned_at<NEW.uploaded_at-interval '5 minutes' OR NEW.scanned_at>clock_timestamp()+interval '5 minutes'
   OR NEW.definitions_updated_at<NEW.scanned_at-interval '24 hours' OR NEW.definitions_updated_at>NEW.scanned_at+interval '5 minutes') THEN
  RAISE EXCEPTION 'invalid scanner provenance' USING ERRCODE='23514';
 END IF;
 IF OLD.status='scan_pending' AND NEW.status='scan_pending' AND NOT retrying AND NEW.updated_at IS DISTINCT FROM OLD.updated_at THEN
  RAISE EXCEPTION 'pending document processing clock is immutable' USING ERRCODE='23514';
 END IF;
 IF NEW.status='scan_pending' AND (OLD.status<>'scan_pending' OR retrying) THEN
  NEW.updated_at:=clock_timestamp();
 END IF;
 RETURN NEW;
END $$;


CREATE OR REPLACE FUNCTION workforce_tax_evidence_frozen() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF ((OLD.frozen OR NEW.frozen) AND NEW.scan_attempt<>OLD.scan_attempt) OR (OLD.frozen AND NOT NEW.frozen) OR (NEW.frozen AND NEW.status='scan_pending' AND OLD.status<>'scan_pending') THEN
  RAISE EXCEPTION 'evidence of a decided declaration proposal cannot change' USING ERRCODE='23514',CONSTRAINT='workforce_tax_evidence_frozen';
 END IF;
 RETURN NEW;
END $$;

CREATE OR REPLACE FUNCTION workforce_benefit_evidence_frozen() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF ((OLD.frozen OR NEW.frozen) AND NEW.scan_attempt<>OLD.scan_attempt) OR (OLD.frozen AND NOT NEW.frozen) OR (NEW.frozen AND NEW.status='scan_pending' AND OLD.status<>'scan_pending') THEN
  RAISE EXCEPTION 'evidence of a decided coverage proposal cannot change' USING ERRCODE='23514',CONSTRAINT='workforce_benefit_evidence_frozen';
 END IF;
 RETURN NEW;
END $$;
