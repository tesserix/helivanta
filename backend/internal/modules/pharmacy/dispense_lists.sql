-- #1295 D8: the Waiting and Dispensed tabs each page and count one status.
-- Partial indexes keep each tab's keyset scan and its total on its own rows
-- as dispensed history grows.
CREATE INDEX pharmacy_dispenses_waiting ON pharmacy_dispenses (tenant_id, created_at, id) WHERE status = 'pending';
CREATE INDEX pharmacy_dispenses_dispensed ON pharmacy_dispenses (tenant_id, dispensed_at, id) WHERE status = 'dispensed';
-- The Dispensed tab pages by dispensed_at, so a dispensed row must carry it.
-- Enforced here rather than trusted to every writer; an existing row that
-- breaks it fails this migration loudly instead of vanishing from the list.
ALTER TABLE pharmacy_dispenses ADD CONSTRAINT pharmacy_dispenses_dispensed_at
  CHECK (status <> 'dispensed' OR dispensed_at IS NOT NULL);
