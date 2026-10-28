SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
ALTER TABLE practitioner_registration_evidence DROP CONSTRAINT practitioner_registration_evidence_status_check;
ALTER TABLE practitioner_registration_evidence DROP CONSTRAINT practitioner_evidence_quarantine_state;
ALTER TABLE practitioner_registration_evidence ADD CONSTRAINT practitioner_registration_evidence_status_check CHECK(status IN ('upload_pending','scan_pending','scan_error','available','rejected','revoked'));
ALTER TABLE practitioner_registration_evidence ADD COLUMN released_generation bigint CHECK(released_generation>0);
ALTER TABLE practitioner_registration_evidence ADD COLUMN engine_version text CHECK(octet_length(engine_version) BETWEEN 1 AND 100);
ALTER TABLE practitioner_registration_evidence ADD COLUMN definition_version bigint CHECK(definition_version>0);
ALTER TABLE practitioner_registration_evidence ADD COLUMN definitions_updated_at timestamptz;
ALTER TABLE practitioner_registration_evidence ADD COLUMN scanned_at timestamptz;
ALTER TABLE practitioner_registration_evidence ADD COLUMN failure_code text CHECK(failure_code IN ('malware_detected','unsupported_content','integrity_mismatch','storage_unavailable','scanner_unavailable'));
ALTER TABLE practitioner_registration_evidence ADD CONSTRAINT practitioner_evidence_quarantine_state CHECK(
 (status='upload_pending' AND source_generation IS NULL) OR (status<>'upload_pending' AND source_generation IS NOT NULL AND uploaded_at IS NOT NULL));
ALTER TABLE practitioner_registration_evidence ADD CONSTRAINT practitioner_evidence_release_state CHECK(
 (status IN ('available','revoked') AND released_generation IS NOT NULL AND engine_version IS NOT NULL AND definition_version IS NOT NULL AND definitions_updated_at IS NOT NULL AND scanned_at IS NOT NULL AND failure_code IS NULL)
 OR (status NOT IN ('available','revoked') AND released_generation IS NULL AND engine_version IS NULL AND definition_version IS NULL AND definitions_updated_at IS NULL AND scanned_at IS NULL AND
  ((status IN ('upload_pending','scan_pending') AND failure_code IS NULL) OR (status='scan_error' AND failure_code IN ('storage_unavailable','scanner_unavailable')) OR (status='rejected' AND failure_code IN ('malware_detected','unsupported_content','integrity_mismatch')))));
CREATE FUNCTION practitioner_evidence_worker_allowed(tenant uuid,practitioner uuid,registration uuid,revision uuid,file uuid) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT current_user='helivanta_practitioner_evidence_worker'
 AND tenant=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND practitioner=NULLIF(current_setting('app.practitioner_evidence_owner',true),'')::uuid
 AND registration=NULLIF(current_setting('app.practitioner_evidence_registration',true),'')::uuid
 AND revision=NULLIF(current_setting('app.practitioner_evidence_revision',true),'')::uuid
 AND file=NULLIF(current_setting('app.practitioner_evidence_file',true),'')::uuid;
$$;
ALTER POLICY tenant_isolation ON practitioner_registration_evidence
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND CASE WHEN current_user='helivanta_practitioner_evidence_worker' THEN practitioner_evidence_worker_allowed(tenant_id,practitioner_id,registration_id,revision_id,id) ELSE current_setting('app.practitioner_evidence_access',true)='true' END)
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND CASE WHEN current_user='helivanta_practitioner_evidence_worker' THEN practitioner_evidence_worker_allowed(tenant_id,practitioner_id,registration_id,revision_id,id) ELSE current_setting('app.practitioner_evidence_manage',true)='true' END);
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


GRANT USAGE ON SCHEMA public TO helivanta_practitioner_evidence_worker;
GRANT SELECT(tenant_id,id,practitioner_id,registration_id,revision_id,content_type,size,sha256,status,created_at,expires_at,uploaded_at,source_generation,released_generation,engine_version,definition_version,definitions_updated_at,scanned_at,failure_code) ON practitioner_registration_evidence TO helivanta_practitioner_evidence_worker;
GRANT UPDATE(status,released_generation,engine_version,definition_version,definitions_updated_at,scanned_at,failure_code) ON practitioner_registration_evidence TO helivanta_practitioner_evidence_worker;
GRANT INSERT ON audit_events,outbox_events TO helivanta_practitioner_evidence_worker;
GRANT SELECT(id) ON outbox_events TO helivanta_practitioner_evidence_worker;
CREATE POLICY practitioner_worker_audit_bound ON audit_events AS RESTRICTIVE FOR INSERT TO helivanta_practitioner_evidence_worker
 WITH CHECK(actor_subject='service:practitioner-evidence-worker' AND resource_type='practitioner_registration_evidence' AND resource_id=NULLIF(current_setting('app.practitioner_evidence_file',true),'')::uuid AND action IN ('read','update') AND EXISTS(SELECT 1 FROM practitioner_registration_evidence d WHERE d.tenant_id=audit_events.tenant_id AND d.id=audit_events.resource_id AND practitioner_evidence_worker_allowed(d.tenant_id,d.practitioner_id,d.registration_id,d.revision_id,d.id)));
CREATE POLICY practitioner_worker_outbox_bound ON outbox_events AS RESTRICTIVE FOR INSERT TO helivanta_practitioner_evidence_worker
 WITH CHECK(subject='helivanta.in.practitioner.evidence_changed.v1' AND payload->>'type'=subject AND payload->'version'='1'::jsonb AND payload->>'tenant_id'=tenant_id::text AND payload-ARRAY['event_id','type','version','occurred_at','tenant_id','data']='{}'::jsonb
 AND EXISTS(SELECT 1 FROM practitioner_registration_evidence d WHERE d.tenant_id=outbox_events.tenant_id AND practitioner_evidence_worker_allowed(d.tenant_id,d.practitioner_id,d.registration_id,d.revision_id,d.id) AND d.status IN ('available','scan_error','rejected') AND payload->'data'=jsonb_build_object('practitioner_id',d.practitioner_id::text,'registration_id',d.registration_id::text,'revision_id',d.revision_id::text,'file_id',d.id::text,'status',d.status)));
CREATE POLICY practitioner_worker_outbox_read_bound ON outbox_events AS RESTRICTIVE FOR SELECT TO helivanta_practitioner_evidence_worker
 USING(subject='helivanta.in.practitioner.evidence_changed.v1' AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND payload->'data'->>'practitioner_id'=current_setting('app.practitioner_evidence_owner',true)
 AND payload->'data'->>'registration_id'=current_setting('app.practitioner_evidence_registration',true)
 AND payload->'data'->>'revision_id'=current_setting('app.practitioner_evidence_revision',true)
 AND payload->'data'->>'file_id'=current_setting('app.practitioner_evidence_file',true));
