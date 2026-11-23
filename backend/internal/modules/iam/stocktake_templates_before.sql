SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
DO $before$
BEGIN
 IF iam_assignment_governance_integrity()IS DISTINCT FROM true
 OR iam_operating_governance_integrity()IS DISTINCT FROM true
 OR iam_staff_selection_integrity()IS DISTINCT FROM true THEN
 RAISE EXCEPTION 'stocktake templates require unchanged prior IAM authority';
 END IF;
 PERFORM set_config('migration.stocktake_prior_template_digest',md5(pg_get_functiondef('iam_operating_template_permissions(text,text)'::regprocedure)),true);
END $before$;
