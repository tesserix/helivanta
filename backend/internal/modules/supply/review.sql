SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

CREATE TABLE supply_license_reviews (
 tenant_id uuid NOT NULL,party_id uuid NOT NULL,license_id uuid NOT NULL,
 id uuid NOT NULL CHECK(substring(id::text,15,1)='7'),
 version bigint NOT NULL CHECK(version BETWEEN 1 AND 9007199254740991),
 party_version bigint NOT NULL,premise_id uuid NOT NULL,premise_version bigint NOT NULL,license_version bigint NOT NULL,evidence_id uuid NOT NULL,
 action text NOT NULL CHECK(action IN('submit','verify','reject','suspend')),
 status text NOT NULL CHECK((action='submit' AND status='submitted') OR(action='verify' AND status='verified') OR(action='reject' AND status='rejected') OR(action='suspend' AND status='suspended')),
 reason text NOT NULL CHECK(length(btrim(reason)) BETWEEN 5 AND 2000 AND reason !~ '[[:cntrl:]]'),
 method text NOT NULL CHECK(method IN('not_applicable','authority_portal','authority_contact','physical_inspection')),
 reference text NOT NULL CHECK(length(reference)<=300 AND reference !~ '[[:cntrl:]]'),
 actor_subject text NOT NULL CHECK(length(btrim(actor_subject)) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT now(),updated_at timestamptz NOT NULL DEFAULT now(),
 CHECK((action IN('submit','suspend') AND method='not_applicable' AND reference='') OR(action IN('verify','reject') AND method<>'not_applicable' AND length(btrim(reference))>0)),
 PRIMARY KEY(tenant_id,party_id,license_id,version),UNIQUE(tenant_id,id),
 FOREIGN KEY(tenant_id,party_id,party_version) REFERENCES supply_party_revisions(tenant_id,party_id,version) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,party_id,premise_id,premise_version) REFERENCES supply_premise_revisions(tenant_id,party_id,premise_id,version) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,party_id,license_id,license_version) REFERENCES supply_license_revisions(tenant_id,party_id,license_id,version) ON DELETE RESTRICT,
 FOREIGN KEY(tenant_id,party_id,license_id,evidence_id) REFERENCES supply_license_evidence(tenant_id,party_id,license_id,id) ON DELETE RESTRICT
);
ALTER TABLE supply_license_reviews ENABLE ROW LEVEL SECURITY;
ALTER TABLE supply_license_reviews FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON supply_license_reviews
 USING(hms_tenant_visible(tenant_id) AND supply_roles_allowed(tenant_id,party_id,ARRAY['administrator','purchasing','finance','compliance','auditor']))
 WITH CHECK(supply_roles_allowed(tenant_id,party_id,CASE WHEN action='submit' THEN ARRAY['administrator','purchasing','compliance'] ELSE ARRAY['compliance'] END));
CREATE INDEX supply_license_reviews_page ON supply_license_reviews(tenant_id,party_id,license_id,created_at DESC,id DESC);
CREATE TRIGGER supply_license_reviews_retain BEFORE UPDATE OR DELETE OR TRUNCATE ON supply_license_reviews FOR EACH STATEMENT EXECUTE FUNCTION supply_history_immutable();
CREATE TRIGGER supply_license_reviews_audit AFTER INSERT ON supply_license_reviews FOR EACH ROW EXECUTE FUNCTION audit_record_change();

CREATE FUNCTION supply_license_review_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE licence supply_licenses;location supply_premises;organisation supply_parties;
 proof supply_license_evidence;previous supply_license_reviews;makers text[];local_day date;
