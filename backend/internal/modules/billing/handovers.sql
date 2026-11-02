SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- Handover between cashiers (#198 slice 5): the outgoing cashier counts the
-- drawer blind, the incoming cashier counts it again without seeing that
-- count, and only equal counts (or a supervisor's count after a dispute)
-- close the one shift and open the next with the count as its float.

-- A shift being handed over takes nothing: every receipt, refund, void,
-- pickup and UPI request already requires an open shift.
ALTER TABLE billing_cash_shifts DROP CONSTRAINT billing_cash_shifts_status_check;
ALTER TABLE billing_cash_shifts ADD CONSTRAINT billing_cash_shifts_status_check
 CHECK (status IN ('open','handing_over','pending_authorisation','closed'));
ALTER TABLE billing_cash_shifts DROP CONSTRAINT billing_cash_shift_close;
ALTER TABLE billing_cash_shifts ADD CONSTRAINT billing_cash_shift_close CHECK (
  (status IN ('open','handing_over') AND expected_minor IS NULL AND declared_minor IS NULL AND closed_at IS NULL AND authorised_by IS NULL)
  OR (status='pending_authorisation' AND expected_minor IS NOT NULL AND declared_minor IS NOT NULL AND closed_at IS NOT NULL
      AND declared_minor<>expected_minor AND authorised_by IS NULL)
  OR (status='closed' AND expected_minor IS NOT NULL AND declared_minor IS NOT NULL AND closed_at IS NOT NULL
      AND (declared_minor=expected_minor) = (authorised_by IS NULL)));

-- The handover a shift opened from, and whether a handover closed it.
ALTER TABLE billing_cash_shifts ADD COLUMN handover_id uuid;
ALTER TABLE billing_cash_shifts ADD COLUMN handed_over boolean NOT NULL DEFAULT false;
ALTER TABLE billing_cash_shifts ADD CONSTRAINT billing_cash_shift_handed_over CHECK (NOT handed_over OR status IN ('pending_authorisation','closed'));
CREATE UNIQUE INDEX billing_cash_shifts_from_handover ON billing_cash_shifts (tenant_id,handover_id);

-- A shift closed by handover no longer holds its counter while its variance
-- waits: the incoming shift holds it. An ordinary close still does.
DROP INDEX billing_cash_shift_counter_open;
CREATE UNIQUE INDEX billing_cash_shift_counter_open ON billing_cash_shifts (tenant_id,counter_id) WHERE status<>'closed' AND NOT handed_over;

CREATE TABLE billing_cash_handovers (
 tenant_id uuid NOT NULL,
 hospital_id uuid NOT NULL,
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 from_shift_id uuid NOT NULL,
 counter_id uuid NOT NULL,
 from_cashier text NOT NULL,
 to_cashier text NOT NULL CHECK (to_cashier=btrim(to_cashier) AND length(to_cashier) BETWEEN 1 AND 200),
 currency text NOT NULL CHECK (currency='INR'),
 status text NOT NULL DEFAULT 'offered' CHECK (status IN ('offered','withdrawn','disputed','accepted','settled')),
 outgoing_counts jsonb NOT NULL,
 incoming_counts jsonb,
 to_shift_id uuid CHECK (substring(to_shift_id::text,15,1)='7'),
 agreed_counts jsonb,
 agreed_minor bigint CHECK (agreed_minor BETWEEN 0 AND 1000000000),
 settled_by text,
 settlement_reason text CHECK (settlement_reason IS NULL OR (settlement_reason=btrim(settlement_reason) AND length(settlement_reason) BETWEEN 1 AND 500)),
 offered_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 decided_at timestamptz,
 settled_at timestamptz,
 PRIMARY KEY (tenant_id,id),
 UNIQUE (tenant_id,hospital_id,id),
 FOREIGN KEY (tenant_id,hospital_id,from_shift_id) REFERENCES billing_cash_shifts (tenant_id,hospital_id,id),
 CONSTRAINT billing_cash_handover_self CHECK (to_cashier<>from_cashier),
 CONSTRAINT billing_cash_handover_count CHECK (
  billing_cash_count_total(currency,outgoing_counts) IS NOT NULL
  AND (incoming_counts IS NULL OR billing_cash_count_total(currency,incoming_counts) IS NOT NULL)
  AND (agreed_counts IS NULL OR agreed_minor=billing_cash_count_total(currency,agreed_counts))),
 -- Equal means the same notes and coins: the handler drops zero entries,
 -- so one count has one form, and any other form reads as a dispute.
 CONSTRAINT billing_cash_handover_form CHECK (
  (status='offered' AND num_nonnulls(incoming_counts,to_shift_id,agreed_counts,agreed_minor,settled_by,settlement_reason,decided_at,settled_at)=0)
  OR (status='withdrawn' AND decided_at IS NOT NULL AND num_nonnulls(incoming_counts,to_shift_id,agreed_counts,agreed_minor,settled_by,settlement_reason,settled_at)=0)
  OR (status='disputed' AND num_nonnulls(incoming_counts,to_shift_id,decided_at)=3 AND incoming_counts<>outgoing_counts
      AND num_nonnulls(agreed_counts,agreed_minor,settled_by,settlement_reason,settled_at)=0)
  OR (status='accepted' AND num_nonnulls(incoming_counts,to_shift_id,decided_at,agreed_minor)=4 AND incoming_counts=outgoing_counts
      AND agreed_counts=outgoing_counts AND num_nonnulls(settled_by,settlement_reason,settled_at)=0)
  OR (status='settled' AND num_nonnulls(incoming_counts,to_shift_id,decided_at,agreed_counts,agreed_minor,settled_by,settlement_reason,settled_at)=8
      AND incoming_counts<>outgoing_counts AND settled_by NOT IN (from_cashier,to_cashier)))
);
-- One handover in progress and one completed per shift.
CREATE UNIQUE INDEX billing_cash_handover_live ON billing_cash_handovers (tenant_id,from_shift_id) WHERE status IN ('offered','disputed');
CREATE UNIQUE INDEX billing_cash_handover_done ON billing_cash_handovers (tenant_id,from_shift_id) WHERE status IN ('accepted','settled');
CREATE UNIQUE INDEX billing_cash_handover_to_shift ON billing_cash_handovers (tenant_id,to_shift_id);
CREATE INDEX billing_cash_handovers_incoming ON billing_cash_handovers (tenant_id,hospital_id,to_cashier,offered_at DESC,id DESC) WHERE status IN ('offered','disputed');
-- A disputed drawer holds its incoming cashier until it is settled, so the
-- settlement can always open their shift: one dispute each.
CREATE UNIQUE INDEX billing_cash_handover_taker_disputed ON billing_cash_handovers (tenant_id,hospital_id,to_cashier) WHERE status='disputed';
CREATE INDEX billing_cash_handovers_disputed ON billing_cash_handovers (tenant_id,hospital_id,offered_at DESC,id DESC) WHERE status='disputed';
ALTER TABLE billing_cash_shifts ADD CONSTRAINT billing_cash_shift_handover_ref FOREIGN KEY (tenant_id,hospital_id,handover_id) REFERENCES billing_cash_handovers (tenant_id,hospital_id,id);

ALTER TABLE billing_cash_handovers ENABLE ROW LEVEL SECURITY;
ALTER TABLE billing_cash_handovers FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON billing_cash_handovers
 USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND billing_hospital_allowed(tenant_id,hospital_id))
 WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND billing_hospital_allowed(tenant_id,hospital_id));
