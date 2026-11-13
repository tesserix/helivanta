SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE TABLE iam_employment_basis_authorities (
 tenant_id uuid NOT NULL,generation_id uuid NOT NULL,
 hospital_id uuid NOT NULL,staff_id uuid NOT NULL,subject text NOT NULL,actor_subject text NOT NULL,
 authority jsonb NOT NULL CHECK(jsonb_typeof(authority)='object'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,generation_id),
 FOREIGN KEY(tenant_id,generation_id) REFERENCES iam_employment_basis_history(tenant_id,generation_id) ON DELETE RESTRICT,
 CHECK(subject<>actor_subject)
);
CREATE INDEX ON iam_employment_basis_authorities(tenant_id,hospital_id,staff_id,generation_id);
ALTER TABLE iam_employment_basis_authorities ENABLE ROW LEVEL SECURITY;
ALTER TABLE iam_employment_basis_authorities FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON iam_employment_basis_authorities
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND current_setting('app.assignment_access',true)='authorized')
 WITH CHECK(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND current_setting('app.assignment_access',true)='authorized'
 AND current_setting('app.iam_employment_basis',true)='consume'
 AND actor_subject=current_setting('app.actor_subject',true)
 AND authority=NULLIF(current_setting('app.iam_employment_governance',true),'')::jsonb);

CREATE FUNCTION iam_employment_basis_governance_record()RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE proof jsonb:=NULLIF(current_setting('app.iam_employment_governance',true),'')::jsonb;
BEGIN
 IF proof IS NULL THEN RETURN NULL;END IF;
 IF NOT COALESCE(jsonb_typeof(proof)='object'
 AND proof->>'capability'='workforce.employment.activate'
 AND proof->>'tenant_id'=NEW.tenant_id::text AND proof->>'hospital_id'=NEW.hospital_id::text
 AND proof->>'staff_id'=NEW.staff_id::text AND proof->>'actor_subject'=NEW.actor_subject
 AND proof->>'actor_subject'=current_setting('app.actor_subject',true)
 AND (proof->>'membership_epoch')::bigint BETWEEN 1 AND 9007199254740991
 AND (proof->>'owner_count')::bigint BETWEEN 1 AND 1000
 AND (proof->>'authenticated_at')::timestamptz<=clock_timestamp()
 AND (proof->>'idle_deadline')::timestamptz>clock_timestamp()
 AND (proof->>'expires_at')::timestamptz>clock_timestamp()
 AND EXISTS(SELECT 1 FROM iam_subject_membership_epochs e WHERE e.tenant_id=NEW.tenant_id AND e.subject=NEW.actor_subject AND e.member_count>0 AND e.epoch=(proof->>'membership_epoch')::bigint)
 AND ((proof->>'authority_kind'='owner'
 AND (proof->>'owner_membership_id')::uuid<>'00000000-0000-0000-0000-000000000000'::uuid
 AND proof->>'source_grant_id' IS NULL AND proof->>'source_grant_version' IS NULL AND proof->>'template_key' IS NULL
 AND EXISTS(SELECT 1 FROM iam_members m WHERE m.tenant_id=NEW.tenant_id AND m.id=(proof->>'owner_membership_id')::uuid AND m.subject=NEW.actor_subject AND m.role_key='tenant_admin'))
 OR(proof->>'authority_kind'='grant' AND proof->>'owner_membership_id' IS NULL
 AND substring(proof->>'source_grant_id',15,1)='7'
 AND (proof->>'source_grant_id')::uuid<>'00000000-0000-0000-0000-000000000000'::uuid
 AND (proof->>'source_grant_version')::bigint BETWEEN 1 AND 9007199254740991
 AND proof->>'template_key'='workforce.employment_activator.v1'
 AND EXISTS(SELECT 1 FROM iam_governance_grants g WHERE g.tenant_id=NEW.tenant_id AND g.id=(proof->>'source_grant_id')::uuid AND g.subject=NEW.actor_subject AND g.version=(proof->>'source_grant_version')::bigint AND g.membership_epoch=(proof->>'membership_epoch')::bigint AND g.template_key='workforce.employment_activator.v1' AND g.scope_kind='hospital' AND g.scope_id=NEW.hospital_id AND g.status IN('active','provisional_active') AND g.valid_from<=clock_timestamp() AND g.expires_at=(proof->>'expires_at')::timestamptz))),false) THEN
  RAISE EXCEPTION 'employment basis requires exact current governance source' USING ERRCODE='42501';
 END IF;
 INSERT INTO iam_employment_basis_authorities(tenant_id,generation_id,hospital_id,staff_id,subject,actor_subject,authority)
 VALUES(NEW.tenant_id,NEW.generation_id,NEW.hospital_id,NEW.staff_id,NEW.subject,NEW.actor_subject,proof);
 RETURN NULL;
END $$;
CREATE TRIGGER iam_employment_basis_zz_authority AFTER INSERT OR UPDATE ON iam_employment_bases
 FOR EACH ROW EXECUTE FUNCTION iam_employment_basis_governance_record();
CREATE TRIGGER iam_employment_basis_authority_preserve BEFORE UPDATE OR DELETE OR TRUNCATE ON iam_employment_basis_authorities
 FOR EACH STATEMENT EXECUTE FUNCTION iam_membership_preserve();
