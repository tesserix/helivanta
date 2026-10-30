SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- A neonatal intensive care bed (#193): GST exempts a room in an ICU, CCU,
-- ICCU or NICU (04/2022-CT(Rate)), so a NICU bed is its own class, apart from
-- a newborn ward (neonatal).
ALTER TABLE medicore_beds DROP CONSTRAINT medicore_beds_bed_class_check;
ALTER TABLE medicore_beds ADD CONSTRAINT medicore_beds_bed_class_check
 CHECK (bed_class IN ('general','semi_private','private','icu','hdu','isolation','neonatal','nicu','day_care'));
