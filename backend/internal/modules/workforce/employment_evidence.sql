SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE TABLE workforce_employment_evidence_bindings (
 tenant_id uuid NOT NULL,hospital_id uuid NOT NULL,staff_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 revision_id uuid NOT NULL CHECK(substring(revision_id::text,15,1)='7'),
 period_id uuid NOT NULL,period_revision_id uuid NOT NULL,
 period_version bigint NOT NULL CHECK(period_version BETWEEN 1 AND 9007199254740990),
 version bigint NOT NULL CHECK(version BETWEEN 1 AND 2),
 status text NOT NULL CHECK(status IN('attached','withdrawn')),
 staff_version bigint NOT NULL CHECK(staff_version>0),employment_epoch bigint NOT NULL CHECK(employment_epoch>0),
 document_id uuid NOT NULL,document_record_version bigint NOT NULL CHECK(document_record_version>0),
 filename text NOT NULL CHECK(length(filename) BETWEEN 1 AND 180),content_type text NOT NULL,size bigint NOT NULL CHECK(size>0),
 sha256 text NOT NULL CHECK(sha256 ~ '^[a-f0-9]{64}$'),
 source_generation bigint NOT NULL CHECK(source_generation>0),released_generation bigint NOT NULL CHECK(released_generation>0),
 scan_attempt bigint NOT NULL CHECK(scan_attempt>0),scanned_at timestamptz NOT NULL,
 engine_version text NOT NULL,definition_version bigint NOT NULL,definitions_updated_at timestamptz NOT NULL,
 proposed_by_subject text NOT NULL CHECK(length(proposed_by_subject) BETWEEN 1 AND 200),
 actor_subject text NOT NULL CHECK(length(actor_subject) BETWEEN 1 AND 200),
 reason text NOT NULL CHECK(reason=btrim(reason) AND length(reason) BETWEEN 1 AND 500),
 input_hash text NOT NULL CHECK(input_hash ~ '^[a-f0-9]{64}$'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,hospital_id,id),
 FOREIGN KEY(tenant_id,hospital_id,staff_id) REFERENCES workforce_staff(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,period_id,period_version) REFERENCES workforce_employment_period_history(tenant_id,hospital_id,id,version) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,document_id) REFERENCES workforce_documents(tenant_id,hospital_id,id) ON DELETE RESTRICT
);
CREATE INDEX workforce_employment_evidence_page ON workforce_employment_evidence_bindings(tenant_id,hospital_id,staff_id,period_id,period_version,created_at DESC,id DESC);
CREATE INDEX workforce_employment_evidence_document ON workforce_employment_evidence_bindings(tenant_id,hospital_id,document_id);
CREATE UNIQUE INDEX workforce_employment_evidence_active_document ON workforce_employment_evidence_bindings(tenant_id,hospital_id,period_id,period_version,document_id) WHERE status='attached';
CREATE TABLE workforce_employment_evidence_history(LIKE workforce_employment_evidence_bindings INCLUDING DEFAULTS INCLUDING CONSTRAINTS);
ALTER TABLE workforce_employment_evidence_history ADD PRIMARY KEY(tenant_id,hospital_id,id,version);
ALTER TABLE workforce_employment_evidence_history ADD FOREIGN KEY(tenant_id,hospital_id,id) REFERENCES workforce_employment_evidence_bindings(tenant_id,hospital_id,id) ON DELETE RESTRICT;
CREATE UNIQUE INDEX workforce_employment_evidence_receipt ON workforce_employment_evidence_history(tenant_id,hospital_id,revision_id);
CREATE INDEX workforce_employment_evidence_history_page ON workforce_employment_evidence_history(tenant_id,hospital_id,id,updated_at DESC);

