SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE TABLE iam_partner_grant_drafts (
 tenant_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7' AND substring(id::text,20,1) IN('8','9','a','b')),
 input_hash text NOT NULL CHECK(input_hash ~ '^[a-f0-9]{64}$'),
 proposer_membership_epoch bigint NOT NULL CHECK(proposer_membership_epoch>0),
 document jsonb NOT NULL CHECK(document ?& ARRAY['id','owner_tenant_id','status','operating_access_enabled','input','prepared_by','created_at'] AND jsonb_typeof(document)='object'
  AND document->>'id'=id::text AND document->>'owner_tenant_id'=tenant_id::text
  AND document->>'status'='pending_verification'
  AND document->'operating_access_enabled'='false'::jsonb
  AND jsonb_typeof(document->'input')='object'
  AND char_length(document->>'prepared_by') BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,id)
);
ALTER TABLE iam_partner_grant_drafts ENABLE ROW LEVEL SECURITY;
ALTER TABLE iam_partner_grant_drafts FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON iam_partner_grant_drafts
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.iam_partner_grant_drafts',true) IN('read','write'))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.iam_partner_grant_drafts',true)='write' AND document->>'prepared_by'=NULLIF(current_setting('app.actor_subject',true),''));
CREATE INDEX ON iam_partner_grant_drafts(tenant_id,created_at DESC,id DESC);
CREATE TRIGGER iam_partner_grant_draft_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON iam_partner_grant_drafts FOR EACH STATEMENT EXECUTE FUNCTION iam_partner_group_preserve();
