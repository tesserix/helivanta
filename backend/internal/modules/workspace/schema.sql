CREATE TABLE workspace_branding (
 tenant_id uuid PRIMARY KEY,
 display_name text NOT NULL CHECK (length(btrim(display_name)) BETWEEN 1 AND 100),
 welcome_text text NOT NULL CHECK (length(welcome_text) <= 240),
 logo_url text NOT NULL CHECK (length(logo_url) <= 1000),
 theme text NOT NULL CHECK (theme IN ('teal','blue','indigo')),
 login_layout text NOT NULL CHECK (login_layout IN ('centered','split')),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE workspace_branding ENABLE ROW LEVEL SECURITY;
ALTER TABLE workspace_branding FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workspace_branding
 USING (hms_tenant_visible(tenant_id) AND tenant_id = NULLIF(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id',true),'')::uuid);
CREATE TABLE workspace_branding_audit (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 tenant_id uuid NOT NULL,
 actor_subject text NOT NULL CHECK (length(actor_subject)>0),
 action text NOT NULL,
 changes jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE workspace_branding_audit ENABLE ROW LEVEL SECURITY;
ALTER TABLE workspace_branding_audit FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workspace_branding_audit
 USING (hms_tenant_visible(tenant_id) AND tenant_id = NULLIF(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK (tenant_id = NULLIF(current_setting('app.tenant_id',true),'')::uuid);
CREATE FUNCTION workspace_audit_branding() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO workspace_branding_audit(tenant_id,actor_subject,action,changes)
 VALUES (NEW.tenant_id,NULLIF(current_setting('app.workspace_subject',true),''),lower(TG_OP),jsonb_build_object('before',to_jsonb(OLD)-'tenant_id','after',to_jsonb(NEW)-'tenant_id'));
 RETURN NULL;
END $$;
CREATE TRIGGER workspace_branding_audit AFTER INSERT OR UPDATE ON workspace_branding
 FOR EACH ROW EXECUTE FUNCTION workspace_audit_branding();
CREATE FUNCTION workspace_prevent_removal() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION 'workspace audit is append only' USING ERRCODE='23514';
END $$;
CREATE TRIGGER workspace_audit_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON workspace_branding_audit
 FOR EACH STATEMENT EXECUTE FUNCTION workspace_prevent_removal();
CREATE TRIGGER workspace_branding_no_delete BEFORE DELETE OR TRUNCATE ON workspace_branding
 FOR EACH STATEMENT EXECUTE FUNCTION workspace_prevent_removal();
