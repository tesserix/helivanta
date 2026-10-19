SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

ALTER TABLE workforce_documents DROP CONSTRAINT workforce_documents_record_kind_check;
ALTER TABLE workforce_documents ADD CONSTRAINT workforce_documents_record_kind_check
 CHECK(record_kind IN('enrollment','credential','qualification','tax_declaration','benefit_proposal'));
ALTER TABLE workforce_documents DROP CONSTRAINT workforce_documents_category_check;
ALTER TABLE workforce_documents ADD CONSTRAINT workforce_documents_category_check
 CHECK(category IN('employment_evidence','qualification_evidence','credential_evidence','identity_evidence','tax_declaration_evidence','benefit_eligibility_evidence'));
ALTER TABLE workforce_documents DROP CONSTRAINT workforce_documents_record_binding;
ALTER TABLE workforce_documents ADD CONSTRAINT workforce_documents_record_binding
 CHECK((record_kind='enrollment' AND record_id=staff_id AND category IN('employment_evidence','identity_evidence'))
  OR(record_kind='credential' AND category='credential_evidence') OR(record_kind='qualification' AND category='qualification_evidence')
  OR(record_kind='tax_declaration' AND category='tax_declaration_evidence')
  OR(record_kind='benefit_proposal' AND category='benefit_eligibility_evidence'));
ALTER TABLE workforce_documents DROP CONSTRAINT workforce_documents_frozen_kind;
ALTER TABLE workforce_documents ADD CONSTRAINT workforce_documents_frozen_kind CHECK(NOT frozen OR record_kind IN('tax_declaration','benefit_proposal'));
CREATE INDEX workforce_documents_benefit_record ON workforce_documents(tenant_id,hospital_id,record_id) WHERE record_kind='benefit_proposal';

CREATE OR REPLACE FUNCTION workforce_document_creation() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE revision record; proposal record;
BEGIN
 IF NEW.status<>'upload_pending' OR NEW.uploaded_at IS NOT NULL OR NEW.source_generation IS NOT NULL OR NEW.released_generation IS NOT NULL
  OR NEW.engine_version IS NOT NULL OR NEW.definition_version IS NOT NULL OR NEW.definitions_updated_at IS NOT NULL OR NEW.scanned_at IS NOT NULL OR NEW.failure_code IS NOT NULL OR NEW.frozen THEN
  RAISE EXCEPTION 'document creation cannot carry a processing verdict' USING ERRCODE='42501';
 END IF;
 IF NEW.record_kind='tax_declaration' THEN
  -- The row lock serialises the count below with concurrent attaches and with the decision.
  SELECT r.status,r.sequence INTO revision FROM workforce_staff_tax_declaration_revisions r
   WHERE r.tenant_id=NEW.tenant_id AND r.hospital_id=NEW.hospital_id AND r.staff_id=NEW.staff_id AND r.id=NEW.record_id FOR NO KEY UPDATE;
  IF NOT FOUND OR revision.sequence<>NEW.record_version THEN
   RAISE EXCEPTION 'document source record binding is invalid' USING ERRCODE='23514';
  END IF;
  IF revision.status<>'pending' THEN
   RAISE EXCEPTION 'evidence attaches only to a pending declaration proposal' USING ERRCODE='23514',CONSTRAINT='workforce_tax_evidence_pending';
  END IF;
  IF (SELECT count(*) FROM workforce_documents d WHERE d.tenant_id=NEW.tenant_id AND d.hospital_id=NEW.hospital_id AND d.record_kind='tax_declaration'
    AND d.record_id=NEW.record_id AND NOT(d.status='upload_pending' AND d.expires_at<=clock_timestamp()))>=10 THEN
   RAISE EXCEPTION 'a declaration proposal takes at most 10 evidence files' USING ERRCODE='23514',CONSTRAINT='workforce_tax_evidence_limit';
  END IF;
  RETURN NEW;
 END IF;
 IF NEW.record_kind='benefit_proposal' THEN
  SELECT p.status,p.base_version INTO proposal FROM workforce_benefit_enrollment_proposals p
   WHERE p.tenant_id=NEW.tenant_id AND p.hospital_id=NEW.hospital_id AND p.staff_id=NEW.staff_id AND p.id=NEW.record_id FOR NO KEY UPDATE;
  IF NOT FOUND OR proposal.base_version+1<>NEW.record_version THEN
   RAISE EXCEPTION 'document source record binding is invalid' USING ERRCODE='23514';
  END IF;
  IF proposal.status<>'pending' THEN
   RAISE EXCEPTION 'evidence attaches only to a pending coverage proposal' USING ERRCODE='23514',CONSTRAINT='workforce_benefit_evidence_pending';
  END IF;
  IF (SELECT count(*) FROM workforce_documents d WHERE d.tenant_id=NEW.tenant_id AND d.hospital_id=NEW.hospital_id AND d.record_kind='benefit_proposal'
    AND d.record_id=NEW.record_id AND NOT(d.status='upload_pending' AND d.expires_at<=clock_timestamp()))>=10 THEN
   RAISE EXCEPTION 'a coverage proposal takes at most 10 evidence files' USING ERRCODE='23514',CONSTRAINT='workforce_benefit_evidence_limit';
  END IF;
  RETURN NEW;
 END IF;
 IF NOT EXISTS(SELECT 1 FROM workforce_staff s WHERE s.tenant_id=NEW.tenant_id AND s.hospital_id=NEW.hospital_id AND s.id=NEW.staff_id AND s.version=NEW.record_version)
  OR(NEW.record_kind='credential' AND NOT EXISTS(SELECT 1 FROM workforce_enrollment_credentials r WHERE r.tenant_id=NEW.tenant_id AND r.hospital_id=NEW.hospital_id AND r.staff_id=NEW.staff_id AND r.id=NEW.record_id))
  OR(NEW.record_kind='qualification' AND NOT EXISTS(SELECT 1 FROM workforce_enrollment_qualifications r WHERE r.tenant_id=NEW.tenant_id AND r.hospital_id=NEW.hospital_id AND r.staff_id=NEW.staff_id AND r.id=NEW.record_id)) THEN
  RAISE EXCEPTION 'document source record binding is invalid' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;