BEGIN
 IF NEW.actor_subject IS DISTINCT FROM NULLIF(current_setting('app.actor_subject',true),'')
 OR NEW.actor_subject IS DISTINCT FROM NULLIF(current_setting('app.supply_subject',true),'') THEN
  RAISE EXCEPTION 'review actor unavailable' USING ERRCODE='42501'; END IF;
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.tenant_id::text||':supply-license:'||NEW.party_id::text||':'||NEW.license_id::text,0));
 SELECT * INTO licence FROM supply_licenses WHERE tenant_id=NEW.tenant_id AND party_id=NEW.party_id AND id=NEW.license_id FOR SHARE;
 IF NOT FOUND THEN RAISE EXCEPTION 'review source unavailable' USING ERRCODE='42501'; END IF;
 SELECT * INTO organisation FROM supply_parties WHERE tenant_id=NEW.tenant_id AND id=NEW.party_id FOR SHARE;
 IF NOT FOUND THEN RAISE EXCEPTION 'review source unavailable' USING ERRCODE='42501'; END IF;
 SELECT * INTO location FROM supply_premises WHERE tenant_id=NEW.tenant_id AND party_id=NEW.party_id AND id=licence.premise_id FOR SHARE;
 IF NOT FOUND THEN RAISE EXCEPTION 'review source unavailable' USING ERRCODE='42501'; END IF;
 SELECT * INTO proof FROM supply_license_evidence WHERE tenant_id=NEW.tenant_id AND party_id=NEW.party_id AND license_id=NEW.license_id AND id=NEW.evidence_id FOR SHARE;
 IF NOT FOUND THEN RAISE EXCEPTION 'review evidence unavailable' USING ERRCODE='42501'; END IF;
 SELECT * INTO previous FROM supply_license_reviews WHERE tenant_id=NEW.tenant_id AND party_id=NEW.party_id AND license_id=NEW.license_id ORDER BY version DESC LIMIT 1;
 IF NEW.version<>COALESCE(previous.version,0)+1 THEN
  RAISE EXCEPTION 'review sequence conflict' USING ERRCODE='23514',CONSTRAINT='supply_review_conflict'; END IF;
 IF NEW.action='suspend' THEN
  IF previous.status IS DISTINCT FROM 'verified' OR
   (NEW.party_version,NEW.premise_id,NEW.premise_version,NEW.license_version,NEW.evidence_id)
   IS DISTINCT FROM(previous.party_version,previous.premise_id,previous.premise_version,previous.license_version,previous.evidence_id) THEN
   RAISE EXCEPTION 'review suspension conflict' USING ERRCODE='23514',CONSTRAINT='supply_review_conflict'; END IF;
 ELSE
  IF (NEW.party_version,NEW.premise_id,NEW.premise_version,NEW.license_version)
   IS DISTINCT FROM(organisation.version,location.id,location.version,licence.version)
   OR proof.license_version<>licence.version THEN
   RAISE EXCEPTION 'review source changed' USING ERRCODE='23514',CONSTRAINT='supply_review_conflict'; END IF;
  local_day:=(clock_timestamp() AT TIME ZONE location.time_zone)::date;
  IF NEW.action IN('submit','verify') AND
   (proof.status<>'available' OR location.status<>'active' OR licence.issued_on>local_day OR licence.expires_on<local_day) THEN
   RAISE EXCEPTION 'review prerequisites unavailable' USING ERRCODE='23514',CONSTRAINT='supply_review_conflict'; END IF;
  IF NEW.action='submit' THEN
   IF previous.status='submitted' AND
    (NEW.party_version,NEW.premise_version,NEW.license_version,NEW.evidence_id)
    IS NOT DISTINCT FROM(previous.party_version,previous.premise_version,previous.license_version,previous.evidence_id)
    OR previous.status='verified' AND
    (NEW.party_version,NEW.premise_version,NEW.license_version,NEW.evidence_id)
    IS NOT DISTINCT FROM(previous.party_version,previous.premise_version,previous.license_version,previous.evidence_id) THEN
    RAISE EXCEPTION 'review already submitted or verified' USING ERRCODE='23514',CONSTRAINT='supply_review_conflict'; END IF;
  ELSE
   IF previous.status IS DISTINCT FROM 'submitted' OR
    (NEW.party_version,NEW.premise_id,NEW.premise_version,NEW.license_version,NEW.evidence_id)
    IS DISTINCT FROM(previous.party_version,previous.premise_id,previous.premise_version,previous.license_version,previous.evidence_id) THEN
    RAISE EXCEPTION 'review submission changed' USING ERRCODE='23514',CONSTRAINT='supply_review_conflict'; END IF;
   SELECT array_agg(actor_subject) INTO makers FROM (
    SELECT actor_subject FROM supply_party_revisions WHERE tenant_id=NEW.tenant_id AND party_id=NEW.party_id AND version=NEW.party_version
    UNION ALL SELECT actor_subject FROM supply_premise_revisions WHERE tenant_id=NEW.tenant_id AND party_id=NEW.party_id AND premise_id=NEW.premise_id AND version=NEW.premise_version
    UNION ALL SELECT actor_subject FROM supply_license_revisions WHERE tenant_id=NEW.tenant_id AND party_id=NEW.party_id AND license_id=NEW.license_id AND version=NEW.license_version
   ) sources WHERE actor_subject IS NOT NULL AND btrim(actor_subject)<>'';
   -- Unavailable source authors cannot establish independence, including under RLS.
   IF cardinality(makers) IS DISTINCT FROM 3 OR NEW.actor_subject=ANY(makers)
    OR NEW.actor_subject IN(previous.actor_subject,proof.uploader_subject) THEN
    RAISE EXCEPTION 'independent reviewer required' USING ERRCODE='23514',CONSTRAINT='supply_review_conflict'; END IF;
  END IF;
 END IF;
 NEW.created_at:=clock_timestamp();NEW.updated_at:=NEW.created_at;
 RETURN NEW;
END $$;
CREATE TRIGGER supply_license_review_guard BEFORE INSERT ON supply_license_reviews FOR EACH ROW EXECUTE FUNCTION supply_license_review_guard();
