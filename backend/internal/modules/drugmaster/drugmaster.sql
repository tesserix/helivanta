SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='60s';

-- The drug master (#476). Global reference data: no tenant_id, on
-- tenantdb's lintAllowlist (spec D1). The app may only read rows of
-- published releases. Content arrives only as a release staged by
-- helivanta_drug_stager and decided by helivanta_drug_approver (D3), and
-- no row is ever updated or deleted: the version in force on a date is the
-- latest published one whose valid_from is on or before it (D4).

CREATE SEQUENCE drugmaster_release_sequence;

CREATE TABLE drugmaster_releases (
 id uuid PRIMARY KEY CHECK (substring(id::text,15,1)='7'),
 country text NOT NULL CHECK (country='IN'),
 source_name text NOT NULL CHECK (length(btrim(source_name)) BETWEEN 1 AND 200),
 licence_ref text NOT NULL CHECK (length(btrim(licence_ref)) BETWEEN 1 AND 200),
 source_version text NOT NULL CHECK (length(btrim(source_version)) BETWEEN 1 AND 100),
 file_sha256 text NOT NULL UNIQUE CHECK (file_sha256 ~ '^[0-9a-f]{64}$'),
 staged_by text NOT NULL CHECK (length(btrim(staged_by)) BETWEEN 1 AND 200),
 staged_at timestamptz NOT NULL DEFAULT now(),
 status text NOT NULL DEFAULT 'staged' CHECK (status IN ('staged','published','rejected')),
 decided_by text CHECK (decided_by IS NULL OR length(btrim(decided_by)) BETWEEN 1 AND 200),
 decided_at timestamptz,
 decision_reason text CHECK (decision_reason IS NULL OR length(btrim(decision_reason)) BETWEEN 1 AND 500),
 sequence bigint UNIQUE,
 -- Four-eyes: whoever staged a release never decides it.
 CONSTRAINT drugmaster_release_four_eyes CHECK (decided_by IS NULL OR lower(btrim(decided_by))<>lower(btrim(staged_by))),
 CONSTRAINT drugmaster_release_decision CHECK ((status='staged')=(decided_by IS NULL AND decided_at IS NULL AND decision_reason IS NULL)),
 CONSTRAINT drugmaster_release_sequence CHECK ((status='published')=(sequence IS NOT NULL))
);

CREATE TABLE drugmaster_molecules (
 id uuid PRIMARY KEY CHECK (substring(id::text,15,1)='7'),
 release_id uuid NOT NULL REFERENCES drugmaster_releases(id),
 inn_name text NOT NULL CHECK (length(inn_name) BETWEEN 1 AND 200 AND inn_name=btrim(inn_name)),
 search_key text GENERATED ALWAYS AS (regexp_replace(lower(inn_name),'[^a-z0-9]','','g')) STORED,
 atc_code text CHECK (atc_code IS NULL OR atc_code ~ '^[A-Z][0-9]{2}[A-Z]{2}[0-9]{2}$'),
 created_at timestamptz NOT NULL DEFAULT now(),
 -- Unique per release, not globally: a rejected release's identity must
 -- not block a corrected one. Publication keeps published ones unique.
 UNIQUE (search_key,release_id),
 CHECK (length(search_key) >= 2 AND inn_name ~ '^[ -~]+$')
);
CREATE INDEX drugmaster_molecule_search ON drugmaster_molecules (search_key text_pattern_ops);

-- A formulation's identity is the source's stable code; everything
-- clinically significant lives on its versions.
CREATE TABLE drugmaster_formulations (
 id uuid PRIMARY KEY CHECK (substring(id::text,15,1)='7'),
 release_id uuid NOT NULL REFERENCES drugmaster_releases(id),
 code text NOT NULL CHECK (code ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,79}$'),
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE (code,release_id)
);

CREATE TABLE drugmaster_formulation_versions (
 id uuid PRIMARY KEY CHECK (substring(id::text,15,1)='7'),
 formulation_id uuid NOT NULL REFERENCES drugmaster_formulations(id),
 release_id uuid NOT NULL REFERENCES drugmaster_releases(id),
 valid_from date NOT NULL,
 label text NOT NULL CHECK (length(label) BETWEEN 1 AND 300 AND label=btrim(label)),
 dose_form text NOT NULL CHECK (dose_form IN ('tablet','capsule','syrup','suspension','solution','drops','injection','infusion',
  'ointment','cream','gel','lotion','inhaler','powder','granules','suppository','patch','spray','lozenge')),
 route text NOT NULL CHECK (route IN ('oral','sublingual','buccal','intravenous','intramuscular','subcutaneous','topical',
  'transdermal','inhalation','nasal','ophthalmic','otic','rectal','vaginal')),
 -- Drugs Rules 1945 lettering (spec D2); NDPS control is never folded in.
 regulatory_class text NOT NULL CHECK (regulatory_class IN ('none','G','H','H1','X')),
 ndps boolean NOT NULL,
 teleconsult_prescribable boolean NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE (formulation_id,release_id),
 UNIQUE (formulation_id,valid_from,release_id)
);
CREATE INDEX drugmaster_formulation_in_force ON drugmaster_formulation_versions (formulation_id,valid_from DESC);

CREATE TABLE drugmaster_formulation_ingredients (
 version_id uuid NOT NULL REFERENCES drugmaster_formulation_versions(id),
 position smallint NOT NULL CHECK (position BETWEEN 1 AND 20),
 molecule_id uuid NOT NULL REFERENCES drugmaster_molecules(id),
 strength_value numeric(14,6) NOT NULL CHECK (strength_value > 0),
 strength_unit text NOT NULL CHECK (strength_unit IN ('mg','g','mcg','IU','mL','%')),
 per_value numeric(14,6) CHECK (per_value IS NULL OR per_value > 0),
 per_unit text CHECK (per_unit IS NULL OR per_unit IN ('mg','g','mL','dose','unit')),
 CHECK ((per_value IS NULL)=(per_unit IS NULL)),
 PRIMARY KEY (version_id,position),
 UNIQUE (version_id,molecule_id)
);
CREATE INDEX drugmaster_ingredient_molecule ON drugmaster_formulation_ingredients (molecule_id);

-- A product is one branded pack of one formulation; a different
-- composition is a different product, so formulation_id never changes.
CREATE TABLE drugmaster_products (
 id uuid PRIMARY KEY CHECK (substring(id::text,15,1)='7'),
 release_id uuid NOT NULL REFERENCES drugmaster_releases(id),
 code text NOT NULL CHECK (code ~ '^[A-Za-z0-9][A-Za-z0-9._:-]{0,79}$'),
 formulation_id uuid NOT NULL REFERENCES drugmaster_formulations(id),
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE (code,release_id)
);
CREATE INDEX drugmaster_product_formulation ON drugmaster_products (formulation_id);

CREATE TABLE drugmaster_product_versions (
 id uuid PRIMARY KEY CHECK (substring(id::text,15,1)='7'),
 product_id uuid NOT NULL REFERENCES drugmaster_products(id),
 release_id uuid NOT NULL REFERENCES drugmaster_releases(id),
 valid_from date NOT NULL,
 brand_name text NOT NULL CHECK (length(brand_name) BETWEEN 1 AND 200 AND brand_name=btrim(brand_name)),
 search_key text GENERATED ALWAYS AS (regexp_replace(lower(brand_name),'[^a-z0-9]','','g')) STORED,
 manufacturer text NOT NULL CHECK (length(btrim(manufacturer)) BETWEEN 1 AND 200),
 pack_size integer NOT NULL CHECK (pack_size BETWEEN 1 AND 1000000),
 unit_of_issue text NOT NULL CHECK (length(btrim(unit_of_issue)) BETWEEN 1 AND 40),
 -- Stored as GTIN-14, so the 8-, 12- and 13-digit forms of one item agree.
 gtin text CHECK (gtin IS NULL OR gtin ~ '^[0-9]{14}$'),
 hsn_code text NOT NULL CHECK (hsn_code ~ '^[0-9]{4}([0-9]{2}){0,2}$'),
 status text NOT NULL CHECK (status IN ('active','discontinued','recalled')),
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE (product_id,release_id),
 CHECK (length(search_key) >= 1)
);
CREATE INDEX drugmaster_product_in_force ON drugmaster_product_versions (product_id,valid_from DESC);
CREATE INDEX drugmaster_product_search ON drugmaster_product_versions (search_key text_pattern_ops);
CREATE INDEX drugmaster_product_gtin ON drugmaster_product_versions (gtin) WHERE gtin IS NOT NULL;

-- Reads see only published content. SECURITY INVOKER, so the release
-- table's own policy still applies to the caller.
CREATE FUNCTION drugmaster_published(release uuid) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT EXISTS (SELECT 1 FROM drugmaster_releases r WHERE r.id=release AND r.status='published')
$$;
CREATE FUNCTION drugmaster_staged(release uuid) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT EXISTS (SELECT 1 FROM drugmaster_releases r WHERE r.id=release AND r.status='staged')
$$;

-- Nothing published is ever rewritten (spec D4): for every role,
-- the owner included.
CREATE FUNCTION drugmaster_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION 'drug master content is never changed or removed' USING ERRCODE='23514';
END $$;

-- Content may only join a release that is still staged.
CREATE FUNCTION drugmaster_content_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE release uuid;
BEGIN
 IF TG_TABLE_NAME='drugmaster_formulation_ingredients' THEN
  SELECT v.release_id INTO release FROM drugmaster_formulation_versions v WHERE v.id=NEW.version_id;
 ELSE
  release:=NEW.release_id;
 END IF;
 -- Only in the transaction that staged the release (review C1): once staged,
 -- a release is sealed, so what the approver reviews is all it will hold.
 IF NOT EXISTS (SELECT 1 FROM drugmaster_releases r WHERE r.id=release AND r.status='staged'
    AND r.xmin::text=(pg_current_xact_id()::text::bigint % 4294967296)::text) THEN
  RAISE EXCEPTION 'drug master content joins only a release staged in this transaction' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;

DO $$
DECLARE t text;
BEGIN
 FOREACH t IN ARRAY ARRAY['drugmaster_molecules','drugmaster_formulations','drugmaster_formulation_versions',
  'drugmaster_formulation_ingredients','drugmaster_products','drugmaster_product_versions'] LOOP
  EXECUTE format('CREATE TRIGGER %I BEFORE INSERT ON %I FOR EACH ROW EXECUTE FUNCTION drugmaster_content_guard()',t||'_guard',t);
  EXECUTE format('CREATE TRIGGER %I BEFORE UPDATE OR DELETE ON %I FOR EACH ROW EXECUTE FUNCTION drugmaster_immutable()',t||'_immutable',t);
  EXECUTE format('CREATE TRIGGER %I BEFORE TRUNCATE ON %I FOR EACH STATEMENT EXECUTE FUNCTION drugmaster_immutable()',t||'_no_truncate',t);
 END LOOP;
END $$;

-- Synthetic fixtures (spec D7) never reach a real catalogue: only the
-- harness's hms_test_N databases accept them.
CREATE FUNCTION drugmaster_source_allowed(source text, db name) RETURNS boolean LANGUAGE sql IMMUTABLE AS $$
 SELECT lower(btrim(source))<>'synthetic-test-fixture' OR db::text LIKE 'hms\_test\_%'
$$;

CREATE FUNCTION drugmaster_release_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='INSERT' THEN
  IF NEW.status<>'staged' THEN
   RAISE EXCEPTION 'a release starts staged' USING ERRCODE='23514';
  END IF;
  IF NOT drugmaster_source_allowed(NEW.source_name,current_database()) THEN
   RAISE EXCEPTION 'synthetic drug data is refused outside test databases' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
 END IF;
 IF TG_OP<>'UPDATE' THEN
  RAISE EXCEPTION 'drug master releases are retained' USING ERRCODE='23514';
 END IF;
 IF OLD.status<>'staged' OR NEW.status NOT IN ('published','rejected')
    OR (NEW.id,NEW.country,NEW.source_name,NEW.licence_ref,NEW.source_version,NEW.file_sha256,NEW.staged_by,NEW.staged_at)
       IS DISTINCT FROM (OLD.id,OLD.country,OLD.source_name,OLD.licence_ref,OLD.source_version,OLD.file_sha256,OLD.staged_by,OLD.staged_at) THEN
  RAISE EXCEPTION 'a staged release is only published or rejected' USING ERRCODE='23514';
 END IF;
 -- One decision at a time (review I2): each sees every earlier one.
 PERFORM pg_advisory_xact_lock(476001);
 -- The database, not the caller, says when and in what order (review M5).
 NEW.decided_at:=now();
 IF NEW.status='rejected' THEN
  NEW.sequence:=NULL;
  RETURN NEW;
 END IF;
 NEW.sequence:=nextval('drugmaster_release_sequence');
 -- A new version of something already published starts no earlier than
 -- today (India): no earlier date is ever answered differently (spec D4).
 -- A version for a future date supersedes one published for that same
 -- date, by release order; a first version may carry its historical date.
 IF EXISTS (SELECT 1 FROM drugmaster_formulation_versions n
     WHERE n.release_id=NEW.id AND n.valid_from<(now() AT TIME ZONE 'Asia/Kolkata')::date
     AND EXISTS (SELECT 1 FROM drugmaster_formulation_versions p WHERE p.formulation_id=n.formulation_id AND drugmaster_published(p.release_id))) THEN
  RAISE EXCEPTION 'a formulation version would rewrite a date already answered' USING ERRCODE='23514';
 END IF;
 IF EXISTS (SELECT 1 FROM drugmaster_product_versions n
     WHERE n.release_id=NEW.id AND n.valid_from<(now() AT TIME ZONE 'Asia/Kolkata')::date
     AND EXISTS (SELECT 1 FROM drugmaster_product_versions p WHERE p.product_id=n.product_id AND drugmaster_published(p.release_id))) THEN
  RAISE EXCEPTION 'a product version would rewrite a date already answered' USING ERRCODE='23514';
 END IF;
 -- A pack is dispensable only with its formulation's rules in force (review I4).
 IF EXISTS (SELECT 1 FROM drugmaster_product_versions n JOIN drugmaster_products p ON p.id=n.product_id
     WHERE n.release_id=NEW.id AND NOT EXISTS (SELECT 1 FROM drugmaster_formulation_versions f
      WHERE f.formulation_id=p.formulation_id AND f.valid_from<=n.valid_from
      AND (f.release_id=NEW.id OR drugmaster_published(f.release_id)))) THEN
  RAISE EXCEPTION 'a product needs its formulation in force from its first day' USING ERRCODE='23514';
 END IF;
 -- Published identities stay unique by key and code (review I1).
 IF EXISTS (SELECT 1 FROM drugmaster_formulation_ingredients i JOIN drugmaster_formulation_versions v ON v.id=i.version_id
     JOIN drugmaster_molecules m ON m.id=i.molecule_id JOIN drugmaster_molecules o ON o.search_key=m.search_key AND o.id<>m.id
     JOIN drugmaster_formulation_ingredients oi ON oi.molecule_id=o.id JOIN drugmaster_formulation_versions ov ON ov.id=oi.version_id
     WHERE v.release_id=NEW.id AND drugmaster_published(ov.release_id)) THEN
  RAISE EXCEPTION 'a molecule would be published twice' USING ERRCODE='23514';
 END IF;
 IF EXISTS (SELECT 1 FROM drugmaster_formulation_versions v JOIN drugmaster_formulations f ON f.id=v.formulation_id
     JOIN drugmaster_formulations o ON o.code=f.code AND o.id<>f.id JOIN drugmaster_formulation_versions ov ON ov.formulation_id=o.id
     WHERE v.release_id=NEW.id AND drugmaster_published(ov.release_id)) THEN
  RAISE EXCEPTION 'a formulation code would be published twice' USING ERRCODE='23514';
 END IF;
 IF EXISTS (SELECT 1 FROM drugmaster_product_versions v JOIN drugmaster_products p ON p.id=v.product_id
     JOIN drugmaster_products o ON o.code=p.code AND o.id<>p.id JOIN drugmaster_product_versions ov ON ov.product_id=o.id
     WHERE v.release_id=NEW.id AND drugmaster_published(ov.release_id)) THEN
  RAISE EXCEPTION 'a product code would be published twice' USING ERRCODE='23514';
 END IF;
 -- A barcode names one product.
 IF EXISTS (SELECT 1 FROM drugmaster_product_versions n JOIN drugmaster_product_versions o
     ON o.gtin=n.gtin AND o.product_id<>n.product_id AND (o.release_id=NEW.id OR drugmaster_published(o.release_id))
     WHERE n.release_id=NEW.id AND n.gtin IS NOT NULL) THEN
  RAISE EXCEPTION 'a GTIN would name two products' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER drugmaster_release_guard BEFORE INSERT OR UPDATE OR DELETE ON drugmaster_releases
 FOR EACH ROW EXECUTE FUNCTION drugmaster_release_guard();
CREATE TRIGGER drugmaster_release_no_truncate BEFORE TRUNCATE ON drugmaster_releases
 FOR EACH STATEMENT EXECUTE FUNCTION drugmaster_immutable();

-- Access (spec D1, D3). Forced RLS binds the owner too, so no role but
-- the three below reads or writes a row.
DO $$
DECLARE t text;
BEGIN
 FOREACH t IN ARRAY ARRAY['drugmaster_releases','drugmaster_molecules','drugmaster_formulations','drugmaster_formulation_versions',
  'drugmaster_formulation_ingredients','drugmaster_products','drugmaster_product_versions'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY',t);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY',t);
  EXECUTE format('REVOKE ALL ON %I FROM PUBLIC, hms_app',t);
  EXECUTE format('GRANT SELECT ON %I TO hms_app, helivanta_drug_stager, helivanta_drug_approver',t);
  EXECUTE format('CREATE POLICY drugmaster_steward_read ON %I FOR SELECT TO helivanta_drug_stager, helivanta_drug_approver USING (true)',t);
  IF t<>'drugmaster_releases' THEN
   EXECUTE format('GRANT INSERT ON %I TO helivanta_drug_stager',t);
  END IF;
 END LOOP;
END $$;

CREATE POLICY drugmaster_published_read ON drugmaster_releases FOR SELECT TO hms_app USING (status='published');
-- An identity row is visible once something published uses it: a
-- rejected release may have introduced it, and a later release reuses it.
CREATE POLICY drugmaster_published_read ON drugmaster_molecules FOR SELECT TO hms_app
 USING (EXISTS (SELECT 1 FROM drugmaster_formulation_ingredients i WHERE i.molecule_id=drugmaster_molecules.id));
CREATE POLICY drugmaster_published_read ON drugmaster_formulations FOR SELECT TO hms_app
 USING (EXISTS (SELECT 1 FROM drugmaster_formulation_versions v WHERE v.formulation_id=drugmaster_formulations.id));
CREATE POLICY drugmaster_published_read ON drugmaster_formulation_versions FOR SELECT TO hms_app USING (drugmaster_published(release_id));
CREATE POLICY drugmaster_published_read ON drugmaster_formulation_ingredients FOR SELECT TO hms_app
 USING (EXISTS (SELECT 1 FROM drugmaster_formulation_versions v WHERE v.id=drugmaster_formulation_ingredients.version_id));
CREATE POLICY drugmaster_published_read ON drugmaster_products FOR SELECT TO hms_app
 USING (EXISTS (SELECT 1 FROM drugmaster_product_versions v WHERE v.product_id=drugmaster_products.id));
CREATE POLICY drugmaster_published_read ON drugmaster_product_versions FOR SELECT TO hms_app USING (drugmaster_published(release_id));

GRANT INSERT(id,country,source_name,licence_ref,source_version,file_sha256,staged_by) ON drugmaster_releases TO helivanta_drug_stager;
CREATE POLICY drugmaster_stage ON drugmaster_releases FOR INSERT TO helivanta_drug_stager WITH CHECK (status='staged');
CREATE POLICY drugmaster_stage ON drugmaster_molecules FOR INSERT TO helivanta_drug_stager WITH CHECK (drugmaster_staged(release_id));
CREATE POLICY drugmaster_stage ON drugmaster_formulations FOR INSERT TO helivanta_drug_stager WITH CHECK (drugmaster_staged(release_id));
CREATE POLICY drugmaster_stage ON drugmaster_formulation_versions FOR INSERT TO helivanta_drug_stager WITH CHECK (drugmaster_staged(release_id));
CREATE POLICY drugmaster_stage ON drugmaster_formulation_ingredients FOR INSERT TO helivanta_drug_stager
 WITH CHECK (EXISTS (SELECT 1 FROM drugmaster_formulation_versions v WHERE v.id=version_id AND drugmaster_staged(v.release_id)));
CREATE POLICY drugmaster_stage ON drugmaster_products FOR INSERT TO helivanta_drug_stager WITH CHECK (drugmaster_staged(release_id));
CREATE POLICY drugmaster_stage ON drugmaster_product_versions FOR INSERT TO helivanta_drug_stager WITH CHECK (drugmaster_staged(release_id));

GRANT UPDATE(status,decided_by,decision_reason) ON drugmaster_releases TO helivanta_drug_approver;
REVOKE ALL ON SEQUENCE drugmaster_release_sequence FROM PUBLIC, hms_app;
GRANT USAGE ON SEQUENCE drugmaster_release_sequence TO helivanta_drug_approver;
CREATE POLICY drugmaster_decide ON drugmaster_releases FOR UPDATE TO helivanta_drug_approver
 USING (status='staged') WITH CHECK (status IN ('published','rejected'));

-- The approver announces what it published, and nothing else (spec D6).
GRANT USAGE ON SCHEMA public TO helivanta_drug_stager, helivanta_drug_approver;
GRANT INSERT ON outbox_events TO helivanta_drug_approver;
GRANT SELECT(id) ON outbox_events TO helivanta_drug_approver;
CREATE POLICY drugmaster_approver_outbox ON outbox_events AS RESTRICTIVE FOR INSERT TO helivanta_drug_approver
 WITH CHECK (tenant_id IS NULL AND subject IN ('helivanta.in.drugmaster.catalogue_published.v1',
  'helivanta.in.drugmaster.formulation_reclassified.v1','helivanta.in.drugmaster.product_status_changed.v1'));
