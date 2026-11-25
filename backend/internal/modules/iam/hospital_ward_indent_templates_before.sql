SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
DO $before$
DECLARE fingerprints jsonb;
BEGIN
 IF iam_assignment_governance_integrity()IS DISTINCT FROM true
 OR iam_operating_governance_integrity()IS DISTINCT FROM true
 OR iam_staff_selection_integrity()IS DISTINCT FROM true THEN
 RAISE EXCEPTION 'hospital ward indent templates require unchanged prior IAM authority';
 END IF;
 SELECT jsonb_object_agg(name,md5(pg_get_functiondef(name::regprocedure)))INTO fingerprints
 FROM unnest(ARRAY['iam_assignment_template_permissions(text)','iam_assignment_template_scope_kinds(text)','iam_assignment_template_establishment_kinds(text)'])name;
 PERFORM set_config('migration.hospital_ward_indent_prior_templates',fingerprints::text,true);
 PERFORM set_config('migration.hospital_ward_indent_prior_integrity',pg_get_functiondef('iam_assignment_governance_integrity()'::regprocedure),true);
END $before$;
