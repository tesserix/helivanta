SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- UPI at the billing desk (#198 slice 3, #211): a cashier asks payments for a
-- UPI QR on an admission's account; when payments reports the money captured,
-- the request's receipt is written here, numbered in the counter's series.
-- The money state is the payments module's; this row is billing's record of
-- asking and of what it was told.

CREATE TABLE billing_upi_requests (
 tenant_id uuid NOT NULL,
 hospital_id uuid NOT NULL,
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 shift_id uuid NOT NULL,
 counter_id uuid NOT NULL,
 cashier text NOT NULL,
 admission_id uuid NOT NULL,
 purpose text NOT NULL CHECK (purpose IN ('payment','deposit')),
 amount_minor bigint NOT NULL CHECK (amount_minor BETWEEN 1 AND 1000000000),
 currency text NOT NULL CHECK (currency='INR'),
 status text NOT NULL DEFAULT 'requested'
  CHECK (status IN ('requested','awaiting_payment','cancelling','paid','failed','expired','cancelled')),
 intent_id uuid,
 qr_image_url text CHECK (qr_image_url IS NULL OR qr_image_url ~ '^https://'),
 expires_at timestamptz,
 failure_reason text CHECK (failure_reason IN ('provider_not_configured','provider_unavailable','provider_rejected')),
 provider_payment_id text,
 rrn text,
 paid_at timestamptz,
 late boolean NOT NULL DEFAULT false,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY (tenant_id,id),
 FOREIGN KEY (tenant_id,hospital_id,shift_id) REFERENCES billing_cash_shifts (tenant_id,hospital_id,id),
 FOREIGN KEY (tenant_id,hospital_id,admission_id) REFERENCES billing_admission_accounts (tenant_id,hospital_id,admission_id),
 CHECK ((status='paid') = (provider_payment_id IS NOT NULL AND paid_at IS NOT NULL)),
 CHECK ((status='failed') = (failure_reason IS NOT NULL)),
 CHECK (NOT late OR status='paid')
);
CREATE INDEX billing_upi_requests_admission ON billing_upi_requests (tenant_id,admission_id,created_at DESC,id DESC);
CREATE INDEX billing_upi_requests_shift ON billing_upi_requests (tenant_id,shift_id) WHERE status IN ('requested','awaiting_payment','cancelling');

-- Billing's outcome consumers are billing consumers: they move requests and
-- write the paid request's receipt.
CREATE OR REPLACE FUNCTION billing_consumer() RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT current_setting('app.actor_subject',true) IN ('system:billing-bed-occupancy-started','system:billing-admission-released',
  'system:billing-pharmacy-admission-charge','system:billing-pharmacy-admission-reversal',
  'system:billing-bed-occupancy-ended','system:billing-bed-census',
  'system:billing-discharge-ordered','system:billing-discharge-withdrawn',
  'system:billing-upi-opened','system:billing-upi-failed','system:billing-upi-paid','system:billing-upi-expired','system:billing-upi-cancelled')
$$;

ALTER TABLE billing_upi_requests ENABLE ROW LEVEL SECURITY;
ALTER TABLE billing_upi_requests FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON billing_upi_requests
 USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND (billing_consumer() OR billing_hospital_allowed(tenant_id,hospital_id)))
 WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND (billing_consumer() OR billing_hospital_allowed(tenant_id,hospital_id)));
CREATE TRIGGER billing_upi_requests_audit AFTER INSERT OR UPDATE ON billing_upi_requests FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER billing_upi_requests_kept BEFORE DELETE OR TRUNCATE ON billing_upi_requests FOR EACH STATEMENT EXECUTE FUNCTION billing_history_retained();

-- The paid consumer writes a receipt in the requesting shift at its counter,
-- with no hospital grant: it reads the shift and locks the counter.
ALTER POLICY tenant_isolation ON billing_counters
 USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND (billing_consumer() OR billing_hospital_allowed(tenant_id,hospital_id)));
