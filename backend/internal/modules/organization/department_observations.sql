SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE TABLE organization_department_observations (
 tenant_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7' AND substring(id::text,20,1) IN ('8','9','a','b')),
 hospital_id uuid NOT NULL CHECK(hospital_id<>'00000000-0000-0000-0000-000000000000'::uuid),
 master_id uuid NOT NULL CHECK(master_id<>'00000000-0000-0000-0000-000000000000'::uuid),
 master_revision_id uuid NOT NULL CHECK(master_revision_id<>'00000000-0000-0000-0000-000000000000'::uuid),
 department_id uuid NOT NULL CHECK(substring(department_id::text,15,1)='7' AND substring(department_id::text,20,1) IN ('8','9','a','b')),
 department_revision_id uuid NOT NULL CHECK(substring(department_revision_id::text,15,1)='7' AND substring(department_revision_id::text,20,1) IN ('8','9','a','b')),
 matches_current boolean NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,id)
);
ALTER TABLE organization_department_observations ENABLE ROW LEVEL SECURITY;
ALTER TABLE organization_department_observations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON organization_department_observations
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.organization_department_observation',true)='authorized');
CREATE TRIGGER organization_department_observation_retained BEFORE UPDATE OR DELETE OR TRUNCATE ON organization_department_observations
 FOR EACH STATEMENT EXECUTE FUNCTION organization_prevent_removal();
