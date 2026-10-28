SET LOCAL lock_timeout='5s';
SET LOCAL statement_timeout='300s';

-- Reads that cost what they return (#476, search latency). 0001's read
-- policies called drugmaster_published() per row, and the ingredients
-- policy reached publication through its version, so one search checked
-- every formulation version in the catalogue (40 ms at 6,000; growing
-- with the catalogue). Each row now names its release, and every hms_app
-- policy is one uncorrelated lookup in the small releases table.

-- An ingredient's release is its version's, enforced by the foreign key.
ALTER TABLE drugmaster_formulation_versions ADD CONSTRAINT drugmaster_formulation_version_release UNIQUE (id,release_id);
ALTER TABLE drugmaster_formulation_ingredients ADD COLUMN release_id uuid;
-- Backfill as the owner: forced RLS binds it, so it needs a policy for the
-- length of this transaction, and the immutability trigger is paused for
-- this one column only.
CREATE POLICY drugmaster_migrate ON drugmaster_formulation_ingredients FOR ALL TO CURRENT_USER USING (true) WITH CHECK (true);
CREATE POLICY drugmaster_migrate ON drugmaster_formulation_versions FOR SELECT TO CURRENT_USER USING (true);
ALTER TABLE drugmaster_formulation_ingredients DISABLE TRIGGER drugmaster_formulation_ingredients_immutable;
UPDATE drugmaster_formulation_ingredients i SET release_id=v.release_id FROM drugmaster_formulation_versions v WHERE v.id=i.version_id;
ALTER TABLE drugmaster_formulation_ingredients ENABLE TRIGGER drugmaster_formulation_ingredients_immutable;
DROP POLICY drugmaster_migrate ON drugmaster_formulation_ingredients;
DROP POLICY drugmaster_migrate ON drugmaster_formulation_versions;
ALTER TABLE drugmaster_formulation_ingredients ALTER COLUMN release_id SET NOT NULL,
 ADD CONSTRAINT drugmaster_ingredient_release FOREIGN KEY (version_id,release_id) REFERENCES drugmaster_formulation_versions(id,release_id);
CREATE INDEX drugmaster_ingredient_release_idx ON drugmaster_formulation_ingredients (release_id);
GRANT INSERT(release_id) ON drugmaster_formulation_ingredients TO helivanta_drug_stager;

DROP POLICY drugmaster_stage ON drugmaster_formulation_ingredients;
CREATE POLICY drugmaster_stage ON drugmaster_formulation_ingredients FOR INSERT TO helivanta_drug_stager WITH CHECK (drugmaster_staged(release_id));

DROP POLICY drugmaster_published_read ON drugmaster_formulation_versions;
DROP POLICY drugmaster_published_read ON drugmaster_formulation_ingredients;
DROP POLICY drugmaster_published_read ON drugmaster_product_versions;
CREATE POLICY drugmaster_published_read ON drugmaster_formulation_versions FOR SELECT TO hms_app
 USING (release_id IN (SELECT r.id FROM drugmaster_releases r WHERE r.status='published'));
CREATE POLICY drugmaster_published_read ON drugmaster_formulation_ingredients FOR SELECT TO hms_app
 USING (release_id IN (SELECT r.id FROM drugmaster_releases r WHERE r.status='published'));
CREATE POLICY drugmaster_published_read ON drugmaster_product_versions FOR SELECT TO hms_app
 USING (release_id IN (SELECT r.id FROM drugmaster_releases r WHERE r.status='published'));
