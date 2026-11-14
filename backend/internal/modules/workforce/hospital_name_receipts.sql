SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- NULL next_attempt_at means pharmacy has confirmed the announced version; otherwise the announcer re-sends it when due.
ALTER TABLE workforce_hospital_name_announcements
  ADD COLUMN attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
  ADD COLUMN next_attempt_at timestamptz;
-- Recovers names lost to dead-lettering before #1653: every existing marker is unconfirmed and due now.
-- The owner is subject to forced RLS and sees no tenant here, so the backfill lifts it for this one statement.
ALTER TABLE workforce_hospital_name_announcements NO FORCE ROW LEVEL SECURITY;
UPDATE workforce_hospital_name_announcements SET next_attempt_at = clock_timestamp();
ALTER TABLE workforce_hospital_name_announcements FORCE ROW LEVEL SECURITY;
CREATE INDEX workforce_hospital_name_announcements_due ON workforce_hospital_name_announcements (next_attempt_at) WHERE next_attempt_at IS NOT NULL;
GRANT SELECT (next_attempt_at) ON workforce_hospital_name_announcements TO helivanta_system;
