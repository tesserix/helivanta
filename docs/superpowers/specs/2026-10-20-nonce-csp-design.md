# A nonce-based Content-Security-Policy for the shell

Resolves #1113. Follow-on from #1112, whose policy (in
`apps/shell/lib/security-headers.ts`) covers only `frame-ancestors`,
`base-uri` and `object-src`.

## Why

Nothing restricts script execution in the shell. One HTML-injection bug
anywhere (a tenant-supplied string rendered unsafely, a vulnerable dependency)
runs attacker script with the clinician's session, and from there it can reach
every patient record that clinician can open. A strict script policy turns that
class of bug from "session takeover" into "a blocked script and a violation
report".

## Goals

1. Every HTML response from the shell carries an **enforced** policy whose
   `script-src` allows only scripts bearing that response's nonce, plus the
   scripts they load (`'strict-dynamic'`). An injected inline script, an
   inline event handler or a `javascript:` URL does not run.
2. `style-src` is nonce-based as well, so injected `<style>` elements are
   blocked too.
3. Every page works under the policy: no violation on any zone page, the
   login and callback flow, dialogs or toasts.
4. Violations that do happen in production are visible: the browser reports
   them to the shell, which logs them.
5. Adding a script or style that the policy would block fails a test, not a
   clinician's page.

## Non-goals

- Renaming `middleware.ts` to `proxy.ts` (Next 16 deprecation). That is
  separate and unrelated.
- Pre-authentication throttling of the report endpoint. That is the shell's
  share of #861; this slice bounds each request instead (D6).
- `report-to` and the Reporting API (D6 explains why).
- `require-trusted-types-for`. It is a separate hardening step and would need
  its own audit of every DOM sink.

## Anti-goals (failure even if it "works")

- A page that renders without the policy, or with a nonce that is reused
  across responses.
- A policy loosened to make something work: `'unsafe-inline'` in `script-src`
  or `style-src`, or a host allow-list in place of `'strict-dynamic'`.
- A violation report that writes a URL query string or fragment to the logs.
  Search terms such as a patient's name live there.

## Design

### D1. Staging: enforce directly, keep reporting

The issue proposes a `Content-Security-Policy-Report-Only` phase first. A
report-only phase only earns its keep with real traffic, and the shell has
none outside development and CI yet. The decision was made on 2026-10-20 to
**enforce from the first release**, with the exhaustive e2e check in D7
standing in for the report-only soak, and to keep the report endpoint (D6)
so production violations still surface. Failing closed is the right default
here. A policy that only reports leaves every injection bug exploitable while
we wait, and the cost of a wrong guess is a broken widget that is loud in
both the logs and the e2e suite.

### D2. Nonce and policy in the middleware

`apps/shell/middleware.ts` runs on every matched request (the existing matcher
already excludes `_next/static`, `_next/image` and `favicon.ico`). It now
starts by generating a nonce: 16 bytes from `crypto.getRandomValues`,
base64-encoded, fresh for every request. It builds the policy, sets it on the
**request** headers (Next.js reads the nonce from the request's
`Content-Security-Policy` header while rendering and stamps it on its own
scripts) and on the **response** headers. Every exit path then carries the
policy: the public passthroughs, the authenticated passthrough, the 401 JSON
and the redirect to `/login`.

The policy is built by a pure function, `contentSecurityPolicy(nonce,
{ dev, issuerOrigin })` in `apps/shell/lib/csp.ts`:

```
default-src 'self';
script-src 'self' 'nonce-N' 'strict-dynamic'          (+ 'unsafe-eval' in dev only);
style-src 'self' 'nonce-N' 'sha256-<sonner>';
style-src-attr 'unsafe-inline';
img-src 'self' data: blob: https:;
font-src 'self';
connect-src 'self' <issuer origin>;
object-src 'none';
base-uri 'self';
form-action 'self';
frame-ancestors 'none';
report-uri /api/csp-report
```

The directive choices that need an argument:

- **`'unsafe-eval'` in development only.** React uses `eval` in development to
  rebuild server error stacks (Next.js CSP guide). `dev` is
  `process.env.NODE_ENV === "development"`, so a production build never
  carries it. `'unsafe-eval'` does not allow inline scripts, so the
  injected-script e2e test (D7) holds under `next dev` too.
- **`style-src-attr 'unsafe-inline'`.** Server-rendered `style="…"`
  attributes are everywhere: the dashboard's bars, and Radix and
  `@tesserix/web` component positioning. A nonce cannot be attached to an
  attribute, and in modern browsers `'unsafe-inline'` is ignored once a nonce
  is present. Without this directive those attributes are dropped. An
  injected style attribute can restyle the element it sits on, but it cannot
  select other elements or exfiltrate attribute values, which is what makes
  injected `<style>` elements dangerous. `<style>` elements stay
  nonce-or-hash only.
- **`img-src https:`.** Tenant logos are arbitrary `https` URLs, validated
  server-side by `validLogo` (`backend/internal/modules/workspace/module.go`).
