SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Restore counter-sale design B5 for NEW sales. Existing invoices and their
-- replay remain unchanged. Available evidence is terminal and retained under
-- 0016_pharmacy, so a prescription citation keeps its released evidence.
CREATE FUNCTION pharmacy_sale_line_released_evidence() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.prescription_id IS NOT NULL AND NOT EXISTS (
  SELECT 1 FROM pharmacy_prescription_images i
   WHERE i.tenant_id=NEW.tenant_id AND i.outlet_id=NEW.outlet_id
    AND i.prescription_id=NEW.prescription_id AND i.status='available'
 ) THEN
  RAISE EXCEPTION 'a prescription sale needs a released prescription image' USING ERRCODE='23514',CONSTRAINT='pharmacy_sale_line_released_evidence';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_sale_line_released_evidence BEFORE INSERT ON pharmacy_sale_lines
 FOR EACH ROW EXECUTE FUNCTION pharmacy_sale_line_released_evidence();
