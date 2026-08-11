---
name: hms-backend
description: Use when writing or modifying any HMS backend Go code (backend/**) — loads the binding standards, reference implementations, and enforcement gates.
---

# HMS backend standards

Read `docs/standards/backend.md` for the full rules. Quick table:

| Concern    | Rule                                                     | Reference                                        |
| ---------- | -------------------------------------------------------- | ------------------------------------------------ |
| New module | make new-module NAME=<n>; register in main.go + archtest | backend/scripts/new-module.sh                    |
| Isolation  | no cross-module imports; events only                     | backend/internal/archtest/arch_test.go           |
| Data       | WithTenant only; forced RLS boilerplate                  | backend/internal/modules/medicore/module.go      |
| HTTP       | respond.* helpers; 404 cross-tenant; 409 guarded UPDATE  | backend/internal/platform/respond/respond.go     |
| Auth       | authn.TenantPrincipal(c)                                 | backend/pkg/authn/authn.go                       |
| Authz      | every route declares a permission via *platform.Router; `Permissions()` never lists RoleTenantAdmin; FGA errors are 503, fail closed | backend/pkg/authz/                               |
| Events     | subject regex + outbox in business tx                    | backend/internal/modules/pharmacy/module.go      |
| Logging    | slog + requestid.Logger(c)                               | backend/internal/platform/requestid/requestid.go |
| Tests      | testutil.ModuleHarness per module                        | backend/internal/modules/pharmacy/module_test.go |
| Gates      | make lint-go; scripts/coverage-gate.sh; go test -race    | backend/.golangci.yml                            |

Never: logrus, panic outside main, raw gin.H error envelopes, cross-module imports, editing applied migrations, 403 for cross-tenant, raw `*gin.RouterGroup` in `Routes`, a route with no declared permission.
