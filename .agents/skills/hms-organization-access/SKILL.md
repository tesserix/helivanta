---
name: hms-organization-access
description: Design or review Helivanta roles, permissions, staff access, departments, supervision, delegation, approvals, tenant-configured workflows and public versus private sector behavior. Use before changing authorization, IAM, workforce postings, department structure or workflow configuration.
---

# Organization and access

Read the [organization knowledge index](../../../docs/domain/organization/README.md),
including its current-implementation gaps and open decisions, then only the
references the task needs:

- [Authority and roles](../../../docs/domain/organization/authority-and-roles.md)
  to place every actor in one kind of authority and find their template and scope.
- [Structure and departments](../../../docs/domain/organization/structure-and-departments.md)
  for hospitals, departments, sub-units, product units and cross-department work.
- [Access model](../../../docs/domain/organization/access-model.md) for permissions,
  templates, assignments, staff lifecycle, delegation, break-glass and migration.
- [Configurable workflows](../../../docs/domain/organization/configurable-workflows.md)
  for tenant-built approvals, forms, escalations, maintenance and CRM boundaries.
- [Public and private sectors](../../../docs/domain/india/public-and-private-sectors.md)
  for government hierarchy, titles, money, staffing and quality frameworks.

For every actor in the change, state the authority kind, role template, scope,
preconditions (credential, privilege, designation) and who approves the
assignment. For every object, state the owner, the scope that contains it and
the status code for out-of-scope (404), missing permission (403) and unavailable
authorization (503). Verify these against the actual handlers, permissions,
migrations and grant tables; the target model is not implemented behavior.

Do not settle an open `ORG-D*` decision inside a feature change. Do not grant
management, administration or platform roles clinical data, create a module
private grant vocabulary, treat a posting or HR job category as access, or seed
financial limits, committee powers or statutory designations from memory. Link
the `ORG-*` and `IN-*` requirements used, report the commands run and state what
still needs review.