CREATE TRIGGER billing_cash_handovers_audit AFTER INSERT OR UPDATE ON billing_cash_handovers FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER billing_cash_handovers_kept BEFORE DELETE OR TRUNCATE ON billing_cash_handovers FOR EACH STATEMENT EXECUTE FUNCTION billing_history_retained();

-- The one who acts on a resolved handover: the incoming cashier who
-- accepted it, or the supervisor who settled it.
-- Serialises a cashier's shift openings with the counts that would hold them
-- for a settlement, so neither slips past the other.
CREATE FUNCTION billing_cashier_lock(row_tenant uuid,row_hospital uuid,row_cashier text) RETURNS void LANGUAGE sql AS $$
 SELECT pg_advisory_xact_lock(hashtextextended(row_tenant::text||':billing-cashier:'||row_hospital::text||':'||row_cashier, 0))
$$;

CREATE FUNCTION billing_handover_resolver(h billing_cash_handovers) RETURNS text LANGUAGE sql IMMUTABLE AS $$
 SELECT CASE h.status WHEN 'accepted' THEN h.to_cashier WHEN 'settled' THEN h.settled_by END
$$;

-- The incoming cashier holds no shift not yet closed: their count either
-- opens their shift now or holds them for the settlement that will.
CREATE FUNCTION billing_handover_taker_free(row_tenant uuid,row_hospital uuid,row_cashier text) RETURNS boolean LANGUAGE plpgsql AS $$
BEGIN
 PERFORM billing_cashier_lock(row_tenant,row_hospital,row_cashier);
 IF EXISTS (SELECT 1 FROM billing_cash_shifts WHERE tenant_id=row_tenant AND hospital_id=row_hospital AND cashier=row_cashier AND status<>'closed') THEN
  RAISE EXCEPTION 'the incoming cashier already has a shift' USING ERRCODE='23514', CONSTRAINT='billing_cash_handover_taker_busy';
 END IF;
 RETURN true;
