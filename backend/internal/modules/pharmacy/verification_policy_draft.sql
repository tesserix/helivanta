SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

-- Administrative preparation only. No effective policy or clinical grant is created.
CREATE TABLE pharmacy_verification_policy_drafts (
 tenant_id uuid NOT NULL,
 outlet_id uuid NOT NULL,
 policy_id uuid NOT NULL CHECK (substring(policy_id::text,15,1)='7' AND substring(policy_id::text,20,1) IN ('8','9','a','b')),
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7' AND substring(id::text,20,1) IN ('8','9','a','b')),
 expected_version bigint NOT NULL,
 version bigint NOT NULL CHECK(version=expected_version+1),
 label text NOT NULL,
 second_check_required boolean NOT NULL,
 review_valid_for_seconds bigint NOT NULL,
 query_response_target_seconds bigint NOT NULL,
 reason text NOT NULL,
 intent_sha256 text NOT NULL CHECK(intent_sha256 ~ '^[a-f0-9]{64}$'),
 prepared_by text NOT NULL CHECK(length(prepared_by) BETWEEN 1 AND 512),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,outlet_id,id),
 UNIQUE(tenant_id,outlet_id,policy_id,version),
 FOREIGN KEY(tenant_id,outlet_id) REFERENCES pharmacy_outlets(tenant_id,id),
 CONSTRAINT pharmacy_verification_policy_draft_intent CHECK(pharmacy_verification_policy_draft_contract(expected_version,label,second_check_required,review_valid_for_seconds,query_response_target_seconds,reason))
);
CREATE INDEX pharmacy_verification_policy_draft_history ON pharmacy_verification_policy_drafts(tenant_id,outlet_id,policy_id,created_at DESC,id DESC);
CREATE INDEX pharmacy_verification_policy_draft_page ON pharmacy_verification_policy_drafts(tenant_id,outlet_id,created_at DESC,id DESC);
ALTER TABLE pharmacy_verification_policy_drafts ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_verification_policy_drafts FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_verification_policy_drafts
 USING(hms_tenant_visible(tenant_id) AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK(pharmacy_outlet_allowed(tenant_id,outlet_id,true) AND prepared_by=NULLIF(current_setting('app.pharmacy_subject',true),''));
CREATE FUNCTION pharmacy_verification_policy_draft_lock(tenant uuid,outlet uuid,policy uuid) RETURNS void
LANGUAGE sql AS $$
 SELECT pg_advisory_xact_lock(hashtextextended(tenant::text||':verification-policy-draft:'||outlet::text||':'||policy::text,0))
$$;
CREATE FUNCTION pharmacy_verification_policy_draft_version_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE previous bigint;
BEGIN
 PERFORM pharmacy_verification_policy_draft_lock(NEW.tenant_id,NEW.outlet_id,NEW.policy_id);
 SELECT COALESCE(max(version),0) INTO previous FROM pharmacy_verification_policy_drafts
 WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND policy_id=NEW.policy_id;
 IF previous<>NEW.expected_version THEN
  RAISE EXCEPTION 'policy draft version changed' USING ERRCODE='23514',CONSTRAINT='pharmacy_verification_policy_draft_version';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_verification_policy_draft_version_guard BEFORE INSERT ON pharmacy_verification_policy_drafts
FOR EACH ROW EXECUTE FUNCTION pharmacy_verification_policy_draft_version_guard();
CREATE TRIGGER pharmacy_verification_policy_draft_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_verification_policy_drafts
FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();
CREATE TRIGGER pharmacy_verification_policy_draft_audit AFTER INSERT ON pharmacy_verification_policy_drafts
FOR EACH ROW EXECUTE FUNCTION audit_record_change();
