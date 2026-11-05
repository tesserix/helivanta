SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

ALTER TABLE workforce_posting_proposal_revisions
 ADD COLUMN supervision_requested boolean NOT NULL DEFAULT false,
 ADD COLUMN creation_transaction bigint;
ALTER TABLE workforce_posting_proposal_revisions ALTER COLUMN creation_transaction SET DEFAULT txid_current();

CREATE TABLE workforce_posting_supervision_intents (
 tenant_id uuid NOT NULL,hospital_id uuid NOT NULL,staff_id uuid NOT NULL,proposal_id uuid NOT NULL,
 initial_revision_id uuid NOT NULL,
 kind text NOT NULL CHECK(kind IN ('hospital_management','department_supervisor','department_member')),
 supervisor_staff_id uuid,supervisor_proposal_id uuid,supervisor_revision_id uuid,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(tenant_id,hospital_id,staff_id,proposal_id),
 FOREIGN KEY(tenant_id,hospital_id,staff_id,proposal_id) REFERENCES workforce_posting_proposals(tenant_id,hospital_id,staff_id,id) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,initial_revision_id) REFERENCES workforce_posting_proposal_revisions(tenant_id,id) ON DELETE RESTRICT,
 CHECK(CASE WHEN kind='hospital_management' THEN supervisor_staff_id IS NULL AND supervisor_proposal_id IS NULL AND supervisor_revision_id IS NULL
 ELSE supervisor_staff_id IS NOT NULL AND supervisor_staff_id<>'00000000-0000-0000-0000-000000000000'::uuid
 AND supervisor_proposal_id IS NOT NULL AND substring(supervisor_proposal_id::text,15,1)='7' AND substring(supervisor_proposal_id::text,20,1) IN ('8','9','a','b')
 AND supervisor_revision_id IS NOT NULL AND substring(supervisor_revision_id::text,15,1)='7' AND substring(supervisor_revision_id::text,20,1) IN ('8','9','a','b') END)
);
ALTER TABLE workforce_posting_supervision_intents ENABLE ROW LEVEL SECURITY;
ALTER TABLE workforce_posting_supervision_intents FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON workforce_posting_supervision_intents
 USING(hms_tenant_visible(tenant_id) AND workforce_posting_proposal_context(tenant_id,hospital_id,staff_id,false))
 WITH CHECK(workforce_posting_proposal_context(tenant_id,hospital_id,staff_id,true));

CREATE FUNCTION workforce_posting_supervision_intent_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT workforce_posting_proposal_context(NEW.tenant_id,NEW.hospital_id,NEW.staff_id,true)
 OR current_setting('app.workforce_posting_review_mode',true) IS DISTINCT FROM 'submit' THEN
  RAISE EXCEPTION 'posting supervision requires current scoped submission context' USING ERRCODE='42501';
 END IF;
 IF NOT EXISTS(SELECT 1 FROM workforce_posting_proposal_revisions r
 WHERE (r.tenant_id,r.hospital_id,r.staff_id,r.proposal_id,r.id,r.version)=
 (NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.proposal_id,NEW.initial_revision_id,1)
 AND r.supervision_requested AND r.creation_transaction=txid_current()
 AND r.actor_subject=current_setting('app.workforce_subject',true)) THEN
  RAISE EXCEPTION 'posting supervision must accompany its initial submission' USING ERRCODE='23514',CONSTRAINT='workforce_posting_supervision_guard';
 END IF;
 NEW.created_at:=clock_timestamp();
 RETURN NEW;
END $$;
CREATE TRIGGER workforce_posting_supervision_intent_guard BEFORE INSERT ON workforce_posting_supervision_intents FOR EACH ROW EXECUTE FUNCTION workforce_posting_supervision_intent_guard();
CREATE TRIGGER workforce_posting_supervision_intent_preserve BEFORE UPDATE OR DELETE OR TRUNCATE ON workforce_posting_supervision_intents FOR EACH STATEMENT EXECUTE FUNCTION workforce_prevent_removal();

CREATE FUNCTION workforce_posting_supervision_integrity() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.supervision_requested AND (NEW.version<>1 OR NEW.creation_transaction IS DISTINCT FROM txid_current()
 OR NOT EXISTS(SELECT 1 FROM workforce_posting_supervision_intents i
 WHERE (i.tenant_id,i.hospital_id,i.staff_id,i.proposal_id,i.initial_revision_id)=
 (NEW.tenant_id,NEW.hospital_id,NEW.staff_id,NEW.proposal_id,NEW.id))) THEN
  RAISE EXCEPTION 'posting submission must retain its supervision intent' USING ERRCODE='23514',CONSTRAINT='workforce_posting_supervision_guard';
 END IF;
 IF NEW.creation_transaction IS DISTINCT FROM txid_current() THEN
  RAISE EXCEPTION 'posting revision requires its creation transaction' USING ERRCODE='23514',CONSTRAINT='workforce_posting_supervision_guard';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER workforce_posting_supervision_integrity AFTER INSERT ON workforce_posting_proposal_revisions DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION workforce_posting_supervision_integrity();
