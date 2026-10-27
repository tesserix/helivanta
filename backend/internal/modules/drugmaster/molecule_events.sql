SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

-- The approver also announces published molecules (#1373 A4).
DROP POLICY drugmaster_approver_outbox ON outbox_events;
CREATE POLICY drugmaster_approver_outbox ON outbox_events AS RESTRICTIVE FOR INSERT TO helivanta_drug_approver
 WITH CHECK (tenant_id IS NULL AND subject IN ('helivanta.in.drugmaster.catalogue_published.v1',
  'helivanta.in.drugmaster.formulation_reclassified.v1','helivanta.in.drugmaster.product_status_changed.v1',
  'helivanta.in.drugmaster.molecule_published.v1'));
