SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

ALTER TABLE workforce_documents ADD COLUMN frozen boolean NOT NULL DEFAULT false;
ALTER TABLE workforce_documents DROP CONSTRAINT workforce_documents_record_kind_check;
ALTER TABLE workforce_documents ADD CONSTRAINT workforce_documents_record_kind_check
 CHECK(record_kind IN('enrollment','credential','qualification','tax_declaration'));
ALTER TABLE workforce_documents DROP CONSTRAINT workforce_documents_category_check;
ALTER TABLE workforce_documents ADD CONSTRAINT workforce_documents_category_check
 CHECK(category IN('employment_evidence','qualification_evidence','credential_evidence','identity_evidence','tax_declaration_evidence'));
ALTER TABLE workforce_documents DROP CONSTRAINT workforce_documents_check;
ALTER TABLE workforce_documents ADD CONSTRAINT workforce_documents_record_binding
 CHECK((record_kind='enrollment' AND record_id=staff_id AND category IN('employment_evidence','identity_evidence'))
  OR(record_kind='credential' AND category='credential_evidence') OR(record_kind='qualification' AND category='qualification_evidence')
  OR(record_kind='tax_declaration' AND category='tax_declaration_evidence'));
ALTER TABLE workforce_documents ADD CONSTRAINT workforce_documents_frozen_kind CHECK(NOT frozen OR record_kind='tax_declaration');
CREATE INDEX workforce_documents_record ON workforce_documents(tenant_id,hospital_id,record_id) WHERE record_kind='tax_declaration';

CREATE OR REPLACE FUNCTION workforce_document_creation() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE revision record;
BEGIN
 IF NEW.status<>'upload_pending' OR NEW.uploaded_at IS NOT NULL OR NEW.source_generation IS NOT NULL OR NEW.released_generation IS NOT NULL
  OR NEW.engine_version IS NOT NULL OR NEW.definition_version IS NOT NULL OR NEW.definitions_updated_at IS NOT NULL OR NEW.scanned_at IS NOT NULL OR NEW.failure_code IS NOT NULL OR NEW.frozen THEN
  RAISE EXCEPTION 'document creation cannot carry a processing verdict' USING ERRCODE='42501';
 END IF;
 IF NEW.record_kind='tax_declaration' THEN
  -- The row lock serialises the count below with concurrent attaches and with the decision.
  SELECT r.status,r.sequence INTO revision FROM workforce_staff_tax_declaration_revisions r
   WHERE r.tenant_id=NEW.tenant_id AND r.hospital_id=NEW.hospital_id AND r.staff_id=NEW.staff_id AND r.id=NEW.record_id FOR NO KEY UPDATE;
  IF NOT FOUND OR revision.sequence<>NEW.record_version THEN
   RAISE EXCEPTION 'document source record binding is invalid' USING ERRCODE='23514';
  END IF;
  IF revision.status<>'pending' THEN
   RAISE EXCEPTION 'evidence attaches only to a pending declaration proposal' USING ERRCODE='23514',CONSTRAINT='workforce_tax_evidence_pending';
  END IF;
  IF (SELECT count(*) FROM workforce_documents d WHERE d.tenant_id=NEW.tenant_id AND d.hospital_id=NEW.hospital_id AND d.record_kind='tax_declaration'
    AND d.record_id=NEW.record_id AND NOT(d.status='upload_pending' AND d.expires_at<=clock_timestamp()))>=10 THEN
   RAISE EXCEPTION 'a declaration proposal takes at most 10 evidence files' USING ERRCODE='23514',CONSTRAINT='workforce_tax_evidence_limit';
  END IF;
  RETURN NEW;
 END IF;
 IF NOT EXISTS(SELECT 1 FROM workforce_staff s WHERE s.tenant_id=NEW.tenant_id AND s.hospital_id=NEW.hospital_id AND s.id=NEW.staff_id AND s.version=NEW.record_version)
  OR(NEW.record_kind='credential' AND NOT EXISTS(SELECT 1 FROM workforce_enrollment_credentials r WHERE r.tenant_id=NEW.tenant_id AND r.hospital_id=NEW.hospital_id AND r.staff_id=NEW.staff_id AND r.id=NEW.record_id))
  OR(NEW.record_kind='qualification' AND NOT EXISTS(SELECT 1 FROM workforce_enrollment_qualifications r WHERE r.tenant_id=NEW.tenant_id AND r.hospital_id=NEW.hospital_id AND r.staff_id=NEW.staff_id AND r.id=NEW.record_id)) THEN
  RAISE EXCEPTION 'document source record binding is invalid' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;

