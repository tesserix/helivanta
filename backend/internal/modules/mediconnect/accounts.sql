CREATE TABLE mediconnect_accounts (
 id uuid PRIMARY KEY CHECK (substring(id::text, 15, 1) = '7'),
 tenant_id uuid NOT NULL,
 subject text NOT NULL CHECK (length(subject) BETWEEN 1 AND 512),
 locale text NOT NULL DEFAULT 'en-IN' CHECK (length(locale) BETWEEN 2 AND 35),
 preferred_channel text NOT NULL DEFAULT 'none' CHECK (preferred_channel IN ('none','email','whatsapp')),
 low_data_mode boolean NOT NULL DEFAULT false,
 version bigint NOT NULL DEFAULT 1 CHECK (version > 0),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE (tenant_id, subject),
 UNIQUE (tenant_id, id)
);
ALTER TABLE mediconnect_accounts ENABLE ROW LEVEL SECURITY;
ALTER TABLE mediconnect_accounts FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON mediconnect_accounts
 USING (hms_tenant_visible(tenant_id)
   AND tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
   AND subject = NULLIF(current_setting('app.actor_subject', true), ''))
 WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id', true), '')::uuid
   AND subject = NULLIF(current_setting('app.actor_subject', true), ''));
CREATE TRIGGER mediconnect_accounts_audit AFTER INSERT OR UPDATE OR DELETE
 ON mediconnect_accounts FOR EACH ROW EXECUTE FUNCTION audit_record_change();
