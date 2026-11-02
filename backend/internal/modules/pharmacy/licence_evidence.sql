-- Licence evidence is a distinct owner record; existing prescription keys and reviews remain unchanged.
CREATE TABLE pharmacy_licence_evidence (
 tenant_id uuid NOT NULL,outlet_id uuid NOT NULL,licence_id uuid NOT NULL,id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 filename text NOT NULL CHECK(length(btrim(filename)) BETWEEN 1 AND 180 AND filename !~ '[[:cntrl:]/\\]'),
 content_type text NOT NULL CHECK(content_type IN('image/png','image/jpeg')),
 size bigint NOT NULL CHECK(size BETWEEN 1 AND 10485760),sha256 text NOT NULL CHECK(sha256 ~ '^[a-f0-9]{64}$'),
 status text NOT NULL DEFAULT 'upload_pending' CHECK(status IN('upload_pending','scan_pending','available','rejected','scan_error')),
 scan_attempt bigint NOT NULL DEFAULT 1 CHECK(scan_attempt BETWEEN 1 AND 9007199254740991),
 uploaded_at timestamptz,source_generation bigint CHECK(source_generation>0),released_generation bigint CHECK(released_generation>0),
 engine_version text,definition_version bigint CHECK(definition_version>0),definitions_updated_at timestamptz,scanned_at timestamptz,
 failure_code text CHECK(failure_code IN('unsupported_content','malware_detected','scanner_unavailable','storage_unavailable','integrity_mismatch')),
 uploader_subject text NOT NULL CHECK(length(btrim(uploader_subject)) BETWEEN 1 AND 200),
 expires_at timestamptz NOT NULL DEFAULT now()+interval '30 minutes',created_at timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,outlet_id,licence_id,id),
 CONSTRAINT pharmacy_licence_evidence_single UNIQUE(tenant_id,outlet_id,licence_id),
 FOREIGN KEY(tenant_id,outlet_id,licence_id) REFERENCES pharmacy_outlet_licences(tenant_id,outlet_id,id) ON DELETE RESTRICT,
 CHECK((status='upload_pending' AND source_generation IS NULL AND released_generation IS NULL)
  OR(status IN('scan_pending','rejected','scan_error') AND uploaded_at IS NOT NULL AND source_generation IS NOT NULL AND released_generation IS NULL)
  OR(status='available' AND uploaded_at IS NOT NULL AND source_generation IS NOT NULL AND released_generation IS NOT NULL
    AND engine_version IS NOT NULL AND definition_version IS NOT NULL AND definitions_updated_at IS NOT NULL AND scanned_at IS NOT NULL AND failure_code IS NULL)),
 CHECK(uploaded_at IS NULL OR uploaded_at>=created_at),CHECK(expires_at>created_at)
);
CREATE INDEX pharmacy_licence_evidence_page ON pharmacy_licence_evidence(tenant_id,outlet_id,licence_id,created_at DESC,id DESC);
ALTER TABLE pharmacy_licence_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_licence_evidence FORCE ROW LEVEL SECURITY;

CREATE FUNCTION pharmacy_licence_evidence_worker_allowed(tenant uuid,outlet uuid,licence uuid,file uuid) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT current_user='helivanta_pharmacy_evidence_worker'
 AND tenant=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND outlet=NULLIF(current_setting('app.pharmacy_outlet',true),'')::uuid
 AND licence=NULLIF(current_setting('app.pharmacy_licence',true),'')::uuid
 AND file=NULLIF(current_setting('app.pharmacy_file',true),'')::uuid
$$;
-- The worker sees exactly the one file its session names; everyone else needs the outlet grant.
CREATE POLICY tenant_isolation ON pharmacy_licence_evidence
 USING(hms_tenant_visible(tenant_id) AND CASE WHEN current_user='helivanta_pharmacy_evidence_worker'
 THEN pharmacy_licence_evidence_worker_allowed(tenant_id,outlet_id,licence_id,id)
 ELSE pharmacy_outlet_allowed(tenant_id,outlet_id,false) END)
 WITH CHECK(CASE WHEN current_user='helivanta_pharmacy_evidence_worker'
 THEN pharmacy_licence_evidence_worker_allowed(tenant_id,outlet_id,licence_id,id)
 ELSE pharmacy_outlet_allowed(tenant_id,outlet_id,true) END);