ALTER POLICY tenant_isolation ON billing_cash_shifts
 USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND (billing_consumer() OR billing_hospital_allowed(tenant_id,hospital_id)));
ALTER POLICY tenant_isolation ON billing_cash_receipts
 WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND (billing_consumer() OR billing_hospital_allowed(tenant_id,hospital_id)));

-- How a receipt was paid. A UPI receipt names the paid request it records,
-- once; a cash receipt names none. Refunds are cash.
ALTER TABLE billing_cash_receipts ADD COLUMN mode text NOT NULL DEFAULT 'cash' CHECK (mode IN ('cash','upi'));
ALTER TABLE billing_cash_receipts ADD COLUMN upi_request_id uuid;
ALTER TABLE billing_cash_receipts ADD CONSTRAINT billing_cash_receipt_upi_request FOREIGN KEY (tenant_id,upi_request_id) REFERENCES billing_upi_requests (tenant_id,id);
ALTER TABLE billing_cash_receipts ADD CONSTRAINT billing_cash_receipt_upi_once UNIQUE (tenant_id,upi_request_id);
ALTER TABLE billing_cash_receipts ADD CONSTRAINT billing_cash_receipt_upi_mode CHECK ((mode='upi') = (upi_request_id IS NOT NULL) AND (mode='cash' OR purpose IN ('payment','deposit')));

-- UPI payments still waiting on an answer hold their amount against the
-- balance due, so the same rupees are not taken twice. A request unanswered
-- 15 minutes after it was asked holds nothing: payments fails one with no QR
-- at 45 seconds and decides an open QR at its 300-second expiry, so by then
-- the event path is down and the dead-letter alert is the control.
CREATE FUNCTION billing_upi_pending(row_status text,row_created timestamptz) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT row_status IN ('requested','awaiting_payment','cancelling') AND row_created > now() - interval '15 minutes'
$$;
CREATE FUNCTION billing_upi_waiting(row_tenant uuid,row_admission uuid,row_currency text) RETURNS bigint LANGUAGE sql STABLE AS $$
 SELECT COALESCE(sum(amount_minor),0) FROM billing_upi_requests
 WHERE tenant_id=row_tenant AND admission_id=row_admission AND currency=row_currency AND purpose='payment' AND billing_upi_pending(status,created_at)
$$;
-- What a new payment may still take: owed less paid, applied and waiting.
CREATE FUNCTION billing_payable(row_tenant uuid,row_admission uuid,row_currency text) RETURNS bigint LANGUAGE sql STABLE AS $$
 SELECT billing_owed(row_tenant,row_admission,row_currency) - billing_received(row_tenant,row_admission,row_currency,'payment')
  - billing_applied(row_tenant,row_admission,row_currency) - billing_upi_waiting(row_tenant,row_admission,row_currency)
$$;

-- A paid request's receipt takes the request's id, so a request id is never
-- a cash receipt's and the other way round. Both inserts take this id's lock
-- first, so two clients reusing one id are one after the other.
CREATE FUNCTION billing_receipt_id_free(row_tenant uuid,row_id uuid,taken text) RETURNS void LANGUAGE plpgsql AS $$
BEGIN
 PERFORM pg_advisory_xact_lock(hashtextextended(row_tenant::text||':billing-receipt-id:'||row_id::text, 0));
 IF EXISTS (SELECT 1 FROM billing_upi_requests WHERE tenant_id=row_tenant AND id=row_id)
  OR EXISTS (SELECT 1 FROM billing_cash_receipts WHERE tenant_id=row_tenant AND id=row_id) THEN
  RAISE EXCEPTION 'this id is already used' USING ERRCODE='23505', CONSTRAINT=taken;
 END IF;
END $$;

