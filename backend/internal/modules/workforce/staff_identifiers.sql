SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE workforce_staff_identifiers (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, staff_id uuid NOT NULL,
 version bigint NOT NULL CHECK (version >= 0), revision_id uuid,
 pan_index text CHECK (pan_index ~ '^vault:v1:[A-Za-z0-9+/]+={0,2}$' AND length(pan_index) <= 200),
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 CHECK ((version = 0) = (revision_id IS NULL) AND (revision_id IS NOT NULL OR pan_index IS NULL)),
 PRIMARY KEY(tenant_id,hospital_id,staff_id),
 CONSTRAINT workforce_staff_identifier_pan_in_use UNIQUE(tenant_id,hospital_id,pan_index),
 FOREIGN KEY(tenant_id,hospital_id,staff_id) REFERENCES workforce_staff(tenant_id,hospital_id,id) ON DELETE RESTRICT
);

CREATE TABLE workforce_staff_identifier_revisions (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL, id uuid NOT NULL, staff_id uuid NOT NULL,
 sequence integer NOT NULL CHECK (sequence > 0), base_version bigint NOT NULL CHECK (base_version >= 0),
 status text NOT NULL CHECK (status IN ('pending','approved','rejected','withdrawn')), approved_version bigint,
 reason text NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 500),
 proposed_by_subject text NOT NULL CHECK (length(proposed_by_subject) BETWEEN 1 AND 200),
 decided_by_subject text CHECK (length(decided_by_subject) BETWEEN 1 AND 200), decided_at timestamptz,
 decision_note text NOT NULL CHECK (length(decision_note) <= 500),
 bank_account_ciphertext text CHECK (bank_account_ciphertext ~ '^vault:v[1-9][0-9]*:[A-Za-z0-9+/]+={0,2}$' AND length(bank_account_ciphertext) <= 512),
 bank_account_masked text CHECK (bank_account_masked ~ '^XXXX[0-9]{4}$'),
 bank_ifsc text CHECK (bank_ifsc ~ '^[A-Z]{4}0[A-Z0-9]{6}$'),
 bank_holder_name text CHECK (length(btrim(bank_holder_name)) BETWEEN 1 AND 150),
 pan_ciphertext text CHECK (pan_ciphertext ~ '^vault:v[1-9][0-9]*:[A-Za-z0-9+/]+={0,2}$' AND length(pan_ciphertext) <= 512),
 pan_masked text CHECK (pan_masked ~ '^XXXXXX[0-9]{3}[A-Z]$'),
 pan_index text CHECK (pan_index ~ '^vault:v1:[A-Za-z0-9+/]+={0,2}$' AND length(pan_index) <= 200),
 uan_ciphertext text CHECK (uan_ciphertext ~ '^vault:v[1-9][0-9]*:[A-Za-z0-9+/]+={0,2}$' AND length(uan_ciphertext) <= 512),
 uan_masked text CHECK (uan_masked ~ '^XXXXXXXX[0-9]{4}$'),
 created_at timestamptz NOT NULL DEFAULT now(), created_tx bigint NOT NULL DEFAULT txid_current(),
 CHECK ((status = 'pending') = (decided_by_subject IS NULL) AND (decided_by_subject IS NULL) = (decided_at IS NULL)),
 CHECK ((status = 'approved') = (approved_version IS NOT NULL) AND (approved_version IS NULL OR approved_version = base_version + 1)),
 CONSTRAINT workforce_staff_identifier_bank_whole CHECK ((bank_account_masked IS NULL) = (bank_ifsc IS NULL) AND (bank_ifsc IS NULL) = (bank_holder_name IS NULL)),
 -- Live revisions hold ciphertext exactly where they show a mask; decided-against ones keep only the mask.
 CONSTRAINT workforce_staff_identifier_sealed CHECK (CASE WHEN status IN ('pending','approved')
  THEN (bank_account_ciphertext IS NULL) = (bank_account_masked IS NULL) AND (pan_ciphertext IS NULL) = (pan_masked IS NULL) AND (pan_index IS NULL) = (pan_masked IS NULL) AND (uan_ciphertext IS NULL) = (uan_masked IS NULL)
  ELSE bank_account_ciphertext IS NULL AND pan_ciphertext IS NULL AND pan_index IS NULL AND uan_ciphertext IS NULL END),
 CONSTRAINT workforce_staff_identifier_separation CHECK (status NOT IN ('approved','rejected') OR decided_by_subject <> proposed_by_subject),
 CONSTRAINT workforce_staff_identifier_withdrawn_by_proposer CHECK (status <> 'withdrawn' OR decided_by_subject = proposed_by_subject),
 CONSTRAINT workforce_staff_identifier_rejection_note CHECK (status <> 'rejected' OR length(btrim(decision_note)) > 0),
 PRIMARY KEY(tenant_id,hospital_id,id),
 UNIQUE(tenant_id,hospital_id,staff_id,sequence), UNIQUE(tenant_id,hospital_id,staff_id,approved_version),
 UNIQUE(tenant_id,hospital_id,staff_id,approved_version,id),
 FOREIGN KEY(tenant_id,hospital_id,staff_id) REFERENCES workforce_staff_identifiers(tenant_id,hospital_id,staff_id) ON DELETE RESTRICT
);
CREATE UNIQUE INDEX workforce_staff_identifier_one_pending ON workforce_staff_identifier_revisions(tenant_id,hospital_id,staff_id) WHERE status = 'pending';
CREATE INDEX workforce_staff_identifier_history ON workforce_staff_identifier_revisions(tenant_id,hospital_id,staff_id,created_at DESC,id DESC);
CREATE UNIQUE INDEX workforce_staff_identifier_pending_pan ON workforce_staff_identifier_revisions(tenant_id,hospital_id,pan_index) WHERE status = 'pending';
ALTER TABLE workforce_staff_identifiers ADD CONSTRAINT workforce_staff_identifier_current
 FOREIGN KEY(tenant_id,hospital_id,staff_id,version,revision_id) REFERENCES workforce_staff_identifier_revisions(tenant_id,hospital_id,staff_id,approved_version,id) DEFERRABLE INITIALLY DEFERRED;

