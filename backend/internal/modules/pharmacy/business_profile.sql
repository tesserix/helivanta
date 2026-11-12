ALTER TABLE pharmacy_organizations ADD COLUMN onboarding_profile jsonb
  CHECK (onboarding_profile IS NULL OR (jsonb_typeof(onboarding_profile)='object' AND octet_length(onboarding_profile::text)<=8192));
