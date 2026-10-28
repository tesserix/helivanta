# Account menu: who is signed in, their role here, and Sign out

Resolves #1428. Plan: `docs/superpowers/plans/2026-10-28-account-menu.md`.

The signed-in header shows the tenant picker and a bare Sign out link. On a
shared ward terminal, "who is signed in, and as what?" is the first question
before acting, and today the only way to answer it is to sign out.

## D1 — The server answers, from verified identity only

`GET /v1/iam/me/profile` (IAM, `authz.NoTenantMembership` like its `/me`
siblings) returns the caller's `display_name` and `email`, read live from Zitadel
`GET /v2/users/{id}` with the login-client PAT the identity guard already uses
for the same read. It also returns `roles`: the caller's FGA role bindings in
**the tenant their session names**, each with its catalogue label
(`iam.systemRoleLabels`, falling back to the key for a role the catalogue does
not label). The response also carries `subject` and `tenant_id`.

The request takes no parameters, so the subject and tenant come only from the
verified Helivanta session. The OIDC profile in the browser was rejected as a
source. It sits in tab-scoped `sessionStorage`, so a new tab has none, and the
server never verified it.

Zitadel owns the profile, and Helivanta does not copy it. Nothing is persisted,
and the profile is not logged.

## D2 — Fail closed, never a partial answer

- An FGA failure returns 503 `authz_unavailable`, the same shape as `/me/tenants`.
- A Zitadel failure returns 503 `identity_unavailable`: transport errors, 5xx,
  404, a body with no human profile, or no profile reader configured.
- The response is never a 200 with empty roles or an empty name standing in for
  "could not tell".
- A caller with no binding in the current tenant gets `roles: []`. That is a
  true answer: membership was revoked mid-session (#781).

## D3 — One account menu in the header; Sign out lives in it

The bare Sign out link in `HmsShell`'s header is replaced by an account menu
(`@helivanta/ui` `Popover`, which portals, flips, labels itself a dialog and
returns focus). Every app renders `HmsShell`, so every zone gets the menu.

- **Trigger:** the person's initials and name. It is named by its content, a
  visually hidden `Account:` before the visible name, so the name holds every
  visible word (WCAG 2.5.3) and the initials avatar is decorative. It carries no
  `aria-label`, so a field-label lookup such as `getByLabel("Tenant")` can never
  match a person's name. While the profile is loading or unavailable, it reads
  `Account`.
- **Panel:** name, email, then "Role at <hospital>" (the branding display name
  the header already shows) listing role labels. With no role it says "No role
  at this hospital". If the profile is unavailable it says so plainly, with no
  placeholder identity.
- **Sign out** is a form at the foot of the panel and runs the existing
  `handleSignOut` unchanged. It never depends on the profile query, so a
  clinician can always leave a shared terminal, even with Zitadel down.
- A failed profile query raises no toast. A background read failing on every
  page would be noise, and the panel already reports it.

## Not covered

- Editing the profile or password. Zitadel owns the profile, and self-service
  edit is a separate story.
- Linking to the person's workforce staff record. Workforce owns that data, and
  a cross-module projection needs its own design.
- Readable tenant names in the picker: #1264.

## Acceptance

- The backend tests fail first. They cover:
  - roles restricted to the current tenant, with labels;
  - the label fallback;
  - empty roles;
  - FGA 503 and Zitadel 503;
  - no reader configured;
  - the login client's wire parsing, a missing human profile, 404/5xx and
    path escaping.
- The Vitest tests cover:
  - the trigger and panel content;
  - Sign out inside the menu, still working with the profile unavailable;
  - exactly one Sign out.
- The existing sign-out unit and e2e tests open the menu first, through one
  e2e helper.
- `contractdocs` is regenerated. The full Go and frontend gates are green.
