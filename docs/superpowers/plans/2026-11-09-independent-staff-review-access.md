# Independent Staff reviewer access implementation

1. Reproduce product-role UI refusal for an explicitly scoped independent Staff
   reviewer, and missing Staff navigation, before implementation.
2. Keep Workspace verification and owner-scoped Workforce discovery; remove the
   unrelated product role proxy, while retaining the hospital projection gate.
   Add Staff aliases of the existing screens without duplicate data/commands.
3. Test both owner families, no-scope accounts, independent review controls and
   absence of unrelated catalogue/hospital/enrollment reads. Existing owner
   guards, stale-source and immutable decision/recovery tests remain unchanged.
4. Run full shell/shared UI tests, typechecks, lints, formatting and the local
   production build. Remove build output. Commit/push, pass CI, merge, release
   through the protected workflow/Kargo, then verify the actual web workspaces.
