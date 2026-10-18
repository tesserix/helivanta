# Emergency intake implementation

Parent #121; programme #1220. Scoped implementation #1227.

1. Add real-Postgres permission/tenant/base tests; prove the absent routes fail.
2. Generate/register emergency module; additive base/grant/case/timeline schema.
3. Implement named route grants and structural base RLS, explicit administrator
   access boundaries, immutable writes and sanitized errors.
4. Add actual OpenBao encryption/HMAC tests; update exact emergency policy scope
   and mirror without changing staff access. Prove outage/context rejection.
5. Create canonical idempotent case and metadata-only outbox/timeline, then test
   same/different-content retries, concurrency, pagination and unknown patient.
6. Add audited detail access, immediate contact expiry and bounded purge tooling;
   test forced RLS, no plaintext and append-only database controls.
7. Add version-guarded supervisor closure with encrypted explanation; prove races
   and unauthorized transitions fail.
8. Add shared frontend schemas, console base/access setup and mobile-friendly
   intake/queue/detail/timeline. Keep no implicit dispatcher access and explicit
   loading/empty/error states; do not infer clinical priority or dispatch.
9. Run full Go race/coverage/lint/vet/build and frontend gates, plus browser journey
   with synthetic data. Record source-backed release boundaries and remaining
   clinical/operator/device evidence. Do not close parent #121 from this slice.
