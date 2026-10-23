# Release: Pakistan removed (#1287)

Migrations `0003_workspace_india_only`, `0011_pharmacy` and `0009_supply` refuse to run if
the database already holds Pakistan records. They never change those records. A refused
migration stops the release at the migration step, so check every environment **before**
promoting it.

## 1. Check before deploying

Run the check as the BYPASSRLS `helivanta_system` role, which sees every tenant. A count
run under the application or owner role can wrongly return 0. Each query is read-only.

```sql
SELECT 'workspace_country_policy_scopes', count(*) FROM workspace_country_policy_scopes WHERE country <> 'IN' OR currency <> 'INR'
UNION ALL SELECT 'pharmacy_outlets', count(*) FROM pharmacy_outlets WHERE country_code = 'PK'
UNION ALL SELECT 'pharmacy_catalogue_products', count(*) FROM pharmacy_catalogue_products WHERE currency = 'PKR'
UNION ALL SELECT 'pharmacy_payables', count(*) FROM pharmacy_payables WHERE currency = 'PKR'
UNION ALL SELECT 'pharmacy_settlements', count(*) FROM pharmacy_settlements WHERE currency = 'PKR'
UNION ALL SELECT 'supply_parties', count(*) FROM supply_parties WHERE country_code = 'PK'
UNION ALL SELECT 'supply_party_addresses', count(*) FROM supply_party_addresses WHERE country_code = 'PK'
UNION ALL SELECT 'supply_party_tax_registrations', count(*) FROM supply_party_tax_registrations WHERE country_code = 'PK' OR kind IN ('ntn','stn')
UNION ALL SELECT 'supply_premises', count(*) FROM supply_premises WHERE country_code = 'PK'
UNION ALL SELECT 'supply_licenses', count(*) FROM supply_licenses WHERE country_code = 'PK'
UNION ALL SELECT 'supply_territories', count(*) FROM supply_territories WHERE country_code = 'PK'
UNION ALL SELECT 'supply_delivery_tasks', count(*) FROM supply_delivery_tasks WHERE country_code = 'PK';
```

The supply migration discovers its tables. It counts every `supply_*` table with a
`country_code` column, so a table added after this release is counted too. Add that table
to the query above.

**If every count is 0, deploy.** Nothing else is needed.

## 2. If any count is not 0

Do not deploy that environment. These are business records:

- outlets, products, bills and payments;
- supplier registrations and licences.

Most of them are append-only, enforced by database triggers. That is deliberate: a clinical,
stock or financial record is corrected by a new record, not erased. The application cannot
remove them, so a "delete them and re-run" shortcut does not exist.

1. **Identify** each record's tenant and owner, using the same queries with
   `tenant_id, id` selected.
2. **Decide** with the tenant's data owner what each record is:
   - **Synthetic or test data** (`TEST-` codes) is removed.
   - **Real data** is exported for the tenant first, then removed or kept on a branch that
     still supports Pakistan. The decision is recorded in the #1287 issue.
3. **Remove** by a reviewed schema-owner change, as for tenant offboarding
   ([data retention and deletion](../runbooks/data-retention-and-deletion.md)). The change:
   - runs as the schema owner, in one transaction;
   - lifts `FORCE ROW LEVEL SECURITY` and the immutability trigger on each table it touches,
     and restores both before commit;
   - deletes children before parents (licences, premises, addresses and identifiers before
     parties; settlements and lines before payables; products before outlets);
   - writes an audit note naming the approval.

   It needs its own review. Never run it ad hoc against a live environment.

4. **Re-run** the check in step 1. When every count is 0, deploy. A migration that refused
   left nothing applied, so it runs cleanly then.

## 3. After deploying

- Each migration ID appears in `schema_migrations`.
- The India-only checks exist:
  `SELECT conname FROM pg_constraint WHERE conname LIKE '%\_india'`
  lists `workspace_country_policy_india` and the six `supply_*_india` constraints.
- Every tenant table still forces RLS: the API's boot-time `LintRLS` check fails the start
  otherwise.
