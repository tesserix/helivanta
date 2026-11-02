SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- Card, netbanking and wallet receipts (#198 slice 6): money the counter's
-- terminal or the bank has already confirmed, recorded with its reference,
-- never in the drawer, at a counter set up to take it.

-- Which of the three a counter takes: a row per change, never edited, the
-- latest one standing. Cash and UPI are not switched here: cash is how a
-- drawer exists, and UPI is gated by the hospital's payment account.
CREATE TABLE billing_counter_modes (
 tenant_id uuid NOT NULL,
 hospital_id uuid NOT NULL,
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 counter_id uuid NOT NULL,
 mode text NOT NULL CHECK (mode IN ('card','netbanking','wallet')),
 accepted boolean NOT NULL,
 set_by text NOT NULL,
 set_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY (tenant_id,id),
 FOREIGN KEY (tenant_id,hospital_id,counter_id) REFERENCES billing_counters (tenant_id,hospital_id,id)
);
CREATE INDEX billing_counter_modes_latest ON billing_counter_modes (tenant_id,counter_id,mode,set_at DESC,id DESC);

ALTER TABLE billing_counter_modes ENABLE ROW LEVEL SECURITY;
ALTER TABLE billing_counter_modes FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON billing_counter_modes
 USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND billing_hospital_allowed(tenant_id,hospital_id))
 WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND billing_hospital_allowed(tenant_id,hospital_id));
CREATE TRIGGER billing_counter_modes_audit AFTER INSERT ON billing_counter_modes FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER billing_counter_modes_kept BEFORE UPDATE OR DELETE OR TRUNCATE ON billing_counter_modes FOR EACH STATEMENT EXECUTE FUNCTION billing_history_retained();

-- Whether a counter takes a mode now. Never set reads as not taken.
CREATE FUNCTION billing_counter_accepts(row_tenant uuid,row_counter uuid,row_mode text) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE((SELECT accepted FROM billing_counter_modes WHERE tenant_id=row_tenant AND counter_id=row_counter AND mode=row_mode
  ORDER BY set_at DESC,id DESC LIMIT 1),false)
$$;

-- The modes a counter takes now, in a fixed order, for its readers.
CREATE FUNCTION billing_counter_mode_list(row_tenant uuid,row_counter uuid) RETURNS jsonb LANGUAGE sql STABLE AS $$
 SELECT COALESCE(jsonb_agg(m ORDER BY m),'[]'::jsonb) FROM unnest(ARRAY['card','netbanking','wallet']) m
  WHERE billing_counter_accepts(row_tenant,row_counter,m)
$$;

-- Set by the acting subject, and only as a change. The counter lock orders a
-- change with every receipt at the counter, so none is taken by a mode
-- switched off before it committed.
CREATE FUNCTION billing_counter_mode_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.set_by IS DISTINCT FROM billing_actor() THEN
  RAISE EXCEPTION 'a counter''s modes are set by the acting subject' USING ERRCODE='23514';
 END IF;
 PERFORM 1 FROM billing_counters WHERE tenant_id=NEW.tenant_id AND id=NEW.counter_id FOR UPDATE;
 IF billing_counter_accepts(NEW.tenant_id,NEW.counter_id,NEW.mode)=NEW.accepted THEN
  RAISE EXCEPTION 'a counter''s mode is recorded only when it changes' USING ERRCODE='23514', CONSTRAINT='billing_counter_mode_unchanged';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER billing_counter_modes_guard BEFORE INSERT ON billing_counter_modes FOR EACH ROW EXECUTE FUNCTION billing_counter_mode_guard();

