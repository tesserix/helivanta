SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- Who occupies which bed, as MediCore announced (#1387). Ids and location
-- codes only. Started and ended may arrive in either order: a row is made
-- by whichever names it first, and a stay is open only once it has started
-- and not ended.
CREATE TABLE pharmacy_bed_occupancies (
 tenant_id uuid NOT NULL,
 occupancy_id uuid NOT NULL,
 admission_id uuid NOT NULL,
 hospital_id uuid NOT NULL,
 bed_id uuid NOT NULL,
 patient_id uuid,
 ward_code text,
 room_code text,
 bed_code text,
 started_at timestamptz,
 ended_at timestamptz,
 PRIMARY KEY (tenant_id,occupancy_id),
 CONSTRAINT pharmacy_bed_occupancy_started CHECK ((started_at IS NULL) = (patient_id IS NULL)
  AND (started_at IS NULL) = (bed_code IS NULL) AND (started_at IS NULL) = (room_code IS NULL) AND (started_at IS NULL) = (ward_code IS NULL))
);
CREATE INDEX pharmacy_bed_occupancy_open_bed ON pharmacy_bed_occupancies (tenant_id,hospital_id,bed_code) WHERE ended_at IS NULL AND started_at IS NOT NULL;
CREATE INDEX pharmacy_bed_occupancy_open_room ON pharmacy_bed_occupancies (tenant_id,hospital_id,room_code) WHERE ended_at IS NULL AND started_at IS NOT NULL;
ALTER TABLE pharmacy_bed_occupancies ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_bed_occupancies FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_bed_occupancies USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid) WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid);
-- Identity is fixed; a start or an end is recorded once.
CREATE FUNCTION pharmacy_bed_occupancy_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'bed occupancy history is retained' USING ERRCODE='23514'; END IF;
 IF (NEW.tenant_id,NEW.occupancy_id,NEW.admission_id,NEW.hospital_id,NEW.bed_id) IS DISTINCT FROM (OLD.tenant_id,OLD.occupancy_id,OLD.admission_id,OLD.hospital_id,OLD.bed_id)
  OR (OLD.started_at IS NOT NULL AND (NEW.started_at,NEW.patient_id,NEW.ward_code,NEW.room_code,NEW.bed_code) IS DISTINCT FROM (OLD.started_at,OLD.patient_id,OLD.ward_code,OLD.room_code,OLD.bed_code))
  OR (OLD.ended_at IS NOT NULL AND NEW.ended_at IS DISTINCT FROM OLD.ended_at) THEN
  RAISE EXCEPTION 'a stay starts and ends once' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_bed_occupancy_guard BEFORE UPDATE OR DELETE ON pharmacy_bed_occupancies FOR EACH ROW EXECUTE FUNCTION pharmacy_bed_occupancy_guard();
CREATE TRIGGER pharmacy_bed_occupancy_no_truncate BEFORE TRUNCATE ON pharmacy_bed_occupancies FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_registered_patients_immutable();

-- A sale billed to the admission occupying a bed: what it resolved through
-- and when, so a later transfer cannot reattribute it.
ALTER TABLE pharmacy_sales
 ADD COLUMN admission_id uuid,
 ADD COLUMN admission_occupancy_id uuid,
 ADD COLUMN admission_bed_code text,
 ADD COLUMN admission_resolved_at timestamptz,
 ADD CONSTRAINT pharmacy_sale_admission_complete CHECK (
  (admission_id IS NULL AND admission_occupancy_id IS NULL AND admission_bed_code IS NULL AND admission_resolved_at IS NULL)
  OR (admission_id IS NOT NULL AND admission_occupancy_id IS NOT NULL AND admission_bed_code IS NOT NULL AND admission_resolved_at IS NOT NULL AND patient_id IS NOT NULL)),
 ADD CONSTRAINT pharmacy_sale_admission_occupancy FOREIGN KEY (tenant_id,admission_occupancy_id) REFERENCES pharmacy_bed_occupancies (tenant_id,occupancy_id);

-- The stay must be open, for that admission, patient and hospital, when the
-- sale posts.
CREATE FUNCTION pharmacy_sale_admission_open() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.admission_id IS NULL THEN RETURN NEW; END IF;
 IF NOT EXISTS (SELECT 1 FROM pharmacy_bed_occupancies o WHERE o.tenant_id=NEW.tenant_id AND o.occupancy_id=NEW.admission_occupancy_id
    AND o.admission_id=NEW.admission_id AND o.patient_id=NEW.patient_id AND o.hospital_id=NEW.patient_hospital_id
    AND o.bed_code=NEW.admission_bed_code AND o.started_at IS NOT NULL AND o.ended_at IS NULL) THEN
  RAISE EXCEPTION 'the patient is no longer in that bed' USING ERRCODE='23514', CONSTRAINT='pharmacy_sale_admission_open';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_sale_admission_open BEFORE INSERT ON pharmacy_sales FOR EACH ROW EXECUTE FUNCTION pharmacy_sale_admission_open();

