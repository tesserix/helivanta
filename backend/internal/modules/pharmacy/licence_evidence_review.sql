SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Existing immutable decisions retain NULL evidence references. New decisions
-- pin the exact released file after owner scope and independence checks.
ALTER TABLE pharmacy_outlet_licence_reviews
 ADD COLUMN evidence_file_id uuid,
 ADD COLUMN evidence_generation bigint CHECK(evidence_generation>0),
 ADD CONSTRAINT pharmacy_licence_review_evidence_pair CHECK((evidence_file_id IS NULL)=(evidence_generation IS NULL)),
 ADD CONSTRAINT pharmacy_licence_review_evidence_fk FOREIGN KEY(tenant_id,outlet_id,licence_id,evidence_file_id)
 REFERENCES pharmacy_licence_evidence(tenant_id,outlet_id,licence_id,id);

CREATE OR REPLACE FUNCTION pharmacy_outlet_licence_review_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE author text;
BEGIN
 SELECT entered_by INTO author FROM pharmacy_outlet_licences
 WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.licence_id FOR UPDATE;
 IF NOT FOUND THEN RAISE EXCEPTION 'licence declaration unavailable' USING ERRCODE='23503'; END IF;
 IF lower(btrim(author))=lower(btrim(NEW.reviewed_by)) THEN
  RAISE EXCEPTION 'a licence is reviewed by someone other than who entered it' USING ERRCODE='23514',CONSTRAINT='pharmacy_licence_four_eyes';
 END IF;
 -- Preserve the existing ON CONFLICT receipt behavior for completed decisions.
 IF EXISTS(SELECT 1 FROM pharmacy_outlet_licence_reviews WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND licence_id=NEW.licence_id) THEN RETURN NEW; END IF;
 SELECT id,released_generation INTO NEW.evidence_file_id,NEW.evidence_generation
 FROM pharmacy_licence_evidence WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND licence_id=NEW.licence_id
 AND status='available' AND released_generation IS NOT NULL;
 IF NOT FOUND THEN
  RAISE EXCEPTION 'clean licence evidence required' USING ERRCODE='23514',CONSTRAINT='pharmacy_licence_evidence_required';
 END IF;
 RETURN NEW;
END $$;
