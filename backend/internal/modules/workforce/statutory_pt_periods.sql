SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE workforce_statutory_pt_params
 ADD COLUMN cadence text NOT NULL DEFAULT 'monthly' CHECK (cadence IN ('monthly','half_yearly','annual')),
 ADD COLUMN period_start_mask smallint,
 ADD CONSTRAINT workforce_statutory_pt_period_starts CHECK (CASE cadence
  WHEN 'monthly' THEN period_start_mask IS NULL
  WHEN 'half_yearly' THEN period_start_mask IS NOT NULL AND period_start_mask IN (65,130,260,520,1040,2080)
  ELSE period_start_mask IS NOT NULL AND period_start_mask BETWEEN 1 AND 2048 AND period_start_mask & (period_start_mask - 1) = 0 END);

-- A periodic rule charges each period in exactly one month, so no period's tax is skipped or doubled.
CREATE FUNCTION workforce_statutory_pt_periods() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE p workforce_statutory_pt_params; named int; span int; period_mask int;
BEGIN
 SELECT * INTO p FROM workforce_statutory_pt_params x WHERE (x.tenant_id,x.hospital_id,x.revision_id)=(NEW.tenant_id,NEW.hospital_id,NEW.id);
 IF NOT FOUND OR p.cadence='monthly' THEN
  RETURN NULL;
 END IF;
 SELECT coalesce(bit_or(s.months_mask),0) INTO named FROM workforce_statutory_pt_slabs s WHERE (s.tenant_id,s.hospital_id,s.revision_id)=(NEW.tenant_id,NEW.hospital_id,NEW.id);
 span := CASE p.cadence WHEN 'half_yearly' THEN 6 ELSE 12 END;
 FOR m IN 0..11 LOOP
  CONTINUE WHEN p.period_start_mask & (1<<m) = 0;
  period_mask := ((((1<<span)-1) << m) | (((1<<span)-1) >> (12-m))) & 4095;
  IF bit_count((named & period_mask)::bit(12)) <> 1 THEN
   RAISE EXCEPTION 'a periodic professional tax is charged in exactly one month of each period' USING ERRCODE='23514', CONSTRAINT='workforce_statutory_pt_periods';
  END IF;
 END LOOP;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER workforce_statutory_pt_periods AFTER INSERT ON workforce_statutory_rule_revisions DEFERRABLE INITIALLY DEFERRED
 FOR EACH ROW WHEN (NEW.kind='pt' AND NEW.covered) EXECUTE FUNCTION workforce_statutory_pt_periods();