-- Billed to the account means nothing is collected: one admission_account
-- tender for the total on an admitted sale, and never otherwise.
ALTER TABLE pharmacy_sale_tenders DROP CONSTRAINT pharmacy_sale_tenders_kind_check;
ALTER TABLE pharmacy_sale_tenders ADD CONSTRAINT pharmacy_sale_tenders_kind_check
 CHECK (kind IN ('cash','upi','card','upi_verified','admission_account'));
ALTER TABLE pharmacy_sale_tenders DROP CONSTRAINT pharmacy_sale_tenders_check;
ALTER TABLE pharmacy_sale_tenders ADD CONSTRAINT pharmacy_sale_tenders_check CHECK (CASE kind
 WHEN 'cash' THEN reference IS NULL AND payment_request_id IS NULL AND tendered_minor IS NOT NULL AND tendered_minor >= amount_minor AND change_minor = tendered_minor - amount_minor
 WHEN 'upi_verified' THEN reference IS NULL AND tendered_minor IS NULL AND change_minor IS NULL AND payment_request_id IS NOT NULL
 WHEN 'admission_account' THEN reference IS NULL AND tendered_minor IS NULL AND change_minor IS NULL AND payment_request_id IS NULL
 ELSE payment_request_id IS NULL AND tendered_minor IS NULL AND change_minor IS NULL AND reference IS NOT NULL AND length(btrim(reference)) BETWEEN 1 AND 64 END);
CREATE FUNCTION pharmacy_sale_admission_tendered() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE admitted boolean; others int; accounts int;
BEGIN
 SELECT s.admission_id IS NOT NULL INTO admitted FROM pharmacy_sales s WHERE s.tenant_id=NEW.tenant_id AND s.outlet_id=NEW.outlet_id AND s.id=NEW.id;
 SELECT count(*) FILTER (WHERE kind<>'admission_account'), count(*) FILTER (WHERE kind='admission_account') INTO others, accounts
  FROM pharmacy_sale_tenders WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND sale_id=NEW.id;
 IF (admitted AND (others<>0 OR accounts<>1)) OR (NOT admitted AND accounts<>0) THEN
  RAISE EXCEPTION 'a sale billed to an admission is paid by the admission account alone, and only such a sale is' USING ERRCODE='23514', CONSTRAINT='pharmacy_sale_admission_tendered';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER pharmacy_sale_admission_tendered AFTER INSERT ON pharmacy_sales DEFERRABLE INITIALLY DEFERRED
 FOR EACH ROW EXECUTE FUNCTION pharmacy_sale_admission_tendered();

-- A return of a sale billed to an admission reverses the charge on the
-- account; it is never refunded in money, and only such a return is.
ALTER TABLE pharmacy_credit_note_refunds DROP CONSTRAINT pharmacy_credit_note_refunds_kind_check;
ALTER TABLE pharmacy_credit_note_refunds ADD CONSTRAINT pharmacy_credit_note_refunds_kind_check CHECK (kind IN ('cash','upi','card','admission_account'));
ALTER TABLE pharmacy_credit_note_refunds DROP CONSTRAINT pharmacy_credit_note_refunds_check;
ALTER TABLE pharmacy_credit_note_refunds ADD CONSTRAINT pharmacy_credit_note_refunds_check CHECK ((kind IN ('cash','admission_account')) = (reference IS NULL));
CREATE FUNCTION pharmacy_credit_note_admission_refunded() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE admitted boolean;
BEGIN
 SELECT s.admission_id IS NOT NULL INTO admitted FROM pharmacy_credit_notes n JOIN pharmacy_sales s ON s.tenant_id=n.tenant_id AND s.outlet_id=n.outlet_id AND s.id=n.sale_id
  WHERE n.tenant_id=NEW.tenant_id AND n.outlet_id=NEW.outlet_id AND n.id=NEW.credit_note_id;
 IF admitted <> (NEW.kind='admission_account') THEN
  RAISE EXCEPTION 'a return of a sale billed to an admission goes back to the admission account, and only it does' USING ERRCODE='23514', CONSTRAINT='pharmacy_credit_note_admission_refunded';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_credit_note_admission_refunded BEFORE INSERT ON pharmacy_credit_note_refunds FOR EACH ROW EXECUTE FUNCTION pharmacy_credit_note_admission_refunded();
