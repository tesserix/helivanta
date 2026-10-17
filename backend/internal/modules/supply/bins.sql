SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE TABLE supply_bins (
 tenant_id uuid NOT NULL,party_id uuid NOT NULL,premise_id uuid NOT NULL,id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),country_code text NOT NULL,
 code text NOT NULL CHECK(code ~ '^[A-Z0-9][A-Z0-9._-]{0,39}$'),name text NOT NULL CHECK(length(btrim(name)) BETWEEN 1 AND 200),
 aisle text NOT NULL CHECK(length(aisle)<=60),rack text NOT NULL CHECK(length(rack)<=60),shelf text NOT NULL CHECK(length(shelf)<=60),
 storage_class text NOT NULL CHECK(storage_class IN ('ambient','cold_chain','controlled','quarantine')),status text NOT NULL CHECK(status IN ('active','inactive')),
 version bigint NOT NULL CHECK(version BETWEEN 1 AND 9007199254740991),created_at timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,party_id,premise_id,id),UNIQUE(tenant_id,party_id,premise_id,code),
 FOREIGN KEY(tenant_id,party_id,premise_id,country_code) REFERENCES supply_premises(tenant_id,party_id,id,country_code) ON DELETE RESTRICT
);
CREATE FUNCTION supply_bin_guard() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF TG_OP='UPDATE' THEN
  IF (NEW.tenant_id,NEW.party_id,NEW.premise_id,NEW.id,NEW.country_code,NEW.created_at) IS DISTINCT FROM (OLD.tenant_id,OLD.party_id,OLD.premise_id,OLD.id,OLD.country_code,OLD.created_at) OR NEW.version<>OLD.version+1 THEN
   RAISE EXCEPTION 'bin identity or version is immutable' USING ERRCODE='23514',CONSTRAINT='supply_bin_identity';
  END IF;
 ELSE
  IF NEW.version<>1 THEN RAISE EXCEPTION 'bin initial version must be one' USING ERRCODE='23514',CONSTRAINT='supply_bin_identity'; END IF;
 END IF;
 PERFORM 1 FROM supply_parties WHERE tenant_id=NEW.tenant_id AND id=NEW.party_id AND record_kind='owned_organisation' FOR SHARE;
 IF NOT FOUND THEN RAISE EXCEPTION 'bin owner is inaccessible' USING ERRCODE='23503',CONSTRAINT='supply_bin_owner'; END IF;
 PERFORM 1 FROM supply_premises WHERE tenant_id=NEW.tenant_id AND party_id=NEW.party_id AND id=NEW.premise_id AND country_code=NEW.country_code AND kind IN ('warehouse','depot') AND (NEW.status='inactive' OR status='active') FOR SHARE;
 IF NOT FOUND THEN RAISE EXCEPTION 'bin premise is inaccessible or inactive' USING ERRCODE='23503',CONSTRAINT='supply_bin_premise'; END IF;
 NEW.updated_at:=clock_timestamp();
 RETURN NEW;
END $$;
ALTER TABLE supply_bins ENABLE ROW LEVEL SECURITY;
ALTER TABLE supply_bins FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON supply_bins USING(hms_tenant_visible(tenant_id) AND supply_roles_allowed(tenant_id,party_id,ARRAY['administrator','purchasing','compliance','auditor'])) WITH CHECK(supply_roles_allowed(tenant_id,party_id,ARRAY['administrator','purchasing']));
CREATE INDEX supply_bins_page ON supply_bins(tenant_id,party_id,premise_id,created_at DESC,id DESC);
CREATE TRIGGER supply_bins_guard BEFORE INSERT OR UPDATE ON supply_bins FOR EACH ROW EXECUTE FUNCTION supply_bin_guard();
CREATE TRIGGER supply_bins_audit AFTER INSERT OR UPDATE ON supply_bins FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER supply_bins_retain BEFORE DELETE OR TRUNCATE ON supply_bins FOR EACH STATEMENT EXECUTE FUNCTION supply_history_immutable();
CREATE TABLE supply_bin_revisions (
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),tenant_id uuid NOT NULL,party_id uuid NOT NULL,premise_id uuid NOT NULL,bin_id uuid NOT NULL,version bigint NOT NULL CHECK(version BETWEEN 1 AND 9007199254740991),
 snapshot jsonb NOT NULL CHECK(jsonb_typeof(snapshot)='object'),actor_subject text NOT NULL CHECK(length(btrim(actor_subject)) BETWEEN 1 AND 200),created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,party_id,premise_id,bin_id,version),UNIQUE(tenant_id,id),
 FOREIGN KEY(tenant_id,party_id,premise_id,bin_id) REFERENCES supply_bins(tenant_id,party_id,premise_id,id) ON DELETE RESTRICT
);
ALTER TABLE supply_bins ADD CONSTRAINT supply_bins_revision_head FOREIGN KEY(tenant_id,party_id,premise_id,id,version) REFERENCES supply_bin_revisions(tenant_id,party_id,premise_id,bin_id,version) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED;
ALTER TABLE supply_bin_revisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE supply_bin_revisions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON supply_bin_revisions USING(hms_tenant_visible(tenant_id) AND supply_roles_allowed(tenant_id,party_id,ARRAY['administrator','purchasing','compliance','auditor'])) WITH CHECK(supply_roles_allowed(tenant_id,party_id,ARRAY['administrator','purchasing']));
CREATE INDEX supply_bin_revisions_page ON supply_bin_revisions(tenant_id,party_id,premise_id,bin_id,created_at DESC,id DESC);
CREATE TRIGGER supply_bin_revisions_retain BEFORE UPDATE OR DELETE OR TRUNCATE ON supply_bin_revisions FOR EACH STATEMENT EXECUTE FUNCTION supply_history_immutable();
CREATE FUNCTION supply_premise_bins_guard() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.kind NOT IN ('warehouse','depot') AND EXISTS(SELECT 1 FROM supply_bins WHERE tenant_id=NEW.tenant_id AND party_id=NEW.party_id AND premise_id=NEW.id) THEN
 RAISE EXCEPTION 'premise retains storage bins' USING ERRCODE='23514',CONSTRAINT='supply_premise_bins_kind'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER supply_premise_bins_guard BEFORE UPDATE OF kind ON supply_premises FOR EACH ROW EXECUTE FUNCTION supply_premise_bins_guard();
