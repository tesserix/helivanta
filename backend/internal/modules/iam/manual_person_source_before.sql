SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
DO $before$
BEGIN
 IF iam_assignment_governance_integrity() IS DISTINCT FROM true
 OR iam_operating_governance_integrity() IS DISTINCT FROM true
 OR iam_staff_selection_integrity() IS DISTINCT FROM true THEN
  RAISE EXCEPTION 'manual identity source requires unchanged IAM authority';
 END IF;
END $before$;

