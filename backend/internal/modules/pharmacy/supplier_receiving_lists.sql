SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE INDEX ON pharmacy_supplier_receipts(tenant_id,shipment_id,outlet_id,created_at DESC,id DESC);
CREATE INDEX ON pharmacy_supplier_receiving_corrections(tenant_id,shipment_id,outlet_id,receipt_id,created_at DESC,id DESC);
