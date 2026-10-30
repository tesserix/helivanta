SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- A manual record of money already returned outside Helivanta, not a
-- provider refund request or confirmation. The captured payment stays paid.
CREATE TABLE pharmacy_unused_payment_refunds (
 tenant_id uuid NOT NULL,
 outlet_id uuid NOT NULL,
 payment_request_id uuid NOT NULL,
 id uuid GENERATED ALWAYS AS (payment_request_id) STORED NOT NULL,
 amount_minor bigint NOT NULL CHECK(amount_minor BETWEEN 1 AND 1000000000000),
 currency char(3) NOT NULL CHECK(currency ~ '^[A-Z]{3}$'),
 reference text NOT NULL CHECK(length(reference) BETWEEN 1 AND 64 AND reference=btrim(reference) AND reference !~ '[[:cntrl:]]'),
 reason text NOT NULL CHECK(length(reason) BETWEEN 5 AND 200 AND reason=btrim(reason) AND reason !~ '[[:cntrl:]]'),
 recorded_by text NOT NULL CHECK(length(recorded_by)>0),
 recorded_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,outlet_id,payment_request_id),
 UNIQUE(tenant_id,id),
 FOREIGN KEY(tenant_id,outlet_id,payment_request_id) REFERENCES pharmacy_payment_requests(tenant_id,outlet_id,id)
);
ALTER TABLE pharmacy_unused_payment_refunds ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_unused_payment_refunds FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_unused_payment_refunds
 USING(hms_tenant_visible(tenant_id) AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK(pharmacy_outlet_allowed(tenant_id,outlet_id,true));
CREATE TRIGGER pharmacy_unused_payment_refund_audit AFTER INSERT ON pharmacy_unused_payment_refunds
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_unused_payment_refund_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_unused_payment_refunds
 FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

CREATE FUNCTION pharmacy_unused_payment_refund_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE r record;
BEGIN
 -- Both consumption paths take this same lock, then read the other ledger
 -- in a separate statement. A waiting command sees the winner's commit.
 SELECT status,amount_minor,currency INTO r FROM pharmacy_payment_requests
  WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.payment_request_id FOR UPDATE;
 IF r.status IS DISTINCT FROM 'paid' OR r.amount_minor IS DISTINCT FROM NEW.amount_minor OR r.currency IS DISTINCT FROM NEW.currency
  OR EXISTS(SELECT 1 FROM pharmacy_sale_tenders WHERE tenant_id=NEW.tenant_id AND payment_request_id=NEW.payment_request_id) THEN
  RAISE EXCEPTION 'only a whole unused paid payment can have a refund recorded' USING ERRCODE='23514',CONSTRAINT='pharmacy_unused_payment_refund_available';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_unused_payment_refund_guard BEFORE INSERT ON pharmacy_unused_payment_refunds
 FOR EACH ROW EXECUTE FUNCTION pharmacy_unused_payment_refund_guard();

CREATE OR REPLACE FUNCTION pharmacy_sale_tender_paid() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE r record; sale_currency char(3);
BEGIN
 IF NEW.payment_request_id IS NULL THEN RETURN NEW; END IF;
 SELECT status,amount_minor,currency INTO r FROM pharmacy_payment_requests
  WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.payment_request_id FOR UPDATE;
 SELECT currency INTO sale_currency FROM pharmacy_sales WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND id=NEW.sale_id;
 IF r.status IS DISTINCT FROM 'paid' OR r.amount_minor IS DISTINCT FROM NEW.amount_minor OR r.currency IS DISTINCT FROM sale_currency
  OR EXISTS(SELECT 1 FROM pharmacy_unused_payment_refunds WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND payment_request_id=NEW.payment_request_id) THEN
  RAISE EXCEPTION 'a verified UPI tender must name an unused paid payment of its amount' USING ERRCODE='23514',CONSTRAINT='pharmacy_sale_tender_paid';
 END IF;
 RETURN NEW;
END $$;
