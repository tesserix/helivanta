SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE workforce_documents (
 tenant_id uuid NOT NULL,hospital_id uuid NOT NULL,id uuid NOT NULL,
 staff_id uuid NOT NULL,record_id uuid NOT NULL,
 record_kind text NOT NULL CHECK(record_kind IN('enrollment','credential','qualification')),
 record_version bigint NOT NULL CHECK(record_version>0),
 category text NOT NULL CHECK(category IN('employment_evidence','qualification_evidence','credential_evidence','identity_evidence')),
 filename text NOT NULL CHECK(length(btrim(filename)) BETWEEN 1 AND 180),
 content_type text NOT NULL CHECK(content_type IN('image/png','image/jpeg')),
 size bigint NOT NULL CHECK(size BETWEEN 1 AND 10485760),sha256 text NOT NULL CHECK(sha256 ~ '^[a-f0-9]{64}$'),
 status text NOT NULL DEFAULT 'upload_pending' CHECK(status IN('upload_pending','scan_pending','available','rejected','scan_error','revoked')),
 uploaded_at timestamptz,source_generation bigint CHECK(source_generation>0),released_generation bigint CHECK(released_generation>0),
 engine_version text,definition_version bigint CHECK(definition_version>0),definitions_updated_at timestamptz,scanned_at timestamptz,
 failure_code text CHECK(failure_code IN('unsupported_content','malware_detected','scanner_unavailable','storage_unavailable')),
 expires_at timestamptz NOT NULL DEFAULT now()+interval '30 minutes',
 created_at timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id,id),
 FOREIGN KEY(tenant_id,hospital_id,staff_id) REFERENCES workforce_staff(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 CHECK((record_kind='enrollment' AND record_id=staff_id AND category IN('employment_evidence','identity_evidence'))
  OR (record_kind='credential' AND category='credential_evidence') OR(record_kind='qualification' AND category='qualification_evidence')),
 CHECK((status='upload_pending' AND source_generation IS NULL AND released_generation IS NULL)
  OR(status IN('scan_pending','rejected','scan_error') AND uploaded_at IS NOT NULL AND source_generation IS NOT NULL AND released_generation IS NULL)
  OR(status IN('available','revoked') AND uploaded_at IS NOT NULL AND source_generation IS NOT NULL AND released_generation IS NOT NULL
     AND engine_version IS NOT NULL AND definition_version IS NOT NULL AND definitions_updated_at IS NOT NULL AND scanned_at IS NOT NULL)),
 CHECK(uploaded_at IS NULL OR uploaded_at>=created_at),CHECK(expires_at>created_at)
);
CREATE INDEX workforce_documents_page ON workforce_documents(tenant_id,hospital_id,staff_id,created_at DESC,id DESC);

CREATE FUNCTION workforce_document_creation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.status<>'upload_pending' OR NEW.uploaded_at IS NOT NULL OR NEW.source_generation IS NOT NULL OR NEW.released_generation IS NOT NULL
  OR NEW.engine_version IS NOT NULL OR NEW.definition_version IS NOT NULL OR NEW.definitions_updated_at IS NOT NULL OR NEW.scanned_at IS NOT NULL OR NEW.failure_code IS NOT NULL THEN
  RAISE EXCEPTION 'document creation cannot carry a processing verdict' USING ERRCODE='42501';
 END IF;
 IF NOT EXISTS(SELECT 1 FROM workforce_staff s WHERE s.tenant_id=NEW.tenant_id AND s.hospital_id=NEW.hospital_id AND s.id=NEW.staff_id AND s.version=NEW.record_version)
  OR(NEW.record_kind='credential' AND NOT EXISTS(SELECT 1 FROM workforce_enrollment_credentials r WHERE r.tenant_id=NEW.tenant_id AND r.hospital_id=NEW.hospital_id AND r.staff_id=NEW.staff_id AND r.id=NEW.record_id))
  OR(NEW.record_kind='qualification' AND NOT EXISTS(SELECT 1 FROM workforce_enrollment_qualifications r WHERE r.tenant_id=NEW.tenant_id AND r.hospital_id=NEW.hospital_id AND r.staff_id=NEW.staff_id AND r.id=NEW.record_id)) THEN
  RAISE EXCEPTION 'document source record binding is invalid' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_document_creation BEFORE INSERT ON workforce_documents FOR EACH ROW EXECUTE FUNCTION workforce_document_creation();

CREATE FUNCTION workforce_document_identity() RETURNS trigger LANGUAGE plpgsql AS $$
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
 IF (NEW.released_generation,NEW.engine_version,NEW.definition_version,NEW.definitions_updated_at,NEW.scanned_at,NEW.failure_code)
   IS DISTINCT FROM(OLD.released_generation,OLD.engine_version,OLD.definition_version,OLD.definitions_updated_at,OLD.scanned_at,OLD.failure_code)
   AND COALESCE(current_setting('app.workforce_document_worker',true),'')<>'authorized' THEN
  RAISE EXCEPTION 'document verdict must be written by its processing activity' USING ERRCODE='42501';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_document_identity BEFORE UPDATE ON workforce_documents FOR EACH ROW EXECUTE FUNCTION workforce_document_identity();
CREATE TRIGGER workforce_document_no_delete BEFORE DELETE OR TRUNCATE ON workforce_documents FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal();
ALTER TABLE workforce_documents ENABLE ROW LEVEL SECURITY;
ALTER TABLE workforce_documents FORCE ROW LEVEL SECURITY;
CREATE POLICY document_isolation ON workforce_documents
 USING(hms_tenant_visible(tenant_id) AND workforce_enrollment_allowed(tenant_id,hospital_id))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND workforce_enrollment_allowed(tenant_id,hospital_id));
CREATE TRIGGER workforce_documents_audit AFTER INSERT OR UPDATE OR DELETE ON workforce_documents FOR EACH ROW EXECUTE FUNCTION audit_record_change();