END $$;

-- Offered by the open shift's cashier; withdrawn by them while it stands;
-- accepted or disputed by the named incoming cashier; a dispute settled by
-- neither of them.
CREATE FUNCTION billing_cash_handover_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE s record;
BEGIN
 IF TG_OP='INSERT' THEN
  SELECT counter_id,cashier,currency,status INTO s FROM billing_cash_shifts
   WHERE tenant_id=NEW.tenant_id AND hospital_id=NEW.hospital_id AND id=NEW.from_shift_id FOR UPDATE;
  IF NOT FOUND OR s.status<>'open' OR NEW.status<>'offered' OR s.cashier IS DISTINCT FROM billing_actor()
   OR NEW.from_cashier<>s.cashier OR NEW.counter_id<>s.counter_id OR NEW.currency<>s.currency THEN
   RAISE EXCEPTION 'a handover is offered by the cashier of an open shift' USING ERRCODE='23514', CONSTRAINT='billing_cash_handover_offer';
  END IF;
  RETURN NEW;
 END IF;
 IF (NEW.tenant_id,NEW.hospital_id,NEW.id,NEW.from_shift_id,NEW.counter_id,NEW.from_cashier,NEW.to_cashier,NEW.currency,NEW.outgoing_counts,NEW.offered_at)
  IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.id,OLD.from_shift_id,OLD.counter_id,OLD.from_cashier,OLD.to_cashier,OLD.currency,OLD.outgoing_counts,OLD.offered_at) THEN
  RAISE EXCEPTION 'a handover keeps its shift, cashiers and count' USING ERRCODE='23514', CONSTRAINT='billing_cash_handover_actor';
 END IF;
 IF (OLD.status='offered' AND NEW.status='withdrawn' AND billing_actor()=OLD.from_cashier)
  OR (OLD.status='offered' AND NEW.status IN ('accepted','disputed') AND billing_actor()=OLD.to_cashier
      AND billing_handover_taker_free(OLD.tenant_id,OLD.hospital_id,OLD.to_cashier))
  OR (OLD.status='disputed' AND NEW.status='settled' AND NEW.settled_by=billing_actor()
      AND (NEW.incoming_counts,NEW.to_shift_id,NEW.decided_at) IS NOT DISTINCT FROM (OLD.incoming_counts,OLD.to_shift_id,OLD.decided_at)) THEN
  RETURN NEW;
 END IF;
 RAISE EXCEPTION 'a handover is withdrawn by its giver, counted by its taker, or settled by someone else' USING ERRCODE='23514', CONSTRAINT='billing_cash_handover_actor';
