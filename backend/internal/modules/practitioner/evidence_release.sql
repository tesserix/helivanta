SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
ALTER TABLE practitioner_registration_evidence ADD CONSTRAINT practitioner_evidence_revocation_parent UNIQUE(tenant_id,practitioner_id,registration_id,revision_id,id);
CREATE TABLE practitioner_evidence_revocations (
 tenant_id uuid NOT NULL,
 practitioner_id uuid NOT NULL,
 registration_id uuid NOT NULL,
 revision_id uuid NOT NULL,
 file_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7' AND substring(id::text,20,1) IN ('8','9','a','b')),
 review_reference uuid NOT NULL CHECK(substring(review_reference::text,15,1)='7' AND substring(review_reference::text,20,1) IN ('8','9','a','b')),
 status text NOT NULL DEFAULT 'revoked' CHECK(status='revoked'),
 created_by text NOT NULL CHECK(octet_length(created_by) BETWEEN 1 AND 512),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,id),
 UNIQUE(tenant_id,file_id),
 FOREIGN KEY(tenant_id,practitioner_id,registration_id,revision_id,file_id) REFERENCES practitioner_registration_evidence(tenant_id,practitioner_id,registration_id,revision_id,id)
);
ALTER TABLE practitioner_evidence_revocations ENABLE ROW LEVEL SECURITY;
ALTER TABLE practitioner_evidence_revocations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON practitioner_evidence_revocations
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.practitioner_evidence_access',true)='true')
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.practitioner_evidence_manage',true)='true' AND created_by=NULLIF(current_setting('app.actor_subject',true),''));
CREATE FUNCTION practitioner_evidence_revocation_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE file_state text;
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'evidence revocation receipt is immutable' USING ERRCODE='23514'; END IF;
 IF current_setting('app.practitioner_evidence_manage',true) IS DISTINCT FROM 'true' OR NEW.created_by IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'') THEN RAISE EXCEPTION 'evidence revocation authority required' USING ERRCODE='42501'; END IF;
 SELECT status INTO file_state FROM practitioner_registration_evidence WHERE tenant_id=NEW.tenant_id AND practitioner_id=NEW.practitioner_id AND registration_id=NEW.registration_id AND revision_id=NEW.revision_id AND id=NEW.file_id FOR UPDATE;
 IF NOT FOUND OR file_state<>'available' THEN RAISE EXCEPTION 'evidence cannot be revoked in this state' USING ERRCODE='23514',CONSTRAINT='practitioner_evidence_revocation'; END IF;
 NEW.created_at:=practitioner_registry_now(); RETURN NEW;
