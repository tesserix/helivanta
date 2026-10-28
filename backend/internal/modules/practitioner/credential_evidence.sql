SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
ALTER TABLE practitioner_registration_evidence RENAME TO practitioner_credential_evidence;
ALTER TABLE practitioner_registration_reviews RENAME TO practitioner_credential_reviews;
ALTER TABLE practitioner_credential_evidence RENAME COLUMN registration_id TO credential_id;
ALTER TABLE practitioner_credential_evidence ADD COLUMN credential_kind text NOT NULL DEFAULT 'registration' CHECK(credential_kind IN ('registration','qualification'));
ALTER TABLE practitioner_credential_reviews RENAME COLUMN registration_id TO credential_id;
ALTER TABLE practitioner_credential_reviews ADD COLUMN credential_kind text NOT NULL DEFAULT 'registration' CHECK(credential_kind IN ('registration','qualification'));
ALTER TABLE practitioner_evidence_revocations RENAME COLUMN registration_id TO credential_id;
ALTER TABLE practitioner_evidence_revocations ADD COLUMN credential_kind text NOT NULL DEFAULT 'registration' CHECK(credential_kind IN ('registration','qualification'));
ALTER TABLE practitioner_evidence_retries RENAME COLUMN registration_id TO credential_id;
ALTER TABLE practitioner_evidence_retries ADD COLUMN credential_kind text NOT NULL DEFAULT 'registration' CHECK(credential_kind IN ('registration','qualification'));

ALTER TABLE practitioner_registration_revisions ADD CONSTRAINT practitioner_registration_revision_source UNIQUE(tenant_id,practitioner_id,registration_id,id);
ALTER TABLE practitioner_qualification_revisions ADD CONSTRAINT practitioner_qualification_revision_source UNIQUE(tenant_id,practitioner_id,qualification_id,id);
DO $$ DECLARE item record; BEGIN
 FOR item IN SELECT conrelid::regclass AS relation,conname FROM pg_constraint WHERE contype='f' AND (
  conrelid IN ('practitioner_credential_evidence'::regclass,'practitioner_credential_reviews'::regclass) AND confrelid IN ('practitioner_registrations'::regclass,'practitioner_registration_revisions'::regclass)
  OR conrelid IN ('practitioner_evidence_revocations'::regclass,'practitioner_evidence_retries'::regclass) AND confrelid='practitioner_credential_evidence'::regclass
 ) LOOP EXECUTE format('ALTER TABLE %s DROP CONSTRAINT %I',item.relation,item.conname); END LOOP;
END $$;
ALTER TABLE practitioner_credential_evidence
 ADD COLUMN registration_parent uuid GENERATED ALWAYS AS (CASE WHEN credential_kind='registration' THEN credential_id END) STORED,
 ADD COLUMN qualification_parent uuid GENERATED ALWAYS AS (CASE WHEN credential_kind='qualification' THEN credential_id END) STORED,
 ADD FOREIGN KEY(tenant_id,practitioner_id,registration_parent,revision_id) REFERENCES practitioner_registration_revisions(tenant_id,practitioner_id,registration_id,id),
 ADD FOREIGN KEY(tenant_id,practitioner_id,qualification_parent,revision_id) REFERENCES practitioner_qualification_revisions(tenant_id,practitioner_id,qualification_id,id);
ALTER TABLE practitioner_credential_reviews
 ADD COLUMN registration_parent uuid GENERATED ALWAYS AS (CASE WHEN credential_kind='registration' THEN credential_id END) STORED,
 ADD COLUMN qualification_parent uuid GENERATED ALWAYS AS (CASE WHEN credential_kind='qualification' THEN credential_id END) STORED,
 ADD FOREIGN KEY(tenant_id,practitioner_id,registration_parent,revision_id) REFERENCES practitioner_registration_revisions(tenant_id,practitioner_id,registration_id,id),
 ADD FOREIGN KEY(tenant_id,practitioner_id,qualification_parent,revision_id) REFERENCES practitioner_qualification_revisions(tenant_id,practitioner_id,qualification_id,id);
