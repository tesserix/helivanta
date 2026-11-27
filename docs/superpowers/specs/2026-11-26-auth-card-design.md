# Auth surfaces: recovery, staff setup, signup and callback in the one sign-in card — design

Issue: #1853 (child of #1767 D1: one centred card per task; found by the 2026-11-25 auth-page audit)
Date: 2026-11-26

## Why

Several auth pages do not match the sign-in theme.

- **The old public shell.** Four pages render the `public-site` shell with a text wordmark, "helivanta.":
  - `/recover-password`
  - `/signup`
  - `/signup/verify`
  - `/staff/accept`

  Each paints its own card. None uses `LoginSurface`, so none shows the Helivanta mark or tenant branding, and each has a different card radius, width and shadow from `/login`. An invited staff member, or someone recovering a password, sees a different product from the one they then sign in to.
- **The callback.** `/api/auth/callback` is a bare `<main>`. It has no brand and no card, and "Signing you in…" has no `role="status"`. Its error is one red line.
- **The TOTP enrol step.** It used a raw `<h1>`, so the organisation name disappeared on that one step.

## Decisions

### D1 One frame for every auth surface

Each of these pages renders through `LoginBrandingProvider`, then `TenantLoginLayout`, then `AuthCardCentered`: the frame `/login` uses for its task steps.

- The tenant's branding shows wherever it is known. The provider already reads `?tenant` or `?workspace`, and the tenant retained by the sign-in round trip. A recovery started from a tenant's sign-in page therefore keeps that tenant's mark. Anywhere else the Helivanta mark shows, linked home.
- `TesserixFooter` and the `public-site` wrapper go from these pages, as on `/login`.
- `/signup` is an entry page (`entry`), so a split layout may show its story beside the card. Every other page is a task (`entry={false}`).
- Each page keeps its `<h1>` and its accessible region name.

### D2 The callback is a card

Loading shows the card with "Signing you in…" as `role="status"`. The error sits in the same card, with its message as `role="alert"` and a "Sign in again" button (`Button asChild` around the existing `signInAgainHref` link). The copy is unchanged.

### D3 The enrol step names the organisation

The enrol step shows `TenantLoginHeading` above its "Set up two-step verification" heading, like the other steps.

- "Set up two-step verification" stays the step's heading. It keeps #1854's focus ref and becomes an `h2`.
- If e2e or a test selects it as level 1, that selector is updated.

## Not covered

- **The split panel's tagline.** On tenant-branded logins it is still hard-coded to Helivanta's (`packages/ui/src/branding.tsx`). Changing that needs a product decision and is listed in the issue.
- **Email templates.** They live in onboarding-service and are #1857.
