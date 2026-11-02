SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- Shift events (#198 slice 7). Written by triggers in the transaction that
-- changes the drawer, as privacy's deadline events are: no handler can open,
-- count, close, pick up from or hand over a drawer without its event, and a
-- change rolled back publishes nothing. Subjects are the Go contract's
-- constants, substituted at migration time. Reasons and note counts stay out.

CREATE FUNCTION billing_publish(row_tenant uuid,row_subject text,row_type text,at timestamptz,body jsonb) RETURNS void LANGUAGE plpgsql AS $$
DECLARE event_id uuid:=gen_random_uuid();
BEGIN
 INSERT INTO outbox_events(id,subject,tenant_id,payload)
  VALUES(event_id,row_subject,row_tenant,jsonb_build_object('event_id',event_id,'type',row_type,'version',1,'tenant_id',row_tenant,'occurred_at',at,'data',body));
END $$;

-- The guards let each transition happen once, so each event is published once:
-- opened on insert; a count that differs from open or handing_over; closed
-- from any of those. Going to or back from handing_over only pauses the drawer.
CREATE FUNCTION billing_shift_events() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  PERFORM billing_publish(NEW.tenant_id,'${SHIFT_OPENED}','ShiftOpened',NEW.opened_at,jsonb_build_object(
   'shift_id',NEW.id,'hospital_id',NEW.hospital_id,'counter_id',NEW.counter_id,'cashier',NEW.cashier,'currency',NEW.currency,
   'opening_float_minor',NEW.opening_float_minor,'handover_id',NEW.handover_id,'opened_at',NEW.opened_at));
 ELSIF OLD.status IN ('open','handing_over') AND NEW.status='pending_authorisation' THEN
  PERFORM billing_publish(NEW.tenant_id,'${SHIFT_VARIANCE_RECORDED}','ShiftVarianceRecorded',NEW.closed_at,jsonb_build_object(
   'shift_id',NEW.id,'hospital_id',NEW.hospital_id,'counter_id',NEW.counter_id,'cashier',NEW.cashier,'currency',NEW.currency,
   'expected_minor',NEW.expected_minor,'declared_minor',NEW.declared_minor,'variance_minor',NEW.variance_minor,
   'handed_over',NEW.handed_over,'counted_at',NEW.closed_at));
 ELSIF OLD.status<>'closed' AND NEW.status='closed' THEN
  PERFORM billing_publish(NEW.tenant_id,'${SHIFT_CLOSED}','ShiftClosed',COALESCE(NEW.authorised_at,NEW.closed_at),jsonb_build_object(
   'shift_id',NEW.id,'hospital_id',NEW.hospital_id,'counter_id',NEW.counter_id,'cashier',NEW.cashier,'currency',NEW.currency,
   'opening_float_minor',NEW.opening_float_minor,'expected_minor',NEW.expected_minor,'declared_minor',NEW.declared_minor,
   'variance_minor',NEW.variance_minor,'handed_over',NEW.handed_over,'counted_at',NEW.closed_at,
   'authorised_by',NEW.authorised_by,'authorised_at',NEW.authorised_at));
 END IF;
 RETURN NULL;
END $$;
CREATE TRIGGER billing_cash_shifts_events AFTER INSERT OR UPDATE ON billing_cash_shifts FOR EACH ROW EXECUTE FUNCTION billing_shift_events();

CREATE FUNCTION billing_pickup_events() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM billing_publish(NEW.tenant_id,'${CASH_PICKUP_RECORDED}','CashPickupRecorded',NEW.recorded_at,jsonb_build_object(
  'pickup_id',NEW.id,'shift_id',NEW.shift_id,'hospital_id',NEW.hospital_id,'counter_id',NEW.counter_id,'kind',NEW.kind,
  'amount_minor',NEW.amount_minor,'currency',NEW.currency,'corrects_id',NEW.corrects_id,
  'recorded_by',NEW.recorded_by,'recorded_at',NEW.recorded_at));
 RETURN NULL;
END $$;
CREATE TRIGGER billing_cash_pickups_events AFTER INSERT ON billing_cash_pickups FOR EACH ROW EXECUTE FUNCTION billing_pickup_events();

-- Offered, withdrawn and disputed only pause the drawer; accepted and settled
-- are each reached once, and only from offered or disputed.
CREATE FUNCTION billing_handover_events() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.status IN ('offered','disputed') AND NEW.status IN ('accepted','settled') THEN
  PERFORM billing_publish(NEW.tenant_id,'${SHIFT_HANDED_OVER}','ShiftHandedOver',COALESCE(NEW.settled_at,NEW.decided_at),jsonb_build_object(
   'handover_id',NEW.id,'hospital_id',NEW.hospital_id,'counter_id',NEW.counter_id,'from_shift_id',NEW.from_shift_id,
   'to_shift_id',NEW.to_shift_id,'from_cashier',NEW.from_cashier,'to_cashier',NEW.to_cashier,'currency',NEW.currency,
   'agreed_minor',NEW.agreed_minor,'settled_by',NEW.settled_by,'handed_over_at',COALESCE(NEW.settled_at,NEW.decided_at)));
 END IF;
 RETURN NULL;
END $$;
CREATE TRIGGER billing_cash_handovers_events AFTER UPDATE ON billing_cash_handovers FOR EACH ROW EXECUTE FUNCTION billing_handover_events();
