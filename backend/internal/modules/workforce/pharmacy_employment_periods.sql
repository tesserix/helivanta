-- ADR-0054 slice 4b: a pharmacy keeps employment terms and their evidence; reviews and generations stay hospital-only.
DROP TRIGGER workforce_hospital_establishment ON workforce_employment_period_drafts;
CREATE TRIGGER workforce_hospital_establishment BEFORE INSERT OR UPDATE OF tenant_id,hospital_id ON workforce_employment_period_drafts FOR EACH ROW EXECUTE FUNCTION workforce_require_enrollment_establishment();
DROP TRIGGER workforce_hospital_establishment ON workforce_employment_evidence_bindings;
CREATE TRIGGER workforce_hospital_establishment BEFORE INSERT OR UPDATE OF tenant_id,hospital_id ON workforce_employment_evidence_bindings FOR EACH ROW EXECUTE FUNCTION workforce_require_enrollment_establishment();