ALTER TABLE practitioner_credential_evidence ADD CONSTRAINT practitioner_credential_evidence_scope UNIQUE(tenant_id,practitioner_id,credential_id,credential_kind,revision_id,id);
ALTER TABLE practitioner_evidence_revocations ADD FOREIGN KEY(tenant_id,practitioner_id,credential_id,credential_kind,revision_id,file_id) REFERENCES practitioner_credential_evidence(tenant_id,practitioner_id,credential_id,credential_kind,revision_id,id);
ALTER TABLE practitioner_evidence_retries ADD FOREIGN KEY(tenant_id,practitioner_id,credential_id,credential_kind,revision_id,file_id) REFERENCES practitioner_credential_evidence(tenant_id,practitioner_id,credential_id,credential_kind,revision_id,id);
ALTER TABLE practitioner_credential_reviews DROP CONSTRAINT practitioner_registration_reviews_finding_check;
ALTER TABLE practitioner_credential_reviews ADD CONSTRAINT practitioner_credential_review_finding CHECK(
 credential_kind='registration' AND finding IN ('active','suspended','expired','not_found','unavailable') OR credential_kind='qualification' AND finding IN ('confirmed','revoked','not_found','unavailable'));
GRANT SELECT(credential_kind) ON practitioner_credential_evidence TO helivanta_practitioner_evidence_worker;
CREATE OR REPLACE FUNCTION practitioner_evidence_worker_allowed(tenant uuid,practitioner uuid,registration uuid,revision uuid,file uuid) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT current_user='helivanta_practitioner_evidence_worker'
 AND tenant=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND practitioner=NULLIF(current_setting('app.practitioner_evidence_owner',true),'')::uuid
 AND registration=NULLIF(current_setting('app.practitioner_evidence_credential',true),'')::uuid
 AND revision=NULLIF(current_setting('app.practitioner_evidence_revision',true),'')::uuid
 AND file=NULLIF(current_setting('app.practitioner_evidence_file',true),'')::uuid;
$$;
CREATE FUNCTION practitioner_evidence_worker_allowed(tenant uuid,practitioner uuid,credential uuid,revision uuid,file uuid,kind text) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT kind IN ('registration','qualification') AND kind=current_setting('app.practitioner_evidence_kind',true) AND practitioner_evidence_worker_allowed(tenant,practitioner,credential,revision,file);
$$;
ALTER POLICY tenant_isolation ON practitioner_credential_evidence
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND CASE WHEN current_user='helivanta_practitioner_evidence_worker' THEN practitioner_evidence_worker_allowed(tenant_id,practitioner_id,credential_id,revision_id,id,credential_kind) ELSE current_setting('app.practitioner_evidence_access',true)='true' END)
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND CASE WHEN current_user='helivanta_practitioner_evidence_worker' THEN practitioner_evidence_worker_allowed(tenant_id,practitioner_id,credential_id,revision_id,id,credential_kind) ELSE current_setting('app.practitioner_evidence_manage',true)='true' END);
ALTER POLICY practitioner_worker_audit_bound ON audit_events
 WITH CHECK(actor_subject='service:practitioner-evidence-worker' AND action IN ('read','update') AND resource_type='practitioner_credential_evidence' AND EXISTS(SELECT 1 FROM practitioner_credential_evidence d WHERE d.tenant_id=audit_events.tenant_id AND d.id=audit_events.resource_id AND practitioner_evidence_worker_allowed(d.tenant_id,d.practitioner_id,d.credential_id,d.revision_id,d.id,d.credential_kind)));
