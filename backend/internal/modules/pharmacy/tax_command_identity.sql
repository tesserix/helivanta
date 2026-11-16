-- #1715: each tax setup command carries its client's UUIDv7 and a hash of
-- what it asked for, so a resend replays the row instead of adding one.
-- Rows recorded before this have no hash; their IDs were never given to a
-- client, and a NULL never matches, so none of them can be replayed.
ALTER TABLE pharmacy_outlet_tax_registrations ADD COLUMN intent_sha256 text;
ALTER TABLE pharmacy_product_tax_classifications ADD COLUMN intent_sha256 text;
