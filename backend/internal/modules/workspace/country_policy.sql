SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE TABLE workspace_country_policy_scopes (
 tenant_id uuid NOT NULL,
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 country text NOT NULL CHECK (country IN ('IN','PK')),
 jurisdiction text NOT NULL CHECK (length(btrim(jurisdiction)) BETWEEN 1 AND 100),
 category text NOT NULL CHECK (category IN ('otc','prescription','controlled','cold_chain')),
 operation text NOT NULL CHECK (operation IN ('listing','order','dispatch','dispense')),
 currency text NOT NULL CHECK ((country='IN' AND currency='INR') OR (country='PK' AND currency='PKR')),
 owner_subject text NOT NULL CHECK (length(btrim(owner_subject)) BETWEEN 1 AND 200),
 status text NOT NULL DEFAULT 'unapproved' CHECK (status IN ('unapproved','active','suspended')),
 current_version uuid,
 revision bigint NOT NULL DEFAULT 0 CHECK (revision >= 0),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,id),
 UNIQUE (tenant_id,country,jurisdiction,category,operation,currency)
);
CREATE INDEX ON workspace_country_policy_scopes(tenant_id,created_at DESC,id DESC);

CREATE TABLE workspace_country_policy_versions (
 tenant_id uuid NOT NULL,
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 scope_id uuid NOT NULL,
 author_subject text NOT NULL CHECK (length(btrim(author_subject)) BETWEEN 1 AND 200),
 effective_from timestamptz NOT NULL,
 effective_until timestamptz NOT NULL CHECK (effective_until > effective_from),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,id),
 UNIQUE (tenant_id,scope_id,id),
 FOREIGN KEY (tenant_id,scope_id) REFERENCES workspace_country_policy_scopes(tenant_id,id) ON DELETE RESTRICT
);
CREATE INDEX ON workspace_country_policy_versions(tenant_id,scope_id,created_at DESC,id DESC);
ALTER TABLE workspace_country_policy_scopes ADD FOREIGN KEY (tenant_id,id,current_version)
 REFERENCES workspace_country_policy_versions(tenant_id,scope_id,id) ON DELETE RESTRICT;