CREATE TRIGGER pharmacy_licence_evidence_audit AFTER INSERT OR UPDATE ON pharmacy_licence_evidence FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_licence_evidence_retain BEFORE DELETE OR TRUNCATE ON pharmacy_licence_evidence FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

CREATE FUNCTION pharmacy_licence_evidence_creation() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 PERFORM 1 FROM pharmacy_outlet_licences WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.licence_id FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'licence declaration unavailable' USING ERRCODE='23503'; END IF;
 IF EXISTS(SELECT 1 FROM pharmacy_outlet_licence_reviews WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND licence_id=NEW.licence_id) THEN
  RAISE EXCEPTION 'reviewed licence declaration cannot receive different evidence' USING ERRCODE='23514',CONSTRAINT='pharmacy_licence_evidence_reviewed';
 END IF;

 IF NEW.status<>'upload_pending' OR NEW.scan_attempt<>1 OR NEW.uploaded_at IS NOT NULL OR NEW.source_generation IS NOT NULL OR NEW.released_generation IS NOT NULL
 OR NEW.engine_version IS NOT NULL OR NEW.definition_version IS NOT NULL OR NEW.definitions_updated_at IS NOT NULL OR NEW.scanned_at IS NOT NULL OR NEW.failure_code IS NOT NULL
 OR NEW.uploader_subject IS DISTINCT FROM NULLIF(current_setting('app.pharmacy_subject',true),'') THEN
 RAISE EXCEPTION 'licence evidence creation cannot carry a verdict or another actor' USING ERRCODE='42501'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_licence_evidence_creation BEFORE INSERT ON pharmacy_licence_evidence FOR EACH ROW EXECUTE FUNCTION pharmacy_licence_evidence_creation();

