SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- Refunds of unused deposits (#199 slice 2): cash out of a counter, as a
-- receipt with purpose refund, bounded by what is held, after a final bill,
-- and above the hospital's threshold only against someone else's approval.

ALTER TABLE billing_cash_receipts DROP CONSTRAINT billing_cash_receipts_purpose_check;
ALTER TABLE billing_cash_receipts ADD CONSTRAINT billing_cash_receipts_purpose_check CHECK (purpose IN ('payment','deposit','refund'));
ALTER TABLE billing_cash_receipts ADD COLUMN approval_id uuid;
ALTER TABLE billing_cash_receipts ADD CONSTRAINT billing_cash_receipt_approval_used UNIQUE (tenant_id,approval_id);
ALTER TABLE billing_cash_receipts ADD CONSTRAINT billing_cash_receipt_approval_refund CHECK (approval_id IS NULL OR purpose='refund');

-- The approval threshold, versioned: the latest applies; none is ₹10,000.
CREATE TABLE billing_refund_policies (
 tenant_id uuid NOT NULL,
 hospital_id uuid NOT NULL CHECK (hospital_id <> '00000000-0000-0000-0000-000000000000'::uuid),
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 threshold_minor bigint NOT NULL CHECK (threshold_minor BETWEEN 0 AND 1000000000),
 currency text NOT NULL CHECK (currency='INR'),
 created_by text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY (tenant_id,id)
);
CREATE INDEX billing_refund_policies_latest ON billing_refund_policies (tenant_id,hospital_id,created_at DESC,id DESC);

-- A supervisor's approval of one refund amount for one admission.
CREATE TABLE billing_refund_approvals (
 tenant_id uuid NOT NULL,
 hospital_id uuid NOT NULL,
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 admission_id uuid NOT NULL,
 amount_minor bigint NOT NULL CHECK (amount_minor BETWEEN 1 AND 1000000000),
 currency text NOT NULL CHECK (currency='INR'),
 reason text NOT NULL CHECK (reason=btrim(reason) AND length(reason) BETWEEN 1 AND 500),
 approved_by text NOT NULL,
 approved_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY (tenant_id,id),
 FOREIGN KEY (tenant_id,hospital_id,admission_id) REFERENCES billing_admission_accounts (tenant_id,hospital_id,admission_id)
);
CREATE INDEX billing_refund_approvals_admission ON billing_refund_approvals (tenant_id,admission_id,approved_at DESC,id DESC);
ALTER TABLE billing_cash_receipts ADD CONSTRAINT billing_cash_receipt_approval FOREIGN KEY (tenant_id,approval_id) REFERENCES billing_refund_approvals (tenant_id,id);

DO $$ DECLARE t text; BEGIN
 FOREACH t IN ARRAY ARRAY['billing_refund_policies','billing_refund_approvals'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
  EXECUTE format('CREATE POLICY tenant_isolation ON %I USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND billing_hospital_allowed(tenant_id,hospital_id)) WITH CHECK (tenant_id=NULLIF(current_setting(''app.tenant_id'',true),'''')::uuid AND billing_hospital_allowed(tenant_id,hospital_id))', t);
  EXECUTE format('CREATE TRIGGER %I AFTER INSERT ON %I FOR EACH ROW EXECUTE FUNCTION audit_record_change()', t||'_audit', t);
  EXECUTE format('CREATE TRIGGER %I BEFORE UPDATE OR DELETE OR TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION billing_history_retained()', t||'_kept', t);
 END LOOP;
END $$;
-- Each is recorded by the acting subject. One function per table: plpgsql
-- may evaluate both sides of an OR, and neither table has the other's column.
CREATE FUNCTION billing_refund_policy_by_actor() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.created_by IS DISTINCT FROM billing_actor() THEN RAISE EXCEPTION 'a refund policy is set by the acting subject' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
CREATE FUNCTION billing_refund_approval_by_actor() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.approved_by IS DISTINCT FROM billing_actor() THEN RAISE EXCEPTION 'a refund is approved by the acting subject' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER billing_refund_policies_actor BEFORE INSERT ON billing_refund_policies FOR EACH ROW EXECUTE FUNCTION billing_refund_policy_by_actor();
CREATE TRIGGER billing_refund_approvals_actor BEFORE INSERT ON billing_refund_approvals FOR EACH ROW EXECUTE FUNCTION billing_refund_approval_by_actor();

CREATE FUNCTION billing_refund_threshold(row_tenant uuid,row_hospital uuid) RETURNS bigint LANGUAGE sql STABLE AS $$
 SELECT COALESCE((SELECT threshold_minor FROM billing_refund_policies WHERE tenant_id=row_tenant AND hospital_id=row_hospital
  ORDER BY created_at DESC,id DESC LIMIT 1),1000000)
$$;

-- Held for the patient: deposits less applied less refunded.
CREATE FUNCTION billing_held(row_tenant uuid,row_admission uuid,row_currency text) RETURNS bigint LANGUAGE sql STABLE AS $$
 SELECT billing_received(row_tenant,row_admission,row_currency,'deposit') - billing_applied(row_tenant,row_admission,row_currency)
  - billing_received(row_tenant,row_admission,row_currency,'refund')
$$;

-- Expected cash nets refunds: they left the drawer.
CREATE OR REPLACE FUNCTION billing_shift_expected(row_tenant uuid,row_shift uuid) RETURNS bigint LANGUAGE sql STABLE AS $$
 SELECT s.opening_float_minor + COALESCE((SELECT sum(CASE r.purpose WHEN 'refund' THEN -r.amount_minor ELSE r.amount_minor END) FROM billing_cash_receipts r
  WHERE r.tenant_id=s.tenant_id AND r.shift_id=s.id AND r.voided_at IS NULL),0)
 FROM billing_cash_shifts s WHERE s.tenant_id=row_tenant AND s.id=row_shift
$$;

-- As in 0006, and: a refund needs a final bill, fits what is held and what
-- the drawer holds, and above the threshold uses an unused approval for the
-- same admission and amount by someone else; a deposit void leaves held at
-- zero or more.
CREATE OR REPLACE FUNCTION billing_receipt_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE s record; due bigint; a record;
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
  IF NEW.purpose='refund' THEN
   IF NOT EXISTS (SELECT 1 FROM billing_interim_bills b WHERE b.tenant_id=NEW.tenant_id AND b.admission_id=NEW.admission_id AND b.kind='final') THEN
    RAISE EXCEPTION 'a deposit is refunded after a final bill' USING ERRCODE='23514', CONSTRAINT='billing_refund_final_bill';
   END IF;
   IF NEW.amount_minor > billing_held(NEW.tenant_id,NEW.admission_id,NEW.currency) THEN
    RAISE EXCEPTION 'a refund is never more than what is held' USING ERRCODE='23514', CONSTRAINT='billing_refund_over_held';
   END IF;
   IF NEW.amount_minor > billing_shift_expected(NEW.tenant_id,NEW.shift_id) THEN
    RAISE EXCEPTION 'a refund is never more than the drawer holds' USING ERRCODE='23514', CONSTRAINT='billing_refund_drawer';
   END IF;
   IF NEW.amount_minor > billing_refund_threshold(NEW.tenant_id,NEW.hospital_id) OR NEW.approval_id IS NOT NULL THEN
    SELECT admission_id,amount_minor,currency,approved_by INTO a FROM billing_refund_approvals WHERE tenant_id=NEW.tenant_id AND id=NEW.approval_id;
    IF NOT FOUND OR a.admission_id<>NEW.admission_id OR a.amount_minor<>NEW.amount_minor OR a.currency<>NEW.currency OR a.approved_by=NEW.cashier THEN
     RAISE EXCEPTION 'a refund above the threshold needs someone else''s approval for that amount' USING ERRCODE='23514', CONSTRAINT='billing_refund_approval';
    END IF;
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
 IF (NEW.tenant_id,NEW.hospital_id,NEW.id,NEW.shift_id,NEW.counter_id,NEW.number,NEW.cashier,NEW.admission_id,NEW.amount_minor,NEW.currency,NEW.received_at,NEW.purpose,NEW.approval_id)
  IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.id,OLD.shift_id,OLD.counter_id,OLD.number,OLD.cashier,OLD.admission_id,OLD.amount_minor,OLD.currency,OLD.received_at,OLD.purpose,OLD.approval_id)
  OR OLD.voided_at IS NOT NULL OR NEW.voided_at IS NULL OR billing_actor() IS DISTINCT FROM OLD.cashier THEN
  RAISE EXCEPTION 'a receipt is voided once, by its cashier, and otherwise fixed' USING ERRCODE='23514';
 END IF;
 PERFORM 1 FROM billing_cash_shifts WHERE tenant_id=OLD.tenant_id AND id=OLD.shift_id AND status='open' FOR SHARE;
 IF NOT FOUND THEN
  RAISE EXCEPTION 'a receipt is voided only while its shift is open' USING ERRCODE='23514', CONSTRAINT='billing_cash_receipt_shift';
 END IF;
 IF OLD.purpose='deposit' THEN
  PERFORM pg_advisory_xact_lock(hashtextextended(OLD.tenant_id::text||':billing-bed-days:'||OLD.admission_id::text, 0));
  IF billing_held(OLD.tenant_id,OLD.admission_id,OLD.currency) - OLD.amount_minor < 0 THEN
   RAISE EXCEPTION 'a deposit already applied or refunded cannot be voided' USING ERRCODE='23514', CONSTRAINT='billing_cash_deposit_applied';
  END IF;
 END IF;
 RETURN NEW;
END $$;

-- Held, for applications and bills, now nets refunds too.
CREATE OR REPLACE FUNCTION billing_deposit_application_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE held bigint; due bigint;
BEGIN
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id::text||':billing-bed-days:'||NEW.admission_id::text, 0));
 IF NEW.applied_by IS DISTINCT FROM billing_actor()
  OR NOT EXISTS (SELECT 1 FROM billing_interim_bills b WHERE b.tenant_id=NEW.tenant_id AND b.admission_id=NEW.admission_id AND b.number=NEW.bill_number AND b.kind='final') THEN
  RAISE EXCEPTION 'deposits are applied against a final bill by the acting subject' USING ERRCODE='23514', CONSTRAINT='billing_deposit_final_bill';
 END IF;
 held := billing_held(NEW.tenant_id,NEW.admission_id,NEW.currency);
 due := billing_owed(NEW.tenant_id,NEW.admission_id,NEW.currency) - billing_received(NEW.tenant_id,NEW.admission_id,NEW.currency,'payment')
  - billing_applied(NEW.tenant_id,NEW.admission_id,NEW.currency);
 IF NEW.amount_minor > held OR NEW.amount_minor > due THEN
  RAISE EXCEPTION 'an application is never more than what is held or due' USING ERRCODE='23514', CONSTRAINT='billing_deposit_application_bound';
 END IF;
 RETURN NEW;
END $$;
CREATE OR REPLACE FUNCTION billing_bill_money() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 NEW.paid_minor := billing_received(NEW.tenant_id,NEW.admission_id,'INR','payment') + billing_applied(NEW.tenant_id,NEW.admission_id,'INR');
 NEW.deposit_held_minor := billing_held(NEW.tenant_id,NEW.admission_id,'INR');
 RETURN NEW;
END $$;