-- A request is asked by the acting cashier in their own open shift, for no
-- more than is payable (a payment) or on an open account (a deposit). Its
-- identity never changes. The cashier who asked may cancel it while it is
-- open; every other move is an outcome consumer's, along the lifecycle, and
-- paid is final.
CREATE FUNCTION billing_upi_request_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE s record;
BEGIN
 IF TG_OP='INSERT' THEN
  SELECT counter_id,cashier,status INTO s FROM billing_cash_shifts WHERE tenant_id=NEW.tenant_id AND hospital_id=NEW.hospital_id AND id=NEW.shift_id FOR SHARE;
  IF NOT FOUND OR s.status<>'open' OR s.cashier IS DISTINCT FROM billing_actor() OR NEW.cashier IS DISTINCT FROM s.cashier OR NEW.counter_id<>s.counter_id
   OR NEW.status<>'requested' OR num_nonnulls(NEW.intent_id,NEW.qr_image_url,NEW.expires_at,NEW.failure_reason,NEW.provider_payment_id,NEW.rrn,NEW.paid_at)<>0 OR NEW.late THEN
   RAISE EXCEPTION 'a UPI request is asked in its cashier''s open shift' USING ERRCODE='23514', CONSTRAINT='billing_cash_receipt_shift';
  END IF;
  PERFORM billing_receipt_id_free(NEW.tenant_id,NEW.id,'billing_upi_requests_pkey');
  PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id::text||':billing-bed-days:'||NEW.admission_id::text, 0));
  IF NEW.purpose='deposit' THEN
   PERFORM 1 FROM billing_admission_accounts WHERE tenant_id=NEW.tenant_id AND admission_id=NEW.admission_id AND status='open' FOR SHARE;
   IF NOT FOUND THEN
    RAISE EXCEPTION 'a deposit is taken on an open account' USING ERRCODE='23514', CONSTRAINT='billing_cash_deposit_open';
   END IF;
  ELSIF NEW.amount_minor > billing_payable(NEW.tenant_id,NEW.admission_id,NEW.currency) THEN
   RAISE EXCEPTION 'a UPI payment is never more than the balance due' USING ERRCODE='23514', CONSTRAINT='billing_cash_receipt_balance';
  END IF;
  RETURN NEW;
 END IF;
 IF (NEW.tenant_id,NEW.hospital_id,NEW.id,NEW.shift_id,NEW.counter_id,NEW.cashier,NEW.admission_id,NEW.purpose,NEW.amount_minor,NEW.currency,NEW.created_at)
  IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.id,OLD.shift_id,OLD.counter_id,OLD.cashier,OLD.admission_id,OLD.purpose,OLD.amount_minor,OLD.currency,OLD.created_at)
  OR OLD.status='paid' THEN
  RAISE EXCEPTION 'a UPI request keeps what it asked for, and paid is final' USING ERRCODE='23514', CONSTRAINT='billing_upi_request_fixed';
 END IF;
 IF billing_consumer() THEN
  IF NEW.status=OLD.status OR NEW.status='paid'
   OR (NEW.status='awaiting_payment' AND OLD.status='requested')
   OR (NEW.status='failed' AND OLD.status IN ('requested','cancelling'))
   OR (NEW.status IN ('expired','cancelled') AND OLD.status IN ('requested','awaiting_payment','cancelling')) THEN
   RETURN NEW;
  END IF;
 ELSIF NEW.status='cancelling' AND OLD.status IN ('requested','awaiting_payment') AND billing_actor() IS NOT DISTINCT FROM OLD.cashier
  AND (NEW.intent_id,NEW.qr_image_url,NEW.expires_at,NEW.failure_reason,NEW.provider_payment_id,NEW.rrn,NEW.paid_at,NEW.late)
   IS NOT DISTINCT FROM (OLD.intent_id,OLD.qr_image_url,OLD.expires_at,OLD.failure_reason,OLD.provider_payment_id,OLD.rrn,OLD.paid_at,OLD.late) THEN
  RETURN NEW;
 END IF;
 RAISE EXCEPTION 'a UPI request moves only along its lifecycle' USING ERRCODE='23514', CONSTRAINT='billing_upi_request_fixed';
