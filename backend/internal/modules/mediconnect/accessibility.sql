SET LOCAL lock_timeout = '5s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE mediconnect_accounts
 ADD COLUMN accessibility_text_size text NOT NULL DEFAULT 'system'
  CHECK (accessibility_text_size IN ('system', 'large', 'extra_large')),
 ADD COLUMN accessibility_contrast text NOT NULL DEFAULT 'system'
  CHECK (accessibility_contrast IN ('system', 'high')),
 ADD COLUMN accessibility_motion text NOT NULL DEFAULT 'system'
  CHECK (accessibility_motion IN ('system', 'reduced'));
