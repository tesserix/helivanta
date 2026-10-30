SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- Deposits (#199 slice 1): cash the patient leaves with the hospital, held as
-- theirs, not revenue, until applied against the account once a final bill is
-- issued. A deposit is a receipt in the counter's drawer and numbering.

ALTER TABLE billing_cash_receipts ADD COLUMN purpose text NOT NULL DEFAULT 'payment' CHECK (purpose IN ('payment','deposit'));

-- Deposits applied against the account, each with the final bill it was
-- applied against. Append-only.
CREATE TABLE billing_deposit_applications (
 tenant_id uuid NOT NULL,
 hospital_id uuid NOT NULL,
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 admission_id uuid NOT NULL,
 bill_number integer NOT NULL,
 amount_minor bigint NOT NULL CHECK (amount_minor >= 1),
 currency text NOT NULL CHECK (currency='INR'),
 applied_by text NOT NULL,
 applied_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY (tenant_id,id),
 FOREIGN KEY (tenant_id,hospital_id,admission_id,bill_number) REFERENCES billing_interim_bills (tenant_id,hospital_id,admission_id,number)
);
CREATE INDEX billing_deposit_applications_admission ON billing_deposit_applications (tenant_id,admission_id,applied_at DESC,id DESC);
ALTER TABLE billing_deposit_applications ENABLE ROW LEVEL SECURITY;
ALTER TABLE billing_deposit_applications FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON billing_deposit_applications
 USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND billing_hospital_allowed(tenant_id,hospital_id))
 WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND billing_hospital_allowed(tenant_id,hospital_id));
CREATE TRIGGER billing_deposit_applications_audit AFTER INSERT ON billing_deposit_applications FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER billing_deposit_applications_kept BEFORE UPDATE OR DELETE OR TRUNCATE ON billing_deposit_applications FOR EACH STATEMENT EXECUTE FUNCTION billing_history_retained();

-- The account's money in a currency: what it owes, what was paid, what is
-- held and what was applied. Balance due is owed less paid less applied.
CREATE FUNCTION billing_owed(row_tenant uuid,row_admission uuid,row_currency text) RETURNS bigint LANGUAGE sql STABLE AS $$
 SELECT COALESCE(sum(CASE kind WHEN 'charge' THEN amount_minor ELSE -amount_minor END),0) FROM billing_account_entries
 WHERE tenant_id=row_tenant AND admission_id=row_admission AND currency=row_currency
$$;
CREATE FUNCTION billing_received(row_tenant uuid,row_admission uuid,row_currency text,row_purpose text) RETURNS bigint LANGUAGE sql STABLE AS $$
 SELECT COALESCE(sum(amount_minor),0) FROM billing_cash_receipts
 WHERE tenant_id=row_tenant AND admission_id=row_admission AND currency=row_currency AND purpose=row_purpose AND voided_at IS NULL
$$;
CREATE FUNCTION billing_applied(row_tenant uuid,row_admission uuid,row_currency text) RETURNS bigint LANGUAGE sql STABLE AS $$
 SELECT COALESCE(sum(amount_minor),0) FROM billing_deposit_applications
 WHERE tenant_id=row_tenant AND admission_id=row_admission AND currency=row_currency
$$;

