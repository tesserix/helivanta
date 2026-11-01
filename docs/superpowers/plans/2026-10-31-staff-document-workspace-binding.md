# Staff document workspace binding

1. Reproduce a private document response accepting a changed expected account.
2. Reuse `staffPrincipal` in `documentStaff`; retain owner/file/release checks
   and document the explicit 404-to-409 precondition error correction.
3. Verify every Staff document route, ambiguous assertions, no mutation/storage
   disclosure, matching/headerless requests and foreign scope.
4. Run real database/race tests, owner package, vet/build/lint and secret/diff
   checks; record exact results and any unrun real-provider/browser acceptance.
5. Update the full Access/RBAC tracker and existing draft PR after validation.
   No deployment or A01–A12 acceptance follows from this correction alone.
