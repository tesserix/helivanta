# Global API implementation plan

1. Add failing transport/configuration tests for explicit product routing,
   authentication/error/cookie preservation, unknown destinations, path/header
   abuse, body bounds, cancellation and dependency failure.
2. Implement the bounded Go connector and independent executable using current
   HTTP security/request-ID infrastructure; retain owner-side authorization.
3. Wire shell rewrites/logout, local startup/cleanup, E2E ingress/fixtures and
   container packaging to the connector. Test actual module route coverage.
4. Run Go tests/race/vet/build/lint, shell checks and frontend tests/typecheck/build;
   exercise the runnable service and client boundary. Record blocked infrastructure
   tests and production GitOps changes explicitly.
5. Publish mandatory future API onboarding and end-to-end acceptance rules in
   AGENTS and contract indexes; preserve existing user changes and avoid commits.
