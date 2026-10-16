SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE TABLE supply_license_evidence (
 tenant_id uuid NOT NULL,party_id uuid NOT NULL,license_id uuid NOT NULL,id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 license_version bigint NOT NULL CHECK(license_version BETWEEN 1 AND 9007199254740991),
 filename text NOT NULL CHECK(length(btrim(filename)) BETWEEN 1 AND 180 AND filename !~ '[[:cntrl:]/\\]'),
 content_type text NOT NULL CHECK(content_type IN('image/png','image/jpeg')),
 size bigint NOT NULL CHECK(size BETWEEN 1 AND 10485760),sha256 text NOT NULL CHECK(sha256 ~ '^[a-f0-9]{64}$'),
 status text NOT NULL DEFAULT 'upload_pending' CHECK(status IN('upload_pending','scan_pending','available','rejected','scan_error','revoked')),
 scan_attempt bigint NOT NULL DEFAULT 1 CHECK(scan_attempt BETWEEN 1 AND 9007199254740991),
 uploaded_at timestamptz,source_generation bigint CHECK(source_generation>0),released_generation bigint CHECK(released_generation>0),
 engine_version text,definition_version bigint CHECK(definition_version>0),definitions_updated_at timestamptz,scanned_at timestamptz,
 failure_code text CHECK(failure_code IN('unsupported_content','malware_detected','scanner_unavailable','storage_unavailable','integrity_mismatch')),
 uploader_subject text NOT NULL CHECK(length(btrim(uploader_subject)) BETWEEN 1 AND 200),
 expires_at timestamptz NOT NULL DEFAULT now()+interval '30 minutes',created_at timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,party_id,license_id,id),
 FOREIGN KEY(tenant_id,party_id,license_id,license_version) REFERENCES supply_license_revisions(tenant_id,party_id,license_id,version) ON DELETE RESTRICT,
 CHECK((status='upload_pending' AND source_generation IS NULL AND released_generation IS NULL)
  OR(status IN('scan_pending','rejected','scan_error') AND uploaded_at IS NOT NULL AND source_generation IS NOT NULL AND released_generation IS NULL)
  OR(status IN('available','revoked') AND uploaded_at IS NOT NULL AND source_generation IS NOT NULL AND released_generation IS NOT NULL
    AND engine_version IS NOT NULL AND definition_version IS NOT NULL AND definitions_updated_at IS NOT NULL AND scanned_at IS NOT NULL AND failure_code IS NULL)),
 CHECK(uploaded_at IS NULL OR uploaded_at>=created_at),CHECK(expires_at>created_at)
);
CREATE INDEX supply_license_evidence_page ON supply_license_evidence(tenant_id,party_id,license_id,created_at DESC,id DESC);
ALTER TABLE supply_license_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE supply_license_evidence FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON supply_license_evidence
 USING(hms_tenant_visible(tenant_id) AND supply_roles_allowed(tenant_id,party_id,ARRAY['administrator','purchasing','finance','compliance','auditor']))
 WITH CHECK(supply_roles_allowed(tenant_id,party_id,ARRAY['administrator','purchasing','compliance']));
CREATE TRIGGER supply_license_evidence_audit AFTER INSERT OR UPDATE ON supply_license_evidence FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER supply_license_evidence_retain BEFORE DELETE OR TRUNCATE ON supply_license_evidence FOR EACH STATEMENT EXECUTE FUNCTION supply_history_immutable();

CREATE FUNCTION supply_evidence_worker_allowed(tenant uuid,party uuid,licence uuid,file uuid) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT current_user='helivanta_supply_evidence_worker'
 AND tenant=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND party=NULLIF(current_setting('app.supply_party',true),'')::uuid
 AND licence=NULLIF(current_setting('app.supply_license',true),'')::uuid
 AND file=NULLIF(current_setting('app.supply_file',true),'')::uuid
$$;
CREATE FUNCTION supply_evidence_creation() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.status<>'upload_pending' OR NEW.scan_attempt<>1 OR NEW.uploaded_at IS NOT NULL OR NEW.source_generation IS NOT NULL OR NEW.released_generation IS NOT NULL
 OR NEW.engine_version IS NOT NULL OR NEW.definition_version IS NOT NULL OR NEW.definitions_updated_at IS NOT NULL OR NEW.scanned_at IS NOT NULL OR NEW.failure_code IS NOT NULL
 OR NEW.uploader_subject IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'') THEN
 RAISE EXCEPTION 'evidence creation cannot carry a verdict or another actor' USING ERRCODE='42501'; END IF;
 IF NOT EXISTS(SELECT 1 FROM supply_licenses l WHERE l.tenant_id=NEW.tenant_id AND l.party_id=NEW.party_id AND l.id=NEW.license_id AND l.version=NEW.license_version) THEN
 RAISE EXCEPTION 'evidence source revision is not current' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER supply_evidence_creation BEFORE INSERT ON supply_license_evidence FOR EACH ROW EXECUTE FUNCTION supply_evidence_creation();
