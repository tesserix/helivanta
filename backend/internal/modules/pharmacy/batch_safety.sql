SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

CREATE TABLE pharmacy_batch_safety_incidents (
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 tenant_id uuid NOT NULL,
 outlet_id uuid NOT NULL,
 batch_id uuid NOT NULL,
 location_id uuid,
 kind text NOT NULL CHECK (kind IN ('quality_concern','temperature_excursion','packaging_damage','suspected_recall')),
 reason text NOT NULL CHECK (length(btrim(reason)) BETWEEN 10 AND 500 AND reason=btrim(reason)),
 occurred_at timestamptz NOT NULL,
 measured_temperature_millicelsius bigint,
 actor_subject text NOT NULL CHECK (length(btrim(actor_subject)) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY (tenant_id,outlet_id,id),
 FOREIGN KEY (tenant_id,outlet_id,batch_id) REFERENCES pharmacy_stock_batches (tenant_id,outlet_id,id),
 FOREIGN KEY (tenant_id,outlet_id,location_id) REFERENCES pharmacy_stock_locations (tenant_id,outlet_id,id),
 CHECK (isfinite(occurred_at) AND occurred_at >= TIMESTAMPTZ '0001-01-01 00:00:00+00' AND occurred_at <= created_at),
 CHECK (measured_temperature_millicelsius BETWEEN -273150 AND 1000000),
 CHECK ((kind='temperature_excursion') = (measured_temperature_millicelsius IS NOT NULL))
);
ALTER TABLE pharmacy_batch_safety_incidents ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_batch_safety_incidents FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_batch_safety_incidents
 USING (hms_tenant_visible(tenant_id) AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK (pharmacy_outlet_allowed(tenant_id,outlet_id,true)
  AND actor_subject=current_setting('app.pharmacy_subject',true)
  AND actor_subject=current_setting('app.actor_subject',true));
CREATE INDEX pharmacy_batch_safety_page ON pharmacy_batch_safety_incidents (tenant_id,outlet_id,batch_id,created_at DESC,id DESC);
CREATE INDEX pharmacy_batch_safety_location ON pharmacy_batch_safety_incidents (tenant_id,outlet_id,location_id) WHERE location_id IS NOT NULL;
CREATE FUNCTION pharmacy_stamp_safety_recorded() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 NEW.created_at := clock_timestamp();
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_batch_safety_recorded BEFORE INSERT ON pharmacy_batch_safety_incidents
 FOR EACH ROW EXECUTE FUNCTION pharmacy_stamp_safety_recorded();
CREATE TRIGGER pharmacy_batch_safety_audit AFTER INSERT ON pharmacy_batch_safety_incidents
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_batch_safety_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_batch_safety_incidents
 FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();