END $$;
CREATE TRIGGER practitioner_evidence_revocation_guard BEFORE INSERT OR UPDATE OR DELETE ON practitioner_evidence_revocations FOR EACH ROW EXECUTE FUNCTION practitioner_evidence_revocation_guard();
CREATE OR REPLACE FUNCTION practitioner_evidence_intent_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE evidence_count bigint; processing boolean;
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'practitioner evidence intent is immutable' USING ERRCODE='23514'; END IF;
 IF TG_OP='UPDATE' THEN
  IF (NEW.tenant_id,NEW.practitioner_id,NEW.registration_id,NEW.revision_id,NEW.id,NEW.content_type,NEW.size,NEW.sha256,NEW.created_by,NEW.created_at,NEW.expires_at) IS DISTINCT FROM (OLD.tenant_id,OLD.practitioner_id,OLD.registration_id,OLD.revision_id,OLD.id,OLD.content_type,OLD.size,OLD.sha256,OLD.created_by,OLD.created_at,OLD.expires_at)
   OR (OLD.uploaded_at IS NOT NULL AND NEW.uploaded_at IS DISTINCT FROM OLD.uploaded_at)
   OR (OLD.source_generation IS NOT NULL AND NEW.source_generation IS DISTINCT FROM OLD.source_generation) THEN RAISE EXCEPTION 'practitioner evidence intent is immutable' USING ERRCODE='23514'; END IF;
  processing:=practitioner_evidence_worker_allowed(OLD.tenant_id,OLD.practitioner_id,OLD.registration_id,OLD.revision_id,OLD.id);
  IF OLD.released_generation IS NOT NULL AND (NEW.released_generation,NEW.engine_version,NEW.definition_version,NEW.definitions_updated_at,NEW.scanned_at) IS DISTINCT FROM (OLD.released_generation,OLD.engine_version,OLD.definition_version,OLD.definitions_updated_at,OLD.scanned_at) THEN RAISE EXCEPTION 'completed evidence provenance is immutable' USING ERRCODE='23514'; END IF;
  IF processing THEN
   IF OLD.status NOT IN ('scan_pending','scan_error') OR NEW.status NOT IN ('available','rejected','scan_error') THEN RAISE EXCEPTION 'invalid evidence processing transition' USING ERRCODE='23514'; END IF;
   IF NEW.status='available' AND (NEW.scanned_at<NEW.uploaded_at-interval '5 minutes' OR NEW.scanned_at>practitioner_registry_now()+interval '5 minutes' OR NEW.definitions_updated_at<NEW.scanned_at-interval '24 hours' OR NEW.definitions_updated_at>NEW.scanned_at+interval '5 minutes') THEN RAISE EXCEPTION 'invalid evidence scanner provenance' USING ERRCODE='23514'; END IF;
  ELSE
   IF (NEW.released_generation,NEW.engine_version,NEW.definition_version,NEW.definitions_updated_at,NEW.scanned_at,NEW.failure_code) IS DISTINCT FROM (OLD.released_generation,OLD.engine_version,OLD.definition_version,OLD.definitions_updated_at,OLD.scanned_at,OLD.failure_code) THEN RAISE EXCEPTION 'evidence verdict requires scoped worker identity' USING ERRCODE='42501'; END IF;
   IF OLD.status='available' AND NEW.status='revoked' AND current_setting('app.practitioner_evidence_manage',true)='true' THEN
    IF NOT EXISTS(SELECT 1 FROM practitioner_evidence_revocations r WHERE r.tenant_id=OLD.tenant_id AND r.practitioner_id=OLD.practitioner_id AND r.registration_id=OLD.registration_id AND r.revision_id=OLD.revision_id AND r.file_id=OLD.id AND r.created_by=NULLIF(current_setting('app.actor_subject',true),'')) THEN RAISE EXCEPTION 'evidence revocation requires retained receipt' USING ERRCODE='23514',CONSTRAINT='practitioner_evidence_revocation'; END IF;
    NEW.updated_at:=practitioner_registry_now(); RETURN NEW;
   END IF;
   IF current_setting('app.practitioner_evidence_manage',true) IS DISTINCT FROM 'true' OR OLD.status<>'upload_pending' OR OLD.expires_at<=practitioner_registry_now() OR NEW.status NOT IN ('upload_pending','scan_pending')
    OR (NEW.uploaded_at IS DISTINCT FROM OLD.uploaded_at AND (NEW.uploaded_at IS NULL OR NEW.uploaded_at<NEW.created_at OR NEW.uploaded_at>=NEW.expires_at))
    OR (NEW.status='scan_pending' AND (NEW.uploaded_at IS NULL OR NEW.source_generation IS NULL)) THEN RAISE EXCEPTION 'invalid practitioner evidence upload transition' USING ERRCODE='23514',CONSTRAINT='practitioner_evidence_upload'; END IF;
  END IF;
  NEW.updated_at:=practitioner_registry_now(); RETURN NEW;
 END IF;
 IF current_setting('app.practitioner_evidence_manage',true) IS DISTINCT FROM 'true' OR NEW.created_by IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'') OR NEW.status<>'upload_pending' OR NEW.uploaded_at IS NOT NULL OR NEW.source_generation IS NOT NULL OR NEW.released_generation IS NOT NULL OR NEW.engine_version IS NOT NULL OR NEW.definition_version IS NOT NULL OR NEW.definitions_updated_at IS NOT NULL OR NEW.scanned_at IS NOT NULL OR NEW.failure_code IS NOT NULL THEN RAISE EXCEPTION 'evidence intake authority required' USING ERRCODE='42501'; END IF;
 PERFORM 1 FROM practitioner_registration_revisions WHERE tenant_id=NEW.tenant_id AND practitioner_id=NEW.practitioner_id AND registration_id=NEW.registration_id AND id=NEW.revision_id FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'evidence revision unavailable' USING ERRCODE='23503'; END IF;
 IF NOT EXISTS(SELECT 1 FROM practitioner_registration_evidence WHERE tenant_id=NEW.tenant_id AND id=NEW.id) THEN
  SELECT count(*) INTO evidence_count FROM practitioner_registration_evidence WHERE tenant_id=NEW.tenant_id AND practitioner_id=NEW.practitioner_id AND registration_id=NEW.registration_id AND revision_id=NEW.revision_id;
  IF evidence_count>=10 THEN RAISE EXCEPTION 'registration revision evidence limit' USING ERRCODE='23514',CONSTRAINT='practitioner_evidence_limit'; END IF;
 END IF;
 NEW.created_at:=practitioner_registry_now(); NEW.updated_at:=NEW.created_at; NEW.expires_at:=NEW.created_at+interval '1 hour';
 RETURN NEW;
END $$;



CREATE FUNCTION practitioner_evidence_revocation_apply() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 UPDATE practitioner_registration_evidence SET status='revoked' WHERE tenant_id=NEW.tenant_id AND practitioner_id=NEW.practitioner_id AND registration_id=NEW.registration_id AND revision_id=NEW.revision_id AND id=NEW.file_id AND status='available';
 IF NOT FOUND THEN RAISE EXCEPTION 'evidence revocation state changed' USING ERRCODE='23514',CONSTRAINT='practitioner_evidence_revocation'; END IF;
 RETURN NULL;
END $$;
CREATE TRIGGER practitioner_evidence_revocation_apply AFTER INSERT ON practitioner_evidence_revocations FOR EACH ROW EXECUTE FUNCTION practitioner_evidence_revocation_apply();
CREATE TRIGGER practitioner_evidence_revocation_audit AFTER INSERT ON practitioner_evidence_revocations FOR EACH ROW EXECUTE FUNCTION audit_record_change();
