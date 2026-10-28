SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
CREATE TABLE mediconnect_notices (
 tenant_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 purpose text NOT NULL CHECK(purpose IN ('account_processing','communications','analytics')),
 locale text NOT NULL CHECK(length(locale) BETWEEN 2 AND 35),
 version_label text NOT NULL CHECK(length(btrim(version_label)) BETWEEN 1 AND 64),
 title text NOT NULL CHECK(length(btrim(title)) BETWEEN 1 AND 200),
 text text NOT NULL CHECK(octet_length(text) BETWEEN 1 AND 8192 AND length(btrim(text))>0),
 content_hash text NOT NULL CHECK(content_hash ~ '^[0-9a-f]{64}$'),
 review_reference uuid NOT NULL CHECK(substring(review_reference::text,15,1)='7'),
 publisher_subject text NOT NULL CHECK(length(publisher_subject) BETWEEN 1 AND 512),
 status text NOT NULL DEFAULT 'published',
 version bigint NOT NULL DEFAULT 1,
 retired_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,id),
 UNIQUE(tenant_id,purpose,locale,version_label),
 UNIQUE(tenant_id,id,purpose,locale,version_label,content_hash),
 CHECK((status='published' AND version=1 AND retired_at IS NULL) OR (status='retired' AND version=2 AND retired_at IS NOT NULL))
);
CREATE INDEX mediconnect_notices_page ON mediconnect_notices(tenant_id,status,created_at DESC,id DESC);
ALTER TABLE mediconnect_notices ENABLE ROW LEVEL SECURITY;
ALTER TABLE mediconnect_notices FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON mediconnect_notices
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid)
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND current_setting('app.mediconnect_notice_write',true)='true');
CREATE FUNCTION mediconnect_notice_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'notice history is retained' USING ERRCODE='23514'; END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.status<>'published' OR NEW.publisher_subject IS DISTINCT FROM current_setting('app.actor_subject',true) THEN RAISE EXCEPTION 'invalid notice publication' USING ERRCODE='23514'; END IF;
 ELSE
  IF (NEW.tenant_id,NEW.id,NEW.purpose,NEW.locale,NEW.version_label,NEW.title,NEW.text,NEW.content_hash,NEW.review_reference,NEW.publisher_subject,NEW.created_at)
  IS DISTINCT FROM (OLD.tenant_id,OLD.id,OLD.purpose,OLD.locale,OLD.version_label,OLD.title,OLD.text,OLD.content_hash,OLD.review_reference,OLD.publisher_subject,OLD.created_at)
  OR (OLD.status='retired' AND (NEW.status,NEW.version,NEW.retired_at) IS DISTINCT FROM (OLD.status,OLD.version,OLD.retired_at)) THEN
   RAISE EXCEPTION 'notice content is immutable' USING ERRCODE='23514'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER mediconnect_notice_guard BEFORE INSERT OR UPDATE OR DELETE ON mediconnect_notices FOR EACH ROW EXECUTE FUNCTION mediconnect_notice_guard();
CREATE TRIGGER mediconnect_notice_audit AFTER INSERT OR UPDATE ON mediconnect_notices FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TABLE mediconnect_account_consents (
 tenant_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 account_id uuid NOT NULL,
 subject text NOT NULL,
 notice_id uuid NOT NULL,
 purpose text NOT NULL,
 locale text NOT NULL,
 version_label text NOT NULL,
 content_hash text NOT NULL,
 status text NOT NULL DEFAULT 'active',
 version bigint NOT NULL DEFAULT 1,
 withdrawn_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(tenant_id,id),
 FOREIGN KEY(tenant_id,account_id,subject) REFERENCES mediconnect_accounts(tenant_id,id,subject),
 FOREIGN KEY(tenant_id,notice_id,purpose,locale,version_label,content_hash) REFERENCES mediconnect_notices(tenant_id,id,purpose,locale,version_label,content_hash),
 CHECK((status='active' AND version=1 AND withdrawn_at IS NULL) OR (status='withdrawn' AND version=2 AND withdrawn_at IS NOT NULL))
);
CREATE UNIQUE INDEX mediconnect_consent_active_purpose ON mediconnect_account_consents(tenant_id,account_id,purpose) WHERE status='active';
CREATE INDEX mediconnect_consent_page ON mediconnect_account_consents(tenant_id,account_id,subject,created_at DESC,id DESC);
CREATE INDEX mediconnect_consent_notice ON mediconnect_account_consents(tenant_id,notice_id,purpose,locale,version_label,content_hash);
ALTER TABLE mediconnect_account_consents ENABLE ROW LEVEL SECURITY;
ALTER TABLE mediconnect_account_consents FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON mediconnect_account_consents
 USING(hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND subject=NULLIF(current_setting('app.actor_subject',true),''))
 WITH CHECK(tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND subject=NULLIF(current_setting('app.actor_subject',true),''));
CREATE FUNCTION mediconnect_consent_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'consent history is retained' USING ERRCODE='23514'; END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.status<>'active' OR NOT EXISTS(SELECT 1 FROM mediconnect_notices WHERE tenant_id=NEW.tenant_id AND id=NEW.notice_id AND status='published') THEN RAISE EXCEPTION 'notice is not published' USING ERRCODE='23514'; END IF;
 ELSE
  IF (NEW.tenant_id,NEW.id,NEW.account_id,NEW.subject,NEW.notice_id,NEW.purpose,NEW.locale,NEW.version_label,NEW.content_hash,NEW.created_at)
  IS DISTINCT FROM (OLD.tenant_id,OLD.id,OLD.account_id,OLD.subject,OLD.notice_id,OLD.purpose,OLD.locale,OLD.version_label,OLD.content_hash,OLD.created_at)
  OR (OLD.status='withdrawn' AND (NEW.status,NEW.version,NEW.withdrawn_at) IS DISTINCT FROM (OLD.status,OLD.version,OLD.withdrawn_at)) THEN
   RAISE EXCEPTION 'consent history is immutable' USING ERRCODE='23514'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER mediconnect_consent_guard BEFORE INSERT OR UPDATE OR DELETE ON mediconnect_account_consents FOR EACH ROW EXECUTE FUNCTION mediconnect_consent_guard();
CREATE TRIGGER mediconnect_consent_audit AFTER INSERT OR UPDATE ON mediconnect_account_consents FOR EACH ROW EXECUTE FUNCTION audit_record_change();
