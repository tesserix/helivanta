CREATE TABLE supply_corporate_structures (
 tenant_id uuid NOT NULL,party_id uuid NOT NULL,id uuid GENERATED ALWAYS AS (party_id) STORED,parent_party_id uuid,
 group_root boolean GENERATED ALWAYS AS (parent_party_id IS NULL) STORED,
 parent_root boolean NOT NULL DEFAULT true CHECK(parent_root),
 version bigint NOT NULL CHECK(version BETWEEN 1 AND 9007199254740991),
 created_at timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,party_id),UNIQUE(tenant_id,party_id,group_root),
 CHECK(parent_party_id IS NULL OR (substring(parent_party_id::text,15,1)='7' AND parent_party_id<>party_id)),
 FOREIGN KEY(tenant_id,party_id) REFERENCES supply_parties(tenant_id,id) ON DELETE RESTRICT,
 CONSTRAINT supply_corporate_root_reference FOREIGN KEY(tenant_id,parent_party_id,parent_root) REFERENCES supply_corporate_structures(tenant_id,party_id,group_root) ON UPDATE RESTRICT ON DELETE RESTRICT
);
CREATE INDEX supply_corporate_parent ON supply_corporate_structures(tenant_id,parent_party_id,parent_root);
CREATE FUNCTION supply_corporate_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE old_scope text; changed_parent boolean:=true;
BEGIN
 IF TG_OP='UPDATE' THEN
  IF (NEW.tenant_id,NEW.party_id,NEW.created_at) IS DISTINCT FROM (OLD.tenant_id,OLD.party_id,OLD.created_at) OR NEW.version<>OLD.version+1 THEN
   RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='supply_corporate_identity';
  END IF;
  changed_parent:=NEW.parent_party_id IS DISTINCT FROM OLD.parent_party_id;
 ELSIF NEW.version<>1 THEN
  RAISE EXCEPTION USING ERRCODE='23514',CONSTRAINT='supply_corporate_identity';
 END IF;
 PERFORM 1 FROM supply_parties WHERE tenant_id=NEW.tenant_id AND id=NEW.party_id AND record_kind='owned_organisation' FOR SHARE;
 IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE='23503',CONSTRAINT='supply_corporate_owner'; END IF;
 IF changed_parent AND NEW.parent_party_id IS NOT NULL THEN
  old_scope:=current_setting('app.supply_party',true);
  PERFORM set_config('app.supply_party',NEW.parent_party_id::text,true);
  IF NOT supply_roles_allowed(NEW.tenant_id,NEW.parent_party_id,ARRAY['administrator']) THEN
   RAISE EXCEPTION USING ERRCODE='23503',CONSTRAINT='supply_corporate_parent_access';
  END IF;
  PERFORM 1 FROM supply_parties WHERE tenant_id=NEW.tenant_id AND id=NEW.parent_party_id AND record_kind='owned_organisation' FOR SHARE;
  IF NOT FOUND THEN RAISE EXCEPTION USING ERRCODE='23503',CONSTRAINT='supply_corporate_parent_access'; END IF;
  PERFORM set_config('app.supply_party',old_scope,true);
 END IF;
 NEW.updated_at:=clock_timestamp();
 RETURN NEW;
END $$;
ALTER TABLE supply_corporate_structures ENABLE ROW LEVEL SECURITY;
ALTER TABLE supply_corporate_structures FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON supply_corporate_structures USING(hms_tenant_visible(tenant_id) AND supply_roles_allowed(tenant_id,party_id,ARRAY['administrator','purchasing','finance','compliance','auditor'])) WITH CHECK(supply_roles_allowed(tenant_id,party_id,ARRAY['administrator']));
CREATE TRIGGER supply_corporate_guard BEFORE INSERT OR UPDATE ON supply_corporate_structures FOR EACH ROW EXECUTE FUNCTION supply_corporate_guard();
CREATE TRIGGER supply_corporate_audit AFTER INSERT OR UPDATE ON supply_corporate_structures FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER supply_corporate_retain BEFORE DELETE OR TRUNCATE ON supply_corporate_structures FOR EACH STATEMENT EXECUTE FUNCTION supply_history_immutable();
CREATE TABLE supply_corporate_revisions (
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),tenant_id uuid NOT NULL,party_id uuid NOT NULL,version bigint NOT NULL,
 snapshot jsonb NOT NULL CHECK(jsonb_typeof(snapshot)='object'),actor_subject text NOT NULL CHECK(length(btrim(actor_subject)) BETWEEN 1 AND 200),created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,party_id,version),UNIQUE(tenant_id,id),
 FOREIGN KEY(tenant_id,party_id) REFERENCES supply_corporate_structures(tenant_id,party_id) ON DELETE RESTRICT
);
ALTER TABLE supply_corporate_structures ADD CONSTRAINT supply_corporate_revision_head FOREIGN KEY(tenant_id,party_id,version) REFERENCES supply_corporate_revisions(tenant_id,party_id,version) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE supply_corporate_revisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE supply_corporate_revisions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON supply_corporate_revisions USING(hms_tenant_visible(tenant_id) AND supply_roles_allowed(tenant_id,party_id,ARRAY['administrator','purchasing','finance','compliance','auditor'])) WITH CHECK(supply_roles_allowed(tenant_id,party_id,ARRAY['administrator']));
CREATE INDEX supply_corporate_revision_page ON supply_corporate_revisions(tenant_id,party_id,created_at DESC,id DESC);
CREATE TRIGGER supply_corporate_revision_retain BEFORE UPDATE OR DELETE OR TRUNCATE ON supply_corporate_revisions FOR EACH STATEMENT EXECUTE FUNCTION supply_history_immutable();
