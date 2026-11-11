# Tenant-owned identity implementation

See ADR-0070 for the user-selected model, security boundary and rollout.

1. Reproduce missing explicit username in native org creation; fix and run operator tests.
2. Remove cross-tenant email reservation uniqueness in owning schema, with real PostgreSQL coverage.
3. Remove global email detection/adoption from public signup, recover only unbound legacy receipts, retain exact-native-owner proof.
4. Add organisation-pinned OIDC and tenant-specific email resolution in the login client; test wrong-org and ambiguous users fail closed.
5. Remove existing-account adoption UI; expose tenant-specific login and explain fresh verification/password/MFA.
6. Verify owner console native security authority and scoped reconciliation.
7. Run local builds/tests without local servers/containers, upstream CI/database tests, then request named migration approval with concrete diff before live activation.
8. Merge/release through gates and verify production using authorised bounded fixtures. Preserve original global identity/key.
