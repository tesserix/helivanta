# ADR-0010: One Next app for the clinician zones

- **Status:** Accepted (2026-10-13)
- **Supersedes:** the frontend half of [ADR-0001](0001-monorepo-and-multi-zone-strategy.md)
  (path-mounted Next.js multi-zones). ADR-0001's monorepo decision stands.
- **Context:** Issue #933. The frontend was four Next apps: `apps/shell`
  owning `/` and `apps/medicore`, `apps/pharmacy`, `apps/lab` under
  `basePath`s, stitched by the shell's rewrites. Multi-zones exist for apps
  that are independently owned and independently released. None of those
  preconditions held: one team, one CI, one `turbo build`, every app
  importing `@helivanta/ui` and `@helivanta/api`, about six pages. The
  split's costs were concrete:
  - every move between departments was a full page load, mid clinical task;
  - zone pages never ran session renewal, so staying on one longer than
    `SESSION_TTL` signed the clinician out;
  - the shell-to-zone proxy hop reset connections under keep-alive (#976);
  - a permissions cache, a "plain `<a>`" rule and a broken zone generator
    (#882) existed only to cope.

  The opposite direction, a subdomain per zone (#890), was weighed and
  closed. The tenant already owns the subdomain label, and sharing a
  session across subdomains would widen the PHI session cookie's scope.
- **Decision:** MediCore, Pharmacy and Lab are route segments of
  `apps/shell` at their existing URLs (`/medicore/opd`, `/pharmacy`, `/lab`, …).
  Navigation between them is `next/link`. There is one image and one
  deployment. Selling products separately is a runtime entitlement check,
  not an app boundary.
- **Consequences:**
  - Moving between departments is client-side and keeps client state;
    `e2e/tests/navigation.spec.ts` fails if it ever reloads.
  - Every page runs session renewal and idle tracking from one layout.
  - A zone is added as routes plus a `zones.ts` entry; there is no generator.
  - The patient app (`apps/mobile`, #11) stays separate. It has a different
    audience and auth realm.
  - If a product ever genuinely needs independent ownership and release,
    splitting it out again is a deliberate decision with its own ADR, not a
    default to drift back to.
- Design spec: `docs/superpowers/specs/2026-10-13-one-next-app-design.md`.
