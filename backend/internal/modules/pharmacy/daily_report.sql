-- #1386 C-3: the daily report and the H1 register read by outlet date; these
-- keep their work bounded by the period, not by the outlet's history.
CREATE INDEX pharmacy_sales_day ON pharmacy_sales (tenant_id,outlet_id,sale_date);
CREATE INDEX pharmacy_credit_notes_day ON pharmacy_credit_notes (tenant_id,outlet_id,credit_note_date);