-- As in 0005, and: a payment is capped at the balance due net of applied
-- deposits; a deposit needs an open account and has no cap; a void never
-- leaves held deposits below what was applied.
CREATE OR REPLACE FUNCTION billing_receipt_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE s record; due bigint;
BEGIN
 IF TG_OP='INSERT' THEN
  SELECT counter_id,cashier,currency,status INTO s FROM billing_cash_shifts WHERE tenant_id=NEW.tenant_id AND hospital_id=NEW.hospital_id AND id=NEW.shift_id FOR SHARE;
  IF NOT FOUND OR s.status<>'open' OR s.cashier IS DISTINCT FROM billing_actor() OR NEW.cashier IS DISTINCT FROM s.cashier
   OR NEW.counter_id<>s.counter_id OR NEW.currency<>s.currency OR NEW.void_id IS NOT NULL THEN
   RAISE EXCEPTION 'a receipt is taken in its cashier''s open shift' USING ERRCODE='23514', CONSTRAINT='billing_cash_receipt_shift';
  END IF;
  PERFORM 1 FROM billing_counters WHERE tenant_id=NEW.tenant_id AND id=NEW.counter_id FOR UPDATE;
  IF NEW.number <> (SELECT COALESCE(max(number),0)+1 FROM billing_cash_receipts WHERE tenant_id=NEW.tenant_id AND counter_id=NEW.counter_id) THEN
   RAISE EXCEPTION 'receipts are numbered in order on their counter' USING ERRCODE='23514';
  END IF;
  PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id::text||':billing-bed-days:'||NEW.admission_id::text, 0));
  IF NEW.purpose='deposit' THEN
   PERFORM 1 FROM billing_admission_accounts WHERE tenant_id=NEW.tenant_id AND admission_id=NEW.admission_id AND status='open' FOR SHARE;
   IF NOT FOUND THEN
    RAISE EXCEPTION 'a deposit is taken on an open account' USING ERRCODE='23514', CONSTRAINT='billing_cash_deposit_open';
   END IF;
   RETURN NEW;
  END IF;
  due := billing_owed(NEW.tenant_id,NEW.admission_id,NEW.currency) - billing_received(NEW.tenant_id,NEW.admission_id,NEW.currency,'payment')
   - billing_applied(NEW.tenant_id,NEW.admission_id,NEW.currency);
  IF NEW.amount_minor > due THEN
   RAISE EXCEPTION 'a receipt is never more than the balance due' USING ERRCODE='23514', CONSTRAINT='billing_cash_receipt_balance';
  END IF;
  RETURN NEW;
 END IF;
 IF (NEW.tenant_id,NEW.hospital_id,NEW.id,NEW.shift_id,NEW.counter_id,NEW.number,NEW.cashier,NEW.admission_id,NEW.amount_minor,NEW.currency,NEW.received_at,NEW.purpose)
  IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.id,OLD.shift_id,OLD.counter_id,OLD.number,OLD.cashier,OLD.admission_id,OLD.amount_minor,OLD.currency,OLD.received_at,OLD.purpose)
  OR OLD.voided_at IS NOT NULL OR NEW.voided_at IS NULL OR billing_actor() IS DISTINCT FROM OLD.cashier THEN
  RAISE EXCEPTION 'a receipt is voided once, by its cashier, and otherwise fixed' USING ERRCODE='23514';
 END IF;
 PERFORM 1 FROM billing_cash_shifts WHERE tenant_id=OLD.tenant_id AND id=OLD.shift_id AND status='open' FOR SHARE;
 IF NOT FOUND THEN
  RAISE EXCEPTION 'a receipt is voided only while its shift is open' USING ERRCODE='23514', CONSTRAINT='billing_cash_receipt_shift';
 END IF;
 IF OLD.purpose='deposit' THEN
  PERFORM pg_advisory_xact_lock(hashtextextended(OLD.tenant_id::text||':billing-bed-days:'||OLD.admission_id::text, 0));
  IF billing_received(OLD.tenant_id,OLD.admission_id,OLD.currency,'deposit') - OLD.amount_minor < billing_applied(OLD.tenant_id,OLD.admission_id,OLD.currency) THEN
   RAISE EXCEPTION 'a deposit already applied cannot be voided' USING ERRCODE='23514', CONSTRAINT='billing_cash_deposit_applied';
  END IF;
 END IF;
 RETURN NEW;
END $$;

-- An application follows a final bill, by the acting subject, for no more
-- than what is held or what is due, under the admission lock.
CREATE FUNCTION billing_deposit_application_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE held bigint; due bigint;
BEGIN
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id::text||':billing-bed-days:'||NEW.admission_id::text, 0));
 IF NEW.applied_by IS DISTINCT FROM billing_actor()
  OR NOT EXISTS (SELECT 1 FROM billing_interim_bills b WHERE b.tenant_id=NEW.tenant_id AND b.admission_id=NEW.admission_id AND b.number=NEW.bill_number AND b.kind='final') THEN
  RAISE EXCEPTION 'deposits are applied against a final bill by the acting subject' USING ERRCODE='23514', CONSTRAINT='billing_deposit_final_bill';
 END IF;
 held := billing_received(NEW.tenant_id,NEW.admission_id,NEW.currency,'deposit') - billing_applied(NEW.tenant_id,NEW.admission_id,NEW.currency);
 due := billing_owed(NEW.tenant_id,NEW.admission_id,NEW.currency) - billing_received(NEW.tenant_id,NEW.admission_id,NEW.currency,'payment')
  - billing_applied(NEW.tenant_id,NEW.admission_id,NEW.currency);
 IF NEW.amount_minor > held OR NEW.amount_minor > due THEN
  RAISE EXCEPTION 'an application is never more than what is held or due' USING ERRCODE='23514', CONSTRAINT='billing_deposit_application_bound';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER billing_deposit_application_guard BEFORE INSERT ON billing_deposit_applications FOR EACH ROW EXECUTE FUNCTION billing_deposit_application_guard();

-- What was paid and held when a bill was issued, in INR, set here and never
-- supplied. Bills issued before this migration have none.
ALTER TABLE billing_interim_bills ADD COLUMN paid_minor bigint, ADD COLUMN deposit_held_minor bigint;
CREATE FUNCTION billing_bill_money() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 NEW.paid_minor := billing_received(NEW.tenant_id,NEW.admission_id,'INR','payment') + billing_applied(NEW.tenant_id,NEW.admission_id,'INR');
 NEW.deposit_held_minor := billing_received(NEW.tenant_id,NEW.admission_id,'INR','deposit') - billing_applied(NEW.tenant_id,NEW.admission_id,'INR');
 RETURN NEW;
END $$;
CREATE TRIGGER billing_bill_money BEFORE INSERT ON billing_interim_bills FOR EACH ROW EXECUTE FUNCTION billing_bill_money();
