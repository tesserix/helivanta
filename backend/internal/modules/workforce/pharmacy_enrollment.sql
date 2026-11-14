-- ADR-0054 slice 4a: enrollment admits a hospital or a pharmacy; a supplier or an unregistered establishment is refused.
CREATE FUNCTION workforce_require_enrollment_establishment() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS (SELECT 1 FROM workforce_hospitals h WHERE h.tenant_id=NEW.tenant_id AND h.id=NEW.hospital_id AND h.establishment_kind IN ('hospital','pharmacy')) THEN
  RAISE EXCEPTION 'workforce establishment does not keep Staff enrollment' USING ERRCODE='23503', CONSTRAINT='workforce_hospital_establishment';
 END IF;
 RETURN NEW;
END $$;
DROP TRIGGER workforce_hospital_establishment ON workforce_enrollment_addresses;
DROP TRIGGER workforce_hospital_establishment ON workforce_enrollment_credentials;
DROP TRIGGER workforce_hospital_establishment ON workforce_enrollment_emergency_contacts;
DROP TRIGGER workforce_hospital_establishment ON workforce_enrollment_experiences;
DROP TRIGGER workforce_hospital_establishment ON workforce_enrollment_profile;
DROP TRIGGER workforce_hospital_establishment ON workforce_enrollment_qualifications;
CREATE TRIGGER workforce_hospital_establishment BEFORE INSERT OR UPDATE OF tenant_id,hospital_id ON workforce_enrollment_addresses FOR EACH ROW EXECUTE FUNCTION workforce_require_enrollment_establishment();
CREATE TRIGGER workforce_hospital_establishment BEFORE INSERT OR UPDATE OF tenant_id,hospital_id ON workforce_enrollment_credentials FOR EACH ROW EXECUTE FUNCTION workforce_require_enrollment_establishment();
CREATE TRIGGER workforce_hospital_establishment BEFORE INSERT OR UPDATE OF tenant_id,hospital_id ON workforce_enrollment_emergency_contacts FOR EACH ROW EXECUTE FUNCTION workforce_require_enrollment_establishment();
CREATE TRIGGER workforce_hospital_establishment BEFORE INSERT OR UPDATE OF tenant_id,hospital_id ON workforce_enrollment_experiences FOR EACH ROW EXECUTE FUNCTION workforce_require_enrollment_establishment();
CREATE TRIGGER workforce_hospital_establishment BEFORE INSERT OR UPDATE OF tenant_id,hospital_id ON workforce_enrollment_profile FOR EACH ROW EXECUTE FUNCTION workforce_require_enrollment_establishment();
CREATE TRIGGER workforce_hospital_establishment BEFORE INSERT OR UPDATE OF tenant_id,hospital_id ON workforce_enrollment_qualifications FOR EACH ROW EXECUTE FUNCTION workforce_require_enrollment_establishment();
