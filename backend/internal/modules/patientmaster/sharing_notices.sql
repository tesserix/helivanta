SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE TABLE patient_sharing_notices (
 tenant_id uuid NOT NULL,
 hospital_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 purpose text NOT NULL CHECK(purpose='care_coordination'),
 locale text NOT NULL CHECK(length(locale) BETWEEN 2 AND 35),
 version_label text NOT NULL CHECK(length(btrim(version_label)) BETWEEN 1 AND 64),
 title text NOT NULL CHECK(length(btrim(title)) BETWEEN 1 AND 200),
 text text NOT NULL CHECK(length(btrim(text)) BETWEEN 1 AND 4096),
 withdrawal_consequence text NOT NULL CHECK(length(btrim(withdrawal_consequence)) BETWEEN 1 AND 1024),
 policy_reference uuid NOT NULL CHECK(substring(policy_reference::text,15,1)='7'),
 content_hash text NOT NULL CHECK(length(content_hash)=64),
 creator_subject text NOT NULL CHECK(length(btrim(creator_subject)) BETWEEN 1 AND 512),
 reviewer_subject text,
 review_reference uuid CHECK(substring(review_reference::text,15,1)='7'),
 status text NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','approved','retired')),
 version bigint NOT NULL DEFAULT 1,
 approved_at timestamptz,
 retired_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,id),
 UNIQUE(tenant_id,hospital_id,purpose,locale,version_label),
 CHECK(updated_at>=created_at),
 CHECK(approved_at IS NULL OR approved_at>=created_at),
 CHECK(retired_at IS NULL OR (retired_at>=created_at AND (approved_at IS NULL OR retired_at>=approved_at))),
 CHECK((status='draft' AND version=1 AND approved_at IS NULL AND retired_at IS NULL)
 OR (status='approved' AND version=2 AND approved_at IS NOT NULL AND retired_at IS NULL)
 OR (status='retired' AND version=CASE WHEN approved_at IS NULL THEN 2 ELSE 3 END AND retired_at IS NOT NULL)),
 CHECK((approved_at IS NULL AND reviewer_subject IS NULL AND review_reference IS NULL)
 OR (approved_at IS NOT NULL AND reviewer_subject IS NOT NULL AND length(btrim(reviewer_subject)) BETWEEN 1 AND 512 AND reviewer_subject<>creator_subject AND review_reference IS NOT NULL))
);
CREATE INDEX patient_sharing_notice_page ON patient_sharing_notices(tenant_id,hospital_id,created_at DESC,id DESC);
ALTER TABLE patient_sharing_notices ENABLE ROW LEVEL SECURITY;
ALTER TABLE patient_sharing_notices FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON patient_sharing_notices
 USING(hms_tenant_visible(tenant_id) AND patient_hospital_allowed(tenant_id,hospital_id,true))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND patient_hospital_allowed(tenant_id,hospital_id,true));
CREATE FUNCTION patient_sharing_notice_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'sharing notice history retained' USING ERRCODE='23514'; END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.status<>'draft' OR NEW.creator_subject IS DISTINCT FROM current_setting('app.actor_subject',true) THEN
   RAISE EXCEPTION 'attributable draft required' USING ERRCODE='23514';
  END IF;
 ELSE
  IF (NEW.tenant_id,NEW.hospital_id,NEW.id,NEW.purpose,NEW.locale,NEW.version_label,NEW.title,NEW.text,NEW.withdrawal_consequence,NEW.policy_reference,NEW.content_hash,NEW.creator_subject,NEW.created_at)
  IS DISTINCT FROM (OLD.tenant_id,OLD.hospital_id,OLD.id,OLD.purpose,OLD.locale,OLD.version_label,OLD.title,OLD.text,OLD.withdrawal_consequence,OLD.policy_reference,OLD.content_hash,OLD.creator_subject,OLD.created_at)
  OR NEW.version<>OLD.version+1 OR OLD.status='retired' THEN
   RAISE EXCEPTION 'sharing notice lineage immutable' USING ERRCODE='23514';
  END IF;
  IF NEW.status='approved' THEN
   IF OLD.status<>'draft' OR NEW.reviewer_subject IS DISTINCT FROM current_setting('app.actor_subject',true) OR NEW.reviewer_subject=OLD.creator_subject THEN
    RAISE EXCEPTION 'independent notice review required' USING ERRCODE='23514';
   END IF;
  ELSIF NEW.status='retired' THEN
   IF (NEW.reviewer_subject,NEW.review_reference,NEW.approved_at) IS DISTINCT FROM (OLD.reviewer_subject,OLD.review_reference,OLD.approved_at) THEN
    RAISE EXCEPTION 'notice review retained' USING ERRCODE='23514';
   END IF;
  ELSE RAISE EXCEPTION 'invalid notice transition' USING ERRCODE='23514';
  END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER patient_sharing_notice_guard BEFORE INSERT OR UPDATE OR DELETE ON patient_sharing_notices FOR EACH ROW EXECUTE FUNCTION patient_sharing_notice_guard();
CREATE TRIGGER patient_sharing_notice_audit AFTER INSERT OR UPDATE ON patient_sharing_notices FOR EACH ROW EXECUTE FUNCTION audit_record_change();
