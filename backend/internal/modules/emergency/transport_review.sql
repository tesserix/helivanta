SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE TABLE emergency_clinical_reviewers (
 tenant_id uuid NOT NULL, base_id uuid NOT NULL,
 id uuid NOT NULL CHECK(get_byte(uuid_send(id),6)/16=7),
 subject text NOT NULL CHECK(length(btrim(subject)) BETWEEN 1 AND 200),
 version bigint NOT NULL CHECK(version>0),
 enabled boolean NOT NULL,
 evidence_reference text NOT NULL CHECK(evidence_reference ~ '^[A-Za-z0-9._/-]{1,120}$'),
 expires_at timestamptz NOT NULL,
 verified_by text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,base_id,id),
 UNIQUE(tenant_id,base_id,subject,version),
 FOREIGN KEY(tenant_id,base_id) REFERENCES emergency_bases(tenant_id,id) ON DELETE RESTRICT
);
CREATE INDEX ON emergency_clinical_reviewers(tenant_id,base_id,subject,version DESC);
ALTER TABLE emergency_clinical_reviewers ENABLE ROW LEVEL SECURITY;
ALTER TABLE emergency_clinical_reviewers FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON emergency_clinical_reviewers
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND base_id=NULLIF(current_setting('app.emergency_base',true),'')::uuid
 AND (current_setting('app.emergency_admin',true)='true' OR subject=NULLIF(current_setting('app.emergency_subject',true),'')))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND base_id=NULLIF(current_setting('app.emergency_base',true),'')::uuid
 AND current_setting('app.emergency_admin',true)='true' AND verified_by=NULLIF(current_setting('app.emergency_subject',true),''));
CREATE TRIGGER emergency_clinical_reviewer_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON emergency_clinical_reviewers FOR EACH STATEMENT EXECUTE FUNCTION emergency_append_only();
CREATE FUNCTION emergency_clinical_reviewer_valid() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 PERFORM 1 FROM emergency_bases WHERE tenant_id=NEW.tenant_id AND id=NEW.base_id FOR UPDATE;
 IF NEW.version<>(SELECT COALESCE(MAX(version),0)+1 FROM emergency_clinical_reviewers WHERE tenant_id=NEW.tenant_id AND base_id=NEW.base_id AND subject=NEW.subject)
 OR NEW.created_at<>now() OR NEW.expires_at>clock_timestamp()+interval '366 days'
 OR (NEW.enabled AND (NEW.expires_at<=clock_timestamp() OR NEW.subject=NEW.verified_by)) THEN
 RAISE EXCEPTION 'invalid clinical verification' USING ERRCODE='23514'; END IF; RETURN NEW;
END $$;
CREATE TRIGGER emergency_clinical_reviewer_valid BEFORE INSERT ON emergency_clinical_reviewers FOR EACH ROW EXECUTE FUNCTION emergency_clinical_reviewer_valid();

CREATE FUNCTION emergency_current_reviewer(t uuid,b uuid) RETURNS uuid LANGUAGE sql STABLE AS $$
 SELECT r.id FROM (SELECT id,enabled,expires_at FROM emergency_clinical_reviewers
 WHERE tenant_id=t AND base_id=b AND subject=NULLIF(current_setting('app.emergency_subject',true),'') ORDER BY version DESC LIMIT 1) r
 WHERE emergency_allowed(t,b,'read') AND r.enabled AND r.expires_at>statement_timestamp();
$$;

CREATE TABLE emergency_transport_assessments (
 tenant_id uuid NOT NULL, base_id uuid NOT NULL,
 id uuid NOT NULL CHECK(get_byte(uuid_send(id),6)/16=7),
 case_id uuid NOT NULL,
 case_version bigint NOT NULL CHECK(case_version>0),
 sequence bigint NOT NULL CHECK(sequence>0),
 reviewer_grant_id uuid NOT NULL,
 reviewer_subject text NOT NULL,
 required_class text NOT NULL CHECK(required_class IN('patient_transport','bls','als')),
 policy_reference text NOT NULL CHECK(policy_reference ~ '^[A-Za-z0-9._/-]{1,120}$'),
 valid_until timestamptz NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,base_id,id),
 UNIQUE(tenant_id,base_id,case_id,sequence),
 FOREIGN KEY(tenant_id,base_id,case_id) REFERENCES emergency_cases(tenant_id,base_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,base_id,reviewer_grant_id) REFERENCES emergency_clinical_reviewers(tenant_id,base_id,id) ON DELETE RESTRICT
);
CREATE INDEX ON emergency_transport_assessments(tenant_id,base_id,case_id,created_at DESC,id DESC);
ALTER TABLE emergency_transport_assessments ENABLE ROW LEVEL SECURITY;
ALTER TABLE emergency_transport_assessments FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON emergency_transport_assessments
 USING(hms_tenant_visible(tenant_id) AND emergency_allowed(tenant_id,base_id,'read'))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND reviewer_subject=NULLIF(current_setting('app.emergency_subject',true),'')
 AND reviewer_grant_id=emergency_current_reviewer(tenant_id,base_id));
CREATE TRIGGER emergency_transport_assessment_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON emergency_transport_assessments FOR EACH STATEMENT EXECUTE FUNCTION emergency_append_only();
CREATE FUNCTION emergency_transport_assessment_valid() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 PERFORM 1 FROM emergency_bases WHERE tenant_id=NEW.tenant_id AND id=NEW.base_id FOR SHARE;
 PERFORM 1 FROM emergency_cases WHERE tenant_id=NEW.tenant_id AND base_id=NEW.base_id AND id=NEW.case_id FOR UPDATE;
 IF NOT EXISTS(SELECT 1 FROM emergency_cases c JOIN emergency_clinical_reviewers r ON r.tenant_id=c.tenant_id AND r.base_id=c.base_id AND r.id=NEW.reviewer_grant_id
 WHERE c.tenant_id=NEW.tenant_id AND c.base_id=NEW.base_id AND c.id=NEW.case_id AND c.version=NEW.case_version AND c.status IN('received','active')
 AND c.requested_service NOT IN('neonatal','mortuary','home_visit') AND r.id=emergency_current_reviewer(c.tenant_id,c.base_id)
 AND r.subject=NEW.reviewer_subject AND r.expires_at>=NEW.valid_until)
 OR NEW.valid_until<=clock_timestamp() OR NEW.valid_until>clock_timestamp()+interval '24 hours' OR NEW.created_at<>now()
 OR NEW.sequence<>(SELECT COALESCE(MAX(sequence),0)+1 FROM emergency_transport_assessments WHERE tenant_id=NEW.tenant_id AND base_id=NEW.base_id AND case_id=NEW.case_id) THEN
 RAISE EXCEPTION 'invalid transport requirement review' USING ERRCODE='23514'; END IF; RETURN NEW;
END $$;
CREATE TRIGGER emergency_transport_assessment_valid BEFORE INSERT ON emergency_transport_assessments FOR EACH ROW EXECUTE FUNCTION emergency_transport_assessment_valid();
