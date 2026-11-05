SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE OR REPLACE FUNCTION workforce_posting_proposal_context(t uuid,h uuid,s uuid,writing boolean)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT COALESCE(workforce_scoped_posting_context(t,h,s,writing)
 AND ((current_setting('app.workforce_posting_review_mode',true) IN ('read','submit','approve','reject','withdraw')
 AND ((NOT writing AND current_setting('app.workforce_posting_permission',true) IN ('workforce.posting.proposal.read','workforce.posting.submit','workforce.posting.review'))
 OR (writing AND ((current_setting('app.workforce_posting_review_mode',true) IN ('submit','withdraw')
 AND current_setting('app.workforce_posting_permission',true)='workforce.posting.submit')
 OR (current_setting('app.workforce_posting_review_mode',true) IN ('approve','reject')
 AND current_setting('app.workforce_posting_permission',true)='workforce.posting.review')))))
 OR (NOT writing AND current_setting('app.workforce_posting_review_mode',true)='source_use'
 AND current_setting('app.workforce_posting_permission',true)='workforce.posting_source.use')),false)
$$;