CREATE FUNCTION workforce_staff_identifier_proposal() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE parent workforce_staff_identifiers;
BEGIN
 SELECT * INTO parent FROM workforce_staff_identifiers s WHERE (s.tenant_id,s.hospital_id,s.staff_id)=(NEW.tenant_id,NEW.hospital_id,NEW.staff_id);
 IF NEW.status<>'pending' OR NEW.decided_by_subject IS NOT NULL OR NEW.approved_version IS NOT NULL OR NEW.base_version IS DISTINCT FROM parent.version THEN
  RAISE EXCEPTION 'a staff identifier proposal starts pending on the current version' USING ERRCODE='23514';
 END IF;
 IF NOT EXISTS(SELECT 1 FROM workforce_staff s WHERE (s.tenant_id,s.hospital_id,s.id)=(NEW.tenant_id,NEW.hospital_id,NEW.staff_id) AND s.employment_status='active') THEN
  RAISE EXCEPTION 'identifiers can only be proposed for an active staff member' USING ERRCODE='23514', CONSTRAINT='workforce_staff_identifier_staff_inactive';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_staff_identifier_proposal BEFORE INSERT ON workforce_staff_identifier_revisions FOR EACH ROW EXECUTE FUNCTION workforce_staff_identifier_proposal();

-- A proposal is decided once; rejecting or withdrawing it may also erase its ciphertext, and nothing else ever changes.
CREATE FUNCTION workforce_staff_identifier_decision() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE allowed text[] := ARRAY['status','approved_version','decided_by_subject','decided_at','decision_note'];
BEGIN
 IF NEW.status IN ('rejected','withdrawn') THEN
  allowed := allowed || ARRAY['bank_account_ciphertext','pan_ciphertext','pan_index','uan_ciphertext'];
 END IF;
 IF OLD.status<>'pending' OR NEW.status='pending' OR (to_jsonb(NEW) - allowed) IS DISTINCT FROM (to_jsonb(OLD) - allowed) THEN
  RAISE EXCEPTION 'a staff identifier proposal is decided once and its content is immutable' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_staff_identifier_decision BEFORE UPDATE ON workforce_staff_identifier_revisions FOR EACH ROW EXECUTE FUNCTION workforce_staff_identifier_decision();

CREATE FUNCTION workforce_staff_identifier_approved() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.status='approved' AND NOT EXISTS(SELECT 1 FROM workforce_staff_identifiers s WHERE (s.tenant_id,s.hospital_id,s.staff_id,s.revision_id)=(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.id)) THEN
  RAISE EXCEPTION 'approved staff identifiers must become the ones in force' USING ERRCODE='23514';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER workforce_staff_identifier_approved AFTER UPDATE ON workforce_staff_identifier_revisions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION workforce_staff_identifier_approved();

-- The in-force PAN fingerprint is copied from the revision, never written by the caller.
CREATE FUNCTION workforce_staff_identifier_advance() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.created_at) IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.staff_id,OLD.created_at) OR NEW.version<>OLD.version+1 THEN
  RAISE EXCEPTION 'staff identifiers keep their staff member and advance one approved version at a time' USING ERRCODE='23514';
 END IF;
 SELECT r.pan_index INTO NEW.pan_index FROM workforce_staff_identifier_revisions r WHERE (r.tenant_id,r.hospital_id,r.id)=(NEW.tenant_id,NEW.hospital_id,NEW.revision_id);
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_staff_identifier_advance BEFORE UPDATE ON workforce_staff_identifiers FOR EACH ROW EXECUTE FUNCTION workforce_staff_identifier_advance();

DO $$DECLARE tbl text;BEGIN
 FOREACH tbl IN ARRAY ARRAY['workforce_staff_identifiers','workforce_staff_identifier_revisions'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',tbl);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',tbl);
  EXECUTE format('CREATE POLICY hospital_isolation ON %I USING (hms_tenant_visible(tenant_id) AND workforce_staff_allowed(tenant_id,hospital_id,false)) WITH CHECK(tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND workforce_staff_allowed(tenant_id,hospital_id,true) AND current_setting(''app.workforce_payroll'',true)=''authorized'')',tbl);
  EXECUTE format('CREATE TRIGGER workforce_payroll_no_delete BEFORE DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal()',tbl);
 END LOOP;
END $$;
