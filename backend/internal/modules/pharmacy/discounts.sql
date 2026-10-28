SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

-- Discounts under the outlet ceiling (#1386 B-3, product decision 3). A
-- discount recorded on the invoice at the time of supply is outside the
-- value of supply (CGST Act s.15(3)(a)), so tax stays inside the discounted
-- amount. The ceiling is the owner's commercial decision; none recorded is 0.

CREATE TABLE pharmacy_outlet_discount_ceilings (
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 tenant_id uuid NOT NULL,
 outlet_id uuid NOT NULL,
 seq bigint GENERATED ALWAYS AS IDENTITY,
 max_bps integer NOT NULL CHECK (max_bps BETWEEN 0 AND 10000),
 reason text NOT NULL CHECK (length(reason) BETWEEN 5 AND 200 AND reason=btrim(reason) AND reason !~ '[[:cntrl:]]'),
 intent_sha256 text NOT NULL CHECK (intent_sha256 ~ '^[0-9a-f]{64}$'),
 recorded_by text NOT NULL CHECK (length(btrim(recorded_by)) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (tenant_id,outlet_id,id),
 UNIQUE (seq),
 FOREIGN KEY (tenant_id,outlet_id) REFERENCES pharmacy_outlets (tenant_id,id)
);
ALTER TABLE pharmacy_outlet_discount_ceilings ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_outlet_discount_ceilings FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_outlet_discount_ceilings
 USING (hms_tenant_visible(tenant_id) AND pharmacy_outlet_allowed(tenant_id,outlet_id,false))
 WITH CHECK (pharmacy_outlet_allowed(tenant_id,outlet_id,true) AND recorded_by=NULLIF(current_setting('app.pharmacy_subject',true),''));
CREATE INDEX pharmacy_discount_ceiling_current ON pharmacy_outlet_discount_ceilings (tenant_id,outlet_id,seq DESC);
CREATE TRIGGER pharmacy_discount_ceiling_audit AFTER INSERT ON pharmacy_outlet_discount_ceilings
 FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_discount_ceiling_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_outlet_discount_ceilings
 FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

-- The latest record is in force; with none, no discount is allowed.
CREATE FUNCTION pharmacy_outlet_discount_ceiling_in_force(tenant uuid, outlet uuid) RETURNS integer LANGUAGE sql STABLE AS $$
 SELECT COALESCE((SELECT max_bps FROM pharmacy_outlet_discount_ceilings
  WHERE tenant_id=tenant AND outlet_id=outlet ORDER BY seq DESC LIMIT 1), 0)
$$;

ALTER TABLE pharmacy_sales
 ADD COLUMN discount_reason text CHECK (length(discount_reason) BETWEEN 5 AND 200 AND discount_reason=btrim(discount_reason) AND discount_reason !~ '[[:cntrl:]]');

-- Lines are immutable, so the list amount is derived, never backfilled: a
-- line before B-3 has no discount and its list amount is what it charged.
ALTER TABLE pharmacy_sale_lines
 ADD COLUMN discount_bps integer NOT NULL DEFAULT 0 CHECK (discount_bps BETWEEN 0 AND 10000),
 ADD COLUMN discount_minor bigint NOT NULL DEFAULT 0 CHECK (discount_minor >= 0),
 ADD COLUMN list_amount_minor bigint GENERATED ALWAYS AS (amount_minor + discount_minor) STORED,
 ADD CONSTRAINT pharmacy_sale_line_list_within_mrp CHECK ((amount_minor + discount_minor)::numeric * pack_size <= mrp_minor::numeric * quantity_units),
 -- The discount is the rounded-down percentage of the list amount, so a
 -- line cannot carry an arbitrary reduction; something is always charged.
 ADD CONSTRAINT pharmacy_sale_line_discount_rounded CHECK (discount_minor = div((amount_minor + discount_minor)::numeric * discount_bps, 10000) AND amount_minor >= 1);

-- The ceiling in force when the line is written bounds its discount.
CREATE FUNCTION pharmacy_sale_line_discount_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.discount_bps > pharmacy_outlet_discount_ceiling_in_force(NEW.tenant_id,NEW.outlet_id) THEN
  RAISE EXCEPTION 'a sale line''s discount is within the outlet''s ceiling' USING ERRCODE='23514',CONSTRAINT='pharmacy_sale_line_discount_ceiling';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_sale_line_discount_guard BEFORE INSERT ON pharmacy_sale_lines
 FOR EACH ROW EXECUTE FUNCTION pharmacy_sale_line_discount_guard();

-- At commit, a discounted sale carries its reason, and only a discounted
-- sale carries one.
CREATE FUNCTION pharmacy_sale_discount_reasoned() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE discounted boolean;
BEGIN
 SELECT COALESCE(bool_or(discount_minor > 0), false) INTO discounted FROM pharmacy_sale_lines
  WHERE tenant_id=NEW.tenant_id AND outlet_id=NEW.outlet_id AND sale_id=NEW.id;
 IF discounted <> (NEW.discount_reason IS NOT NULL) THEN
  RAISE EXCEPTION 'a discounted sale records its reason' USING ERRCODE='23514',CONSTRAINT='pharmacy_sale_discount_reason';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER pharmacy_sale_discount_reasoned AFTER INSERT ON pharmacy_sales
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION pharmacy_sale_discount_reasoned();
