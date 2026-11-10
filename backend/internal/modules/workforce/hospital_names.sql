-- A hospital's name is versioned by the database so that no writer can skip or set a version.
ALTER TABLE workforce_hospitals ADD COLUMN name_version bigint NOT NULL DEFAULT 1 CHECK (name_version >= 1);

CREATE FUNCTION workforce_hospital_name_version() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP = 'INSERT' THEN
    NEW.name_version := 1;
  ELSIF NEW.name IS DISTINCT FROM OLD.name THEN
    NEW.name_version := OLD.name_version + 1;
  ELSE
    NEW.name_version := OLD.name_version;
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER workforce_hospital_name_version BEFORE INSERT OR UPDATE ON workforce_hospitals
  FOR EACH ROW EXECUTE FUNCTION workforce_hospital_name_version();

-- The marker lives beside the hospital, so announcing never rewrites (or audits) the hospital row.
CREATE TABLE workforce_hospital_name_announcements (
  tenant_id uuid NOT NULL,
  hospital_id uuid NOT NULL,
  announced_version bigint NOT NULL CHECK (announced_version >= 1),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (tenant_id, hospital_id),
  FOREIGN KEY (tenant_id, hospital_id) REFERENCES workforce_hospitals (tenant_id, id)
);
ALTER TABLE workforce_hospital_name_announcements ENABLE ROW LEVEL SECURITY;
ALTER TABLE workforce_hospital_name_announcements FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workforce_hospital_name_announcements
  USING (hms_tenant_visible(tenant_id) AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid)
  WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid);

-- The announcer's one cross-tenant read: ids and versions, never names.
GRANT SELECT (tenant_id, id, establishment_kind, name_version) ON workforce_hospitals TO helivanta_system;
GRANT SELECT (tenant_id, hospital_id, announced_version) ON workforce_hospital_name_announcements TO helivanta_system;

-- Only registered hospitals are announced; a pharmacy or supplier row can never gain a marker.
CREATE TRIGGER workforce_hospital_establishment BEFORE INSERT OR UPDATE OF tenant_id,hospital_id ON workforce_hospital_name_announcements FOR EACH ROW EXECUTE FUNCTION workforce_require_hospital_establishment();
