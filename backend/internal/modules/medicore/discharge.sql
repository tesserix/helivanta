SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- Discharge orders (#96 slice 1): the doctor's decision that an admitted
-- patient is fit for discharge. Separate from the release, which is the
-- departure: clinical readiness, departure, financial close and bed cleaning
-- stay four facts.
CREATE TABLE medicore_discharge_orders (
 tenant_id uuid NOT NULL,
 hospital_id uuid NOT NULL,
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 admission_id uuid NOT NULL,
 disposition text NOT NULL CHECK (disposition IN ('home','transfer_out')),
 note text CHECK (note IS NULL OR (note=btrim(note) AND length(note) BETWEEN 1 AND 500)),
 ordered_by text NOT NULL,
 ordered_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 withdrawal_id uuid UNIQUE CHECK (substring(withdrawal_id::text,15,1)='7'),
 withdrawal_reason text CHECK (withdrawal_reason IS NULL OR (withdrawal_reason=btrim(withdrawal_reason) AND length(withdrawal_reason) BETWEEN 1 AND 500)),
 withdrawn_by text,
 withdrawn_at timestamptz,
 PRIMARY KEY (tenant_id,id),
 FOREIGN KEY (tenant_id,hospital_id,admission_id) REFERENCES medicore_admissions (tenant_id,hospital_id,id),
 CONSTRAINT medicore_discharge_withdrawal CHECK (
  (withdrawal_id IS NULL AND withdrawal_reason IS NULL AND withdrawn_by IS NULL AND withdrawn_at IS NULL)
  OR (withdrawal_id IS NOT NULL AND withdrawal_reason IS NOT NULL AND withdrawn_by IS NOT NULL AND withdrawn_at IS NOT NULL))
);
-- One active order per admission.
CREATE UNIQUE INDEX medicore_discharge_active ON medicore_discharge_orders (tenant_id,admission_id) WHERE withdrawn_at IS NULL;
ALTER TABLE medicore_discharge_orders ENABLE ROW LEVEL SECURITY;
ALTER TABLE medicore_discharge_orders FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON medicore_discharge_orders USING (hms_tenant_visible(tenant_id) AND medicore_hospital_allowed(tenant_id,hospital_id))
 WITH CHECK (medicore_hospital_allowed(tenant_id,hospital_id));
CREATE TRIGGER medicore_discharge_orders_audit AFTER INSERT OR UPDATE ON medicore_discharge_orders FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER medicore_discharge_orders_kept BEFORE DELETE OR TRUNCATE ON medicore_discharge_orders FOR EACH STATEMENT EXECUTE FUNCTION medicore_bed_history_retained();

-- Ordered and withdrawn only while the patient is admitted, which is
-- share-locked so a release cannot slip between this check and commit. An
-- order never changes; it is withdrawn once.
CREATE FUNCTION medicore_discharge_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='UPDATE' AND ((NEW.tenant_id,NEW.hospital_id,NEW.id,NEW.admission_id,NEW.disposition,NEW.note,NEW.ordered_by,NEW.ordered_at)
  IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.id,OLD.admission_id,OLD.disposition,OLD.note,OLD.ordered_by,OLD.ordered_at)
  OR OLD.withdrawn_at IS NOT NULL) THEN
  RAISE EXCEPTION 'a discharge order is withdrawn once and otherwise fixed' USING ERRCODE='23514';
 END IF;
 IF TG_OP='INSERT' AND NEW.withdrawn_at IS NOT NULL THEN
  RAISE EXCEPTION 'a discharge order is made before it is withdrawn' USING ERRCODE='23514';
 END IF;
 PERFORM 1 FROM medicore_admissions a WHERE a.tenant_id=NEW.tenant_id AND a.hospital_id=NEW.hospital_id AND a.id=NEW.admission_id AND a.status='admitted' FOR SHARE;
 IF NOT FOUND THEN
  RAISE EXCEPTION 'discharge is ordered and withdrawn only while the patient is admitted' USING ERRCODE='23514', CONSTRAINT='medicore_discharge_admitted';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER medicore_discharge_guard BEFORE INSERT OR UPDATE ON medicore_discharge_orders FOR EACH ROW EXECUTE FUNCTION medicore_discharge_guard();