CREATE FUNCTION pharmacy_licence_evidence_identity() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE processing boolean := pharmacy_licence_evidence_worker_allowed(OLD.tenant_id,OLD.outlet_id,OLD.licence_id,OLD.id);
BEGIN
 IF (NEW.tenant_id,NEW.outlet_id,NEW.licence_id,NEW.id,NEW.filename,NEW.content_type,NEW.size,NEW.sha256,NEW.uploader_subject,NEW.created_at,NEW.expires_at)
 IS DISTINCT FROM(OLD.tenant_id,OLD.outlet_id,OLD.licence_id,OLD.id,OLD.filename,OLD.content_type,OLD.size,OLD.sha256,OLD.uploader_subject,OLD.created_at,OLD.expires_at)
 OR(OLD.uploaded_at IS NOT NULL AND NEW.uploaded_at IS DISTINCT FROM OLD.uploaded_at)
 OR(OLD.source_generation IS NOT NULL AND NEW.source_generation IS DISTINCT FROM OLD.source_generation)
 OR(OLD.released_generation IS NOT NULL AND NEW.released_generation IS DISTINCT FROM OLD.released_generation) THEN
 RAISE EXCEPTION 'licence evidence identity and provenance are immutable' USING ERRCODE='23514'; END IF;
 IF OLD.status<>NEW.status AND NOT((OLD.status='upload_pending' AND NEW.status='scan_pending')
 OR(OLD.status='scan_pending' AND NEW.status IN('available','rejected','scan_error'))
 OR(OLD.status='scan_error' AND NEW.status='scan_pending')) THEN
 RAISE EXCEPTION 'invalid licence evidence transition' USING ERRCODE='23514'; END IF;
 IF NEW.scan_attempt<>OLD.scan_attempt AND NOT(OLD.status='scan_error' AND NEW.status='scan_pending' AND NEW.scan_attempt=OLD.scan_attempt+1 AND NOT processing) THEN
 RAISE EXCEPTION 'invalid licence evidence attempt' USING ERRCODE='23514'; END IF;
 IF OLD.status='scan_error' AND NEW.status='scan_pending' AND NOT((processing AND NEW.scan_attempt=OLD.scan_attempt) OR(NOT processing AND NEW.scan_attempt=OLD.scan_attempt+1)) THEN
 RAISE EXCEPTION 'retry requires next attempt' USING ERRCODE='23514'; END IF;
 IF (NEW.released_generation,NEW.engine_version,NEW.definition_version,NEW.definitions_updated_at,NEW.scanned_at,NEW.failure_code)
 IS DISTINCT FROM(OLD.released_generation,OLD.engine_version,OLD.definition_version,OLD.definitions_updated_at,OLD.scanned_at,OLD.failure_code)
 AND NOT(processing AND OLD.status IN('scan_pending','scan_error')) AND NOT(OLD.status='scan_error' AND NEW.status='scan_pending' AND NEW.scan_attempt=OLD.scan_attempt+1 AND NEW.failure_code IS NULL
 AND(NEW.released_generation,NEW.engine_version,NEW.definition_version,NEW.definitions_updated_at,NEW.scanned_at)
 IS NOT DISTINCT FROM(OLD.released_generation,OLD.engine_version,OLD.definition_version,OLD.definitions_updated_at,OLD.scanned_at)) THEN
 RAISE EXCEPTION 'licence evidence verdict requires scoped worker identity' USING ERRCODE='42501'; END IF;
 IF OLD.status='scan_pending' AND NEW.status IN('available','rejected','scan_error') AND NOT processing THEN
 RAISE EXCEPTION 'licence evidence transition requires scoped worker identity' USING ERRCODE='42501'; END IF;
 IF NEW.status='available' AND(NEW.scanned_at<NEW.uploaded_at-interval '5 minutes' OR NEW.scanned_at>clock_timestamp()+interval '5 minutes'
 OR NEW.definitions_updated_at<NEW.scanned_at-interval '24 hours' OR NEW.definitions_updated_at>NEW.scanned_at+interval '5 minutes') THEN
 RAISE EXCEPTION 'invalid scanner provenance' USING ERRCODE='23514'; END IF;
 NEW.updated_at:=clock_timestamp();
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_licence_evidence_identity BEFORE UPDATE ON pharmacy_licence_evidence FOR EACH ROW EXECUTE FUNCTION pharmacy_licence_evidence_identity();

-- The worker reads and updates only image lifecycle columns; it holds nothing on prescriptions.
GRANT SELECT(tenant_id,id,outlet_id,licence_id,content_type,size,sha256,status,scan_attempt,
 uploaded_at,source_generation,released_generation,engine_version,definition_version,definitions_updated_at,scanned_at,failure_code,expires_at,created_at,updated_at)
 ON pharmacy_licence_evidence TO helivanta_pharmacy_evidence_worker;
GRANT UPDATE(status,released_generation,engine_version,definition_version,definitions_updated_at,scanned_at,failure_code,updated_at)
 ON pharmacy_licence_evidence TO helivanta_pharmacy_evidence_worker;
GRANT INSERT ON audit_events,outbox_events TO helivanta_pharmacy_evidence_worker;
GRANT SELECT(id) ON outbox_events TO helivanta_pharmacy_evidence_worker;

DROP POLICY pharmacy_worker_audit_boundary ON audit_events;
CREATE POLICY pharmacy_worker_audit_boundary ON audit_events AS RESTRICTIVE FOR INSERT TO helivanta_pharmacy_evidence_worker
 WITH CHECK((tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND actor_subject='service:pharmacy-evidence-worker' AND resource_type='pharmacy_prescription_images'
 AND resource_id=NULLIF(current_setting('app.pharmacy_file',true),'')::uuid AND action='update'
 AND cardinality(changed_fields) BETWEEN 1 AND 8
 AND changed_fields<@ARRAY['status','released_generation','engine_version','definition_version','definitions_updated_at','scanned_at','failure_code','updated_at']::text[]
 AND occurred_at BETWEEN clock_timestamp()-interval '5 minutes' AND clock_timestamp()+interval '5 minutes') OR (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND actor_subject='service:pharmacy-evidence-worker' AND resource_type='pharmacy_licence_evidence'
 AND resource_id=NULLIF(current_setting('app.pharmacy_file',true),'')::uuid AND action='update'
 AND cardinality(changed_fields) BETWEEN 1 AND 8
 AND changed_fields<@ARRAY['status','released_generation','engine_version','definition_version','definitions_updated_at','scanned_at','failure_code','updated_at']::text[]
 AND occurred_at BETWEEN clock_timestamp()-interval '5 minutes' AND clock_timestamp()+interval '5 minutes'));

