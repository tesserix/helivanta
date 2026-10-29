SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';
-- One row per verification step (#1416). The declaration stays immutable;
-- the latest row is the affiliation's current status.
CREATE TABLE pharmacy_affiliation_verifications (
 id uuid NOT NULL CHECK (substring(id::text,15,1)='7'),
 tenant_id uuid NOT NULL,
 affiliation_id uuid NOT NULL,
 sequence integer NOT NULL CHECK (sequence >= 1),
 kind text NOT NULL CHECK (kind IN ('requested','accepted','declined','revoked','withdrawn')),
 request_id uuid NOT NULL CHECK (substring(request_id::text,15,1)='7'),
 decision_id uuid CHECK (decision_id IS NULL OR substring(decision_id::text,15,1)='7'),
 actor_subject text CHECK (actor_subject IS NULL OR length(btrim(actor_subject)) BETWEEN 1 AND 200),
 reason text CHECK (reason IS NULL OR (reason=btrim(reason) AND length(reason) BETWEEN 1 AND 500)),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY (tenant_id,id),
 UNIQUE (tenant_id,affiliation_id,sequence),
 UNIQUE (tenant_id,decision_id),
 FOREIGN KEY (tenant_id,affiliation_id) REFERENCES pharmacy_hospital_affiliations (tenant_id,id),
 -- Pharmacy steps are attributed to the pharmacy actor; hospital steps to
 -- patientmaster's decision receipt, where the reviewer is recorded.
 CONSTRAINT pharmacy_affiliation_verification_attribution CHECK (
  (kind IN ('requested','withdrawn') AND actor_subject IS NOT NULL AND decision_id IS NULL)
  OR (kind IN ('accepted','declined','revoked') AND actor_subject IS NULL AND decision_id IS NOT NULL)),
 CONSTRAINT pharmacy_affiliation_verification_reason CHECK ((kind='withdrawn') = (reason IS NOT NULL))
);
-- A round id names exactly one request, tenant-wide.
CREATE UNIQUE INDEX pharmacy_affiliation_request_once ON pharmacy_affiliation_verifications (tenant_id,request_id) WHERE kind='requested';
CREATE INDEX pharmacy_affiliation_verification_page ON pharmacy_affiliation_verifications (tenant_id,affiliation_id,created_at DESC,id DESC);
ALTER TABLE pharmacy_affiliation_verifications ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_affiliation_verifications FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_affiliation_verifications USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid) WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid);

-- The lifecycle is enforced here, not only in Go: a step must follow the
-- latest one, in the same round, with the next number. Two concurrent steps
-- read the same latest row and collide on the sequence unique key.
CREATE FUNCTION pharmacy_affiliation_verification_step() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE prev record;
BEGIN
 SELECT kind,request_id,sequence INTO prev FROM pharmacy_affiliation_verifications
  WHERE tenant_id=NEW.tenant_id AND affiliation_id=NEW.affiliation_id ORDER BY sequence DESC LIMIT 1;
 IF NEW.sequence <> COALESCE(prev.sequence,0)+1 THEN
  RAISE EXCEPTION 'affiliation verification steps are consecutive' USING ERRCODE='23514';
 END IF;
 IF NEW.kind='requested' THEN
  IF prev.kind IS NOT NULL AND prev.kind NOT IN ('declined','revoked','withdrawn') THEN
   RAISE EXCEPTION 'a verification round is already open' USING ERRCODE='23514';
  END IF;
 ELSIF prev.kind IS NULL OR prev.request_id <> NEW.request_id
  OR (NEW.kind IN ('accepted','declined') AND prev.kind <> 'requested')
  OR (NEW.kind IN ('revoked','withdrawn') AND prev.kind NOT IN ('requested','accepted')) THEN
  RAISE EXCEPTION 'invalid affiliation verification step' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER pharmacy_affiliation_verification_step BEFORE INSERT ON pharmacy_affiliation_verifications FOR EACH ROW EXECUTE FUNCTION pharmacy_affiliation_verification_step();
CREATE TRIGGER pharmacy_affiliation_verification_audit AFTER INSERT ON pharmacy_affiliation_verifications FOR EACH ROW EXECUTE FUNCTION audit_record_change();
CREATE TRIGGER pharmacy_affiliation_verification_immutable BEFORE UPDATE OR DELETE OR TRUNCATE ON pharmacy_affiliation_verifications FOR EACH STATEMENT EXECUTE FUNCTION pharmacy_catalogue_immutable();

-- The round's latest pharmacy step, re-sent until patientmaster confirms
-- it (#1416): the bus dead-letters after five failures and nothing replays
-- the dead-letter subject, so a lost withdrawal would otherwise last.
CREATE TABLE pharmacy_affiliation_deliveries (
 tenant_id uuid NOT NULL,
 request_id uuid NOT NULL,
 affiliation_id uuid NOT NULL,
 state text NOT NULL CHECK (state IN ('requested','withdrawn')),
 attempts integer NOT NULL DEFAULT 0 CHECK (attempts >= 0),
 -- NULL once the current state is confirmed.
 next_attempt_at timestamptz,
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY (tenant_id,request_id),
 FOREIGN KEY (tenant_id,affiliation_id) REFERENCES pharmacy_hospital_affiliations (tenant_id,id)
);
CREATE INDEX pharmacy_affiliation_deliveries_due ON pharmacy_affiliation_deliveries (next_attempt_at) WHERE next_attempt_at IS NOT NULL;
ALTER TABLE pharmacy_affiliation_deliveries ENABLE ROW LEVEL SECURITY;
ALTER TABLE pharmacy_affiliation_deliveries FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON pharmacy_affiliation_deliveries USING (hms_tenant_visible(tenant_id) AND tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid) WITH CHECK (tenant_id=NULLIF(current_setting('app.tenant_id',true),'')::uuid);
-- The reconciler's one cross-tenant read: which rounds are due, nothing else.
GRANT SELECT (tenant_id, request_id, next_attempt_at) ON pharmacy_affiliation_deliveries TO helivanta_system;
