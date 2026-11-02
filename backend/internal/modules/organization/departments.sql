SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE TABLE organization_departments (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7' AND substring(id::text,20,1) IN ('8','9','a','b')),
 code text NOT NULL CHECK(code ~ '^[A-Z0-9_-]{1,40}$'),
 kind text NOT NULL CHECK(kind IN ('department','sub_unit')),
 name text NOT NULL CHECK(length(name) BETWEEN 1 AND 120 AND name=btrim(name) AND name !~ '[[:cntrl:]]'),
 parent_id uuid, status text NOT NULL CHECK(status IN ('active','retired')),
 version bigint NOT NULL CHECK(version BETWEEN 1 AND 9007199254740991),
 revision_id uuid NOT NULL,
 effective_at timestamptz NOT NULL,
 created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,hospital_id,id), UNIQUE(tenant_id,hospital_id,code),
 CHECK((kind='department' AND parent_id IS NULL) OR (kind='sub_unit' AND parent_id IS NOT NULL)),
 CHECK(parent_id IS DISTINCT FROM id),
 FOREIGN KEY(tenant_id,hospital_id) REFERENCES organization_hospital_registrations(tenant_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,hospital_id,parent_id) REFERENCES organization_departments(tenant_id,hospital_id,id) ON DELETE RESTRICT
);
CREATE TABLE organization_department_revisions (
 tenant_id uuid NOT NULL, hospital_id uuid NOT NULL,
 node_id uuid NOT NULL, id uuid NOT NULL CHECK(substring(id::text,15,1)='7' AND substring(id::text,20,1) IN ('8','9','a','b')),
 code text NOT NULL, kind text NOT NULL, name text NOT NULL, parent_id uuid, status text NOT NULL,
 version bigint NOT NULL CHECK(version BETWEEN 1 AND 9007199254740991),
 expected_version bigint NOT NULL CHECK(expected_version=version-1),
 reason text NOT NULL CHECK(length(reason) BETWEEN 1 AND 500 AND reason=btrim(reason) AND reason !~ '[[:cntrl:]]'),
 actor_subject text NOT NULL CHECK(length(actor_subject) BETWEEN 1 AND 200),
 effective_at timestamptz NOT NULL, node_created_at timestamptz NOT NULL, created_at timestamptz NOT NULL, updated_at timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,hospital_id,node_id,version),
 UNIQUE(tenant_id,hospital_id,node_id,id,version),
 FOREIGN KEY(tenant_id,hospital_id,node_id) REFERENCES organization_departments(tenant_id,hospital_id,id) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED
);
ALTER TABLE organization_departments ADD CONSTRAINT organization_department_current_revision
 FOREIGN KEY(tenant_id,hospital_id,id,revision_id,version) REFERENCES organization_department_revisions(tenant_id,hospital_id,node_id,id,version) ON DELETE RESTRICT DEFERRABLE INITIALLY DEFERRED;
CREATE INDEX organization_department_parent ON organization_departments(tenant_id,hospital_id,parent_id);
CREATE INDEX organization_department_page ON organization_departments(tenant_id,hospital_id,created_at DESC,id DESC);
CREATE INDEX organization_department_history ON organization_department_revisions(tenant_id,hospital_id,node_id,created_at DESC,id DESC);

ALTER TABLE organization_departments ENABLE ROW LEVEL SECURITY;
ALTER TABLE organization_departments FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON organization_departments
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.organization_department_write',true)='authorized');
ALTER TABLE organization_department_revisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE organization_department_revisions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON organization_department_revisions
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.organization_department_write',true)='authorized');

CREATE TRIGGER organization_department_retained BEFORE DELETE OR TRUNCATE ON organization_departments
 FOR EACH STATEMENT EXECUTE FUNCTION organization_prevent_removal();
CREATE TRIGGER organization_department_revision_retained BEFORE UPDATE OR DELETE OR TRUNCATE ON organization_department_revisions
 FOR EACH STATEMENT EXECUTE FUNCTION organization_prevent_removal();

