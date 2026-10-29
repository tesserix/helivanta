SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- A sale may name a registered patient (#1386 B-2c): the two ids only,
-- never with a walk-in buyer, and only a real registration.
ALTER TABLE pharmacy_sales
 ADD COLUMN patient_id uuid,
 ADD COLUMN patient_hospital_id uuid,
 ADD CONSTRAINT pharmacy_sale_patient_pair CHECK ((patient_id IS NULL) = (patient_hospital_id IS NULL)),
 ADD CONSTRAINT pharmacy_sale_patient_or_buyer CHECK (patient_id IS NULL OR (buyer_name IS NULL AND buyer_phone IS NULL)),
 ADD CONSTRAINT pharmacy_sale_registered_patient FOREIGN KEY (tenant_id,patient_id,patient_hospital_id)
  REFERENCES pharmacy_registered_patients (tenant_id,patient_id,hospital_id);

-- The outlet's pharmacy must hold a link with the patient's hospital whose
-- latest verification step is an acceptance. Enforced here, not only in Go,
-- so no code path can post a sale to an unverified hospital's patient.
CREATE FUNCTION pharmacy_sale_patient_affiliated() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.patient_id IS NULL THEN RETURN NEW; END IF;
 IF NOT EXISTS (
  SELECT 1 FROM pharmacy_outlets o JOIN pharmacy_hospital_affiliations a ON a.tenant_id=o.tenant_id AND a.pharmacy_id=o.pharmacy_id
  WHERE o.tenant_id=NEW.tenant_id AND o.id=NEW.outlet_id AND a.hospital_id=NEW.patient_hospital_id
   AND (SELECT v.kind FROM pharmacy_affiliation_verifications v WHERE v.tenant_id=a.tenant_id AND v.affiliation_id=a.id ORDER BY v.sequence DESC LIMIT 1)='accepted'
 ) THEN
  RAISE EXCEPTION 'the patient''s hospital has not verified this pharmacy' USING ERRCODE='23514', CONSTRAINT='pharmacy_sale_patient_affiliated';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_sale_patient_affiliated BEFORE INSERT ON pharmacy_sales FOR EACH ROW EXECUTE FUNCTION pharmacy_sale_patient_affiliated();
