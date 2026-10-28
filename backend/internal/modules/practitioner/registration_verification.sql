SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE TABLE practitioner_registration_verifications (
 tenant_id uuid NOT NULL, practitioner_id uuid NOT NULL, registration_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7' AND substring(id::text,20,1) IN ('8','9','a','b')),
 action text NOT NULL CHECK(action IN ('verify','revoke')),
 expected_version bigint NOT NULL CHECK(expected_version BETWEEN 0 AND 9007199254740990),
 version bigint NOT NULL CHECK(version=expected_version+1),
 revision_id uuid,
 policy_id uuid, policy_version bigint, review_id uuid,
 identity_review_reference uuid CHECK(identity_review_reference IS NULL OR substring(identity_review_reference::text,15,1)='7' AND substring(identity_review_reference::text,20,1) IN ('8','9','a','b')),
 undated_attestation_reference uuid CHECK(undated_attestation_reference IS NULL OR substring(undated_attestation_reference::text,15,1)='7' AND substring(undated_attestation_reference::text,20,1) IN ('8','9','a','b')),
 target_verification_id uuid,
 reason_reference uuid CHECK(reason_reference IS NULL OR substring(reason_reference::text,15,1)='7' AND substring(reason_reference::text,20,1) IN ('8','9','a','b')),
 canonical_value text,
 evidence_snapshot jsonb,
 source_checked_at timestamptz,
 valid_until timestamptz,
 intent_sha256 text NOT NULL CHECK(intent_sha256 ~ '^[0-9a-f]{64}$'),
 created_by text NOT NULL CHECK(octet_length(created_by) BETWEEN 1 AND 512),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,registration_id,version),
 FOREIGN KEY(tenant_id,practitioner_id,registration_id) REFERENCES practitioner_registrations(tenant_id,practitioner_id,id),
 FOREIGN KEY(tenant_id,practitioner_id,registration_id,revision_id) REFERENCES practitioner_registration_revisions(tenant_id,practitioner_id,registration_id,id),
 FOREIGN KEY(tenant_id,policy_id,policy_version) REFERENCES practitioner_registration_policy_revisions(tenant_id,policy_id,version),
 FOREIGN KEY(tenant_id,review_id) REFERENCES practitioner_credential_reviews(tenant_id,id),
 FOREIGN KEY(tenant_id,target_verification_id) REFERENCES practitioner_registration_verifications(tenant_id,id),
 CHECK(action='verify' AND revision_id IS NOT NULL AND policy_id IS NOT NULL AND policy_version IS NOT NULL AND review_id IS NOT NULL AND identity_review_reference IS NOT NULL AND target_verification_id IS NULL AND reason_reference IS NULL AND canonical_value IS NOT NULL AND evidence_snapshot IS NOT NULL AND source_checked_at IS NOT NULL AND valid_until IS NOT NULL
  OR action='revoke' AND revision_id IS NULL AND policy_id IS NULL AND policy_version IS NULL AND review_id IS NULL AND identity_review_reference IS NULL AND undated_attestation_reference IS NULL AND target_verification_id IS NOT NULL AND reason_reference IS NOT NULL AND canonical_value IS NULL AND evidence_snapshot IS NULL AND source_checked_at IS NULL AND valid_until IS NULL)
);
CREATE INDEX practitioner_registration_verification_page ON practitioner_registration_verifications(tenant_id,practitioner_id,registration_id,created_at DESC,id DESC);
ALTER TABLE practitioner_registration_verifications ENABLE ROW LEVEL SECURITY;
ALTER TABLE practitioner_registration_verifications FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON practitioner_registration_verifications
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.practitioner_access',true)='true')
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND created_by=NULLIF(current_setting('app.actor_subject',true),'') AND (action='verify' AND current_setting('app.practitioner_verify',true)='true' OR action='revoke' AND current_setting('app.practitioner_verification_revoke',true)='true'));
CREATE TABLE practitioner_registration_identifiers (
 tenant_id uuid NOT NULL, policy_id uuid NOT NULL, canonical_value text NOT NULL CHECK(octet_length(canonical_value) BETWEEN 1 AND 150),
 practitioner_id uuid NOT NULL, registration_id uuid NOT NULL, first_verification_id uuid NOT NULL,
 id uuid GENERATED ALWAYS AS (first_verification_id) STORED,
 created_by text NOT NULL CHECK(octet_length(created_by) BETWEEN 1 AND 512),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,policy_id,canonical_value), UNIQUE(tenant_id,id),
 FOREIGN KEY(tenant_id,practitioner_id,registration_id) REFERENCES practitioner_registrations(tenant_id,practitioner_id,id),
 FOREIGN KEY(tenant_id,policy_id) REFERENCES practitioner_registration_policies(tenant_id,id),
 FOREIGN KEY(tenant_id,first_verification_id) REFERENCES practitioner_registration_verifications(tenant_id,id)
);
ALTER TABLE practitioner_registration_identifiers ENABLE ROW LEVEL SECURITY;
ALTER TABLE practitioner_registration_identifiers FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON practitioner_registration_identifiers
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.practitioner_access',true)='true')
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.practitioner_verify',true)='true' AND created_by=NULLIF(current_setting('app.actor_subject',true),''));
CREATE INDEX practitioner_review_adverse_lookup ON practitioner_credential_reviews(tenant_id,credential_kind,credential_id,finding);
CREATE FUNCTION practitioner_registration_verification_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE registration practitioner_registrations; revision practitioner_registration_revisions; source practitioner_credential_reviews; policy record; previous practitioner_registration_verifications; author text; at_time timestamptz; expiry_at timestamptz; seal jsonb; evidence practitioner_credential_evidence;
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'verification receipt is immutable' USING ERRCODE='23514'; END IF;
 IF NEW.created_by IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'') OR NOT (NEW.action='verify' AND current_setting('app.practitioner_verify',true)='true' OR NEW.action='revoke' AND current_setting('app.practitioner_verification_revoke',true)='true') THEN RAISE EXCEPTION 'verification authority required' USING ERRCODE='42501'; END IF;
 SELECT * INTO registration FROM practitioner_registrations WHERE tenant_id=NEW.tenant_id AND practitioner_id=NEW.practitioner_id AND id=NEW.registration_id FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'registration unavailable' USING ERRCODE='P0002'; END IF;
 SELECT * INTO previous FROM practitioner_registration_verifications WHERE tenant_id=NEW.tenant_id AND registration_id=NEW.registration_id ORDER BY version DESC LIMIT 1;
 IF COALESCE(previous.version,0)<>NEW.expected_version THEN RAISE EXCEPTION 'verification version changed' USING ERRCODE='23514',CONSTRAINT='registration_verification_version'; END IF;
 NEW.version:=NEW.expected_version+1;NEW.created_at:=practitioner_registry_now();
 IF NEW.action='revoke' THEN
  IF previous.action IS DISTINCT FROM 'verify' OR previous.id IS DISTINCT FROM NEW.target_verification_id THEN RAISE EXCEPTION 'verification target changed' USING ERRCODE='23514',CONSTRAINT='registration_verification_target'; END IF;
  RETURN NEW;
 END IF;
 SELECT * INTO revision FROM practitioner_registration_revisions WHERE tenant_id=NEW.tenant_id AND practitioner_id=NEW.practitioner_id AND registration_id=NEW.registration_id AND id=NEW.revision_id AND version=registration.version;
 IF NOT FOUND THEN RAISE EXCEPTION 'registration revision changed' USING ERRCODE='23514',CONSTRAINT='registration_verification_revision'; END IF;
 SELECT created_by INTO author FROM practitioner_records WHERE tenant_id=NEW.tenant_id AND id=NEW.practitioner_id;
 IF NEW.created_by IN (registration.created_by,revision.created_by,author) THEN RAISE EXCEPTION 'claim submitter cannot attest own claim' USING ERRCODE='23514',CONSTRAINT='registration_verification_independence'; END IF;
 SELECT h.source_system,h.normalization,h.expiry_time_zone,r.max_source_age_seconds,r.date_rule,r.expiry_boundary,r.valid_until INTO policy FROM practitioner_registration_policies h JOIN practitioner_registration_policy_revisions r ON r.tenant_id=h.tenant_id AND r.policy_id=h.id AND r.version=h.version WHERE h.tenant_id=NEW.tenant_id AND h.id=NEW.policy_id AND h.version=NEW.policy_version FOR SHARE OF h;
 IF NOT FOUND OR policy.source_system<>registration.council_system THEN RAISE EXCEPTION 'issuer policy is not current or matching' USING ERRCODE='23514',CONSTRAINT='registration_verification_policy'; END IF;
 SELECT * INTO source FROM practitioner_credential_reviews WHERE tenant_id=NEW.tenant_id AND practitioner_id=NEW.practitioner_id AND credential_kind='registration' AND credential_id=NEW.registration_id AND revision_id=NEW.revision_id AND id=NEW.review_id;
 IF NOT FOUND OR source.finding<>'active' THEN RAISE EXCEPTION 'active matching source review required' USING ERRCODE='23514',CONSTRAINT='registration_verification_review'; END IF;
 IF practitioner_registration_unresolved(NEW.tenant_id,NEW.registration_id,NEW.review_id) THEN RAISE EXCEPTION 'source findings require reconciliation' USING ERRCODE='23514',CONSTRAINT='registration_verification_conflict'; END IF;
 FOR seal IN SELECT value FROM jsonb_array_elements(source.evidence_snapshot) LOOP
  SELECT * INTO evidence FROM practitioner_credential_evidence WHERE tenant_id=NEW.tenant_id AND practitioner_id=NEW.practitioner_id AND credential_kind='registration' AND credential_id=NEW.registration_id AND revision_id=NEW.revision_id AND id=(seal->>'id')::uuid FOR SHARE;
  IF NOT FOUND OR evidence.status<>'available' OR evidence.created_by=NEW.created_by OR jsonb_build_object('id',evidence.id,'sha256',evidence.sha256,'size',evidence.size,'source_generation',evidence.source_generation,'released_generation',evidence.released_generation,'scan_attempt',evidence.scan_attempt,'scanned_at',evidence.scanned_at)<>seal THEN RAISE EXCEPTION 'reviewed evidence changed or unavailable' USING ERRCODE='23514',CONSTRAINT='registration_verification_evidence'; END IF;
 END LOOP;
 at_time:=practitioner_registry_now();
 IF policy.valid_until<=at_time OR source.source_checked_at>at_time OR source.source_checked_at+make_interval(secs=>policy.max_source_age_seconds)<=at_time THEN RAISE EXCEPTION 'source or policy freshness elapsed' USING ERRCODE='23514',CONSTRAINT='registration_verification_freshness'; END IF;
 IF revision.issued_on>(at_time AT TIME ZONE policy.expiry_time_zone)::date THEN RAISE EXCEPTION 'reported issue date is in future' USING ERRCODE='23514',CONSTRAINT='registration_verification_dates'; END IF;
 IF revision.expires_on IS NULL THEN
  IF policy.date_rule='date_required' OR NEW.undated_attestation_reference IS NULL THEN RAISE EXCEPTION 'expiry requires source attestation' USING ERRCODE='23514',CONSTRAINT='registration_verification_expiry'; END IF;
 ELSE
  expiry_at:=CASE WHEN policy.expiry_boundary='end_of_day' THEN (revision.expires_on+1)::timestamp AT TIME ZONE policy.expiry_time_zone ELSE revision.expires_on::timestamp AT TIME ZONE policy.expiry_time_zone END;
  IF expiry_at<=at_time THEN RAISE EXCEPTION 'reported registration expired' USING ERRCODE='23514',CONSTRAINT='registration_verification_expiry'; END IF;
 END IF;
 IF policy.normalization='ascii_upper' THEN
  IF octet_length(registration.registration_number)<>length(registration.registration_number) THEN RAISE EXCEPTION 'identifier requires supported issuer normalization' USING ERRCODE='23514',CONSTRAINT='registration_verification_normalization'; END IF;
  NEW.canonical_value:=translate(registration.registration_number,'abcdefghijklmnopqrstuvwxyz','ABCDEFGHIJKLMNOPQRSTUVWXYZ');
 ELSE NEW.canonical_value:=registration.registration_number; END IF;
 NEW.source_checked_at:=source.source_checked_at;NEW.evidence_snapshot:=source.evidence_snapshot;
 NEW.valid_until:=LEAST(policy.valid_until,source.source_checked_at+make_interval(secs=>policy.max_source_age_seconds),expiry_at);
 NEW.created_at:=at_time;RETURN NEW;
