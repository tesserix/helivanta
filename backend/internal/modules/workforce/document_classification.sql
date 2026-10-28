SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE workforce_document_classification_revisions (
 tenant_id uuid NOT NULL,hospital_id uuid NOT NULL,file_id uuid NOT NULL,
 id uuid NOT NULL,version bigint NOT NULL CHECK(version BETWEEN 1 AND 9007199254740991),
 classification_revision_id uuid,
 classification_kind text NOT NULL DEFAULT 'document_category' CHECK(classification_kind='document_category'),
 actor_subject text NOT NULL CHECK(length(actor_subject) BETWEEN 1 AND 255),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,hospital_id,id),
 UNIQUE(tenant_id,hospital_id,file_id,version,id),
 UNIQUE(tenant_id,hospital_id,file_id,version),
 FOREIGN KEY(tenant_id,hospital_id,file_id) REFERENCES workforce_documents(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,classification_revision_id,classification_kind)
  REFERENCES workforce_hr_master_revisions(tenant_id,hospital_id,id,kind) ON DELETE RESTRICT
);
CREATE INDEX workforce_document_classification_history_page ON workforce_document_classification_revisions(tenant_id,hospital_id,file_id,created_at DESC,id DESC);
CREATE INDEX workforce_document_classification_master_ref ON workforce_document_classification_revisions(tenant_id,hospital_id,classification_revision_id);
CREATE TABLE workforce_document_classifications (
 tenant_id uuid NOT NULL,hospital_id uuid NOT NULL,id uuid NOT NULL,
 version bigint NOT NULL CHECK(version BETWEEN 1 AND 9007199254740991),revision_id uuid NOT NULL,
 PRIMARY KEY(tenant_id,hospital_id,id),
 FOREIGN KEY(tenant_id,hospital_id,id,version,revision_id)
  REFERENCES workforce_document_classification_revisions(tenant_id,hospital_id,file_id,version,id) ON DELETE RESTRICT
);
CREATE FUNCTION workforce_document_classification_identity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.version<>1 THEN
   RAISE EXCEPTION 'classification must start at version one' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;
 IF (NEW.tenant_id,NEW.hospital_id,NEW.id) IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.id)
   OR NEW.version<>OLD.version+1 OR NEW.revision_id=OLD.revision_id THEN
  RAISE EXCEPTION 'classification identity and version must be preserved' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_document_classification_identity BEFORE INSERT OR UPDATE ON workforce_document_classifications
 FOR EACH ROW EXECUTE FUNCTION workforce_document_classification_identity();
CREATE TRIGGER workforce_document_classification_no_delete BEFORE DELETE OR TRUNCATE ON workforce_document_classifications
 FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal();
CREATE TRIGGER workforce_document_classification_history_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON workforce_document_classification_revisions
 FOR EACH STATEMENT EXECUTE FUNCTION workforce_audit_immutable();
ALTER TABLE workforce_document_classifications ENABLE ROW LEVEL SECURITY;
ALTER TABLE workforce_document_classifications FORCE ROW LEVEL SECURITY;
ALTER TABLE workforce_document_classification_revisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE workforce_document_classification_revisions FORCE ROW LEVEL SECURITY;
CREATE POLICY classification_isolation ON workforce_document_classifications
 USING(hms_tenant_visible(tenant_id) AND workforce_enrollment_allowed(tenant_id,hospital_id))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND workforce_enrollment_allowed(tenant_id,hospital_id));
CREATE POLICY classification_revision_isolation ON workforce_document_classification_revisions
 USING(hms_tenant_visible(tenant_id) AND workforce_enrollment_allowed(tenant_id,hospital_id))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND workforce_enrollment_allowed(tenant_id,hospital_id));
CREATE TRIGGER workforce_document_classification_audit AFTER INSERT OR UPDATE OR DELETE ON workforce_document_classifications
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER workforce_document_classification_revision_audit AFTER INSERT OR UPDATE OR DELETE ON workforce_document_classification_revisions
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();