CREATE TABLE workspace_country_policy_sources (
 tenant_id uuid NOT NULL,
 version_id uuid NOT NULL,
 key text NOT NULL CHECK (length(btrim(key)) BETWEEN 1 AND 64),
 title text NOT NULL CHECK (length(btrim(title)) BETWEEN 1 AND 200),
 url text NOT NULL CHECK (length(url) BETWEEN 1 AND 2048),
 reviewed_at timestamptz NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,version_id,key),
 FOREIGN KEY (tenant_id,version_id) REFERENCES workspace_country_policy_versions(tenant_id,id) ON DELETE RESTRICT
);
CREATE TABLE workspace_country_policy_requirements (
 tenant_id uuid NOT NULL,
 version_id uuid NOT NULL,
 domain text NOT NULL CHECK (domain IN ('licensing','product_registration','prescription','controlled','cold_chain','tax_invoice','credit_note','retention','privacy','residency','reporting')),
 applicability text NOT NULL CHECK (applicability IN ('required','not_applicable')),
 reason text NOT NULL CHECK (length(btrim(reason)) BETWEEN 1 AND 4000),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,version_id,domain),
 FOREIGN KEY (tenant_id,version_id) REFERENCES workspace_country_policy_versions(tenant_id,id) ON DELETE RESTRICT
);
CREATE TABLE workspace_country_policy_citations (
 tenant_id uuid NOT NULL,
 version_id uuid NOT NULL,
 domain text NOT NULL,
 source_key text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,version_id,domain,source_key),
 FOREIGN KEY (tenant_id,version_id,domain) REFERENCES workspace_country_policy_requirements(tenant_id,version_id,domain) ON DELETE RESTRICT,
 FOREIGN KEY (tenant_id,version_id,source_key) REFERENCES workspace_country_policy_sources(tenant_id,version_id,key) ON DELETE RESTRICT
);
CREATE INDEX ON workspace_country_policy_citations(tenant_id,version_id,source_key);
CREATE TABLE workspace_country_policy_access (
 tenant_id uuid NOT NULL,
 scope_id uuid NOT NULL,
 subject text NOT NULL CHECK (length(btrim(subject)) BETWEEN 1 AND 200),
 role text NOT NULL CHECK (role IN ('reader','editor','reviewer')),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,scope_id,subject),
 FOREIGN KEY (tenant_id,scope_id) REFERENCES workspace_country_policy_scopes(tenant_id,id) ON DELETE RESTRICT
);
CREATE INDEX ON workspace_country_policy_access(tenant_id,subject,scope_id);
CREATE TABLE workspace_country_policy_actions (
 tenant_id uuid NOT NULL,
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 scope_id uuid NOT NULL,
 version_id uuid,
 actor_subject text NOT NULL CHECK (length(btrim(actor_subject)) BETWEEN 1 AND 200),
 action text NOT NULL CHECK (action IN ('submitted','approved','rejected','suspended','access_granted','access_revoked')),
 reason text NOT NULL CHECK (length(reason) <= 4000),
 expected_revision bigint NOT NULL CHECK (expected_revision BETWEEN 0 AND 9007199254740990),
 result_revision bigint NOT NULL CHECK (result_revision=expected_revision+1),
 request_hash text NOT NULL CHECK (length(request_hash)=64),
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,id),
 UNIQUE (tenant_id,scope_id,result_revision),
 FOREIGN KEY (tenant_id,scope_id) REFERENCES workspace_country_policy_scopes(tenant_id,id) ON DELETE RESTRICT,
 FOREIGN KEY (tenant_id,scope_id,version_id) REFERENCES workspace_country_policy_versions(tenant_id,scope_id,id) ON DELETE RESTRICT
);
CREATE INDEX ON workspace_country_policy_actions(tenant_id,scope_id,created_at DESC,id DESC);
CREATE INDEX ON workspace_country_policy_actions(tenant_id,version_id,action);
CREATE UNIQUE INDEX ON workspace_country_policy_actions(tenant_id,version_id) WHERE action IN ('approved','rejected');

CREATE FUNCTION workspace_country_policy_guard_action() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
 policy_author text;
 scope_owner text;
 required_role text;
BEGIN
 IF NEW.actor_subject IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'') THEN
  RAISE EXCEPTION 'country policy actor mismatch' USING ERRCODE='42501';
 END IF;
 SELECT owner_subject INTO scope_owner FROM workspace_country_policy_scopes
  WHERE tenant_id=NEW.tenant_id AND id=NEW.scope_id FOR SHARE;
 IF scope_owner IS NULL THEN
  RAISE EXCEPTION 'country policy scope unavailable' USING ERRCODE='42501';
 END IF;
 IF NEW.action IN ('approved','rejected') THEN
  SELECT author_subject INTO policy_author FROM workspace_country_policy_versions
   WHERE tenant_id=NEW.tenant_id AND scope_id=NEW.scope_id AND id=NEW.version_id;
  IF policy_author IS NULL OR policy_author=NEW.actor_subject THEN
   RAISE EXCEPTION 'independent country policy review required' USING ERRCODE='23514';
  END IF;
 END IF;
 IF NEW.action IN ('access_granted','access_revoked') AND scope_owner<>NEW.actor_subject THEN
  RAISE EXCEPTION 'country policy owner required' USING ERRCODE='42501';
 END IF;
 required_role:=CASE WHEN NEW.action='submitted' THEN 'editor' ELSE 'reviewer' END;
 IF scope_owner<>NEW.actor_subject AND NOT EXISTS (
  SELECT 1 FROM workspace_country_policy_access WHERE tenant_id=NEW.tenant_id
   AND scope_id=NEW.scope_id AND subject=NEW.actor_subject AND role=required_role
 ) THEN
  RAISE EXCEPTION 'country policy scope access required' USING ERRCODE='42501';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workspace_country_policy_action_guard BEFORE INSERT ON workspace_country_policy_actions
 FOR EACH ROW EXECUTE FUNCTION workspace_country_policy_guard_action();

