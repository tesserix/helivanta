SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

-- ADR-0054: a registry row is a hospital or a standalone pharmacy employer; existing rows read as hospitals.
ALTER TABLE workforce_hospitals
 ADD COLUMN establishment_kind text NOT NULL DEFAULT 'hospital',
 ADD COLUMN pharmacy_ownership_type text,
 ADD CONSTRAINT workforce_establishment_kind CHECK (establishment_kind IN ('hospital','pharmacy')),
 ADD CONSTRAINT workforce_establishment_pharmacy_ownership CHECK (
  (establishment_kind='pharmacy' AND pharmacy_ownership_type IS NOT NULL AND pharmacy_ownership_type IN ('independent','chain'))
  OR (establishment_kind<>'pharmacy' AND pharmacy_ownership_type IS NULL)),
 DROP CONSTRAINT workforce_hospitals_name_check,
 ADD CONSTRAINT workforce_establishment_name CHECK (length(btrim(name)) BETWEEN 1 AND CASE establishment_kind WHEN 'pharmacy' THEN 200 ELSE 150 END);

-- A pharmacy name mirrors its immutable Pharma organization, so only hospital names may change.
CREATE FUNCTION workforce_establishment_identity_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.tenant_id IS DISTINCT FROM OLD.tenant_id OR NEW.id IS DISTINCT FROM OLD.id
  OR NEW.establishment_kind IS DISTINCT FROM OLD.establishment_kind
  OR NEW.pharmacy_ownership_type IS DISTINCT FROM OLD.pharmacy_ownership_type
  OR (OLD.establishment_kind='pharmacy' AND NEW.name IS DISTINCT FROM OLD.name) THEN
  RAISE EXCEPTION 'workforce establishment identity is immutable' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_establishment_identity_immutable BEFORE UPDATE ON workforce_hospitals
 FOR EACH ROW EXECUTE FUNCTION workforce_establishment_identity_immutable();

-- Fails closed: an unregistered, invisible or pharmacy establishment is refused until a later slice lifts the guard per table.
CREATE FUNCTION workforce_require_hospital_establishment() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS (SELECT 1 FROM workforce_hospitals h WHERE h.tenant_id=NEW.tenant_id AND h.id=NEW.hospital_id AND h.establishment_kind='hospital') THEN
  RAISE EXCEPTION 'workforce establishment is not a registered hospital' USING ERRCODE='23503', CONSTRAINT='workforce_hospital_establishment';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_hospital_establishment BEFORE INSERT OR UPDATE OF tenant_id,hospital_id ON workforce_attendance_periods FOR EACH ROW EXECUTE FUNCTION workforce_require_hospital_establishment();
CREATE TRIGGER workforce_hospital_establishment BEFORE INSERT OR UPDATE OF tenant_id,hospital_id ON workforce_attendance_settings FOR EACH ROW EXECUTE FUNCTION workforce_require_hospital_establishment();
CREATE TRIGGER workforce_hospital_establishment BEFORE INSERT OR UPDATE OF tenant_id,hospital_id ON workforce_benefit_plans FOR EACH ROW EXECUTE FUNCTION workforce_require_hospital_establishment();
CREATE TRIGGER workforce_hospital_establishment BEFORE INSERT OR UPDATE OF tenant_id,hospital_id ON workforce_credential_reminder_policies FOR EACH ROW EXECUTE FUNCTION workforce_require_hospital_establishment();
CREATE TRIGGER workforce_hospital_establishment BEFORE INSERT OR UPDATE OF tenant_id,hospital_id ON workforce_hospital_access FOR EACH ROW EXECUTE FUNCTION workforce_require_hospital_establishment();
CREATE TRIGGER workforce_hospital_establishment BEFORE INSERT OR UPDATE OF tenant_id,hospital_id ON workforce_hr_masters FOR EACH ROW EXECUTE FUNCTION workforce_require_hospital_establishment();
CREATE TRIGGER workforce_hospital_establishment BEFORE INSERT OR UPDATE OF tenant_id,hospital_id ON workforce_leave_configs FOR EACH ROW EXECUTE FUNCTION workforce_require_hospital_establishment();
CREATE TRIGGER workforce_hospital_establishment BEFORE INSERT OR UPDATE OF tenant_id,hospital_id ON workforce_leave_routing_revisions FOR EACH ROW EXECUTE FUNCTION workforce_require_hospital_establishment();
CREATE TRIGGER workforce_hospital_establishment BEFORE INSERT OR UPDATE OF tenant_id,hospital_id ON workforce_leave_settings FOR EACH ROW EXECUTE FUNCTION workforce_require_hospital_establishment();
CREATE TRIGGER workforce_hospital_establishment BEFORE INSERT OR UPDATE OF tenant_id,hospital_id ON workforce_overtime_settings FOR EACH ROW EXECUTE FUNCTION workforce_require_hospital_establishment();
CREATE TRIGGER workforce_hospital_establishment BEFORE INSERT OR UPDATE OF tenant_id,hospital_id ON workforce_roster_configs FOR EACH ROW EXECUTE FUNCTION workforce_require_hospital_establishment();
CREATE TRIGGER workforce_hospital_establishment BEFORE INSERT OR UPDATE OF tenant_id,hospital_id ON workforce_roster_settings FOR EACH ROW EXECUTE FUNCTION workforce_require_hospital_establishment();
CREATE TRIGGER workforce_hospital_establishment BEFORE INSERT OR UPDATE OF tenant_id,hospital_id ON workforce_rosters FOR EACH ROW EXECUTE FUNCTION workforce_require_hospital_establishment();
CREATE TRIGGER workforce_hospital_establishment BEFORE INSERT OR UPDATE OF tenant_id,hospital_id ON workforce_salary_heads FOR EACH ROW EXECUTE FUNCTION workforce_require_hospital_establishment();
CREATE TRIGGER workforce_hospital_establishment BEFORE INSERT OR UPDATE OF tenant_id,hospital_id ON workforce_salary_structures FOR EACH ROW EXECUTE FUNCTION workforce_require_hospital_establishment();
CREATE TRIGGER workforce_hospital_establishment BEFORE INSERT OR UPDATE OF tenant_id,hospital_id ON workforce_shifts FOR EACH ROW EXECUTE FUNCTION workforce_require_hospital_establishment();
CREATE TRIGGER workforce_hospital_establishment BEFORE INSERT OR UPDATE OF tenant_id,hospital_id ON workforce_staff FOR EACH ROW EXECUTE FUNCTION workforce_require_hospital_establishment();
CREATE TRIGGER workforce_hospital_establishment BEFORE INSERT OR UPDATE OF tenant_id,hospital_id ON workforce_statutory_rules FOR EACH ROW EXECUTE FUNCTION workforce_require_hospital_establishment();
