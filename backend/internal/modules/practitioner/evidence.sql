SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
ALTER TABLE practitioner_registration_revisions ADD CONSTRAINT practitioner_revision_evidence_parent UNIQUE(tenant_id,practitioner_id,registration_id,id);
CREATE TABLE practitioner_registration_evidence (
 tenant_id uuid NOT NULL,
 practitioner_id uuid NOT NULL,
 registration_id uuid NOT NULL,
 revision_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7' AND substring(id::text,20,1) IN ('8','9','a','b')),
 content_type text NOT NULL CHECK(content_type IN ('image/png','image/jpeg')),
 size bigint NOT NULL CHECK(size BETWEEN 1 AND 10485760),
 sha256 text NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'),
 status text NOT NULL DEFAULT 'upload_pending' CHECK(status='upload_pending'),
 created_by text NOT NULL CHECK(octet_length(created_by) BETWEEN 1 AND 512),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 expires_at timestamptz NOT NULL DEFAULT clock_timestamp()+interval '1 hour',
 PRIMARY KEY(tenant_id,id),
 FOREIGN KEY(tenant_id,practitioner_id,registration_id,revision_id) REFERENCES practitioner_registration_revisions(tenant_id,practitioner_id,registration_id,id),
 CHECK(expires_at>created_at)
);
CREATE INDEX practitioner_evidence_page ON practitioner_registration_evidence(tenant_id,practitioner_id,registration_id,revision_id,created_at DESC,id DESC);
ALTER TABLE practitioner_registration_evidence ENABLE ROW LEVEL SECURITY;
ALTER TABLE practitioner_registration_evidence FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON practitioner_registration_evidence
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.practitioner_evidence_access',true)='true')
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.practitioner_evidence_manage',true)='true' AND created_by=NULLIF(current_setting('app.actor_subject',true),''));
CREATE FUNCTION practitioner_evidence_intent_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE evidence_count bigint;
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'practitioner evidence intent is immutable' USING ERRCODE='23514'; END IF;
 IF current_setting('app.practitioner_evidence_manage',true) IS DISTINCT FROM 'true' OR NEW.created_by IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'') OR NEW.status<>'upload_pending' THEN RAISE EXCEPTION 'evidence intake authority required' USING ERRCODE='42501'; END IF;
 PERFORM 1 FROM practitioner_registration_revisions WHERE tenant_id=NEW.tenant_id AND practitioner_id=NEW.practitioner_id AND registration_id=NEW.registration_id AND id=NEW.revision_id FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'evidence revision unavailable' USING ERRCODE='23503'; END IF;
 IF NOT EXISTS(SELECT 1 FROM practitioner_registration_evidence WHERE tenant_id=NEW.tenant_id AND id=NEW.id) THEN
  SELECT count(*) INTO evidence_count FROM practitioner_registration_evidence WHERE tenant_id=NEW.tenant_id AND practitioner_id=NEW.practitioner_id AND registration_id=NEW.registration_id AND revision_id=NEW.revision_id;
  IF evidence_count>=10 THEN RAISE EXCEPTION 'registration revision evidence limit' USING ERRCODE='23514',CONSTRAINT='practitioner_evidence_limit'; END IF;
 END IF;
 NEW.created_at:=clock_timestamp(); NEW.updated_at:=NEW.created_at; NEW.expires_at:=NEW.created_at+interval '1 hour';
 RETURN NEW;
END $$;
CREATE TRIGGER practitioner_evidence_intent_guard BEFORE INSERT OR UPDATE OR DELETE ON practitioner_registration_evidence FOR EACH ROW EXECUTE FUNCTION practitioner_evidence_intent_guard();
CREATE TRIGGER practitioner_evidence_audit AFTER INSERT ON practitioner_registration_evidence FOR EACH ROW EXECUTE FUNCTION audit_record_change();
