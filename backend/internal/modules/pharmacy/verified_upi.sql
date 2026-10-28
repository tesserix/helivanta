-- A UPI tender the system saw settle (#1386 C-4b): it names a pharmacy
-- payment request that payments reported paid. The reference-only `upi`
-- tender stays for outlets that take UPI on their own static QR.
ALTER TABLE pharmacy_sale_tenders ADD COLUMN payment_request_id uuid;
ALTER TABLE pharmacy_sale_tenders DROP CONSTRAINT pharmacy_sale_tenders_kind_check;
ALTER TABLE pharmacy_sale_tenders DROP CONSTRAINT pharmacy_sale_tenders_check;
ALTER TABLE pharmacy_sale_tenders ADD CONSTRAINT pharmacy_sale_tenders_kind_check
 CHECK (kind IN ('cash','upi','card','upi_verified'));
ALTER TABLE pharmacy_sale_tenders ADD CONSTRAINT pharmacy_sale_tenders_check CHECK (CASE kind
 WHEN 'cash' THEN reference IS NULL AND payment_request_id IS NULL AND tendered_minor IS NOT NULL AND tendered_minor >= amount_minor AND change_minor = tendered_minor - amount_minor
 WHEN 'upi_verified' THEN reference IS NULL AND tendered_minor IS NULL AND change_minor IS NULL AND payment_request_id IS NOT NULL
 ELSE payment_request_id IS NULL AND tendered_minor IS NULL AND change_minor IS NULL AND reference IS NOT NULL AND length(btrim(reference)) BETWEEN 1 AND 64 END);
-- The same outlet, structurally; and one sale per payment.
ALTER TABLE pharmacy_sale_tenders ADD CONSTRAINT pharmacy_sale_tender_payment_request
 FOREIGN KEY (tenant_id,outlet_id,payment_request_id) REFERENCES pharmacy_payment_requests (tenant_id,outlet_id,id);
CREATE UNIQUE INDEX pharmacy_sale_tender_payment_once ON pharmacy_sale_tenders (tenant_id,payment_request_id) WHERE payment_request_id IS NOT NULL;

-- Only money payments reported paid, for exactly this tender's amount and
-- the sale's currency. Paid is terminal (guarded below), so the check made
-- at insert cannot go stale.
CREATE FUNCTION pharmacy_sale_tender_paid() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.payment_request_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM pharmacy_payment_requests r JOIN pharmacy_sales s
    ON s.tenant_id=r.tenant_id AND s.outlet_id=r.outlet_id AND s.id=NEW.sale_id
    WHERE r.tenant_id=NEW.tenant_id AND r.outlet_id=NEW.outlet_id AND r.id=NEW.payment_request_id
    AND r.status='paid' AND r.amount_minor=NEW.amount_minor AND r.currency=s.currency) THEN
  RAISE EXCEPTION 'a verified UPI tender names a paid payment for its amount' USING ERRCODE='23514',CONSTRAINT='pharmacy_sale_tender_paid';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_sale_tender_paid BEFORE INSERT ON pharmacy_sale_tenders
 FOR EACH ROW EXECUTE FUNCTION pharmacy_sale_tender_paid();

-- A request keeps what it was asked for, and paid is final: a verified UPI
-- tender relies on both, checked once at insert.
CREATE FUNCTION pharmacy_payment_request_settled() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.status = 'paid' OR NEW.tenant_id <> OLD.tenant_id OR NEW.outlet_id <> OLD.outlet_id OR NEW.purpose <> OLD.purpose
    OR NEW.amount_minor <> OLD.amount_minor OR NEW.currency <> OLD.currency THEN
  RAISE EXCEPTION 'a paid payment request, and every request''s amount, never change' USING ERRCODE='23514',CONSTRAINT='pharmacy_payment_request_settled';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_payment_request_settled BEFORE UPDATE ON pharmacy_payment_requests
 FOR EACH ROW EXECUTE FUNCTION pharmacy_payment_request_settled();
