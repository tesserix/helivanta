SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- A pharmacy's request that this hospital verify its affiliation (#1416).
-- The claim is the pharmacy's and never changes; status follows decisions.
CREATE TABLE patient_pharmacy_affiliation_reviews (
 tenant_id uuid NOT NULL,
 request_id uuid NOT NULL CHECK (substring(request_id::text,15,1)='7'),
 id uuid GENERATED ALWAYS AS (request_id) STORED,
 affiliation_id uuid NOT NULL,
 pharmacy_id uuid NOT NULL,
 pharmacy_name text NOT NULL CHECK (length(btrim(pharmacy_name)) BETWEEN 1 AND 200),
 hospital_id uuid NOT NULL CHECK (hospital_id <> '00000000-0000-0000-0000-000000000000'::uuid),
 relationship text NOT NULL CHECK (relationship IN ('owner','serving','partner')),
 requester_subject text NOT NULL CHECK (length(btrim(requester_subject)) BETWEEN 1 AND 200),
 status text NOT NULL CHECK (status IN ('pending','accepted','declined','revoked','withdrawn')),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY (tenant_id,request_id),
 UNIQUE (tenant_id,hospital_id,request_id)
);
CREATE INDEX patient_pharmacy_affiliation_review_page ON patient_pharmacy_affiliation_reviews (tenant_id,hospital_id,created_at DESC,id DESC);
-- The lookup in #1416b asks whether a pharmacy currently holds an accepted link.
CREATE INDEX patient_pharmacy_affiliation_accepted ON patient_pharmacy_affiliation_reviews (tenant_id,hospital_id,pharmacy_id) WHERE status='accepted';
ALTER TABLE patient_pharmacy_affiliation_reviews ENABLE ROW LEVEL SECURITY;
ALTER TABLE patient_pharmacy_affiliation_reviews FORCE ROW LEVEL SECURITY;
CREATE FUNCTION patient_pharmacy_affiliation_delivery_allowed(row_tenant uuid,row_hospital uuid,row_request uuid) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT row_tenant=NULLIF(current_setting('app.tenant_id',true),'')::uuid
 AND row_hospital=NULLIF(current_setting('app.pharmacy_affiliation_hospital',true),'')::uuid
 AND row_request=NULLIF(current_setting('app.pharmacy_affiliation_request',true),'')::uuid
 AND current_setting('app.actor_subject',true) IN ('system:patient-pharmacy-affiliation-request','system:patient-pharmacy-affiliation-withdrawal')
$$;
CREATE POLICY tenant_isolation ON patient_pharmacy_affiliation_reviews
 USING (hms_tenant_visible(tenant_id) AND (patient_hospital_allowed(tenant_id,hospital_id,true) OR patient_pharmacy_affiliation_delivery_allowed(tenant_id,hospital_id,request_id)))
 WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND (patient_hospital_allowed(tenant_id,hospital_id,true) OR patient_pharmacy_affiliation_delivery_allowed(tenant_id,hospital_id,request_id)));

-- Every accept, decline and revoke, with who decided and why. Append-only.
CREATE TABLE patient_pharmacy_affiliation_decisions (
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 tenant_id uuid NOT NULL,
 hospital_id uuid NOT NULL,
 request_id uuid NOT NULL,
 outcome text NOT NULL CHECK (outcome IN ('accepted','declined','revoked')),
 reason text NOT NULL CHECK (reason=btrim(reason) AND length(reason) BETWEEN 1 AND 500),
 fingerprint text NOT NULL CHECK (length(fingerprint)=64),
 reviewer_subject text NOT NULL CHECK (length(btrim(reviewer_subject)) BETWEEN 1 AND 200),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY (tenant_id,id),
 -- One acceptance or decline, and at most one revocation, per round.
 UNIQUE (tenant_id,request_id,outcome),
 FOREIGN KEY (tenant_id,hospital_id,request_id) REFERENCES patient_pharmacy_affiliation_reviews (tenant_id,hospital_id,request_id)
);
ALTER TABLE patient_pharmacy_affiliation_decisions ENABLE ROW LEVEL SECURITY;
ALTER TABLE patient_pharmacy_affiliation_decisions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON patient_pharmacy_affiliation_decisions
 USING (hms_tenant_visible(tenant_id) AND patient_hospital_allowed(tenant_id,hospital_id,true))
 WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid AND patient_hospital_allowed(tenant_id,hospital_id,true));

