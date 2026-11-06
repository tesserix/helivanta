SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- ADR-0054 §4: expand only; a constant default backfills every existing row as a hospital root without a rewrite.
ALTER TABLE iam_assignment_proposals ADD COLUMN establishment_kind text NOT NULL DEFAULT 'hospital';
ALTER TABLE iam_assignment_snapshots ADD COLUMN establishment_kind text NOT NULL DEFAULT 'hospital';
-- Every template published before 0034 is hospital-only; generated SQL from 0034 replaces this from declarations.
CREATE FUNCTION iam_assignment_template_establishment_kinds(key text) RETURNS text[] LANGUAGE sql IMMUTABLE AS $$
 SELECT CASE WHEN iam_assignment_template_permissions(key) IS NULL THEN ARRAY[]::text[] ELSE ARRAY['hospital']::text[] END
$$;
-- A pharmacy root admits only its own root scope; the template's hospital scope kind stands for the root.
ALTER TABLE iam_assignment_proposals
 DROP CONSTRAINT iam_assignment_proposals_scope_check,
 DROP CONSTRAINT iam_assignment_proposals_template_scope_check,
 ADD CONSTRAINT iam_assignment_proposals_scope_check CHECK(
  ((establishment_kind='hospital' AND scope_kind IN('hospital','department')) OR (establishment_kind='pharmacy' AND scope_kind='pharmacy'))
  AND (scope_kind='department')=(department_id IS NOT NULL)
  AND (department_id IS NULL OR (substring(department_id::text,15,1)='7' AND substring(department_id::text,20,1) IN('8','9','a','b') AND department_id<>hospital_id))) NOT VALID,
 ADD CONSTRAINT iam_assignment_proposals_template_scope_check CHECK(iam_assignment_template_scope_kinds(template_key) @> ARRAY[CASE scope_kind WHEN 'pharmacy' THEN 'hospital' ELSE scope_kind END]) NOT VALID,
 ADD CONSTRAINT iam_assignment_proposals_template_establishment_check CHECK(COALESCE(iam_assignment_template_establishment_kinds(template_key) @> ARRAY[establishment_kind],false)) NOT VALID;
ALTER TABLE iam_assignment_proposals VALIDATE CONSTRAINT iam_assignment_proposals_scope_check;
ALTER TABLE iam_assignment_proposals VALIDATE CONSTRAINT iam_assignment_proposals_template_scope_check;
ALTER TABLE iam_assignment_proposals VALIDATE CONSTRAINT iam_assignment_proposals_template_establishment_check;
ALTER TABLE iam_assignment_snapshots
 DROP CONSTRAINT iam_assignment_snapshots_scope_check,
 ADD CONSTRAINT iam_assignment_snapshots_scope_check CHECK(
  ((establishment_kind='hospital' AND scope_kind IN('hospital','department')) OR (establishment_kind='pharmacy' AND scope_kind='pharmacy'))
  AND (scope_kind='department')=(department_id IS NOT NULL)) NOT VALID;
ALTER TABLE iam_assignment_snapshots VALIDATE CONSTRAINT iam_assignment_snapshots_scope_check;
-- Restrictive policies AND with tenant_isolation, so no path that has not declared the root sees a pharmacy row.
CREATE POLICY establishment_root ON iam_assignment_proposals AS RESTRICTIVE
 USING(establishment_kind='hospital' OR current_setting('app.assignment_establishment_root',true)='typed')
 WITH CHECK(establishment_kind='hospital' OR current_setting('app.assignment_establishment_root',true)='typed');
CREATE POLICY establishment_root ON iam_assignment_snapshots AS RESTRICTIVE
 USING(establishment_kind='hospital' OR current_setting('app.assignment_establishment_root',true)='typed')
 WITH CHECK(establishment_kind='hospital' OR current_setting('app.assignment_establishment_root',true)='typed');
-- The department policy guards departments only; a pharmacy root is governed by establishment_root alone.
ALTER POLICY department_scope ON iam_assignment_proposals
 USING(scope_kind IN('hospital','pharmacy') OR current_setting('app.assignment_department_scope',true)='enabled')
 WITH CHECK(scope_kind IN('hospital','pharmacy') OR current_setting('app.assignment_department_scope',true)='enabled');
ALTER POLICY department_scope ON iam_assignment_snapshots
 USING(scope_kind IN('hospital','pharmacy') OR current_setting('app.assignment_department_scope',true)='enabled')
 WITH CHECK(scope_kind IN('hospital','pharmacy') OR current_setting('app.assignment_department_scope',true)='enabled');
-- Named to fire before iam_assignment_proposal_guard, whose 0031 body predates the root column.
CREATE FUNCTION iam_assignment_establishment_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.establishment_kind IS DISTINCT FROM OLD.establishment_kind THEN
  RAISE EXCEPTION 'assignment establishment root is immutable' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER iam_assignment_establishment_immutable BEFORE UPDATE ON iam_assignment_proposals FOR EACH ROW EXECUTE FUNCTION iam_assignment_establishment_immutable();
-- Extends the 0031 bodies: a snapshot carries and must match its proposal's root kind.
CREATE OR REPLACE FUNCTION iam_assignment_snapshot_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM iam_assignment_proposals p JOIN iam_assignment_decisions d ON d.tenant_id=p.tenant_id AND d.proposal_id=p.id AND d.request_id=p.request_id
  WHERE p.tenant_id=NEW.tenant_id AND p.id=NEW.id AND p.version=NEW.version AND p.status='approved_pending_activation' AND d.decision='approve' AND d.actor_subject=NEW.approved_by
  AND ROW(p.hospital_id,p.subject,p.template_key,p.valid_from,p.expires_at,p.scope_kind,p.department_id,p.establishment_kind) IS NOT DISTINCT FROM ROW(NEW.hospital_id,NEW.subject,NEW.template_key,NEW.valid_from,NEW.expires_at,NEW.scope_kind,NEW.department_id,NEW.establishment_kind)) THEN
  RAISE EXCEPTION 'reviewed snapshot required' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE OR REPLACE FUNCTION iam_assignment_snapshot_record() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.status='approved_pending_activation' THEN
  INSERT INTO iam_assignment_snapshots(tenant_id,id,version,hospital_id,subject,template_key,permissions,approved_by,valid_from,expires_at,scope_kind,department_id,establishment_kind)
   SELECT NEW.tenant_id,NEW.id,NEW.version,NEW.hospital_id,NEW.subject,NEW.template_key,iam_assignment_template_permissions(NEW.template_key),d.actor_subject,NEW.valid_from,NEW.expires_at,NEW.scope_kind,NEW.department_id,NEW.establishment_kind
   FROM iam_assignment_decisions d WHERE d.tenant_id=NEW.tenant_id AND d.proposal_id=NEW.id AND d.request_id=NEW.request_id AND d.decision='approve';
 END IF;
 IF NEW.status='approved_pending_activation' OR (NEW.status='withdrawn' AND OLD.status IN('approved_pending_activation','active')) THEN
  PERFORM set_config('app.assignment_epoch_change','authorized',true);
  INSERT INTO iam_assignment_scope_epochs(tenant_id,hospital_id,subject,epoch) VALUES(NEW.tenant_id,NEW.hospital_id,NEW.subject,1)
   ON CONFLICT(tenant_id,hospital_id,subject) DO UPDATE SET epoch=iam_assignment_scope_epochs.epoch+1;
 END IF;
 RETURN NULL;
END $$;
