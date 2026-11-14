-- ADR-0054 slice 4d: a pharmacy keeps enrollment, credential and qualification files only; anything else stays hospital-only.
CREATE FUNCTION workforce_require_document_establishment() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS (SELECT 1 FROM workforce_hospitals h WHERE h.tenant_id=NEW.tenant_id AND h.id=NEW.hospital_id AND (h.establishment_kind='hospital' OR (h.establishment_kind='pharmacy' AND NEW.record_kind IN ('enrollment','credential','qualification')))) THEN
  RAISE EXCEPTION 'workforce establishment does not keep this Staff file' USING ERRCODE='23503', CONSTRAINT='workforce_hospital_establishment';
 END IF;
 RETURN NEW;
END $$;
DROP TRIGGER workforce_hospital_establishment ON workforce_documents;
CREATE TRIGGER workforce_hospital_establishment BEFORE INSERT OR UPDATE OF tenant_id,hospital_id,record_kind ON workforce_documents FOR EACH ROW EXECUTE FUNCTION workforce_require_document_establishment();
DROP TRIGGER workforce_hospital_establishment ON workforce_document_contributors;
CREATE TRIGGER workforce_hospital_establishment BEFORE INSERT OR UPDATE OF tenant_id,hospital_id ON workforce_document_contributors FOR EACH ROW EXECUTE FUNCTION workforce_require_enrollment_establishment();
-- A cleared classification names no HR master revision, so the master's refusal does not reach it.
CREATE TRIGGER workforce_hospital_establishment BEFORE INSERT OR UPDATE OF tenant_id,hospital_id ON workforce_document_classification_revisions FOR EACH ROW EXECUTE FUNCTION workforce_require_hospital_establishment();