CREATE FUNCTION workspace_country_policy_guard_snapshot() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE
 policy_author text;
BEGIN
 SELECT author_subject INTO policy_author FROM workspace_country_policy_versions
  WHERE tenant_id=NEW.tenant_id AND id=NEW.version_id;
 IF policy_author IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'') THEN
  RAISE EXCEPTION 'country policy author required' USING ERRCODE='42501';
 END IF;
 IF EXISTS(SELECT 1 FROM workspace_country_policy_actions
  WHERE tenant_id=NEW.tenant_id AND version_id=NEW.version_id AND action='submitted') THEN
  RAISE EXCEPTION 'country policy snapshot is immutable' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workspace_country_policy_sources_frozen BEFORE INSERT ON workspace_country_policy_sources
 FOR EACH ROW EXECUTE FUNCTION workspace_country_policy_guard_snapshot();
CREATE TRIGGER workspace_country_policy_requirements_frozen BEFORE INSERT ON workspace_country_policy_requirements
 FOR EACH ROW EXECUTE FUNCTION workspace_country_policy_guard_snapshot();
CREATE TRIGGER workspace_country_policy_citations_frozen BEFORE INSERT ON workspace_country_policy_citations
 FOR EACH ROW EXECUTE FUNCTION workspace_country_policy_guard_snapshot();

ALTER TABLE workspace_country_policy_scopes ENABLE ROW LEVEL SECURITY;
ALTER TABLE workspace_country_policy_scopes FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workspace_country_policy_scopes
 USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid);
ALTER TABLE workspace_country_policy_versions ENABLE ROW LEVEL SECURITY;
ALTER TABLE workspace_country_policy_versions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workspace_country_policy_versions
 USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid);
ALTER TABLE workspace_country_policy_sources ENABLE ROW LEVEL SECURITY;
ALTER TABLE workspace_country_policy_sources FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workspace_country_policy_sources
 USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid);
ALTER TABLE workspace_country_policy_requirements ENABLE ROW LEVEL SECURITY;
ALTER TABLE workspace_country_policy_requirements FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workspace_country_policy_requirements
 USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid);
ALTER TABLE workspace_country_policy_citations ENABLE ROW LEVEL SECURITY;
ALTER TABLE workspace_country_policy_citations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workspace_country_policy_citations
 USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid);
ALTER TABLE workspace_country_policy_access ENABLE ROW LEVEL SECURITY;
ALTER TABLE workspace_country_policy_access FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workspace_country_policy_access
 USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid);
ALTER TABLE workspace_country_policy_actions ENABLE ROW LEVEL SECURITY;
ALTER TABLE workspace_country_policy_actions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workspace_country_policy_actions
 USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid);

CREATE TRIGGER workspace_country_policy_versions_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON workspace_country_policy_versions FOR EACH STATEMENT EXECUTE FUNCTION workspace_prevent_removal();
CREATE TRIGGER workspace_country_policy_sources_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON workspace_country_policy_sources FOR EACH STATEMENT EXECUTE FUNCTION workspace_prevent_removal();
CREATE TRIGGER workspace_country_policy_requirements_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON workspace_country_policy_requirements FOR EACH STATEMENT EXECUTE FUNCTION workspace_prevent_removal();
CREATE TRIGGER workspace_country_policy_citations_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON workspace_country_policy_citations FOR EACH STATEMENT EXECUTE FUNCTION workspace_prevent_removal();
CREATE TRIGGER workspace_country_policy_actions_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON workspace_country_policy_actions FOR EACH STATEMENT EXECUTE FUNCTION workspace_prevent_removal();
CREATE TRIGGER workspace_country_policy_scopes_no_removal BEFORE DELETE OR TRUNCATE ON workspace_country_policy_scopes FOR EACH STATEMENT EXECUTE FUNCTION workspace_prevent_removal();
CREATE TRIGGER workspace_country_policy_actions_audit AFTER INSERT ON workspace_country_policy_actions FOR EACH ROW EXECUTE FUNCTION audit_record_change();