ALTER POLICY practitioner_worker_outbox_read_bound ON outbox_events
 USING(subject='helivanta.in.practitioner.evidence_changed.v1' AND EXISTS(SELECT 1 FROM practitioner_credential_evidence d WHERE d.tenant_id=outbox_events.tenant_id AND practitioner_evidence_worker_allowed(d.tenant_id,d.practitioner_id,d.credential_id,d.revision_id,d.id,d.credential_kind)));
ALTER POLICY practitioner_worker_outbox_bound ON outbox_events
 WITH CHECK(subject='helivanta.in.practitioner.evidence_changed.v1' AND payload->>'type'=subject AND payload->'version'='1'::jsonb AND payload->>'tenant_id'=tenant_id::text AND payload-ARRAY['event_id','type','version','occurred_at','tenant_id','data']='{}'::jsonb
 AND EXISTS(SELECT 1 FROM practitioner_credential_evidence d WHERE d.tenant_id=outbox_events.tenant_id AND practitioner_evidence_worker_allowed(d.tenant_id,d.practitioner_id,d.credential_id,d.revision_id,d.id,d.credential_kind) AND d.status IN ('available','scan_error','rejected') AND d.scan_attempt::text=current_setting('app.practitioner_evidence_attempt',true) AND payload->'data'=jsonb_build_object('practitioner_id',d.practitioner_id::text,CASE WHEN d.credential_kind='registration' THEN 'registration_id' ELSE 'qualification_id' END,d.credential_id::text,'revision_id',d.revision_id::text,'file_id',d.id::text,'scan_attempt',d.scan_attempt,'status',d.status)));