END $$;
CREATE TRIGGER billing_upi_request_guard BEFORE INSERT OR UPDATE ON billing_upi_requests FOR EACH ROW EXECUTE FUNCTION billing_upi_request_guard();

-- Paid makes the receipt, in the same transaction: in the shift that asked,
-- numbered next on its counter, whether or not the shift is still open. The
-- receipt's id is the request's, so one payment is one receipt.
CREATE FUNCTION billing_upi_paid() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM 1 FROM billing_counters WHERE tenant_id=NEW.tenant_id AND id=NEW.counter_id FOR UPDATE;
 INSERT INTO billing_cash_receipts (tenant_id,hospital_id,id,shift_id,counter_id,number,cashier,admission_id,purpose,amount_minor,currency,mode,upi_request_id)
 VALUES (NEW.tenant_id,NEW.hospital_id,NEW.id,NEW.shift_id,NEW.counter_id,
  (SELECT COALESCE(max(number),0)+1 FROM billing_cash_receipts WHERE tenant_id=NEW.tenant_id AND counter_id=NEW.counter_id),
  NEW.cashier,NEW.admission_id,NEW.purpose,NEW.amount_minor,NEW.currency,'upi',NEW.id);
 RETURN NULL;
END $$;
CREATE TRIGGER billing_upi_paid AFTER UPDATE ON billing_upi_requests FOR EACH ROW
 WHEN (NEW.status='paid' AND OLD.status<>'paid') EXECUTE FUNCTION billing_upi_paid();

-- Expected cash counts cash: UPI never enters the drawer.
CREATE OR REPLACE FUNCTION billing_shift_expected(row_tenant uuid,row_shift uuid) RETURNS bigint LANGUAGE sql STABLE AS $$
 SELECT s.opening_float_minor + COALESCE((SELECT sum(CASE r.purpose WHEN 'refund' THEN -r.amount_minor ELSE r.amount_minor END) FROM billing_cash_receipts r
  WHERE r.tenant_id=s.tenant_id AND r.shift_id=s.id AND r.voided_at IS NULL AND r.mode='cash'),0)
 FROM billing_cash_shifts s WHERE s.tenant_id=row_tenant AND s.id=row_shift
$$;

-- As in 0005, and: a shift does not close while one of its UPI requests
-- still waits for an answer.
CREATE OR REPLACE FUNCTION billing_shift_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.status<>'open' OR NEW.cashier IS DISTINCT FROM billing_actor() THEN
   RAISE EXCEPTION 'a shift opens for the acting cashier' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;
 IF (NEW.tenant_id,NEW.hospital_id,NEW.id,NEW.counter_id,NEW.cashier,NEW.currency,NEW.opening_float_minor,NEW.opened_at)
  IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.id,OLD.counter_id,OLD.cashier,OLD.currency,OLD.opening_float_minor,OLD.opened_at) THEN
  RAISE EXCEPTION 'a shift keeps its counter, cashier and float' USING ERRCODE='23514';
 END IF;
 IF OLD.status='open' AND NEW.status<>'open' THEN
  IF billing_actor() IS DISTINCT FROM OLD.cashier OR NEW.authorised_by IS NOT NULL
   OR NEW.expected_minor IS DISTINCT FROM billing_shift_expected(OLD.tenant_id,OLD.id) THEN
   RAISE EXCEPTION 'a shift is closed by its cashier against the cash it took' USING ERRCODE='23514';
  END IF;
  IF EXISTS (SELECT 1 FROM billing_upi_requests u WHERE u.tenant_id=OLD.tenant_id AND u.shift_id=OLD.id AND billing_upi_pending(u.status,u.created_at)) THEN
   RAISE EXCEPTION 'a shift does not close while a UPI payment waits' USING ERRCODE='23514', CONSTRAINT='billing_cash_shift_upi_waiting';
  END IF;
  RETURN NEW;
 END IF;
 IF OLD.status='pending_authorisation' AND NEW.status='closed' THEN
  IF (NEW.expected_minor,NEW.declared_minor,NEW.closed_at) IS DISTINCT FROM (OLD.expected_minor,OLD.declared_minor,OLD.closed_at)
   OR NEW.authorised_by IS DISTINCT FROM billing_actor() OR NEW.authorised_by=OLD.cashier THEN
   RAISE EXCEPTION 'a variance is authorised by someone other than the cashier' USING ERRCODE='23514', CONSTRAINT='billing_cash_shift_own_variance';
  END IF;
  RETURN NEW;
 END IF;
 RAISE EXCEPTION 'a shift closes once' USING ERRCODE='23514';