END $$;
CREATE TRIGGER billing_cash_handovers_guard BEFORE INSERT OR UPDATE ON billing_cash_handovers FOR EACH ROW EXECUTE FUNCTION billing_cash_handover_guard();

-- As in 0013, and: a shift goes to handing_over only with its cashier's
-- standing offer, back to open only once no handover is in progress, and
-- closes from it only with the resolved handover's count, by its resolver;
-- a shift opens from a handover only with its agreed float, for its taker.
CREATE OR REPLACE FUNCTION billing_shift_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE h billing_cash_handovers;
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.status<>'open' OR NEW.declared_counts IS NOT NULL OR NEW.handed_over THEN
   RAISE EXCEPTION 'a shift opens for the acting cashier' USING ERRCODE='23514';
  END IF;
  PERFORM billing_cashier_lock(NEW.tenant_id,NEW.hospital_id,NEW.cashier);
  IF EXISTS (SELECT 1 FROM billing_cash_handovers WHERE tenant_id=NEW.tenant_id AND hospital_id=NEW.hospital_id AND to_cashier=NEW.cashier AND status='disputed') THEN
   RAISE EXCEPTION 'a cashier whose drawer waits for a settlement opens no other shift' USING ERRCODE='23514', CONSTRAINT='billing_cash_shift_awaiting_settlement';
  END IF;
  IF NEW.handover_id IS NULL THEN
   IF NEW.cashier IS DISTINCT FROM billing_actor() THEN
    RAISE EXCEPTION 'a shift opens for the acting cashier' USING ERRCODE='23514';
   END IF;
   RETURN NEW;
  END IF;
  SELECT * INTO h FROM billing_cash_handovers WHERE tenant_id=NEW.tenant_id AND hospital_id=NEW.hospital_id AND id=NEW.handover_id;
  IF NOT FOUND OR h.status NOT IN ('accepted','settled') OR h.to_shift_id<>NEW.id OR h.to_cashier<>NEW.cashier
   OR h.counter_id<>NEW.counter_id OR h.currency<>NEW.currency OR h.agreed_minor<>NEW.opening_float_minor
   OR billing_actor() IS DISTINCT FROM billing_handover_resolver(h)
   OR NOT EXISTS (SELECT 1 FROM billing_cash_shifts o WHERE o.tenant_id=h.tenant_id AND o.id=h.from_shift_id AND o.handed_over) THEN
   RAISE EXCEPTION 'a shift opens from a handover with its agreed count, for its taker' USING ERRCODE='23514', CONSTRAINT='billing_cash_shift_from_handover';
  END IF;
  RETURN NEW;
 END IF;
 IF (NEW.tenant_id,NEW.hospital_id,NEW.id,NEW.counter_id,NEW.cashier,NEW.currency,NEW.opening_float_minor,NEW.opened_at,NEW.handover_id)
  IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.id,OLD.counter_id,OLD.cashier,OLD.currency,OLD.opening_float_minor,OLD.opened_at,OLD.handover_id) THEN
  RAISE EXCEPTION 'a shift keeps its counter, cashier and float' USING ERRCODE='23514';
 END IF;
 IF OLD.status='open' AND NEW.status='handing_over' THEN
  IF billing_actor() IS DISTINCT FROM OLD.cashier OR NEW.handed_over OR NOT EXISTS (SELECT 1 FROM billing_cash_handovers
    WHERE tenant_id=OLD.tenant_id AND from_shift_id=OLD.id AND status='offered' AND from_cashier=OLD.cashier) THEN
   RAISE EXCEPTION 'a shift is handed over by its cashier with a standing offer' USING ERRCODE='23514', CONSTRAINT='billing_cash_shift_handover';
  END IF;
  IF EXISTS (SELECT 1 FROM billing_upi_requests u WHERE u.tenant_id=OLD.tenant_id AND u.shift_id=OLD.id AND billing_upi_pending(u.status,u.created_at)) THEN
   RAISE EXCEPTION 'a shift is not handed over while a UPI payment waits' USING ERRCODE='23514', CONSTRAINT='billing_cash_shift_upi_waiting';
  END IF;
  RETURN NEW;
 END IF;
 IF OLD.status='handing_over' AND NEW.status='open' THEN
  IF billing_actor() IS DISTINCT FROM OLD.cashier OR NEW.handed_over OR EXISTS (SELECT 1 FROM billing_cash_handovers
    WHERE tenant_id=OLD.tenant_id AND from_shift_id=OLD.id AND status<>'withdrawn') THEN
   RAISE EXCEPTION 'a shift reopens only when its handover is withdrawn' USING ERRCODE='23514', CONSTRAINT='billing_cash_shift_handover';
  END IF;
  RETURN NEW;
 END IF;
 IF OLD.status='handing_over' AND NEW.status IN ('pending_authorisation','closed') THEN
  SELECT * INTO h FROM billing_cash_handovers WHERE tenant_id=OLD.tenant_id AND from_shift_id=OLD.id AND status IN ('accepted','settled');
  IF NOT FOUND OR NOT NEW.handed_over OR NEW.authorised_by IS NOT NULL
   OR NEW.declared_counts IS DISTINCT FROM h.agreed_counts
   OR NEW.expected_minor IS DISTINCT FROM billing_shift_expected(OLD.tenant_id,OLD.id)
   OR billing_actor() IS DISTINCT FROM billing_handover_resolver(h) THEN
   RAISE EXCEPTION 'a handed-over shift closes on its agreed count, by who resolved it' USING ERRCODE='23514', CONSTRAINT='billing_cash_shift_handover';
  END IF;
  RETURN NEW;
 END IF;
 IF OLD.status='open' AND NEW.status IN ('pending_authorisation','closed') THEN
  IF billing_actor() IS DISTINCT FROM OLD.cashier OR NEW.authorised_by IS NOT NULL OR NEW.handed_over
   OR NEW.expected_minor IS DISTINCT FROM billing_shift_expected(OLD.tenant_id,OLD.id) THEN
   RAISE EXCEPTION 'a shift is closed by its cashier against the cash it took' USING ERRCODE='23514';
  END IF;
  IF NEW.declared_counts IS NULL THEN
   RAISE EXCEPTION 'a shift is closed by counting its notes and coins' USING ERRCODE='23514', CONSTRAINT='billing_cash_shift_count';
  END IF;
  IF EXISTS (SELECT 1 FROM billing_upi_requests u WHERE u.tenant_id=OLD.tenant_id AND u.shift_id=OLD.id AND billing_upi_pending(u.status,u.created_at)) THEN
   RAISE EXCEPTION 'a shift does not close while a UPI payment waits' USING ERRCODE='23514', CONSTRAINT='billing_cash_shift_upi_waiting';
  END IF;
  RETURN NEW;
 END IF;
 IF OLD.status='pending_authorisation' AND NEW.status='closed' THEN
  IF (NEW.expected_minor,NEW.declared_minor,NEW.closed_at,NEW.declared_counts,NEW.handed_over) IS DISTINCT FROM (OLD.expected_minor,OLD.declared_minor,OLD.closed_at,OLD.declared_counts,OLD.handed_over)
   OR NEW.authorised_by IS DISTINCT FROM billing_actor() OR NEW.authorised_by=OLD.cashier THEN
   RAISE EXCEPTION 'a variance is authorised by someone other than the cashier' USING ERRCODE='23514', CONSTRAINT='billing_cash_shift_own_variance';
  END IF;
  RETURN NEW;
 END IF;
 RAISE EXCEPTION 'a shift closes once' USING ERRCODE='23514';
END $$;