DROP POLICY pharmacy_worker_outbox_boundary ON outbox_events;
CREATE POLICY pharmacy_worker_outbox_boundary ON outbox_events AS RESTRICTIVE FOR INSERT TO helivanta_pharmacy_evidence_worker
 WITH CHECK((COALESCE(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND subject='helivanta.in.pharmacy.prescription_image_changed.v1'
 AND jsonb_typeof(payload)='object' AND payload-ARRAY['event_id','type','version','occurred_at','tenant_id','data']='{}'::jsonb
 AND payload->>'event_id'=id::text AND payload->>'type'=subject AND payload->'version'='1'::jsonb AND payload->>'tenant_id'=tenant_id::text
 AND jsonb_typeof(payload->'occurred_at')='string' AND length(payload->>'occurred_at') BETWEEN 20 AND 35
 AND (payload->>'occurred_at')::timestamptz BETWEEN created_at-interval '5 minutes' AND created_at+interval '5 minutes'
 AND jsonb_typeof(payload->'data')='object' AND (payload->'data')-ARRAY['outlet_id','prescription_id','file_id','scan_attempt','status']='{}'::jsonb
 AND jsonb_typeof(payload->'data'->'scan_attempt')='number'
 AND payload->'data'->>'status' IN('available','rejected','scan_error')
 AND payload->'data'->>'outlet_id'=current_setting('app.pharmacy_outlet',true)
 AND payload->'data'->>'prescription_id'=current_setting('app.pharmacy_prescription',true)
 AND payload->'data'->>'file_id'=current_setting('app.pharmacy_file',true)
 AND EXISTS(SELECT 1 FROM pharmacy_prescription_images e WHERE pharmacy_evidence_worker_allowed(e.tenant_id,e.outlet_id,e.prescription_id,e.id)
 AND e.tenant_id=outbox_events.tenant_id AND e.status=payload->'data'->>'status' AND e.scan_attempt::text=payload->'data'->>'scan_attempt'),false)) OR (COALESCE(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND subject='helivanta.in.pharmacy.licence_evidence_changed.v1'
 AND jsonb_typeof(payload)='object' AND payload-ARRAY['event_id','type','version','occurred_at','tenant_id','data']='{}'::jsonb
 AND payload->>'event_id'=id::text AND payload->>'type'=subject AND payload->'version'='1'::jsonb AND payload->>'tenant_id'=tenant_id::text
 AND jsonb_typeof(payload->'occurred_at')='string' AND length(payload->>'occurred_at') BETWEEN 20 AND 35
 AND (payload->>'occurred_at')::timestamptz BETWEEN created_at-interval '5 minutes' AND created_at+interval '5 minutes'
 AND jsonb_typeof(payload->'data')='object' AND (payload->'data')-ARRAY['outlet_id','licence_id','file_id','scan_attempt','status']='{}'::jsonb
 AND jsonb_typeof(payload->'data'->'scan_attempt')='number'
 AND payload->'data'->>'status' IN('available','rejected','scan_error')
 AND payload->'data'->>'outlet_id'=current_setting('app.pharmacy_outlet',true)
 AND payload->'data'->>'licence_id'=current_setting('app.pharmacy_licence',true)
 AND payload->'data'->>'file_id'=current_setting('app.pharmacy_file',true)
 AND EXISTS(SELECT 1 FROM pharmacy_licence_evidence e WHERE pharmacy_licence_evidence_worker_allowed(e.tenant_id,e.outlet_id,e.licence_id,e.id)
 AND e.tenant_id=outbox_events.tenant_id AND e.status=payload->'data'->>'status' AND e.scan_attempt::text=payload->'data'->>'scan_attempt'),false)));