- **`connect-src` adds the issuer's origin.** `oidc-client-ts` fetches
  Zitadel's discovery document and token endpoint from the browser
  (`lib/oidc.ts`). The origin comes from `env.NEXT_PUBLIC_ZITADEL_ISSUER_URL`.
- **No `upgrade-insecure-requests`.** HSTS already covers production, and the
  directive would break the `http://*.localhost` development stack.

`SECURITY_HEADERS` drops its `Content-Security-Policy` entry. Two CSP headers
are both enforced, so keeping the old one would only obscure which policy is
in force. Its three directives live on in the new policy, and the other
headers stay as they are.

### D3. Every page renders dynamically

Nonces exist only at request time. The root layout reads the nonce with
`(await headers()).get("x-nonce")`, which makes every route dynamic. Today
every route except four is prerendered static (`next build`, 2026-10-20). If
the nonce is missing, the layout **throws**. A page rendered without a nonce
would be a page whose scripts the policy blocks, or a page with no policy at
all. Neither may render silently.

The cost is measured before the change ships and recorded under "As built":

- Time to first byte of `/login`, prerendered static and then dynamic. That is
  the median of 50 sequential requests against `next start` on the same
  machine.
- Client-side navigation time between two zone pages. Static pages are
  prefetched in full, while dynamic pages are prefetched only down to
  `(workspace)/loading.tsx`, so every navigation now makes one server round
  trip.
- Caching: the `Cache-Control` value for an HTML response before and after.

All of these routes require sign-in, and their HTML is a client shell that
fetches its data from the API, so no shared cache could have served them.
That is why the cost is expected to be small. The measurement, not this
expectation, decides. If a navigation round trip is more than 150 ms at the
median, the builder stops and brings the numbers back before going further.

**As built (2026-10-20):** measured on the same machine against `next start`
production builds of `main` (`d3cd99a`) and of this branch.

|                                                   | `main` (static)     | branch (dynamic)                                          |
| ------------------------------------------------- | ------------------- | --------------------------------------------------------- |
| `/login` time to first byte, median of 50         | 1.85 ms             | 4.32 ms                                                   |
| `Cache-Control` on HTML                           | `s-maxage=31536000` | `private, no-cache, no-store, max-age=0, must-revalidate` |
| Rail move Lab ↔ PharmaConnect, median / p90 of 20 | 81 / 96 ms          | 79 / 96 ms                                                |

Navigation did not get slower, so the 150 ms stop did not apply. 43 of 44
routes are dynamic; the static one is `/icon.svg`, which is not HTML.

### D4. Inline scripts carry the nonce

The root layout passes `nonce={nonce}` to the two package-authored inline
scripts (`THEME_INIT_SCRIPT` and `CHROME_INIT_SCRIPT`). Next.js stamps the
nonce on its own framework scripts.

### D5. Runtime-injected styles

A production build shows three places that create `<style>` elements in the
browser:

1. **`sonner`** inserts its stylesheet at module load, with no nonce support.
   The policy allows it by **hash**: `'sha256-…'` of the exact CSS string.
   A unit test reads the CSS string out of the installed `sonner` package,
   hashes it, and asserts the hash appears in the policy. A `sonner` upgrade
   that changes its CSS then fails CI instead of silently dropping toast
   styles.
2. **`react-style-singleton`** inserts styles through `react-remove-scroll`
   from Radix dialogs. It takes its nonce from `get-nonce`. A client
   component, `StyleNonce`, is rendered first inside the root layout's
   providers. It receives the nonce as a prop and calls `setNonce(nonce)`
   during render, so the nonce is set before any dialog can open. `get-nonce`
   becomes a direct dependency of `apps/shell`, at the version already in the
   tree (1.0.1), so it resolves to the same module instance.
3. **React's hoisted `<style precedence>`** is unused in this codebase. It
   would carry a nonce if anyone added one.

**As built (2026-10-20):** the e2e test in D7 found two more style sources
that a scan of this codebase could not see.

- `sonner` appends its `<style>` empty and fills it afterwards, and the browser
  checks the empty element as well. The policy also allows the hash of the
  empty string (`EMPTY_STYLE_HASH`); an empty stylesheet applies nothing.
- `@tesserix/web`'s sign-in components (`AuthField`, `AuthProviderButton`,
  `AuthPanel`) render `<style>` on the client with no nonce prop. `AuthPanel`'s
  CSS is built from the tenant's branding colours, so no hash can name it.
  `StyleNonce` therefore also stamps the nonce on every `<style>` created
  through `document.createElement`, which is how React creates them. This is
  `'strict-dynamic'` applied to styles: code that is already running may add
  styles, and injected markup stays blocked, because the parser does not call
  `createElement`. `StyleNonce` latches the first nonce it sees. A later render
  carries the RSC request's own nonce, which the page's policy never named.
  Nonce support in `@tesserix/web` would let this stamping go.

