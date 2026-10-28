# Login: one click to the credential form, and a calm layout — design

Issue: #1431 (keeps #847's sign-out guarantee; follows #1255 one look)
Date: 2026-10-29

## Why

Measured on `c2e29ec` at 1440x807:

- **Two clicks.** The home page's "Sign in" link (`app/page.tsx`) and the
  `/workspace` and `/signup` links all go to bare `/login`. That page shows
  `RedirectLanding`, a second "Sign in" button, which calls
  `signinRedirect()`, and only then does the credential form appear.
- **The landing exists for a reason.** #847 removed a redirect on arrival
  because it raced Zitadel's session teardown after sign-out: the next
  clinician on a shared terminal got "User not found". Sign-out
  (`post_logout_redirect_uri`), idle timeout, expired auth requests and
  middleware redirects all land on `/login`, and the landing is right for
  every one of them. It is wrong only for someone who has just clicked
  "Sign in".
- **Header.** "Back to Helivanta" is a 12px muted link alone at the top
  right, aligned with nothing in the content below it.
- **Panel.** The left panel uses an uppercase, letter-spaced eyebrow and a
  two-tone headline, and the form column a decorative tinted lock tile. #1255
  D3 bans all three inside the app. The login page is exempt only because it
  is public, and it is the first screen of the app every clinician sees.
- **Two headings.** The landing heading says "Welcome back", but the
  credential form says the organisation's name.
- **Height.** Measured on production: at 1440x807 the page fits exactly, but
  at 1280x720 (a common laptop) it is 787px tall. The panel and the footer
  run 67px past the fold. A local dev server adds a 46px absolutely
  positioned script, so heights must be measured on a production build.

## Decisions

### D1. Deliberate entry links start sign-in; everything else keeps the landing

- Links whose only purpose is to sign in go to `/login?start=1`. These are
  the home page's "Sign in", `/workspace`'s "Sign in", and `/signup`'s
  "Sign in".
- On `/login` with `start=1` and no `authRequest`, `RedirectLanding` starts
  `signinRedirect({ prompt: "login" })` as soon as it mounts. It shows the
  existing "Redirecting…" state rather than a button.
- **Fails closed to the landing.** The landing is shown, and nothing starts,
  when:
  - a `SIGNED_OUT_MARK` or `IDLE_ENDED_MARK` is present, which means a
    session has just ended;
  - sessionStorage cannot be read;
  - the page has a `message`, meaning an expired auth request.

  Auto-start is opt-in per link. Sign-out never carries `start`, so the #847
  race cannot return through this path.

- **No back-button trap.** Before redirecting, the page replaces its own
  history entry with the same URL minus `start`, so Back from the credential
  form returns to the landing rather than starting again.
- **Unchanged:** middleware redirects stay on bare `/login`. That is someone
  opening a deep link while signed out, and the e2e helper's contract
  (`startSignIn`) depends on it.
- `prompt=login` still applies, so auto-start never signs anyone in. It only
  shows the credential form.

### D2. The brand mark is the way home

- "Back to Helivanta" is removed.
- On the platform's own login (default branding), the brand mark at the top
  left becomes a link to `/`, with the accessible name "Helivanta home".
  This is the convention on Linear, Vercel and Stripe.
- On a tenant-branded login, the mark is the hospital's logo, and a link from
  it to Helivanta's marketing page would surprise users. So it is a plain
  mark there.
- `LoginSurface` takes an optional `homeHref`. `TenantLoginLayout` passes
  `"/"` only when no tenant branding resolved. The branding settings preview
  passes nothing.

### D3. The panel and form follow the app's restraint

- **Removed:** the eyebrow and the lock tile.
- **Headline:** one colour (`text-foreground`), `font-semibold` with tight
  tracking, and no accent span. The two lines and their wording stay.
- `branding.tsx` stays on the public-pages lint exemption. The tests below
  enforce the removals instead.
- **One heading for both states.** `RedirectLanding` uses
  `TenantLoginHeading` (the organisation's name), like the credential form.
  Its status line keeps the existing wording: "Sign in to continue.", "You
  are signed out." or the inactivity message.

### D4. Fits a laptop viewport

At 1280x720 and 1440x807 the document is no taller than the viewport. This
comes from removing the eyebrow and tightening the panel's vertical padding
and card margin. The footer stays.

## Not in scope

- The mock "Your organisation" card's content.
- Signup and the credential form's fields and behaviour.
- The MFA steps.

## Tests

- `login.test.tsx`:
  - `start=1` calls `signinRedirect` once with `prompt: "login"` and strips
    `start` from the URL;
  - with `SIGNED_OUT_MARK` or `IDLE_ENDED_MARK` set, `start=1` does not
    redirect and shows the button;
  - without `start`, nothing redirects (#847 unchanged).
- `branding.test.tsx`:
  - with `homeHref`, the mark is a link named "Helivanta home" to `/`;
  - without it, there is no link;
  - no "Back to Helivanta";
  - no uppercase eyebrow.
- Entry links: the home page's "Sign in" link points at `/login?start=1`.
- Measured: document height at 1280x720 and 1440x807, before and after.
