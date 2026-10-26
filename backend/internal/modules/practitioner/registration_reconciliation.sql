SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE TABLE practitioner_registration_reconciliations (
 tenant_id uuid NOT NULL, practitioner_id uuid NOT NULL, registration_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7' AND substring(id::text,20,1) IN ('8','9','a','b')),
 revision_id uuid NOT NULL, active_review_id uuid NOT NULL,
 adverse_review_ids jsonb NOT NULL CHECK(jsonb_typeof(adverse_review_ids)='array' AND jsonb_array_length(adverse_review_ids) BETWEEN 1 AND 10),
 reason_reference uuid NOT NULL CHECK(substring(reason_reference::text,15,1)='7' AND substring(reason_reference::text,20,1) IN ('8','9','a','b')),
 intent_sha256 text NOT NULL CHECK(intent_sha256 ~ '^[0-9a-f]{64}$'),
 created_by text NOT NULL CHECK(octet_length(created_by) BETWEEN 1 AND 512),
 created_at timestamptz NOT NULL,
 PRIMARY KEY(tenant_id,id),
 FOREIGN KEY(tenant_id,practitioner_id,registration_id,revision_id) REFERENCES practitioner_registration_revisions(tenant_id,practitioner_id,registration_id,id),
 FOREIGN KEY(tenant_id,active_review_id) REFERENCES practitioner_credential_reviews(tenant_id,id)
);
CREATE INDEX practitioner_reconciliation_page ON practitioner_registration_reconciliations(tenant_id,practitioner_id,registration_id,created_at DESC,id DESC);
CREATE INDEX practitioner_reconciliation_review ON practitioner_registration_reconciliations(tenant_id,registration_id,active_review_id);
ALTER TABLE practitioner_registration_reconciliations ENABLE ROW LEVEL SECURITY;
ALTER TABLE practitioner_registration_reconciliations FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON practitioner_registration_reconciliations
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.practitioner_access',true)='true')
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.practitioner_reconcile',true)='true' AND created_by=NULLIF(current_setting('app.actor_subject',true),''));
CREATE FUNCTION practitioner_reconciliation_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE registration practitioner_registrations; replacement practitioner_credential_reviews; adverse practitioner_credential_reviews; seal jsonb; evidence practitioner_credential_evidence; item text; previous text; author text; revision_author text;
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'reconciliation is immutable' USING ERRCODE='23514'; END IF;
 IF current_setting('app.practitioner_reconcile',true) IS DISTINCT FROM 'true' OR NEW.created_by IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'') THEN RAISE EXCEPTION 'reconciliation authority required' USING ERRCODE='42501'; END IF;
 SELECT * INTO registration FROM practitioner_registrations WHERE tenant_id=NEW.tenant_id AND practitioner_id=NEW.practitioner_id AND id=NEW.registration_id FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'registration unavailable' USING ERRCODE='P0002'; END IF;
 SELECT created_by INTO revision_author FROM practitioner_registration_revisions WHERE tenant_id=NEW.tenant_id AND practitioner_id=NEW.practitioner_id AND registration_id=NEW.registration_id AND id=NEW.revision_id AND version=registration.version;
 IF NOT FOUND THEN RAISE EXCEPTION 'reconciliation revision changed' USING ERRCODE='23514',CONSTRAINT='registration_verification_revision'; END IF;
 SELECT created_by INTO author FROM practitioner_records WHERE tenant_id=NEW.tenant_id AND id=NEW.practitioner_id;
 IF NEW.created_by IN (author,registration.created_by,revision_author) THEN RAISE EXCEPTION 'independent reconciler required' USING ERRCODE='23514',CONSTRAINT='registration_verification_independence'; END IF;
 SELECT * INTO replacement FROM practitioner_credential_reviews WHERE tenant_id=NEW.tenant_id AND practitioner_id=NEW.practitioner_id AND credential_kind='registration' AND credential_id=NEW.registration_id AND revision_id=NEW.revision_id AND id=NEW.active_review_id;
 IF NOT FOUND THEN RAISE EXCEPTION 'review unavailable' USING ERRCODE='P0002'; END IF;
 IF replacement.finding<>'active' OR replacement.source_checked_at>practitioner_registry_now() THEN RAISE EXCEPTION 'active matching review required' USING ERRCODE='23514',CONSTRAINT='registration_verification_review'; END IF;
 FOR seal IN SELECT value FROM jsonb_array_elements(replacement.evidence_snapshot) LOOP
  SELECT * INTO evidence FROM practitioner_credential_evidence WHERE tenant_id=NEW.tenant_id AND practitioner_id=NEW.practitioner_id AND credential_kind='registration' AND credential_id=NEW.registration_id AND revision_id=NEW.revision_id AND id=(seal->>'id')::uuid FOR SHARE;
  IF NOT FOUND OR evidence.status<>'available' OR evidence.created_by=NEW.created_by OR jsonb_build_object('id',evidence.id,'sha256',evidence.sha256,'size',evidence.size,'source_generation',evidence.source_generation,'released_generation',evidence.released_generation,'scan_attempt',evidence.scan_attempt,'scanned_at',evidence.scanned_at)<>seal THEN RAISE EXCEPTION 'replacement evidence unavailable' USING ERRCODE='23514',CONSTRAINT='registration_verification_evidence'; END IF;
 END LOOP;
 FOR item IN SELECT jsonb_array_elements_text(NEW.adverse_review_ids) ORDER BY 1 LOOP
  IF item=previous OR item::uuid::text<>item THEN RAISE EXCEPTION 'unique canonical adverse references required' USING ERRCODE='23514'; END IF;
  SELECT * INTO adverse FROM practitioner_credential_reviews WHERE tenant_id=NEW.tenant_id AND practitioner_id=NEW.practitioner_id AND credential_kind='registration' AND credential_id=NEW.registration_id AND id=item::uuid;
  IF NOT FOUND THEN RAISE EXCEPTION 'adverse review unavailable' USING ERRCODE='P0002'; END IF;
  IF adverse.finding='active' OR adverse.source_checked_at>=replacement.source_checked_at THEN RAISE EXCEPTION 'replacement source must be later than adverse observation' USING ERRCODE='23514',CONSTRAINT='registration_reconciliation_source'; END IF;
  previous:=item;
 END LOOP;
 NEW.adverse_review_ids:=(SELECT jsonb_agg(value ORDER BY value) FROM jsonb_array_elements(NEW.adverse_review_ids));
 NEW.created_at:=practitioner_registry_now();RETURN NEW;
END $$;
CREATE TRIGGER practitioner_reconciliation_guard BEFORE INSERT OR UPDATE OR DELETE ON practitioner_registration_reconciliations FOR EACH ROW EXECUTE FUNCTION practitioner_reconciliation_guard();
CREATE TRIGGER practitioner_reconciliation_audit AFTER INSERT ON practitioner_registration_reconciliations FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE FUNCTION practitioner_registration_unresolved(tenant uuid, registration uuid, active_review uuid) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT EXISTS(SELECT 1 FROM practitioner_credential_reviews c WHERE c.tenant_id=tenant AND c.credential_kind='registration' AND c.credential_id=registration AND c.finding<>'active' AND NOT EXISTS(SELECT 1 FROM practitioner_registration_reconciliations x WHERE x.tenant_id=c.tenant_id AND x.registration_id=c.credential_id AND x.active_review_id=active_review AND x.adverse_review_ids ? c.id::text))
$$;