The nonce reaches the client inside the RSC payload, not in an attribute. CSS
attribute selectors can read attributes, and browsers hide the `nonce`
attribute to prevent exactly that. No `<meta>` tag carries it.

### D6. Violation reports

`report-uri /api/csp-report` points at a new route handler,
`apps/shell/app/api/csp-report/route.ts`. A filesystem route takes precedence
over the `/api/*` rewrite to the Go API, the same way `/api/emergency/access`
does. The middleware lists it as public, because violations happen on
`/login` before any session exists, and browsers do not reliably send cookies
with reports.

The handler:

- Accepts `POST` with `Content-Type: application/csp-report` only, and
  answers 415 to anything else.
- Rejects bodies over 16 KiB with 413. It reads at most that much and does
  not trust `Content-Length`.
- Answers 400 to a body that is not a `{"csp-report": {…}}` object.
- Logs one `console.warn` JSON line per report, with fixed fields only:
  `effective-directive`, `disposition`, `document-uri` and `blocked-uri`
  reduced to origin and path (or the keyword `inline`, `eval` and so on),
  `source-file` reduced the same way, and `line-number` and `column-number`.
  Each string is truncated to 512 characters. The policy does not ask for
  `'report-sample'`, so no script text is ever sent.
- Answers 204.

**`report-uri` only, not `report-to`.** Chrome ignores `report-uri` once
`report-to` is present, and it delivers Reporting API reports in delayed
batches, so the e2e test (D7) could not wait for them reliably. `report-uri`
is deprecated but still supported by every engine. Moving to `report-to` is
future work.

**Abuse bound.** The endpoint is unauthenticated, so anyone can post to it.
Each request costs at most 16 KiB read and one bounded log line. Throttling
is #861's job.

### D7. Tests (each shown failing before the change)

1. **`lib/csp.test.ts`**:
   - the exact production policy string for a fixed nonce and issuer;
   - `'unsafe-eval'` appears only when `dev` is true;
   - the `sonner` hash test from D5.
2. **Middleware unit test**:
   - every exit path (public, authenticated, 401, redirect) carries the
     policy;
   - two requests get different nonces;
   - the request headers carry the same policy as the response.
3. **`app/api/csp-report/route.test.ts`**:
   - the 204, 400, 413 and 415 cases;
   - the logged line is stripped of query and fragment, and has only the
     fixed fields.
4. **`security-headers.test.ts`**: updated for the dropped entry.
5. **e2e `e2e/tests/csp.spec.ts`** (specs project, run under `next dev` in
   CI):
   - An init script records every `securitypolicyviolation` event. Signed in
     as the seeded admin, visit every zone page `ZONES` lists, open one
     `ConfirmDialog`, raise one toast, then sign out. Assert there were zero
     violations. On `main` it fails differently, with no policy at all, so its
     red state is the next test.
   - Every HTML response carries `script-src` with a nonce, and two loads get
     different nonces.
   - An inline script injected after load (`page.evaluate` appending a
     `<script>` element, and an `onerror` attribute on an `<img>`) does not
     run, and a report arrives at `/api/csp-report`. On `main` both run.
6. **`smoke.spec.ts`** passes unchanged under the enforced policy.

**As built (2026-10-20):**

- The injection test splices its payload into the real `/dashboard` HTML
  through `page.route`, with the real policy header left in place.
  `'strict-dynamic'` deliberately trusts scripts that running code inserts,
  including `createContextualFragment` output, so an in-page `page.evaluate`
  cannot model an HTML-injection bug.
- The recorder ignores violations from Next's development overlay chunk
  (`_next-devtools_`). That overlay styles its shadow-DOM portal without a
  nonce, and it never ships in a production build. Against `next start` the
  test passes with nothing filtered.
- No toast is raised. `sonner` inserts its stylesheet at module load, so every
  page visit checks it.

The emergency suite's standalone production server serves `/ambulance`, so
that page is covered under the production policy without `'unsafe-eval'`.

### D8. Docs corrected in the same change

- `docs/standards/frontend.md`: a new rule under §8 (App Router files).
  Inline `<script>` and `<style>` need the request nonce; a library that
  injects styles needs D5's treatment; `csp.spec.ts` is the check. A matching
  line goes into `CLAUDE.md`'s frontend rules.
- `docs/runbooks/soc2-controls.md`: the "Script CSP not enforced" row becomes
  enforced, pointing at this spec.
- `apps/shell/lib/security-headers.ts`: the comment pointing at #1113 now
  points at `lib/csp.ts`.

## Left to the builder

- Whether the middleware sets the policy through one wrapper around its exits
  or at each exit, as long as the D7 middleware test covers every path.
- The e2e listener mechanics, as long as an event from any frame is counted.

## What this slice does not cover

- Report endpoint throttling (#861).
- `report-to` / Reporting API.
- Trusted Types.
- `middleware.ts` → `proxy.ts`.