-- Freezing a scan_error file at its proposal's decision keeps its verdict, so it needs no processing identity.
CREATE OR REPLACE FUNCTION workforce_document_identity() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
 processing boolean := workforce_document_worker_allowed(OLD.tenant_id,OLD.hospital_id,OLD.staff_id,OLD.id);
 record_frozen boolean := NEW.frozen AND NOT OLD.frozen;
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
 IF OLD.status IN('scan_pending','scan_error') AND NEW.status IN('available','rejected','scan_error') AND NOT processing
  AND NOT(record_frozen AND OLD.status=NEW.status) THEN
  RAISE EXCEPTION 'processing transition requires the scoped processing identity' USING ERRCODE='42501';
 END IF;
 IF NEW.status='available' AND (NEW.scanned_at<NEW.uploaded_at-interval '5 minutes' OR NEW.scanned_at>clock_timestamp()+interval '5 minutes'
   OR NEW.definitions_updated_at<NEW.scanned_at-interval '24 hours' OR NEW.definitions_updated_at>NEW.scanned_at+interval '5 minutes') THEN
  RAISE EXCEPTION 'invalid scanner provenance' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;

-- A decided proposal's evidence is frozen: it never unfreezes and never re-enters scanning, whoever asks.
CREATE FUNCTION workforce_tax_evidence_frozen() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (OLD.frozen AND NOT NEW.frozen) OR (NEW.frozen AND NEW.status='scan_pending' AND OLD.status<>'scan_pending') THEN
  RAISE EXCEPTION 'evidence of a decided declaration proposal cannot change' USING ERRCODE='23514',CONSTRAINT='workforce_tax_evidence_frozen';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_tax_evidence_frozen BEFORE UPDATE ON workforce_documents FOR EACH ROW
 WHEN(OLD.record_kind='tax_declaration') EXECUTE FUNCTION workforce_tax_evidence_frozen();

-- Revision updates pass RLS only with the payroll write flag, which also makes every evidence row visible here.
CREATE FUNCTION workforce_tax_evidence_decision() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM workforce_documents d WHERE d.tenant_id=NEW.tenant_id AND d.hospital_id=NEW.hospital_id AND d.record_kind='tax_declaration' AND d.record_id=NEW.id
   AND d.status='scan_pending') THEN
  RAISE EXCEPTION 'declaration evidence is still being scanned' USING ERRCODE='23514',CONSTRAINT='workforce_tax_evidence_scanning';
 END IF;
 IF EXISTS(SELECT 1 FROM workforce_documents d WHERE d.tenant_id=NEW.tenant_id AND d.hospital_id=NEW.hospital_id AND d.record_kind='tax_declaration' AND d.record_id=NEW.id
   AND (NOT d.frozen OR (NEW.status<>'approved' AND d.status='available'))) THEN
  RAISE EXCEPTION 'declaration evidence must be frozen, and revoked unless approved, before the decision' USING ERRCODE='23514',CONSTRAINT='workforce_tax_evidence_unsettled';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_tax_evidence_decision BEFORE UPDATE ON workforce_staff_tax_declaration_revisions FOR EACH ROW
 WHEN(OLD.status='pending' AND NEW.status<>'pending') EXECUTE FUNCTION workforce_tax_evidence_decision();

CREATE FUNCTION workforce_payroll_evidence_allowed(row_tenant uuid,row_hospital uuid,writing boolean)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT (COALESCE(current_setting('app.workforce_payroll',true)='authorized',false)
   OR (NOT writing AND COALESCE(current_setting('app.workforce_payroll_read',true)='authorized',false)))
  AND workforce_staff_allowed(row_tenant,row_hospital,writing)
$$;

-- Payroll evidence and HR staff files never share a transaction scope.
ALTER POLICY document_isolation ON workforce_documents
 USING(hms_tenant_visible(tenant_id) AND CASE WHEN current_user='helivanta_document_worker'
  THEN workforce_document_worker_allowed(tenant_id,hospital_id,staff_id,id)
  WHEN record_kind='tax_declaration' THEN workforce_payroll_evidence_allowed(tenant_id,hospital_id,false)
  ELSE workforce_enrollment_allowed(tenant_id,hospital_id) END)
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND CASE WHEN current_user='helivanta_document_worker'
  THEN workforce_document_worker_allowed(tenant_id,hospital_id,staff_id,id)
  WHEN record_kind='tax_declaration' THEN workforce_payroll_evidence_allowed(tenant_id,hospital_id,true)
  ELSE workforce_enrollment_allowed(tenant_id,hospital_id) END);