-- A decided proposal's evidence is frozen: it never unfreezes and never re-enters scanning, whoever asks.
CREATE FUNCTION workforce_benefit_evidence_frozen() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (OLD.frozen AND NOT NEW.frozen) OR (NEW.frozen AND NEW.status='scan_pending' AND OLD.status<>'scan_pending') THEN
  RAISE EXCEPTION 'evidence of a decided coverage proposal cannot change' USING ERRCODE='23514',CONSTRAINT='workforce_benefit_evidence_frozen';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_benefit_evidence_frozen BEFORE UPDATE ON workforce_documents FOR EACH ROW
 WHEN(OLD.record_kind='benefit_proposal') EXECUTE FUNCTION workforce_benefit_evidence_frozen();

-- Proposal updates pass RLS only with the benefits write flag, which also makes every evidence row visible here.
CREATE FUNCTION workforce_benefit_evidence_decision() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM workforce_documents d WHERE d.tenant_id=NEW.tenant_id AND d.hospital_id=NEW.hospital_id AND d.record_kind='benefit_proposal' AND d.record_id=NEW.id
   AND d.status='scan_pending') THEN
  RAISE EXCEPTION 'coverage evidence is still being scanned' USING ERRCODE='23514',CONSTRAINT='workforce_benefit_evidence_scanning';
 END IF;
 IF EXISTS(SELECT 1 FROM workforce_documents d WHERE d.tenant_id=NEW.tenant_id AND d.hospital_id=NEW.hospital_id AND d.record_kind='benefit_proposal' AND d.record_id=NEW.id
   AND (NOT d.frozen OR (NEW.status<>'approved' AND d.status='available'))) THEN
  RAISE EXCEPTION 'coverage evidence must be frozen, and revoked unless approved, before the decision' USING ERRCODE='23514',CONSTRAINT='workforce_benefit_evidence_unsettled';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_benefit_evidence_decision BEFORE UPDATE ON workforce_benefit_enrollment_proposals FOR EACH ROW
 WHEN(OLD.status='pending' AND NEW.status<>'pending') EXECUTE FUNCTION workforce_benefit_evidence_decision();

-- Benefits evidence is visible only inside a benefits transaction, never to HR document or payroll scopes.
ALTER POLICY document_isolation ON workforce_documents
 USING(hms_tenant_visible(tenant_id) AND CASE WHEN current_user='helivanta_document_worker'
  THEN workforce_document_worker_allowed(tenant_id,hospital_id,staff_id,id)
  WHEN record_kind='tax_declaration' THEN workforce_payroll_evidence_allowed(tenant_id,hospital_id,false)
  WHEN record_kind='benefit_proposal' THEN workforce_benefits_allowed(tenant_id,hospital_id,false)
  ELSE workforce_enrollment_allowed(tenant_id,hospital_id) END)
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND CASE WHEN current_user='helivanta_document_worker'
  THEN workforce_document_worker_allowed(tenant_id,hospital_id,staff_id,id)
  WHEN record_kind='tax_declaration' THEN workforce_payroll_evidence_allowed(tenant_id,hospital_id,true)
  WHEN record_kind='benefit_proposal' THEN workforce_benefits_allowed(tenant_id,hospital_id,true)
  ELSE workforce_enrollment_allowed(tenant_id,hospital_id) END);