-- A decision is the session's own, by a registrar of that hospital, from a
-- state that allows it, and never an acceptance by the person who asked.
-- It moves the review's status in the same statement, so status cannot
-- drift from the decision history.
CREATE FUNCTION patient_pharmacy_affiliation_decide() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE review record;
BEGIN
 IF NEW.reviewer_subject IS DISTINCT FROM current_setting('app.actor_subject',true) OR NOT patient_hospital_allowed(NEW.tenant_id,NEW.hospital_id,true) THEN
  RAISE EXCEPTION 'registrar authority required' USING ERRCODE='23514';
 END IF;
 SELECT status,requester_subject INTO review FROM patient_pharmacy_affiliation_reviews
  WHERE tenant_id=NEW.tenant_id AND hospital_id=NEW.hospital_id AND request_id=NEW.request_id FOR UPDATE;
 IF review.status IS NULL
  OR (NEW.outcome IN ('accepted','declined') AND review.status<>'pending')
  OR (NEW.outcome='revoked' AND review.status<>'accepted')
  OR (NEW.outcome='accepted' AND review.requester_subject=NEW.reviewer_subject) THEN
  RAISE EXCEPTION 'pharmacy affiliation decision not allowed' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER patient_pharmacy_affiliation_decide BEFORE INSERT ON patient_pharmacy_affiliation_decisions FOR EACH ROW EXECUTE FUNCTION patient_pharmacy_affiliation_decide();
-- AFTER, so the review guard below can see the decision it is moving to.
CREATE FUNCTION patient_pharmacy_affiliation_decided() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 UPDATE patient_pharmacy_affiliation_reviews SET status=NEW.outcome,updated_at=clock_timestamp()
  WHERE tenant_id=NEW.tenant_id AND hospital_id=NEW.hospital_id AND request_id=NEW.request_id;
 RETURN NULL;
END $$;
CREATE TRIGGER patient_pharmacy_affiliation_decided AFTER INSERT ON patient_pharmacy_affiliation_decisions FOR EACH ROW EXECUTE FUNCTION patient_pharmacy_affiliation_decided();
CREATE FUNCTION patient_pharmacy_affiliation_history() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION 'pharmacy affiliation decisions are retained' USING ERRCODE='23514';
END $$;
CREATE TRIGGER patient_pharmacy_affiliation_decision_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON patient_pharmacy_affiliation_decisions FOR EACH STATEMENT EXECUTE FUNCTION patient_pharmacy_affiliation_history();
CREATE TRIGGER patient_pharmacy_affiliation_decision_audit AFTER INSERT ON patient_pharmacy_affiliation_decisions FOR EACH ROW EXECUTE FUNCTION audit_record_change();

-- The review row: claim fixed, status only along the lifecycle, and a
-- decided status only with its decision row.
CREATE FUNCTION patient_pharmacy_affiliation_review_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'pharmacy affiliation reviews are retained' USING ERRCODE='23514'; END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.status NOT IN ('pending','withdrawn') THEN RAISE EXCEPTION 'review must begin undecided' USING ERRCODE='23514'; END IF;
  RETURN NEW;
 END IF;
 IF (NEW.tenant_id,NEW.request_id,NEW.affiliation_id,NEW.pharmacy_id,NEW.pharmacy_name,NEW.hospital_id,NEW.relationship,NEW.requester_subject,NEW.created_at)
  IS DISTINCT FROM (OLD.tenant_id,OLD.request_id,OLD.affiliation_id,OLD.pharmacy_id,OLD.pharmacy_name,OLD.hospital_id,OLD.relationship,OLD.requester_subject,OLD.created_at)
  OR NOT ((OLD.status='pending' AND NEW.status IN ('accepted','declined','withdrawn'))
   OR (OLD.status='accepted' AND NEW.status IN ('revoked','withdrawn')))
  OR (NEW.status IN ('accepted','declined','revoked') AND NOT EXISTS (SELECT 1 FROM patient_pharmacy_affiliation_decisions d
   WHERE d.tenant_id=NEW.tenant_id AND d.request_id=NEW.request_id AND d.outcome=NEW.status)) THEN
  RAISE EXCEPTION 'pharmacy affiliation review change not allowed' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER patient_pharmacy_affiliation_review_guard BEFORE INSERT OR UPDATE OR DELETE ON patient_pharmacy_affiliation_reviews FOR EACH ROW EXECUTE FUNCTION patient_pharmacy_affiliation_review_guard();
CREATE TRIGGER patient_pharmacy_affiliation_review_no_truncate BEFORE TRUNCATE ON patient_pharmacy_affiliation_reviews FOR EACH STATEMENT EXECUTE FUNCTION patient_pharmacy_affiliation_history();
CREATE TRIGGER patient_pharmacy_affiliation_review_audit AFTER INSERT OR UPDATE ON patient_pharmacy_affiliation_reviews FOR EACH ROW EXECUTE FUNCTION audit_record_change();
