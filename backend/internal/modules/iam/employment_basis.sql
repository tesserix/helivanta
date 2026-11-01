SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE TABLE iam_employment_bases (
 tenant_id uuid NOT NULL,hospital_id uuid NOT NULL,subject text NOT NULL,
 staff_id uuid NOT NULL,generation_id uuid NOT NULL CHECK(substring(generation_id::text,15,1)='7'),
 employment_epoch bigint NOT NULL CHECK(employment_epoch BETWEEN 2 AND 9007199254740990),
 valid_until timestamptz NOT NULL,actor_subject text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,hospital_id,subject),UNIQUE(tenant_id,generation_id),
 FOREIGN KEY(tenant_id,subject) REFERENCES iam_subject_membership_epochs(tenant_id,subject) ON DELETE RESTRICT
);
CREATE INDEX ON iam_employment_bases(tenant_id,subject,hospital_id);
CREATE TABLE iam_employment_basis_history (LIKE iam_employment_bases INCLUDING DEFAULTS INCLUDING CONSTRAINTS);
ALTER TABLE iam_employment_basis_history ADD PRIMARY KEY(tenant_id,generation_id);
CREATE INDEX ON iam_employment_basis_history(tenant_id,hospital_id,subject,employment_epoch);
CREATE TABLE iam_assignment_employment_bases (
 tenant_id uuid NOT NULL,proposal_id uuid NOT NULL,generation_id uuid NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,proposal_id),
 FOREIGN KEY(tenant_id,proposal_id) REFERENCES iam_assignment_proposals(tenant_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,generation_id) REFERENCES iam_employment_basis_history(tenant_id,generation_id) ON DELETE RESTRICT
);
CREATE INDEX ON iam_assignment_employment_bases(tenant_id,generation_id,proposal_id);
DO $$ DECLARE item text; BEGIN
 FOREACH item IN ARRAY ARRAY['iam_employment_bases','iam_employment_basis_history','iam_assignment_employment_bases'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',item);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',item);
  EXECUTE format('CREATE POLICY tenant_isolation ON %I USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND current_setting(''app.assignment_access'',true)=''authorized'') WITH CHECK(tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND current_setting(''app.assignment_access'',true)=''authorized'')',item);
 END LOOP;
END $$;
CREATE FUNCTION iam_employment_basis_write() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF current_setting('app.iam_employment_basis',true) IS DISTINCT FROM 'consume'
  OR NEW.actor_subject IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'')
  OR NEW.actor_subject=NEW.subject OR NEW.valid_until<=clock_timestamp() THEN
  RAISE EXCEPTION 'employment basis requires current independent admission' USING ERRCODE='23514';
 END IF;
 IF TG_OP='UPDATE' THEN
  IF (NEW.tenant_id,NEW.hospital_id,NEW.subject,NEW.staff_id,NEW.created_at) IS DISTINCT FROM
   (OLD.tenant_id,OLD.hospital_id,OLD.subject,OLD.staff_id,OLD.created_at)
   OR NEW.employment_epoch<=OLD.employment_epoch OR NEW.generation_id=OLD.generation_id THEN
   RAISE EXCEPTION 'employment basis must advance without reassigning identity' USING ERRCODE='23514';
  END IF;
 END IF;
 NEW.updated_at:=clock_timestamp();
 RETURN NEW;
END $$;
CREATE TRIGGER iam_employment_basis_write BEFORE INSERT OR UPDATE ON iam_employment_bases
 FOR EACH ROW EXECUTE FUNCTION iam_employment_basis_write();
CREATE FUNCTION iam_employment_basis_record() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO iam_employment_basis_history SELECT NEW.*;
 INSERT INTO audit_events(tenant_id,actor_subject,action,resource_type,resource_id,changed_fields)
 VALUES(NEW.tenant_id,NEW.actor_subject,'employment_basis_consumed','iam_employment_bases',NEW.generation_id,ARRAY['generation_id','employment_epoch','valid_until']);
 RETURN NULL;
END $$;
CREATE TRIGGER iam_employment_basis_record AFTER INSERT OR UPDATE ON iam_employment_bases
 FOR EACH ROW EXECUTE FUNCTION iam_employment_basis_record();
CREATE TRIGGER iam_employment_basis_preserve BEFORE DELETE OR TRUNCATE ON iam_employment_bases
 FOR EACH STATEMENT EXECUTE FUNCTION iam_membership_preserve();
CREATE TRIGGER iam_employment_basis_history_preserve BEFORE UPDATE OR DELETE OR TRUNCATE ON iam_employment_basis_history
 FOR EACH STATEMENT EXECUTE FUNCTION iam_membership_preserve();
CREATE TRIGGER iam_assignment_employment_basis_preserve BEFORE UPDATE OR DELETE OR TRUNCATE ON iam_assignment_employment_bases
 FOR EACH STATEMENT EXECUTE FUNCTION iam_membership_preserve();
CREATE FUNCTION iam_employment_evidence_insert() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF pg_trigger_depth()<2 THEN RAISE EXCEPTION 'employment basis evidence is owner recorded' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER iam_employment_history_insert BEFORE INSERT ON iam_employment_basis_history
 FOR EACH ROW EXECUTE FUNCTION iam_employment_evidence_insert();
CREATE TRIGGER iam_employment_assignment_insert BEFORE INSERT ON iam_assignment_employment_bases
 FOR EACH ROW EXECUTE FUNCTION iam_employment_evidence_insert();
CREATE FUNCTION iam_capture_assignment_employment_basis() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 INSERT INTO iam_assignment_employment_bases(tenant_id,proposal_id,generation_id)
  SELECT NEW.tenant_id,NEW.id,b.generation_id FROM iam_employment_bases b
  WHERE (b.tenant_id,b.hospital_id,b.subject)=(NEW.tenant_id,NEW.hospital_id,NEW.subject);
 RETURN NULL;
END $$;
CREATE TRIGGER iam_capture_assignment_employment_basis AFTER INSERT ON iam_assignment_proposals
 FOR EACH ROW EXECUTE FUNCTION iam_capture_assignment_employment_basis();
CREATE FUNCTION iam_employment_membership_boundary() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE prior_access text:=current_setting('app.assignment_access',true); bound boolean;
BEGIN
 IF NEW.role_key='staff_member' THEN RETURN NULL; END IF;
 PERFORM set_config('app.assignment_access','authorized',true);
 SELECT EXISTS(SELECT 1 FROM iam_employment_bases WHERE tenant_id=NEW.tenant_id AND subject=NEW.subject) INTO bound;
 PERFORM set_config('app.assignment_access',COALESCE(prior_access,''),true);
 IF bound THEN RAISE EXCEPTION 'reviewed employment requires scoped assignments, not legacy role elevation' USING ERRCODE='23514',CONSTRAINT='iam_employment_legacy_role'; END IF;
 RETURN NULL;
END $$;
CREATE TRIGGER zz_iam_employment_membership_boundary AFTER INSERT OR UPDATE ON iam_members
 FOR EACH ROW EXECUTE FUNCTION iam_employment_membership_boundary();