CREATE FUNCTION organization_department_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE ancestor_depth integer:=0; subtree_depth integer:=0; cyclic boolean:=false;
BEGIN
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id::text||':organization-departments:'||NEW.hospital_id::text,0));
 IF current_setting('app.organization_department_write',true) IS DISTINCT FROM 'authorized'
 OR NOT EXISTS(SELECT 1 FROM organization_hospital_registrations WHERE tenant_id=NEW.tenant_id AND id=NEW.hospital_id AND status='verified') THEN
  RAISE EXCEPTION 'department reference is unavailable' USING ERRCODE='23514',CONSTRAINT='organization_department_guard';
 END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.version<>1 OR NEW.status<>'active' OR (SELECT count(*) FROM organization_departments WHERE tenant_id=NEW.tenant_id AND hospital_id=NEW.hospital_id)>=5000 THEN
   RAISE EXCEPTION 'department creation is invalid' USING ERRCODE='23514',CONSTRAINT='organization_department_guard';
  END IF;
 ELSE
  IF (NEW.tenant_id,NEW.hospital_id,NEW.id,NEW.code,NEW.kind,NEW.created_at) IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.id,OLD.code,OLD.kind,OLD.created_at)
   OR OLD.status<>'active' OR NEW.version<>OLD.version+1 OR NEW.effective_at<=OLD.effective_at THEN
   RAISE EXCEPTION 'department identity or transition is invalid' USING ERRCODE='23514',CONSTRAINT='organization_department_guard';
  END IF;
 END IF;
 IF NEW.updated_at<>NEW.effective_at OR NEW.effective_at<NEW.created_at THEN
  RAISE EXCEPTION 'department effective time is invalid' USING ERRCODE='23514',CONSTRAINT='organization_department_guard';
 END IF;
 IF NEW.status='retired' AND EXISTS(SELECT 1 FROM organization_departments WHERE tenant_id=NEW.tenant_id AND hospital_id=NEW.hospital_id AND parent_id=NEW.id AND status='active') THEN
  RAISE EXCEPTION 'department has active children' USING ERRCODE='23514',CONSTRAINT='organization_department_guard';
 END IF;
 IF NEW.parent_id IS NOT NULL THEN
  IF NOT EXISTS(SELECT 1 FROM organization_departments WHERE tenant_id=NEW.tenant_id AND hospital_id=NEW.hospital_id AND id=NEW.parent_id AND status='active') THEN
   RAISE EXCEPTION 'department parent is unavailable' USING ERRCODE='23514',CONSTRAINT='organization_department_guard';
  END IF;
  WITH RECURSIVE ancestors AS (
   SELECT id,parent_id,1 AS depth FROM organization_departments WHERE tenant_id=NEW.tenant_id AND hospital_id=NEW.hospital_id AND id=NEW.parent_id
   UNION ALL SELECT d.id,d.parent_id,a.depth+1 FROM organization_departments d JOIN ancestors a ON d.id=a.parent_id WHERE d.tenant_id=NEW.tenant_id AND d.hospital_id=NEW.hospital_id AND a.depth<=8
  ) SELECT COALESCE(max(depth),0),COALESCE(bool_or(id=NEW.id),false) INTO ancestor_depth,cyclic FROM ancestors;
 END IF;
 WITH RECURSIVE descendants AS (
  SELECT id,1 AS depth FROM organization_departments WHERE tenant_id=NEW.tenant_id AND hospital_id=NEW.hospital_id AND parent_id=NEW.id
  UNION ALL SELECT d.id,c.depth+1 FROM organization_departments d JOIN descendants c ON d.parent_id=c.id WHERE d.tenant_id=NEW.tenant_id AND d.hospital_id=NEW.hospital_id AND c.depth<=8
 ) SELECT COALESCE(max(depth),0) INTO subtree_depth FROM descendants;
 IF cyclic OR ancestor_depth+1+subtree_depth>8 THEN
  RAISE EXCEPTION 'department hierarchy is invalid' USING ERRCODE='23514',CONSTRAINT='organization_department_guard';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER organization_department_guard BEFORE INSERT OR UPDATE ON organization_departments
 FOR EACH ROW EXECUTE FUNCTION organization_department_guard();
CREATE FUNCTION organization_department_revision_consistent() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM organization_department_revisions r WHERE
  (r.tenant_id,r.hospital_id,r.node_id,r.id,r.version)=(NEW.tenant_id,NEW.hospital_id,NEW.id,NEW.revision_id,NEW.version)
  AND (r.code,r.kind,r.name,r.parent_id,r.status,r.effective_at,r.node_created_at,r.updated_at) IS NOT DISTINCT FROM
      (NEW.code,NEW.kind,NEW.name,NEW.parent_id,NEW.status,NEW.effective_at,NEW.created_at,NEW.updated_at)) THEN
  RAISE EXCEPTION 'department head and revision must agree' USING ERRCODE='23514',CONSTRAINT='organization_department_guard';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER organization_department_revision_consistent AFTER INSERT OR UPDATE ON organization_departments
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION organization_department_revision_consistent();