CREATE FUNCTION workforce_employment_evidence_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE active_count int;
BEGIN
 IF TG_OP='DELETE' OR current_setting('app.workforce_employment_evidence',true) IS DISTINCT FROM 'write' OR NEW.actor_subject IS DISTINCT FROM NULLIF(current_setting('app.workforce_subject',true),'') OR workforce_enrollment_allowed(NEW.tenant_id,NEW.hospital_id) IS NOT TRUE THEN
  RAISE EXCEPTION 'employment evidence requires current owner authority' USING ERRCODE='42501';
 END IF;
 IF TG_OP='UPDATE' THEN
  IF OLD.status<>'attached' OR NEW.status<>'withdrawn' OR NEW.version<>OLD.version+1 OR NEW.revision_id=OLD.revision_id OR (to_jsonb(NEW)-ARRAY['revision_id','version','status','reason','actor_subject','input_hash','updated_at']) IS DISTINCT FROM (to_jsonb(OLD)-ARRAY['revision_id','version','status','reason','actor_subject','input_hash','updated_at']) THEN
   RAISE EXCEPTION 'employment evidence only permits attributable withdrawal' USING ERRCODE='23514';
  END IF;
  NEW.updated_at:=GREATEST(clock_timestamp(),OLD.updated_at+interval '1 microsecond');
  RETURN NEW;
 END IF;
 IF NEW.version<>1 OR NEW.status<>'attached' OR NEW.proposed_by_subject<>NEW.actor_subject THEN RAISE EXCEPTION 'invalid initial evidence binding' USING ERRCODE='23514'; END IF;
 IF current_setting('transaction_isolation') NOT IN ('read committed','read uncommitted') THEN RAISE EXCEPTION 'evidence requires a current snapshot' USING ERRCODE='25001'; END IF;
 PERFORM workforce_lock_supervision(NEW.tenant_id,NEW.hospital_id);
 IF NOT EXISTS(SELECT 1 FROM workforce_employment_period_drafts p JOIN workforce_staff s ON s.tenant_id=p.tenant_id AND s.hospital_id=p.hospital_id AND s.id=p.staff_id WHERE p.tenant_id=NEW.tenant_id AND p.hospital_id=NEW.hospital_id AND p.staff_id=NEW.staff_id AND p.id=NEW.period_id AND p.version=NEW.period_version AND p.revision_id=NEW.period_revision_id AND p.staff_version=NEW.staff_version AND p.employment_epoch=NEW.employment_epoch AND s.version=NEW.staff_version AND s.employment_epoch=NEW.employment_epoch) THEN
  RAISE EXCEPTION 'employment evidence source changed' USING ERRCODE='23514',CONSTRAINT='workforce_employment_evidence_source';
 END IF;
 IF NOT EXISTS(SELECT 1 FROM workforce_documents d WHERE d.tenant_id=NEW.tenant_id AND d.hospital_id=NEW.hospital_id AND d.staff_id=NEW.staff_id AND d.id=NEW.document_id AND d.record_kind='enrollment' AND d.record_id=NEW.staff_id AND d.category='employment_evidence' AND d.status='available' AND d.record_version=NEW.staff_version AND (d.record_version,d.filename,d.content_type,d.size,d.sha256,d.source_generation,d.released_generation,d.scan_attempt,d.scanned_at,d.engine_version,d.definition_version,d.definitions_updated_at) IS NOT DISTINCT FROM (NEW.document_record_version,NEW.filename,NEW.content_type,NEW.size,NEW.sha256,NEW.source_generation,NEW.released_generation,NEW.scan_attempt,NEW.scanned_at,NEW.engine_version,NEW.definition_version,NEW.definitions_updated_at)) THEN
  RAISE EXCEPTION 'employment evidence must name exact released bytes' USING ERRCODE='23514',CONSTRAINT='workforce_employment_evidence_release';
 END IF;
 SELECT count(*) INTO active_count FROM workforce_employment_evidence_bindings WHERE tenant_id=NEW.tenant_id AND hospital_id=NEW.hospital_id AND period_id=NEW.period_id AND period_version=NEW.period_version AND status='attached';
 IF active_count>=20 THEN RAISE EXCEPTION 'employment evidence set is full' USING ERRCODE='23514',CONSTRAINT='workforce_employment_evidence_limit'; END IF;
 NEW.created_at:=clock_timestamp();NEW.updated_at:=NEW.created_at;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_employment_evidence_guard BEFORE INSERT OR UPDATE OR DELETE ON workforce_employment_evidence_bindings FOR EACH ROW EXECUTE FUNCTION workforce_employment_evidence_guard();
CREATE FUNCTION workforce_employment_evidence_snapshot() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN INSERT INTO workforce_employment_evidence_history SELECT NEW.*;RETURN NULL;END $$;
CREATE TRIGGER workforce_employment_evidence_snapshot AFTER INSERT OR UPDATE ON workforce_employment_evidence_bindings FOR EACH ROW EXECUTE FUNCTION workforce_employment_evidence_snapshot();
CREATE FUNCTION workforce_employment_evidence_history_guard() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF TG_OP<>'INSERT' OR pg_trigger_depth()<>2 THEN RAISE EXCEPTION 'employment evidence history is immutable' USING ERRCODE='23514';END IF;RETURN NEW;END $$;
CREATE TRIGGER workforce_employment_evidence_history_guard BEFORE INSERT OR UPDATE OR DELETE ON workforce_employment_evidence_history FOR EACH ROW EXECUTE FUNCTION workforce_employment_evidence_history_guard();
DO $$ DECLARE t text;BEGIN
 FOREACH t IN ARRAY ARRAY['workforce_employment_evidence_bindings','workforce_employment_evidence_history'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',t);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',t);
  EXECUTE format('CREATE POLICY employment_evidence_scope ON %I USING(hms_tenant_visible(tenant_id) AND workforce_enrollment_allowed(tenant_id,hospital_id) AND current_setting(''app.workforce_employment_evidence'',true) IN (''read'',''write'')) WITH CHECK(tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND workforce_enrollment_allowed(tenant_id,hospital_id) AND current_setting(''app.workforce_employment_evidence'',true)=''write'')',t);
  EXECUTE format('CREATE POLICY employment_evidence_no_delete ON %I AS RESTRICTIVE FOR DELETE USING(false)',t);
  EXECUTE format('CREATE TRIGGER employment_evidence_no_truncate BEFORE TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal()',t);
 END LOOP;
END $$;
