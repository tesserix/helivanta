ALTER TABLE pharmacy_catalogue_products ADD COLUMN drug_master jsonb;
ALTER TABLE pharmacy_catalogue_products ADD CONSTRAINT pharmacy_drug_master_reference CHECK (
 drug_master IS NULL OR (jsonb_typeof(drug_master)='object'
 AND drug_master ?& ARRAY['product_id','version_id','formulation_id','formulation_version_id','release_id']
 AND (drug_master->>'product_id')::uuid <> '00000000-0000-0000-0000-000000000000'
 AND (drug_master->>'version_id')::uuid <> '00000000-0000-0000-0000-000000000000')
);
CREATE UNIQUE INDEX pharmacy_catalogue_master_product ON pharmacy_catalogue_products
 (tenant_id,outlet_id,(drug_master->>'version_id'),(drug_master->>'formulation_version_id')) WHERE drug_master IS NOT NULL;

CREATE FUNCTION pharmacy_drug_master_identity_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.drug_master IS NOT NULL AND (
  NEW.drug_master IS DISTINCT FROM OLD.drug_master OR
  (NEW.brand_name,NEW.generic_name,NEW.dosage_form,NEW.route,NEW.manufacturer,
   NEW.marketing_company,NEW.ingredients,NEW.pack_size,NEW.unit_of_issue)
  IS DISTINCT FROM
  (OLD.brand_name,OLD.generic_name,OLD.dosage_form,OLD.route,OLD.manufacturer,
   OLD.marketing_company,OLD.ingredients,OLD.pack_size,OLD.unit_of_issue)
 ) THEN
  RAISE EXCEPTION 'linked medicine identity is immutable' USING ERRCODE='23514',CONSTRAINT='pharmacy_drug_master_identity';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_drug_master_identity BEFORE UPDATE ON pharmacy_catalogue_products
 FOR EACH ROW EXECUTE FUNCTION pharmacy_drug_master_identity_guard();