CREATE OR REPLACE FUNCTION practitioner_evidence_intent_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE evidence_count bigint; processing boolean;
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'practitioner evidence intent is immutable' USING ERRCODE='23514'; END IF;
 IF TG_OP='UPDATE' THEN
  IF (NEW.tenant_id,NEW.practitioner_id,NEW.credential_id,NEW.credential_kind,NEW.revision_id,NEW.id,NEW.content_type,NEW.size,NEW.sha256,NEW.created_by,NEW.created_at,NEW.expires_at) IS DISTINCT FROM (OLD.tenant_id,OLD.practitioner_id,OLD.credential_id,OLD.credential_kind,OLD.revision_id,OLD.id,OLD.content_type,OLD.size,OLD.sha256,OLD.created_by,OLD.created_at,OLD.expires_at)
   OR (OLD.uploaded_at IS NOT NULL AND NEW.uploaded_at IS DISTINCT FROM OLD.uploaded_at)
   OR (OLD.source_generation IS NOT NULL AND NEW.source_generation IS DISTINCT FROM OLD.source_generation) THEN RAISE EXCEPTION 'practitioner evidence intent is immutable' USING ERRCODE='23514'; END IF;
  processing:=practitioner_evidence_worker_allowed(OLD.tenant_id,OLD.practitioner_id,OLD.credential_id,OLD.revision_id,OLD.id,OLD.credential_kind);
  IF NEW.scan_attempt IS DISTINCT FROM OLD.scan_attempt THEN
   IF processing OR OLD.status<>'scan_error' OR NEW.status<>'scan_pending' OR NEW.scan_attempt<>OLD.scan_attempt+1 OR NEW.failure_code IS NOT NULL
    OR (NEW.uploaded_at,NEW.source_generation,NEW.released_generation,NEW.engine_version,NEW.definition_version,NEW.definitions_updated_at,NEW.scanned_at) IS DISTINCT FROM (OLD.uploaded_at,OLD.source_generation,OLD.released_generation,OLD.engine_version,OLD.definition_version,OLD.definitions_updated_at,OLD.scanned_at)
    OR current_setting('app.practitioner_evidence_manage',true) IS DISTINCT FROM 'true'
    OR NOT EXISTS(SELECT 1 FROM practitioner_evidence_retries r WHERE r.tenant_id=OLD.tenant_id AND r.file_id=OLD.id AND r.credential_kind=OLD.credential_kind AND r.expected_attempt=OLD.scan_attempt AND r.scan_attempt=NEW.scan_attempt AND r.created_by=NULLIF(current_setting('app.actor_subject',true),'')) THEN RAISE EXCEPTION 'evidence retry requires retained receipt' USING ERRCODE='23514',CONSTRAINT='practitioner_evidence_retry'; END IF;
   NEW.updated_at:=practitioner_registry_now(); RETURN NEW;
  END IF;
  IF processing AND OLD.scan_attempt::text IS DISTINCT FROM current_setting('app.practitioner_evidence_attempt',true) THEN RAISE EXCEPTION 'evidence processing attempt is stale' USING ERRCODE='23514',CONSTRAINT='practitioner_evidence_retry'; END IF;
  IF OLD.released_generation IS NOT NULL AND (NEW.released_generation,NEW.engine_version,NEW.definition_version,NEW.definitions_updated_at,NEW.scanned_at) IS DISTINCT FROM (OLD.released_generation,OLD.engine_version,OLD.definition_version,OLD.definitions_updated_at,OLD.scanned_at) THEN RAISE EXCEPTION 'completed evidence provenance is immutable' USING ERRCODE='23514'; END IF;
  IF processing THEN
   IF OLD.status NOT IN ('scan_pending','scan_error') OR NEW.status NOT IN ('available','rejected','scan_error') THEN RAISE EXCEPTION 'invalid evidence processing transition' USING ERRCODE='23514'; END IF;
   IF NEW.status='available' AND (NEW.scanned_at<NEW.uploaded_at-interval '5 minutes' OR NEW.scanned_at>practitioner_registry_now()+interval '5 minutes' OR NEW.definitions_updated_at<NEW.scanned_at-interval '24 hours' OR NEW.definitions_updated_at>NEW.scanned_at+interval '5 minutes') THEN RAISE EXCEPTION 'invalid evidence scanner provenance' USING ERRCODE='23514'; END IF;
  ELSE
   IF (NEW.released_generation,NEW.engine_version,NEW.definition_version,NEW.definitions_updated_at,NEW.scanned_at,NEW.failure_code) IS DISTINCT FROM (OLD.released_generation,OLD.engine_version,OLD.definition_version,OLD.definitions_updated_at,OLD.scanned_at,OLD.failure_code) THEN RAISE EXCEPTION 'evidence verdict requires scoped worker identity' USING ERRCODE='42501'; END IF;
   IF OLD.status='available' AND NEW.status='revoked' AND current_setting('app.practitioner_evidence_manage',true)='true' THEN
    IF NOT EXISTS(SELECT 1 FROM practitioner_evidence_revocations r WHERE r.tenant_id=OLD.tenant_id AND r.practitioner_id=OLD.practitioner_id AND r.credential_id=OLD.credential_id AND r.revision_id=OLD.revision_id AND r.file_id=OLD.id AND r.credential_kind=OLD.credential_kind AND r.created_by=NULLIF(current_setting('app.actor_subject',true),'')) THEN RAISE EXCEPTION 'evidence revocation requires retained receipt' USING ERRCODE='23514',CONSTRAINT='practitioner_evidence_revocation'; END IF;
    NEW.updated_at:=practitioner_registry_now(); RETURN NEW;
   END IF;
   IF current_setting('app.practitioner_evidence_manage',true) IS DISTINCT FROM 'true' OR OLD.status<>'upload_pending' OR OLD.expires_at<=practitioner_registry_now() OR NEW.status NOT IN ('upload_pending','scan_pending')
    OR (NEW.uploaded_at IS DISTINCT FROM OLD.uploaded_at AND (NEW.uploaded_at IS NULL OR NEW.uploaded_at<NEW.created_at OR NEW.uploaded_at>=NEW.expires_at))
    OR (NEW.status='scan_pending' AND (NEW.uploaded_at IS NULL OR NEW.source_generation IS NULL)) THEN RAISE EXCEPTION 'invalid practitioner evidence upload transition' USING ERRCODE='23514',CONSTRAINT='practitioner_evidence_upload'; END IF;
  END IF;
  NEW.updated_at:=practitioner_registry_now(); RETURN NEW;
 END IF;
 IF NEW.scan_attempt<>1 OR current_setting('app.practitioner_evidence_manage',true) IS DISTINCT FROM 'true' OR NEW.created_by IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'') OR NEW.status<>'upload_pending' OR NEW.uploaded_at IS NOT NULL OR NEW.source_generation IS NOT NULL OR NEW.released_generation IS NOT NULL OR NEW.engine_version IS NOT NULL OR NEW.definition_version IS NOT NULL OR NEW.definitions_updated_at IS NOT NULL OR NEW.scanned_at IS NOT NULL OR NEW.failure_code IS NOT NULL THEN RAISE EXCEPTION 'evidence intake authority required' USING ERRCODE='42501'; END IF;
  IF NEW.credential_kind='registration' THEN
  PERFORM id FROM practitioner_registration_revisions WHERE tenant_id=NEW.tenant_id AND practitioner_id=NEW.practitioner_id AND registration_id=NEW.credential_id AND id=NEW.revision_id FOR UPDATE;
 ELSIF NEW.credential_kind='qualification' THEN
  PERFORM id FROM practitioner_qualification_revisions WHERE tenant_id=NEW.tenant_id AND practitioner_id=NEW.practitioner_id AND qualification_id=NEW.credential_id AND id=NEW.revision_id FOR UPDATE;
 ELSE RAISE EXCEPTION 'invalid credential kind' USING ERRCODE='23514'; END IF;
 IF NOT FOUND THEN RAISE EXCEPTION 'evidence revision unavailable' USING ERRCODE='23503'; END IF;
 IF NOT EXISTS(SELECT 1 FROM practitioner_credential_evidence WHERE tenant_id=NEW.tenant_id AND id=NEW.id) THEN
  SELECT count(*) INTO evidence_count FROM practitioner_credential_evidence WHERE tenant_id=NEW.tenant_id AND practitioner_id=NEW.practitioner_id AND credential_id=NEW.credential_id AND credential_kind=NEW.credential_kind AND revision_id=NEW.revision_id;
  IF evidence_count>=10 THEN RAISE EXCEPTION 'registration revision evidence limit' USING ERRCODE='23514',CONSTRAINT='practitioner_evidence_limit'; END IF;
 END IF;
 NEW.created_at:=practitioner_registry_now(); NEW.updated_at:=NEW.created_at; NEW.expires_at:=NEW.created_at+interval '1 hour';
 RETURN NEW;
