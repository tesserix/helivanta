SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE TABLE workforce_partner_group_observations (
 tenant_id uuid NOT NULL,
 group_id uuid NOT NULL CHECK(substring(group_id::text,15,1)='7' AND substring(group_id::text,20,1) IN ('8','9','a','b')),
 hospital_id uuid NOT NULL CHECK(hospital_id<>'00000000-0000-0000-0000-000000000000'::uuid),
 observed_exists boolean NOT NULL,
 source_event_id uuid NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(), updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,group_id)
);
ALTER TABLE workforce_partner_group_observations ENABLE ROW LEVEL SECURITY;
ALTER TABLE workforce_partner_group_observations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workforce_partner_group_observations
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.workforce_partner_group_observation',true)='authorized');
CREATE TRIGGER workforce_partner_group_observation_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON workforce_partner_group_observations FOR EACH STATEMENT EXECUTE FUNCTION workforce_audit_immutable();
