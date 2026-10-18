SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Reviewed validity without number or issuer, readable by hospital managers for rostering.
CREATE TABLE workforce_roster_credential_standing (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, staff_id uuid NOT NULL, credential_id uuid NOT NULL,
 "type" text NOT NULL, valid_from date NOT NULL, valid_until date NOT NULL CHECK (valid_until >= valid_from),
 version bigint NOT NULL CHECK (version > 0),
 PRIMARY KEY(tenant_id,hospital_id,staff_id,credential_id),
 FOREIGN KEY(tenant_id,hospital_id,staff_id,credential_id) REFERENCES workforce_credential_validity(tenant_id,hospital_id,staff_id,credential_id) ON DELETE RESTRICT
);

-- A standing row must equal its validity row, so only the sync trigger can produce one.
CREATE FUNCTION workforce_roster_standing_matches() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM workforce_credential_validity v WHERE (v.tenant_id,v.hospital_id,v.staff_id,v.credential_id,v.type,v.valid_from,v.valid_until,v.version)
   =(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.credential_id,NEW.type,NEW.valid_from,NEW.valid_until,NEW.version)) THEN
  RAISE EXCEPTION 'roster credential standing mirrors reviewed validity only' USING ERRCODE='23514', CONSTRAINT='workforce_roster_standing_matches';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_roster_standing_matches BEFORE INSERT OR UPDATE ON workforce_roster_credential_standing FOR EACH ROW EXECUTE FUNCTION workforce_roster_standing_matches();
CREATE TRIGGER workforce_roster_standing_no_delete BEFORE DELETE OR TRUNCATE ON workforce_roster_credential_standing FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal();

CREATE FUNCTION workforce_roster_standing_sync() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO workforce_roster_credential_standing(tenant_id,hospital_id,staff_id,credential_id,type,valid_from,valid_until,version)
 VALUES(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.credential_id,NEW.type,NEW.valid_from,NEW.valid_until,NEW.version)
 ON CONFLICT(tenant_id,hospital_id,staff_id,credential_id) DO UPDATE SET type=EXCLUDED.type,valid_from=EXCLUDED.valid_from,valid_until=EXCLUDED.valid_until,version=EXCLUDED.version;
 RETURN NULL;
END $$;
CREATE TRIGGER workforce_roster_standing_sync AFTER INSERT OR UPDATE ON workforce_credential_validity FOR EACH ROW EXECUTE FUNCTION workforce_roster_standing_sync();

-- The owner is subject to forced RLS; lift it for this transaction only to copy every tenant's reviewed validity.
ALTER TABLE workforce_credential_validity NO FORCE ROW LEVEL SECURITY;
INSERT INTO workforce_roster_credential_standing(tenant_id,hospital_id,staff_id,credential_id,type,valid_from,valid_until,version)
SELECT tenant_id,hospital_id,staff_id,credential_id,type,valid_from,valid_until,version FROM workforce_credential_validity;
ALTER TABLE workforce_credential_validity FORCE ROW LEVEL SECURITY;

ALTER TABLE workforce_roster_credential_standing ENABLE ROW LEVEL SECURITY;
ALTER TABLE workforce_roster_credential_standing FORCE ROW LEVEL SECURITY;
-- Roster managers read; only an enrollment-scoped credential approval writes, through the sync trigger.
CREATE POLICY hospital_isolation ON workforce_roster_credential_standing
 USING (hms_tenant_visible(tenant_id) AND (workforce_staff_allowed(tenant_id,hospital_id,true) OR workforce_enrollment_allowed(tenant_id,hospital_id)))
 WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND workforce_enrollment_allowed(tenant_id,hospital_id));

ALTER TABLE workforce_roster_revisions ADD COLUMN credential_clashes integer NOT NULL DEFAULT 0 CHECK (credential_clashes >= 0);

CREATE TABLE workforce_roster_credential_acknowledgements (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL DEFAULT gen_random_uuid(),
 revision_id uuid NOT NULL, allocation_id uuid NOT NULL, staff_id uuid NOT NULL, credential_id uuid NOT NULL,
 validity_version bigint NOT NULL CHECK (validity_version > 0),
 standing text NOT NULL CHECK (standing IN ('expired','not_yet_valid')),
 acknowledged_by_subject text NOT NULL CHECK (length(acknowledged_by_subject) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,hospital_id,id),
 UNIQUE(tenant_id,hospital_id,revision_id,allocation_id,credential_id),
 FOREIGN KEY(tenant_id,hospital_id,revision_id) REFERENCES workforce_roster_revisions(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,allocation_id) REFERENCES workforce_roster_allocations(tenant_id,hospital_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,staff_id,credential_id) REFERENCES workforce_roster_credential_standing(tenant_id,hospital_id,staff_id,credential_id) ON DELETE RESTRICT
);
CREATE TRIGGER workforce_roster_acknowledgement_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON workforce_roster_credential_acknowledgements FOR EACH STATEMENT EXECUTE FUNCTION workforce_audit_immutable();
CREATE TRIGGER workforce_roster_acknowledgement_audit AFTER INSERT ON workforce_roster_credential_acknowledgements FOR EACH ROW EXECUTE FUNCTION audit_record_change();

ALTER TABLE workforce_roster_credential_acknowledgements ENABLE ROW LEVEL SECURITY;
ALTER TABLE workforce_roster_credential_acknowledgements FORCE ROW LEVEL SECURITY;
CREATE POLICY hospital_isolation ON workforce_roster_credential_acknowledgements
 USING (hms_tenant_visible(tenant_id) AND workforce_staff_allowed(tenant_id,hospital_id,true))
 WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND workforce_staff_allowed(tenant_id,hospital_id,true) AND current_setting('app.workforce_rostering',true)='authorized');