END $$;
CREATE OR REPLACE FUNCTION practitioner_evidence_revocation_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE file_state text;
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'evidence revocation receipt is immutable' USING ERRCODE='23514'; END IF;
 IF current_setting('app.practitioner_evidence_manage',true) IS DISTINCT FROM 'true' OR NEW.created_by IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'') THEN RAISE EXCEPTION 'evidence revocation authority required' USING ERRCODE='42501'; END IF;
 SELECT status INTO file_state FROM practitioner_credential_evidence WHERE tenant_id=NEW.tenant_id AND practitioner_id=NEW.practitioner_id AND credential_id=NEW.credential_id AND credential_kind=NEW.credential_kind AND revision_id=NEW.revision_id AND id=NEW.file_id FOR UPDATE;
 IF NOT FOUND OR file_state<>'available' THEN RAISE EXCEPTION 'evidence cannot be revoked in this state' USING ERRCODE='23514',CONSTRAINT='practitioner_evidence_revocation'; END IF;
 NEW.created_at:=practitioner_registry_now(); RETURN NEW;
END $$;
CREATE OR REPLACE FUNCTION practitioner_evidence_revocation_apply() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 UPDATE practitioner_credential_evidence SET status='revoked' WHERE tenant_id=NEW.tenant_id AND practitioner_id=NEW.practitioner_id AND credential_id=NEW.credential_id AND credential_kind=NEW.credential_kind AND revision_id=NEW.revision_id AND id=NEW.file_id AND status='available';
 IF NOT FOUND THEN RAISE EXCEPTION 'evidence revocation state changed' USING ERRCODE='23514',CONSTRAINT='practitioner_evidence_revocation'; END IF;
 RETURN NULL;
