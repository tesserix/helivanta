-- #1295 D8: the Waiting and Dispensed tabs each page and count one status.
-- Partial indexes keep each tab's keyset scan and its total on its own rows
-- as dispensed history grows.
CREATE INDEX pharmacy_dispenses_waiting ON pharmacy_dispenses (tenant_id, created_at, id) WHERE status = 'pending';
CREATE INDEX pharmacy_dispenses_dispensed ON pharmacy_dispenses (tenant_id, dispensed_at, id) WHERE status = 'dispensed';