END $$;

-- As in 0007, and: a cash payment fits what is payable net of waiting UPI
-- payments; a UPI receipt is written only by a billing consumer for its paid
-- request, into the shift that asked, open or not; a UPI receipt is never
-- voided: the money is at the bank, and a void is cash handed back.
CREATE OR REPLACE FUNCTION billing_receipt_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE s record; a record; u record;
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.mode='upi' THEN
   SELECT hospital_id,shift_id,counter_id,cashier,admission_id,purpose,amount_minor,currency,status INTO u
    FROM billing_upi_requests WHERE tenant_id=NEW.tenant_id AND id=NEW.upi_request_id;
   IF NOT billing_consumer() OR NOT FOUND OR u.status<>'paid' OR NEW.id<>NEW.upi_request_id OR NEW.void_id IS NOT NULL
    OR (u.hospital_id,u.shift_id,u.counter_id,u.cashier,u.admission_id,u.purpose,u.amount_minor,u.currency)
     IS DISTINCT FROM (NEW.hospital_id,NEW.shift_id,NEW.counter_id,NEW.cashier,NEW.admission_id,NEW.purpose,NEW.amount_minor,NEW.currency) THEN
    RAISE EXCEPTION 'a UPI receipt records its paid request' USING ERRCODE='23514', CONSTRAINT='billing_cash_receipt_upi_request';
   END IF;
   PERFORM 1 FROM billing_cash_shifts WHERE tenant_id=NEW.tenant_id AND id=NEW.shift_id FOR SHARE;
   PERFORM 1 FROM billing_counters WHERE tenant_id=NEW.tenant_id AND id=NEW.counter_id FOR UPDATE;
   IF NEW.number <> (SELECT COALESCE(max(number),0)+1 FROM billing_cash_receipts WHERE tenant_id=NEW.tenant_id AND counter_id=NEW.counter_id) THEN
    RAISE EXCEPTION 'receipts are numbered in order on their counter' USING ERRCODE='23514';
   END IF;
   PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id::text||':billing-bed-days:'||NEW.admission_id::text, 0));
   RETURN NEW;
  END IF;
  SELECT counter_id,cashier,currency,status INTO s FROM billing_cash_shifts WHERE tenant_id=NEW.tenant_id AND hospital_id=NEW.hospital_id AND id=NEW.shift_id FOR SHARE;
  IF NOT FOUND OR s.status<>'open' OR s.cashier IS DISTINCT FROM billing_actor() OR NEW.cashier IS DISTINCT FROM s.cashier
   OR NEW.counter_id<>s.counter_id OR NEW.currency<>s.currency OR NEW.void_id IS NOT NULL THEN
   RAISE EXCEPTION 'a receipt is taken in its cashier''s open shift' USING ERRCODE='23514', CONSTRAINT='billing_cash_receipt_shift';
  END IF;
  PERFORM billing_receipt_id_free(NEW.tenant_id,NEW.id,'billing_cash_receipts_pkey');
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
   IF NEW.amount_minor > billing_refundable(NEW.tenant_id,NEW.admission_id,NEW.currency) THEN
    RAISE EXCEPTION 'a refund is never more than what is held beyond the bill' USING ERRCODE='23514', CONSTRAINT='billing_refund_over_held';
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
  IF NEW.amount_minor > billing_payable(NEW.tenant_id,NEW.admission_id,NEW.currency) THEN
   RAISE EXCEPTION 'a receipt is never more than the balance due' USING ERRCODE='23514', CONSTRAINT='billing_cash_receipt_balance';
  END IF;
  RETURN NEW;
 END IF;
 IF (NEW.tenant_id,NEW.hospital_id,NEW.id,NEW.shift_id,NEW.counter_id,NEW.number,NEW.cashier,NEW.admission_id,NEW.amount_minor,NEW.currency,NEW.received_at,NEW.purpose,NEW.approval_id,NEW.mode,NEW.upi_request_id)
  IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.id,OLD.shift_id,OLD.counter_id,OLD.number,OLD.cashier,OLD.admission_id,OLD.amount_minor,OLD.currency,OLD.received_at,OLD.purpose,OLD.approval_id,OLD.mode,OLD.upi_request_id)
  OR OLD.voided_at IS NOT NULL OR NEW.voided_at IS NULL OR billing_actor() IS DISTINCT FROM OLD.cashier THEN
  RAISE EXCEPTION 'a receipt is voided once, by its cashier, and otherwise fixed' USING ERRCODE='23514';
 END IF;
 IF OLD.mode='upi' THEN
  RAISE EXCEPTION 'a UPI receipt is not voided: the money is at the bank' USING ERRCODE='23514', CONSTRAINT='billing_cash_receipt_upi_void';
 END IF;
 PERFORM 1 FROM billing_cash_shifts WHERE tenant_id=OLD.tenant_id AND id=OLD.shift_id AND status='open' FOR SHARE;
 IF NOT FOUND THEN
  RAISE EXCEPTION 'a receipt is voided only while its shift is open' USING ERRCODE='23514', CONSTRAINT='billing_cash_receipt_shift';
 END IF;
 IF OLD.purpose<>'refund' THEN
  PERFORM 1 FROM billing_counters WHERE tenant_id=OLD.tenant_id AND id=OLD.counter_id FOR UPDATE;
  IF billing_shift_expected(OLD.tenant_id,OLD.shift_id) - OLD.amount_minor < 0 THEN
   RAISE EXCEPTION 'the drawer no longer holds this cash' USING ERRCODE='23514', CONSTRAINT='billing_refund_drawer';
  END IF;
 END IF;
 IF OLD.purpose='deposit' THEN
  PERFORM pg_advisory_xact_lock(hashtextextended(OLD.tenant_id::text||':billing-bed-days:'||OLD.admission_id::text, 0));
  IF billing_held(OLD.tenant_id,OLD.admission_id,OLD.currency) - OLD.amount_minor < 0 THEN
   RAISE EXCEPTION 'a deposit already applied or refunded cannot be voided' USING ERRCODE='23514', CONSTRAINT='billing_cash_deposit_applied';
  END IF;
 END IF;
 RETURN NEW;
END $$;

-- As in 0007, and: an application fits what is payable net of waiting UPI
-- payments, so a QR being paid and a deposit applied never cover one rupee.
CREATE OR REPLACE FUNCTION billing_deposit_application_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE held bigint;
BEGIN
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id::text||':billing-bed-days:'||NEW.admission_id::text, 0));
 IF NEW.applied_by IS DISTINCT FROM billing_actor()
  OR NOT EXISTS (SELECT 1 FROM billing_interim_bills b WHERE b.tenant_id=NEW.tenant_id AND b.admission_id=NEW.admission_id AND b.number=NEW.bill_number AND b.kind='final') THEN
  RAISE EXCEPTION 'deposits are applied against a final bill by the acting subject' USING ERRCODE='23514', CONSTRAINT='billing_deposit_final_bill';
 END IF;
 held := billing_held(NEW.tenant_id,NEW.admission_id,NEW.currency);
 IF NEW.amount_minor > held OR NEW.amount_minor > billing_payable(NEW.tenant_id,NEW.admission_id,NEW.currency) THEN
  RAISE EXCEPTION 'an application is never more than what is held or due' USING ERRCODE='23514', CONSTRAINT='billing_deposit_application_bound';
 END IF;
 RETURN NEW;
END $$;
