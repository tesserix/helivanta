# The clinician zones collapse into one Next app

**Issue:** [#933](https://github.com/tesserix/helivanta/issues/933)
**Also closes:** [#882](https://github.com/tesserix/helivanta/issues/882), by deleting the generator it reports broken.
**Supersedes:** the frontend half of ADR-0001 and of
`2026-08-04-hms-repo-setup-design.md` D2/D3. ADR-0010 records the decision;
ADR-0001 is marked superseded in part. The direction #890 proposed, a
subdomain per zone, was closed in favour of this.

## The problem, stated precisely

The frontend is four Next apps stitched by rewrites. `apps/shell` owns `/`;
`apps/medicore`, `apps/pharmacy` and `apps/lab` each mount under a `basePath`.
Multi-zones exist for independently owned and released apps. None of that
holds here: one team, one CI, one `turbo build`, every app importing
`@helivanta/ui` and `@helivanta/api`, and about six pages. The cost is real:

- **Every cross-department move is a full page load.** OPD → pharmacy is one
  clinical action (#932's prescription routing) and it reloads the browser.
- **Zone pages do not renew the session.** `SessionRenewal` runs only in the
  shell's layout, so a clinician who stays on OPD, pharmacy or lab longer than
  `SESSION_TTL` is signed out on their next API call. Its own comment records
  this as a known gap.
- **The shell → zone proxy hop resets connections** under keep-alive (#976),
  which the e2e harness had to route around with an ingress.
- Workarounds encoded as standards: "cross-zone links are plain `<a>`", a
  `localStorage` permissions cache that exists because hops are hard reloads,
  `basePath: false` repeated in three configs, and a `new-zone` generator that
  produces apps that cannot install (#882).

## D1 — Pages move into `apps/shell` at the same URLs

| URL                     | Was                                                               | Now                                                             |
| ----------------------- | ----------------------------------------------------------------- | --------------------------------------------------------------- |
| `/medicore`             | `apps/medicore/app/page.tsx`, `redirect("/opd")` under `basePath` | `apps/shell/app/medicore/page.tsx`, `redirect("/medicore/opd")` |
| `/medicore/opd`         | `apps/medicore/app/opd/page.tsx`                                  | `apps/shell/app/medicore/opd/page.tsx`                          |
| `/medicore/ipd`         | `apps/medicore/app/ipd/page.tsx`                                  | `apps/shell/app/medicore/ipd/page.tsx`                          |
| `/pharmacy`             | `apps/pharmacy/app/page.tsx`                                      | `apps/shell/app/pharmacy/page.tsx`                              |
| `/pharmacy/medications` | `apps/pharmacy/app/medications/page.tsx`                          | `apps/shell/app/pharmacy/medications/page.tsx`                  |
| `/lab`                  | `apps/lab/app/page.tsx`                                           | `apps/shell/app/lab/page.tsx`                                   |

Plain route segments, not `(group)` route groups: a group drops its name from
the URL, and keeping the URL contract is the point (`packages/ui/src/zones.ts`
and `e2e/tests/smoke.spec.ts` are unchanged). Panel components and their
tests move to `apps/shell/components/{medicore,pharmacy,lab}/`, unchanged
apart from import paths.

The zone apps' `layout`, `error`, `loading`, `not-found` and `globals.css` are
byte-identical to the shell's (verified), except that the shell's layout also
mounts `SessionRenewal`. So the shell's own files serve the moved pages, and
zone pages gain session renewal as a consequence. That fixes the bug above.

## D2 — Navigation becomes client-side

`HmsShell`'s rail, panel and home links and the dashboard's department cards
use `next/link`. `@helivanta/ui` declares `next` as a peer dependency; its only
consumer is now the shell. Sign-out stays a form POST (#781). That was never a
navigation.

The guarantee, "OPD → Pharmacy → Lab performs no full page load", is enforced
by an e2e spec (`e2e/tests/navigation.spec.ts`). It sets a marker on `window`
and walks the rail across all three departments, asserting the marker
survives. It was run against the multi-zone build first and failed there.
Lint cannot enforce this. Next's `no-html-link-for-pages` only understands the
`pages/` router, so a CI failure is the strongest available rung.

## D3 — One app, one image

Deleted: `apps/medicore`, `apps/pharmacy`, `apps/lab`; the six zone rewrites
in `apps/shell/next.config.ts`; `MEDICORE_URL`, `PHARMACY_URL` and `LAB_URL`
from `Dockerfile.shell`, `images.yml`, `turbo.json` and the env examples; the
`new-zone` script and generator (#882); ports 4302–4304 from the dev scripts.
The shell's `/api/:path*` rewrite stays: `make dev` has no ingress.

The e2e harness (#976) drops its zone builds and zone routes. The ingress
stays, because it still routes `/api/v1` the way production does, rather than
through the shell's rewrite.

Production was never running the zone apps. Only `helivanta-shell` is
deployed, so nothing is removed from the cluster. The `tesserix-k8s` shell
chart still documents the three URLs as baked build arguments. They become
inert, and their removal there is a follow-up in that repository.

## D4 — The permissions cache stays, no longer load-bearing

`packages/api/src/permissions-cache.ts` still paints the rail on a cold load
before `/me/permissions` returns. Its comment now says that soft navigation
keeps permissions in React Query's memory, so the cache matters only on a hard
load.

## What this slice does not cover

- The patient app (#11, `apps/mobile`): a separate audience and auth realm.
- Moving `HmsShell` into a shared layout so the chrome does not remount per
  page. Each page still renders its own `HmsShell`, exactly as before. This
  change is the move, not a chrome redesign. Done in #1091; see
  `2026-10-20-workspace-layout-design.md`.
- Any change to routes' content, permissions, or the API.

## Proof

- Every URL in D1 serves the same page as before, checked through the
  production build and the ingress.
- `smoke.spec.ts` passes **unmodified**; the full `make e2e` is green.
- `navigation.spec.ts` fails on the multi-zone build and passes after.
- The moved component tests pass in the shell's suite;
  `pnpm turbo lint type-check test build format:check` is green.