CREATE FUNCTION supply_evidence_identity() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE processing boolean := supply_evidence_worker_allowed(OLD.tenant_id,OLD.party_id,OLD.license_id,OLD.id);
BEGIN
 IF (NEW.tenant_id,NEW.party_id,NEW.license_id,NEW.id,NEW.license_version,NEW.filename,NEW.content_type,NEW.size,NEW.sha256,NEW.uploader_subject,NEW.created_at,NEW.expires_at)
 IS DISTINCT FROM(OLD.tenant_id,OLD.party_id,OLD.license_id,OLD.id,OLD.license_version,OLD.filename,OLD.content_type,OLD.size,OLD.sha256,OLD.uploader_subject,OLD.created_at,OLD.expires_at)
 OR(OLD.uploaded_at IS NOT NULL AND NEW.uploaded_at IS DISTINCT FROM OLD.uploaded_at)
 OR(OLD.source_generation IS NOT NULL AND NEW.source_generation IS DISTINCT FROM OLD.source_generation)
 OR(OLD.released_generation IS NOT NULL AND NEW.released_generation IS DISTINCT FROM OLD.released_generation) THEN
 RAISE EXCEPTION 'evidence identity and provenance are immutable' USING ERRCODE='23514'; END IF;
 IF OLD.status<>NEW.status AND NOT((OLD.status='upload_pending' AND NEW.status='scan_pending')
 OR(OLD.status='scan_pending' AND NEW.status IN('available','rejected','scan_error'))
 OR(OLD.status='scan_error' AND NEW.status='scan_pending') OR(OLD.status='available' AND NEW.status='revoked')) THEN
 RAISE EXCEPTION 'invalid evidence transition' USING ERRCODE='23514'; END IF;
 IF NEW.scan_attempt<>OLD.scan_attempt AND NOT(OLD.status='scan_error' AND NEW.status='scan_pending' AND NEW.scan_attempt=OLD.scan_attempt+1 AND NOT processing) THEN
 RAISE EXCEPTION 'invalid evidence attempt' USING ERRCODE='23514'; END IF;
 IF OLD.status='scan_error' AND NEW.status='scan_pending' AND NOT((processing AND NEW.scan_attempt=OLD.scan_attempt) OR(NOT processing AND NEW.scan_attempt=OLD.scan_attempt+1)) THEN
 RAISE EXCEPTION 'retry requires next attempt' USING ERRCODE='23514'; END IF;
 IF (NEW.released_generation,NEW.engine_version,NEW.definition_version,NEW.definitions_updated_at,NEW.scanned_at,NEW.failure_code)
 IS DISTINCT FROM(OLD.released_generation,OLD.engine_version,OLD.definition_version,OLD.definitions_updated_at,OLD.scanned_at,OLD.failure_code)
 AND NOT(processing AND OLD.status IN('scan_pending','scan_error')) AND NOT(OLD.status='scan_error' AND NEW.status='scan_pending' AND NEW.scan_attempt=OLD.scan_attempt+1 AND NEW.failure_code IS NULL
 AND(NEW.released_generation,NEW.engine_version,NEW.definition_version,NEW.definitions_updated_at,NEW.scanned_at)
 IS NOT DISTINCT FROM(OLD.released_generation,OLD.engine_version,OLD.definition_version,OLD.definitions_updated_at,OLD.scanned_at)) THEN
 RAISE EXCEPTION 'evidence verdict requires scoped worker identity' USING ERRCODE='42501'; END IF;
 IF OLD.status='scan_pending' AND NEW.status IN('available','rejected','scan_error') AND NOT processing THEN
 RAISE EXCEPTION 'evidence transition requires scoped worker identity' USING ERRCODE='42501'; END IF;
 IF NEW.status='available' AND(NEW.scanned_at<NEW.uploaded_at-interval '5 minutes' OR NEW.scanned_at>clock_timestamp()+interval '5 minutes'
 OR NEW.definitions_updated_at<NEW.scanned_at-interval '24 hours' OR NEW.definitions_updated_at>NEW.scanned_at+interval '5 minutes') THEN
 RAISE EXCEPTION 'invalid scanner provenance' USING ERRCODE='23514'; END IF;
 NEW.updated_at:=clock_timestamp();
 RETURN NEW;