-- The reference each mode carries, upper case so one reference has one form;
-- no card digits are ever stored. Refunds stay cash (0011's check).
ALTER TABLE billing_cash_receipts DROP CONSTRAINT billing_cash_receipts_mode_check;
ALTER TABLE billing_cash_receipts ADD CONSTRAINT billing_cash_receipts_mode_check CHECK (mode IN ('cash','upi','card','netbanking','wallet'));
ALTER TABLE billing_cash_receipts ADD COLUMN reference text CHECK (reference ~ '^[A-Z0-9][A-Z0-9/-]{3,39}$');
ALTER TABLE billing_cash_receipts ADD COLUMN approval_code text CHECK (approval_code ~ '^[A-Z0-9]{6}$');
ALTER TABLE billing_cash_receipts ADD COLUMN wallet text CHECK (wallet ~ '^[a-z0-9]([a-z0-9 .-]{0,28}[a-z0-9])?$');
ALTER TABLE billing_cash_receipts ADD CONSTRAINT billing_cash_receipt_reference CHECK (
 (reference IS NOT NULL) = (mode IN ('card','netbanking','wallet'))
 AND (approval_code IS NOT NULL) = (mode='card')
 AND (wallet IS NOT NULL) = (mode='wallet'));
-- No card number is ever stored, typed by mistake into a reference: a card's
-- reference is its 12-digit RRN, and in any mode a Luhn-valid run of 13 to 19
-- digits, or a masked number, is refused. A rare real reference that happens
-- to pass Luhn is refused too: storing a card number is the worse failure.
-- A pinned search path: a CHECK calls it, and a dump restore runs it with an
-- empty one.
CREATE FUNCTION billing_card_number(t text) RETURNS boolean LANGUAGE plpgsql IMMUTABLE
 SET search_path = pg_catalog, public AS $$
DECLARE total int := 0; d int; n int;
BEGIN
 IF t IS NULL THEN RETURN false; END IF;
 IF t ~ 'X{4,}[0-9]{4}$' THEN RETURN true; END IF;
 IF t !~ '^[0-9]{13,19}$' THEN RETURN false; END IF;
 n := length(t);
 FOR i IN 1..n LOOP
  d := substr(t, n-i+1, 1)::int;
  IF i % 2 = 0 THEN d := d*2; IF d > 9 THEN d := d-9; END IF; END IF;
  total := total + d;
 END LOOP;
 RETURN total % 10 = 0;
END $$;
ALTER TABLE billing_cash_receipts ADD CONSTRAINT billing_cash_receipt_rrn CHECK (mode<>'card' OR reference ~ '^[0-9]{12}$');
ALTER TABLE billing_cash_receipts ADD CONSTRAINT billing_cash_receipt_no_card_number CHECK (NOT billing_card_number(reference));
-- One standing receipt per reference at a hospital, whatever its mode or
-- wallet name: a free-text name or another mode must not let one payment
-- clear two balances. A card's RRN is keyed with its approval code, as an RRN
-- alone can repeat across acquirers. A voided receipt frees its reference, so
-- an entry against the wrong patient is voided and entered again.
CREATE UNIQUE INDEX billing_cash_receipt_reference_once ON billing_cash_receipts
 (tenant_id,hospital_id,reference,COALESCE(approval_code,'')) WHERE reference IS NOT NULL AND voided_at IS NULL;

-- As in 0011, and: a card, netbanking or wallet receipt is taken only at a
-- counter that takes that mode now; its reference is fixed like the rest;
-- voiding anything but cash takes nothing out of the drawer.
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
  IF NEW.mode<>'cash' AND NOT billing_counter_accepts(NEW.tenant_id,NEW.counter_id,NEW.mode) THEN
   RAISE EXCEPTION 'a receipt is taken by a mode its counter takes' USING ERRCODE='23514', CONSTRAINT='billing_cash_receipt_mode';
  END IF;
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
 IF (NEW.tenant_id,NEW.hospital_id,NEW.id,NEW.shift_id,NEW.counter_id,NEW.number,NEW.cashier,NEW.admission_id,NEW.amount_minor,NEW.currency,NEW.received_at,NEW.purpose,NEW.approval_id,NEW.mode,NEW.upi_request_id,
     NEW.reference,NEW.approval_code,NEW.wallet)
  IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.id,OLD.shift_id,OLD.counter_id,OLD.number,OLD.cashier,OLD.admission_id,OLD.amount_minor,OLD.currency,OLD.received_at,OLD.purpose,OLD.approval_id,OLD.mode,OLD.upi_request_id,
     OLD.reference,OLD.approval_code,OLD.wallet)
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
 IF OLD.purpose<>'refund' AND OLD.mode='cash' THEN
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
