SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE FUNCTION iam_membership_recovery_admission() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF OLD.member_count=0 AND NEW.member_count>0
  AND (NULLIF(current_setting('app.membership_command',true),'') IS NULL
   OR NULLIF(current_setting('app.membership_recovery_basis',true),'') IS NULL) THEN
  RAISE EXCEPTION 'explicit membership recovery required'
   USING ERRCODE='23514',CONSTRAINT='iam_membership_recovery_required';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER iam_membership_recovery_admission BEFORE UPDATE ON iam_subject_membership_epochs
 FOR EACH ROW EXECUTE FUNCTION iam_membership_recovery_admission();