END $$;
CREATE TRIGGER supply_evidence_identity BEFORE UPDATE ON supply_license_evidence FOR EACH ROW EXECUTE FUNCTION supply_evidence_identity();

ALTER POLICY tenant_isolation ON supply_license_evidence
 USING(hms_tenant_visible(tenant_id) AND CASE WHEN current_user='helivanta_supply_evidence_worker'
 THEN supply_evidence_worker_allowed(tenant_id,party_id,license_id,id)
 ELSE supply_roles_allowed(tenant_id,party_id,ARRAY['administrator','purchasing','finance','compliance','auditor']) END)
 WITH CHECK(CASE WHEN current_user='helivanta_supply_evidence_worker'
 THEN supply_evidence_worker_allowed(tenant_id,party_id,license_id,id)
 ELSE supply_roles_allowed(tenant_id,party_id,ARRAY['administrator','purchasing','compliance']) END);
GRANT USAGE ON SCHEMA public TO helivanta_supply_evidence_worker;
GRANT SELECT(tenant_id,id,party_id,license_id,license_version,content_type,size,sha256,status,scan_attempt,
 uploaded_at,source_generation,released_generation,engine_version,definition_version,definitions_updated_at,scanned_at,failure_code,expires_at,created_at,updated_at)
 ON supply_license_evidence TO helivanta_supply_evidence_worker;
GRANT UPDATE(status,released_generation,engine_version,definition_version,definitions_updated_at,scanned_at,failure_code,updated_at)
 ON supply_license_evidence TO helivanta_supply_evidence_worker;
GRANT INSERT ON audit_events,outbox_events TO helivanta_supply_evidence_worker;
GRANT SELECT(id) ON outbox_events TO helivanta_supply_evidence_worker;
-- Restrictive policies survive permissive policy changes by other modules.
CREATE POLICY supply_worker_audit_boundary ON audit_events AS RESTRICTIVE FOR INSERT TO helivanta_supply_evidence_worker
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND actor_subject='service:supply-evidence-worker' AND resource_type='supply_license_evidence'
 AND resource_id=NULLIF(current_setting('app.supply_file',true),'')::uuid AND action='update'
 AND cardinality(changed_fields) BETWEEN 1 AND 8
 AND changed_fields<@ARRAY['status','released_generation','engine_version','definition_version','definitions_updated_at','scanned_at','failure_code','updated_at']::text[]
 AND occurred_at BETWEEN clock_timestamp()-interval '5 minutes' AND clock_timestamp()+interval '5 minutes');
CREATE POLICY supply_worker_outbox_boundary ON outbox_events AS RESTRICTIVE FOR INSERT TO helivanta_supply_evidence_worker
 WITH CHECK(COALESCE(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND subject='helivanta.in.supply.evidence_changed.v1'
 AND jsonb_typeof(payload)='object' AND payload-ARRAY['event_id','type','version','occurred_at','tenant_id','data']='{}'::jsonb
 AND payload->>'event_id'=id::text AND payload->>'type'=subject AND payload->'version'='1'::jsonb AND payload->>'tenant_id'=tenant_id::text
 AND jsonb_typeof(payload->'occurred_at')='string' AND length(payload->>'occurred_at') BETWEEN 20 AND 35
 AND (payload->>'occurred_at')::timestamptz BETWEEN created_at-interval '5 minutes' AND created_at+interval '5 minutes'
 AND jsonb_typeof(payload->'data')='object' AND (payload->'data')-ARRAY['party_id','license_id','file_id','scan_attempt','status']='{}'::jsonb
 AND jsonb_typeof(payload->'data'->'scan_attempt')='number'
 AND payload->'data'->>'status' IN('available','rejected','scan_error')
 AND payload->'data'->>'party_id'=current_setting('app.supply_party',true)
 AND payload->'data'->>'license_id'=current_setting('app.supply_license',true)
 AND payload->'data'->>'file_id'=current_setting('app.supply_file',true)
 AND EXISTS(SELECT 1 FROM supply_license_evidence e WHERE supply_evidence_worker_allowed(e.tenant_id,e.party_id,e.license_id,e.id)
 AND e.tenant_id=outbox_events.tenant_id AND e.status=payload->'data'->>'status' AND e.scan_attempt::text=payload->'data'->>'scan_attempt'),false));