END $$;
CREATE FUNCTION practitioner_registration_identifier_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'identifier ownership is retained' USING ERRCODE='23514'; END IF;
 IF current_setting('app.practitioner_verify',true) IS DISTINCT FROM 'true' OR NEW.created_by IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'') OR NOT EXISTS(SELECT 1 FROM practitioner_registration_verifications WHERE tenant_id=NEW.tenant_id AND practitioner_id=NEW.practitioner_id AND registration_id=NEW.registration_id AND id=NEW.first_verification_id AND action='verify' AND policy_id=NEW.policy_id AND canonical_value=NEW.canonical_value AND created_by=NEW.created_by) THEN RAISE EXCEPTION 'identifier requires an attributed attestation' USING ERRCODE='23514'; END IF;
 NEW.created_at:=practitioner_registry_now();RETURN NEW;
END $$;
CREATE FUNCTION practitioner_registration_identifier_apply() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE owner practitioner_registration_identifiers;
BEGIN
 IF NEW.action<>'verify' THEN RETURN NULL; END IF;
 INSERT INTO practitioner_registration_identifiers(tenant_id,policy_id,canonical_value,practitioner_id,registration_id,first_verification_id,created_by) VALUES(NEW.tenant_id,NEW.policy_id,NEW.canonical_value,NEW.practitioner_id,NEW.registration_id,NEW.id,NEW.created_by) ON CONFLICT(tenant_id,policy_id,canonical_value) DO NOTHING;
 SELECT * INTO owner FROM practitioner_registration_identifiers WHERE tenant_id=NEW.tenant_id AND policy_id=NEW.policy_id AND canonical_value=NEW.canonical_value FOR UPDATE;
 IF owner.practitioner_id<>NEW.practitioner_id OR owner.registration_id<>NEW.registration_id THEN RAISE EXCEPTION 'identifier already bound; reconciliation required' USING ERRCODE='23514',CONSTRAINT='registration_identifier_conflict'; END IF;
 RETURN NULL;
END $$;
CREATE TRIGGER practitioner_registration_verification_guard BEFORE INSERT OR UPDATE OR DELETE ON practitioner_registration_verifications FOR EACH ROW EXECUTE FUNCTION practitioner_registration_verification_guard();
CREATE TRIGGER practitioner_registration_identifier_guard BEFORE INSERT OR UPDATE OR DELETE ON practitioner_registration_identifiers FOR EACH ROW EXECUTE FUNCTION practitioner_registration_identifier_guard();
CREATE TRIGGER practitioner_registration_identifier_apply AFTER INSERT ON practitioner_registration_verifications FOR EACH ROW EXECUTE FUNCTION practitioner_registration_identifier_apply();
CREATE TRIGGER practitioner_registration_verification_audit AFTER INSERT ON practitioner_registration_verifications FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER practitioner_registration_identifier_audit AFTER INSERT ON practitioner_registration_identifiers FOR EACH ROW EXECUTE FUNCTION audit_record_change();
