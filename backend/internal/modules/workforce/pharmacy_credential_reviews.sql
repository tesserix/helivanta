-- ADR-0054 slice 4e: a pharmacy reviews credentials and keeps reminders on its own HR time zone; roster standing stays hospital-only.
SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

DROP TRIGGER workforce_hospital_establishment ON workforce_credential_reviews;
CREATE TRIGGER workforce_hospital_establishment BEFORE INSERT OR UPDATE OF tenant_id,hospital_id ON workforce_credential_reviews FOR EACH ROW EXECUTE FUNCTION workforce_require_enrollment_establishment();
DROP TRIGGER workforce_hospital_establishment ON workforce_credential_reminder_policies;
CREATE TRIGGER workforce_hospital_establishment BEFORE INSERT OR UPDATE OF tenant_id,hospital_id ON workforce_credential_reminder_policies FOR EACH ROW EXECUTE FUNCTION workforce_require_enrollment_establishment();
CREATE TRIGGER workforce_hospital_establishment BEFORE INSERT OR UPDATE OF tenant_id,hospital_id ON workforce_roster_credential_standing FOR EACH ROW EXECUTE FUNCTION workforce_require_hospital_establishment();

-- Skips only a known pharmacy; any other establishment still reaches the hospital guard and fails closed.
CREATE OR REPLACE FUNCTION workforce_roster_standing_sync() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM workforce_hospitals h WHERE h.tenant_id=NEW.tenant_id AND h.id=NEW.hospital_id AND h.establishment_kind='pharmacy') THEN
  INSERT INTO workforce_roster_credential_standing(tenant_id,hospital_id,staff_id,credential_id,type,valid_from,valid_until,version)
  VALUES(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.credential_id,NEW.type,NEW.valid_from,NEW.valid_until,NEW.version)
  ON CONFLICT(tenant_id,hospital_id,staff_id,credential_id) DO UPDATE SET type=EXCLUDED.type,valid_from=EXCLUDED.valid_from,valid_until=EXCLUDED.valid_until,version=EXCLUDED.version;
 END IF;
 RETURN NULL;
END $$;

CREATE FUNCTION workforce_require_pharmacy_establishment() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS (SELECT 1 FROM workforce_hospitals h WHERE h.tenant_id=NEW.tenant_id AND h.id=NEW.hospital_id AND h.establishment_kind='pharmacy') THEN
  RAISE EXCEPTION 'workforce establishment is not a registered pharmacy' USING ERRCODE='23503', CONSTRAINT='workforce_pharmacy_establishment';
 END IF;
 RETURN NEW;
END $$;

CREATE TABLE workforce_pharmacy_time_zones (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL,
 timezone text NOT NULL CHECK (length(timezone) BETWEEN 1 AND 64),
 version bigint NOT NULL CHECK (version > 0),
 updated_by_subject text NOT NULL CHECK (length(updated_by_subject) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id),
 FOREIGN KEY(tenant_id,hospital_id) REFERENCES workforce_hospitals(tenant_id,id) ON DELETE RESTRICT
);

CREATE FUNCTION workforce_pharmacy_time_zone_version() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (TG_OP='INSERT' AND NEW.version<>1)
  OR (TG_OP='UPDATE' AND ((NEW.tenant_id,NEW.hospital_id,NEW.created_at) IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.created_at) OR NEW.version<>OLD.version+1)) THEN
  RAISE EXCEPTION 'pharmacy time zone identity is immutable and versions advance by one' USING ERRCODE='23514', CONSTRAINT='workforce_pharmacy_time_zone_version';
 END IF;
 RETURN NEW;
END $$;
-- BEFORE triggers fire by name: establishment, then zone, then version.
CREATE TRIGGER workforce_pharmacy_establishment BEFORE INSERT OR UPDATE OF tenant_id,hospital_id ON workforce_pharmacy_time_zones FOR EACH ROW EXECUTE FUNCTION workforce_require_pharmacy_establishment();
CREATE TRIGGER workforce_pharmacy_time_zone_valid BEFORE INSERT OR UPDATE ON workforce_pharmacy_time_zones FOR EACH ROW EXECUTE FUNCTION workforce_roster_timezone_valid();
CREATE TRIGGER workforce_pharmacy_time_zone_version BEFORE INSERT OR UPDATE ON workforce_pharmacy_time_zones FOR EACH ROW EXECUTE FUNCTION workforce_pharmacy_time_zone_version();
CREATE TRIGGER workforce_pharmacy_time_zone_no_delete BEFORE DELETE OR TRUNCATE ON workforce_pharmacy_time_zones FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal();

ALTER TABLE workforce_pharmacy_time_zones ENABLE ROW LEVEL SECURITY;
ALTER TABLE workforce_pharmacy_time_zones FORCE ROW LEVEL SECURITY;
CREATE POLICY enrollment_isolation ON workforce_pharmacy_time_zones
 USING(hms_tenant_visible(tenant_id) AND CASE WHEN current_user='helivanta_document_worker' THEN workforce_reminder_worker_hospital(tenant_id,hospital_id) ELSE workforce_enrollment_allowed(tenant_id,hospital_id) END)
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND CASE WHEN current_user='helivanta_document_worker' THEN false ELSE workforce_enrollment_allowed(tenant_id,hospital_id) END);
GRANT SELECT(tenant_id,hospital_id,timezone) ON workforce_pharmacy_time_zones TO helivanta_document_worker;
