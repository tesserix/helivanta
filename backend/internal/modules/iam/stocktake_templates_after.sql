DO $after$
DECLARE prior text:=current_setting('migration.stocktake_prior_template_digest');
 fresh text:=md5(pg_get_functiondef('iam_operating_template_permissions(text,text)'::regprocedure));
 definition text:=pg_get_functiondef('iam_operating_governance_integrity()'::regprocedure);
 needle text;
BEGIN
 needle:=quote_literal(prior);
 IF length(prior)<>32 OR strpos(definition,quote_literal('iam_operating_template_permissions(text,text)'))=0
 OR(length(definition)-length(replace(definition,needle,'')))<>length(needle)THEN
 RAISE EXCEPTION 'stocktake template fingerprint is not uniquely captured';
 END IF;
 -- Preserve every other captured function, policy and trigger, including their expected digests.
 EXECUTE replace(definition,needle,quote_literal(fresh));
 IF iam_assignment_governance_integrity()IS DISTINCT FROM true
 OR iam_operating_governance_integrity()IS DISTINCT FROM true
 OR iam_staff_selection_integrity()IS DISTINCT FROM true THEN
 RAISE EXCEPTION 'stocktake template migration changed unrelated IAM authority';
 END IF;
 PERFORM set_config('migration.stocktake_prior_template_digest','',true);
END $after$;
