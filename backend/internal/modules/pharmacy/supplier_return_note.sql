SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

-- Existing dispatches have no historical name snapshot; never backfill a guessed name.
ALTER TABLE pharmacy_supplier_return_dispatches
 ADD COLUMN note_pharmacy_name text,
 ADD COLUMN note_outlet_name text,
 ADD COLUMN note_outlet_code text;
ALTER TABLE pharmacy_supplier_return_dispatches ADD CONSTRAINT pharmacy_supplier_return_note_names CHECK(
 (note_pharmacy_name IS NULL AND note_outlet_name IS NULL AND note_outlet_code IS NULL) OR
 (note_pharmacy_name IS NOT NULL AND note_outlet_name IS NOT NULL AND note_outlet_code IS NOT NULL AND
  length(btrim(note_pharmacy_name)) BETWEEN 1 AND 200 AND length(btrim(note_outlet_name)) BETWEEN 1 AND 200 AND
  length(btrim(note_outlet_code)) BETWEEN 1 AND 40)
) NOT VALID;
ALTER TABLE pharmacy_supplier_return_dispatches VALIDATE CONSTRAINT pharmacy_supplier_return_note_names;

CREATE FUNCTION pharmacy_supplier_return_note_snapshot() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 SELECT p.name,o.name,o.code INTO NEW.note_pharmacy_name,NEW.note_outlet_name,NEW.note_outlet_code
 FROM pharmacy_outlets o JOIN pharmacy_organizations p ON p.tenant_id=o.tenant_id AND p.id=o.pharmacy_id
 WHERE o.tenant_id=NEW.tenant_id AND o.id=NEW.outlet_id;
 IF NEW.note_pharmacy_name IS NULL OR NEW.note_outlet_name IS NULL OR NEW.note_outlet_code IS NULL THEN
  RAISE EXCEPTION 'dispatch note requires current outlet identity' USING ERRCODE='23514',CONSTRAINT='pharmacy_supplier_return_note_source';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_supplier_return_note_snapshot BEFORE INSERT ON pharmacy_supplier_return_dispatches
 FOR EACH ROW EXECUTE FUNCTION pharmacy_supplier_return_note_snapshot();
