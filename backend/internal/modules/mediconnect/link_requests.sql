ALTER TABLE mediconnect_accounts ADD CONSTRAINT mediconnect_account_owner_key
 UNIQUE (tenant_id, id, subject);
CREATE TABLE mediconnect_link_requests (
 tenant_id uuid NOT NULL,
 id uuid NOT NULL CHECK (substring(id::text, 15, 1) = '7'),
 account_id uuid NOT NULL,
 subject text NOT NULL,
 hospital_id uuid NOT NULL CHECK (hospital_id <> '00000000-0000-0000-0000-000000000000'),
 patient_id uuid NOT NULL CHECK (patient_id <> '00000000-0000-0000-0000-000000000000'),
 relationship text NOT NULL CHECK (relationship IN ('self','child','parent','spouse','sibling','ward','other')),
 status text NOT NULL DEFAULT 'pending_verification' CHECK (status IN ('pending_verification','withdrawn')),
 version bigint NOT NULL DEFAULT 1,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 withdrawn_at timestamptz,
 PRIMARY KEY (tenant_id, id),
 FOREIGN KEY (tenant_id, account_id, subject) REFERENCES mediconnect_accounts(tenant_id, id, subject),
 CHECK ((status = 'pending_verification' AND version = 1 AND withdrawn_at IS NULL)
 OR (status = 'withdrawn' AND version = 2 AND withdrawn_at IS NOT NULL))
);
CREATE INDEX ON mediconnect_link_requests (tenant_id, subject, created_at DESC, id DESC);
CREATE INDEX ON mediconnect_link_requests (tenant_id, account_id, subject);
ALTER TABLE mediconnect_link_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE mediconnect_link_requests FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON mediconnect_link_requests
 USING (hms_tenant_visible(tenant_id)
 AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
 AND subject = NULLIF(current_setting('app.actor_subject', true), ''))
 WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
 AND subject = NULLIF(current_setting('app.actor_subject', true), ''));
CREATE FUNCTION mediconnect_link_request_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP = 'DELETE' THEN
  RAISE EXCEPTION 'link requests preserve history' USING ERRCODE = '23514';
 END IF;
 IF (NEW.tenant_id, NEW.id, NEW.account_id, NEW.subject, NEW.hospital_id, NEW.patient_id, NEW.relationship, NEW.created_at)
 IS DISTINCT FROM (OLD.tenant_id, OLD.id, OLD.account_id, OLD.subject, OLD.hospital_id, OLD.patient_id, OLD.relationship, OLD.created_at)
 OR OLD.status <> 'pending_verification' OR NEW.status <> 'withdrawn' THEN
  RAISE EXCEPTION 'invalid link request transition' USING ERRCODE = '23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER mediconnect_link_request_guard BEFORE UPDATE OR DELETE ON mediconnect_link_requests
 FOR EACH ROW EXECUTE FUNCTION mediconnect_link_request_guard();
CREATE TRIGGER mediconnect_link_requests_audit AFTER INSERT OR UPDATE ON mediconnect_link_requests
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();
