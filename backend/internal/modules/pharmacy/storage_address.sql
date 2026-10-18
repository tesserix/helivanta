SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE pharmacy_stock_locations
 ADD COLUMN zone_code text NOT NULL DEFAULT '',
 ADD COLUMN aisle_code text NOT NULL DEFAULT '',
 ADD COLUMN rack_code text NOT NULL DEFAULT '',
 ADD COLUMN shelf_code text NOT NULL DEFAULT '',
 ADD CONSTRAINT pharmacy_storage_zone CHECK (zone_code ~ '^(?:[A-Z0-9][A-Z0-9._-]{0,31})?$') NOT VALID,
 ADD CONSTRAINT pharmacy_storage_aisle CHECK (aisle_code ~ '^(?:[A-Z0-9][A-Z0-9._-]{0,31})?$') NOT VALID,
 ADD CONSTRAINT pharmacy_storage_rack CHECK (rack_code ~ '^(?:[A-Z0-9][A-Z0-9._-]{0,31})?$') NOT VALID,
 ADD CONSTRAINT pharmacy_storage_shelf CHECK (shelf_code ~ '^(?:[A-Z0-9][A-Z0-9._-]{0,31})?$') NOT VALID;
ALTER TABLE pharmacy_stock_locations VALIDATE CONSTRAINT pharmacy_storage_zone;
ALTER TABLE pharmacy_stock_locations VALIDATE CONSTRAINT pharmacy_storage_aisle;
ALTER TABLE pharmacy_stock_locations VALIDATE CONSTRAINT pharmacy_storage_rack;
ALTER TABLE pharmacy_stock_locations VALIDATE CONSTRAINT pharmacy_storage_shelf;
