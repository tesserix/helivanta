DO $after$
DECLARE fingerprints jsonb:=current_setting('migration.hospital_stocktake_prior_templates')::jsonb;
 definition text:=current_setting('migration.hospital_stocktake_prior_integrity');
 item record;needle text;fresh text;
BEGIN
 IF (SELECT count(*)FROM jsonb_object_keys(fingerprints))<>3
 OR definition IS DISTINCT FROM pg_get_functiondef('iam_assignment_governance_integrity()'::regprocedure)THEN
 RAISE EXCEPTION 'hospital template migration changed the original integrity guard';
 END IF;
 FOR item IN SELECT key,value FROM jsonb_each_text(fingerprints)LOOP
  IF item.key NOT IN('iam_assignment_template_permissions(text)','iam_assignment_template_scope_kinds(text)','iam_assignment_template_establishment_kinds(text)')THEN
   RAISE EXCEPTION 'unexpected hospital template fingerprint';
  END IF;
  needle:=quote_literal(item.value);
  fresh:=md5(pg_get_functiondef(item.key::regprocedure));
  IF length(item.value)<>32 OR fresh=item.value OR strpos(definition,quote_literal(item.key))=0
  OR(length(definition)-length(replace(definition,needle,'')))<>length(needle)THEN
   RAISE EXCEPTION 'hospital template fingerprint is not uniquely captured';
  END IF;
  definition:=replace(definition,needle,quote_literal(fresh));
 END LOOP;
 -- Preserve every other expected function, policy and trigger fingerprint.
 EXECUTE definition;
 IF iam_assignment_governance_integrity()IS DISTINCT FROM true
 OR iam_operating_governance_integrity()IS DISTINCT FROM true
 OR iam_staff_selection_integrity()IS DISTINCT FROM true THEN
 RAISE EXCEPTION 'hospital template migration changed unrelated IAM authority';
 END IF;
 PERFORM set_config('migration.hospital_stocktake_prior_templates','',true);
 PERFORM set_config('migration.hospital_stocktake_prior_integrity','',true);
END $after$;