END $$;
CREATE OR REPLACE FUNCTION practitioner_evidence_retry_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE current_attempt bigint; file_state text; failure text;
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'evidence retry receipt is immutable' USING ERRCODE='23514'; END IF;
 IF current_setting('app.practitioner_evidence_manage',true) IS DISTINCT FROM 'true' OR NEW.created_by IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'') THEN RAISE EXCEPTION 'evidence retry authority required' USING ERRCODE='42501'; END IF;
 SELECT scan_attempt,status,failure_code INTO current_attempt,file_state,failure FROM practitioner_credential_evidence WHERE tenant_id=NEW.tenant_id AND practitioner_id=NEW.practitioner_id AND credential_id=NEW.credential_id AND credential_kind=NEW.credential_kind AND revision_id=NEW.revision_id AND id=NEW.file_id FOR UPDATE;
 IF NOT FOUND OR file_state<>'scan_error' OR current_attempt<>NEW.expected_attempt OR failure NOT IN ('storage_unavailable','scanner_unavailable') THEN RAISE EXCEPTION 'evidence cannot be retried in this state' USING ERRCODE='23514',CONSTRAINT='practitioner_evidence_retry'; END IF;
 NEW.previous_failure:=failure; NEW.created_at:=practitioner_registry_now(); RETURN NEW;
END $$;
CREATE OR REPLACE FUNCTION practitioner_evidence_retry_apply() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 UPDATE practitioner_credential_evidence SET status='scan_pending',scan_attempt=NEW.scan_attempt,failure_code=NULL WHERE tenant_id=NEW.tenant_id AND practitioner_id=NEW.practitioner_id AND credential_id=NEW.credential_id AND credential_kind=NEW.credential_kind AND revision_id=NEW.revision_id AND id=NEW.file_id AND status='scan_error' AND scan_attempt=NEW.expected_attempt;
 IF NOT FOUND THEN RAISE EXCEPTION 'evidence retry state changed' USING ERRCODE='23514',CONSTRAINT='practitioner_evidence_retry'; END IF;
 RETURN NULL;
