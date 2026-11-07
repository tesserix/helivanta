SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
ALTER TABLE workforce_hospitals
 DROP CONSTRAINT workforce_establishment_kind,
 ADD CONSTRAINT workforce_establishment_kind CHECK(establishment_kind IN('hospital','pharmacy','supplier')),
 DROP CONSTRAINT workforce_establishment_name,
 ADD CONSTRAINT workforce_establishment_name CHECK(length(btrim(name)) BETWEEN 1 AND CASE WHEN establishment_kind IN('pharmacy','supplier')THEN 200 ELSE 150 END),
 ADD COLUMN supplier_source_version bigint,
 ADD CONSTRAINT workforce_supplier_source CHECK((establishment_kind='supplier' AND supplier_source_version IS NOT NULL AND supplier_source_version BETWEEN 1 AND 9007199254740991)OR(establishment_kind<>'supplier' AND supplier_source_version IS NULL));
CREATE OR REPLACE FUNCTION workforce_establishment_identity_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.tenant_id IS DISTINCT FROM OLD.tenant_id OR NEW.id IS DISTINCT FROM OLD.id
 OR NEW.establishment_kind IS DISTINCT FROM OLD.establishment_kind
 OR NEW.pharmacy_ownership_type IS DISTINCT FROM OLD.pharmacy_ownership_type
 OR NEW.supplier_source_version IS DISTINCT FROM OLD.supplier_source_version
 OR (OLD.establishment_kind IN('pharmacy','supplier') AND NEW.name IS DISTINCT FROM OLD.name)THEN
  RAISE EXCEPTION 'workforce establishment identity is immutable' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
