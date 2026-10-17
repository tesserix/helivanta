SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
ALTER TABLE supply_premises ADD COLUMN branch_premise_id uuid;
ALTER TABLE supply_premises ADD CONSTRAINT supply_premise_branch_reference CHECK(branch_premise_id IS NULL OR (substring(branch_premise_id::text,15,1)='7' AND branch_premise_id<>id AND kind IN ('warehouse','depot')));
ALTER TABLE supply_premises ADD CONSTRAINT supply_premise_branch_scope FOREIGN KEY(tenant_id,party_id,branch_premise_id,country_code) REFERENCES supply_premises(tenant_id,party_id,id,country_code) ON DELETE RESTRICT;
CREATE FUNCTION supply_premise_branch_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE require_active boolean:=true;
BEGIN
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id::text||':supply-structure:'||NEW.party_id::text,0));
 IF TG_OP='UPDATE' THEN
  require_active:=NEW.branch_premise_id IS DISTINCT FROM OLD.branch_premise_id;
  IF OLD.kind='branch' AND NEW.kind<>'branch' AND EXISTS(SELECT 1 FROM supply_premises WHERE tenant_id=OLD.tenant_id AND party_id=OLD.party_id AND branch_premise_id=OLD.id) THEN
   RAISE EXCEPTION 'branch retains linked storage premises' USING ERRCODE='23514',CONSTRAINT='supply_premise_branch_kind';
  END IF;
 END IF;
 IF NEW.branch_premise_id IS NOT NULL THEN
  PERFORM 1 FROM supply_parties WHERE tenant_id=NEW.tenant_id AND id=NEW.party_id AND record_kind='owned_organisation' FOR SHARE;
  IF NOT FOUND THEN RAISE EXCEPTION 'branch owner is inaccessible' USING ERRCODE='23503',CONSTRAINT='supply_premise_branch_owner'; END IF;
  PERFORM 1 FROM supply_premises WHERE tenant_id=NEW.tenant_id AND party_id=NEW.party_id AND id=NEW.branch_premise_id AND country_code=NEW.country_code AND kind='branch' AND (NOT require_active OR status='active') FOR SHARE;
  IF NOT FOUND THEN RAISE EXCEPTION 'branch is inaccessible or incompatible' USING ERRCODE='23503',CONSTRAINT='supply_premise_branch_parent'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER supply_premises_branch_guard BEFORE INSERT OR UPDATE ON supply_premises FOR EACH ROW EXECUTE FUNCTION supply_premise_branch_guard();
