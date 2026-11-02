SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE TABLE workforce_organization_observations (
 tenant_id uuid NOT NULL, request_id uuid NOT NULL CHECK(substring(request_id::text,15,1)='7' AND substring(request_id::text,20,1) IN ('8','9','a','b')),
 hospital_id uuid NOT NULL CHECK(hospital_id<>'00000000-0000-0000-0000-000000000000'::uuid),
 observed_exists boolean NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,request_id)
);
ALTER TABLE workforce_organization_observations ENABLE ROW LEVEL SECURITY;
ALTER TABLE workforce_organization_observations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workforce_organization_observations
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.workforce_organization_observation',true)='authorized');
CREATE TRIGGER workforce_organization_observation_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON workforce_organization_observations
 FOR EACH STATEMENT EXECUTE FUNCTION workforce_audit_immutable();