END $$;
CREATE OR REPLACE FUNCTION practitioner_review_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE head bigint; registration_author text; revision_author text; identity_author text; appointment practitioner_steward_appointments; evidence practitioner_credential_evidence; file text; snapshots jsonb:='[]'::jsonb; previous_file text; at_time timestamptz:=practitioner_registry_now();
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'registration review is immutable' USING ERRCODE='23514'; END IF;
 IF current_setting('app.practitioner_review',true) IS DISTINCT FROM 'true' OR NEW.reviewer IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'') THEN RAISE EXCEPTION 'registration review authority required' USING ERRCODE='42501'; END IF;
 IF NEW.credential_kind='registration' THEN
  SELECT version,created_by INTO head,registration_author FROM practitioner_registrations WHERE tenant_id=NEW.tenant_id AND practitioner_id=NEW.practitioner_id AND id=NEW.credential_id FOR UPDATE;
  IF NOT FOUND THEN RAISE EXCEPTION 'credential review parent unavailable' USING ERRCODE='P0002'; END IF;
  SELECT created_by INTO revision_author FROM practitioner_registration_revisions WHERE tenant_id=NEW.tenant_id AND practitioner_id=NEW.practitioner_id AND registration_id=NEW.credential_id AND id=NEW.revision_id AND version=head;
 ELSIF NEW.credential_kind='qualification' THEN
  SELECT version,created_by INTO head,registration_author FROM practitioner_qualifications WHERE tenant_id=NEW.tenant_id AND practitioner_id=NEW.practitioner_id AND id=NEW.credential_id FOR UPDATE;
  IF NOT FOUND THEN RAISE EXCEPTION 'credential review parent unavailable' USING ERRCODE='P0002'; END IF;
  SELECT created_by INTO revision_author FROM practitioner_qualification_revisions WHERE tenant_id=NEW.tenant_id AND practitioner_id=NEW.practitioner_id AND qualification_id=NEW.credential_id AND id=NEW.revision_id AND version=head;
 ELSE RAISE EXCEPTION 'invalid credential kind' USING ERRCODE='23514'; END IF;
 IF NOT FOUND THEN RAISE EXCEPTION 'credential review revision is stale' USING ERRCODE='23514',CONSTRAINT='practitioner_review_state'; END IF;
 SELECT created_by INTO identity_author FROM practitioner_records WHERE tenant_id=NEW.tenant_id AND id=NEW.practitioner_id;
 SELECT * INTO appointment FROM practitioner_steward_appointments WHERE tenant_id=NEW.tenant_id AND id=NEW.appointment_id AND subject=NEW.reviewer FOR UPDATE;
 at_time:=practitioner_registry_now();
 IF NOT FOUND OR appointment.status<>'active' OR appointment.version<>NEW.appointment_version OR appointment.valid_until<=at_time OR NEW.source_checked_at>at_time OR NEW.reviewer IN (identity_author,registration_author,revision_author) THEN RAISE EXCEPTION 'current independent reviewer appointment required' USING ERRCODE='23514',CONSTRAINT='practitioner_review_state'; END IF;
 IF jsonb_typeof(NEW.evidence_ids)<>'array' OR jsonb_array_length(NEW.evidence_ids) NOT BETWEEN 1 AND 10 THEN RAISE EXCEPTION 'bounded review evidence required' USING ERRCODE='23514',CONSTRAINT='practitioner_review_state'; END IF;
 FOR file IN SELECT jsonb_array_elements_text(NEW.evidence_ids) ORDER BY 1 LOOP
  IF file=previous_file OR file::uuid::text<>file THEN RAISE EXCEPTION 'unique canonical review evidence required' USING ERRCODE='23514',CONSTRAINT='practitioner_review_state'; END IF;
  SELECT * INTO evidence FROM practitioner_credential_evidence WHERE tenant_id=NEW.tenant_id AND practitioner_id=NEW.practitioner_id AND credential_id=NEW.credential_id AND credential_kind=NEW.credential_kind AND revision_id=NEW.revision_id AND id=file::uuid FOR UPDATE;
  IF NOT FOUND OR evidence.status<>'available' OR evidence.created_by=NEW.reviewer OR evidence.released_generation IS NULL OR evidence.scanned_at IS NULL THEN RAISE EXCEPTION 'released independent review evidence required' USING ERRCODE='23514',CONSTRAINT='practitioner_review_state'; END IF;
  snapshots:=snapshots||jsonb_build_array(jsonb_build_object('id',evidence.id,'sha256',evidence.sha256,'size',evidence.size,'source_generation',evidence.source_generation,'released_generation',evidence.released_generation,'scan_attempt',evidence.scan_attempt,'scanned_at',evidence.scanned_at));
  previous_file:=file;
 END LOOP;
 at_time:=practitioner_registry_now();
 IF appointment.valid_until<=at_time THEN RAISE EXCEPTION 'reviewer appointment elapsed before review' USING ERRCODE='23514',CONSTRAINT='practitioner_review_state'; END IF;
 NEW.evidence_ids:=(SELECT jsonb_agg(value ORDER BY value) FROM jsonb_array_elements(NEW.evidence_ids));
 NEW.evidence_snapshot:=snapshots; NEW.created_at:=at_time;
 RETURN NEW;
END $$;
