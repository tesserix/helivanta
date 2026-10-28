# The public pages and login use the app's buttons and panels — design

Issue: #1434 (part of #542 / #541; extends #1275 to the surfaces it left out)
Date: 2026-10-29

## Why

These were measured on production. The same primary action has four shapes:

| Surface                           | Corner radius | Height      | Built from                          |
| --------------------------------- | ------------- | ----------- | ----------------------------------- |
| `/`, `/workspace`, `/signup` CTAs | pill          | 44 and 52px | three hand-written link styles      |
| `/login` Sign in                  | 12px          | 46 and 48px | `Button` plus login-only CSS        |
| `/ambulance` dial                 | 16px          | 60px        | hand-written link                   |
| App                               | 12px          | 40px        | `@tesserix/web` `Button` (755 uses) |

The app already has one system:

- The scale is set in `apps/shell/app/globals.css` (#1255 D3): `sm` 6px,
  `md` 8px, `lg` 12px and `xl` 16px.
- `Button` and `Input` use `rounded-lg`.
- Every card is `Panel`/`PanelLink` (#1275): `rounded-lg`, with a nested
  panel drawn as a `rounded-md` muted inset.
- A lint rule bans hand-painted surfaces in workspace files.
- Files that still paint their own are on the shrinking `SURFACE_LEGACY` list,
  which each zone's tidy-up clears (#1258, #1260, #1261 and #1262).

#1275 left two surfaces out: "The public pages (`hmsPublicPages`) and the
login branding keep their own surfaces". They are where the drift is:

- Public pages import nothing but `cn` from `@tesserix/web`. Every button and
  card there is hand-written.
- They use radii the app never does: pill CTAs, `rounded-2xl` (16px),
  `rounded-3xl` (24px), `rounded-[2rem]`, `rounded-[14px]` and
  `rounded-[11px]`.
- The login page overrides `Button` with its own CSS
  (`.login-content button { min-height: 2.875rem; border-radius: 0.75rem }`,
  `--auth-radius`), and its story panel is `rounded-[2rem]`.

## Decisions

### D1. The scale stays; the public pages adopt it

**The scale is unchanged.** Changing it would resize every `Panel` and every
control in the app to fix pages that simply do not use it.

| Role                                                                                                | Shape                  |
| --------------------------------------------------------------------------------------------------- | ---------------------- |
| Controls (buttons, inputs, selects)                                                                 | `rounded-lg` (12px)    |
| Cards                                                                                               | `Panel` (`rounded-lg`) |
| Regions inside a card                                                                               | `rounded-md` (8px)     |
| Large surfaces: overlays, the app chrome, diagram boards, page-wide bands and the login story panel | `rounded-xl` (16px)    |
| Badges, chips, avatars, dots and icon-only round buttons                                            | `rounded-full`         |

### D2. One button

Public CTAs on `/`, `/workspace`, `/signup` and `/ambulance` become
`@tesserix/web`'s `Button`. A link is rendered with `asChild` around a
`next/link` or `<a>`.

| Where                            | Size                                                                                                                                |
| -------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------- |
| Header actions                   | default (40px)                                                                                                                      |
| Hero and section primary actions | `lg` (48px), the size of the login submit                                                                                           |
| The ambulance dialler            | `xl` (56px), full width. This is the one place a larger target is warranted: an emergency call, often made under stress on a phone. |

Text tags that are not actions ("Product preview", the sign-in status tag)
become `rounded-md`. Pills remain only for D1's `rounded-full` roles. The
story pickers, the text buttons that choose a care-board or AI-board story,
become `rounded-lg` controls.

### D3. One card

- **Login:** the story panel is a large surface (`rounded-xl`). The
  organisation card takes the `Panel` shape (`rounded-lg`), and the icon tiles
  inside it become `rounded-md` regions.
- **Public pages** (`/`, `/workspace`, `/signup`, `/ambulance`):
  - cards take the `Panel` shape (`rounded-lg`), keeping their own padding;
  - regions inside them are `rounded-md`;
  - page-wide bands are `rounded-xl`.

  Swapping their markup for `Panel` would also change padding and shadow, so
  it is not this change.

- **Landing diagrams** (`components/landing`): the board frames are large
  surfaces (`rounded-xl`, which is 16px as before). The AI board's double-framed
  cards are `rounded-lg` outside and `rounded-md` inside. Dots, status badges
  and icon buttons stay `rounded-full`.

### D4. Login reads the scale

- `.login-content button { min-height; border-radius }` is removed.
- `--auth-radius` becomes `var(--radius-lg)`, so `@tesserix/web`'s auth form
  reads the scale.
- The submit button keeps its 48px `min-height`, which
  `login-appearance.spec.ts` asserts.

### D5. Enforced

In `packages/config/eslint.config.mjs`, for every design-checked file,
public pages included:

- Any arbitrary `rounded-[…]` is an error everywhere.
- `rounded-2xl` and `rounded-3xl` are errors outside the workspace. Inside
  it, #1275's surface rule already rejects them, and the `SURFACE_LEGACY`
  files keep that exemption until their zone tidy-up.
- The app chrome in `packages/ui` (rail, header and page panel) moves from
  `md:rounded-2xl` to `md:rounded-xl`. Both are 16px here.
- `rounded-full` together with a filled primary background (`bg-primary`,
  `bg-(--hms-accent)` or `bg-(--hms-accent-strong)`) and text-button padding
  (`px-4` or wider) is an error. That combination is a pill CTA. Tinted
  badges and the tab underline bar stay allowed.

`hmsPublicPages` keeps its headline-type exemption but gains these shape
rules. The app's files already fail on `rounded-2xl`, `rounded-3xl` and
`rounded-xl` through #1275's surface rule; the `SURFACE_LEGACY` files keep
their exemption until their zone tidy-up.

Both rules must be shown to fail on an injected `rounded-3xl` and on a pill
CTA.

### D6. Verified on the rendered pages

The #1430 capture is extended to record `border-*-radius`, `height` and
`font-weight` on `/` (every story step), `/login`, `/workspace`, `/signup`
and `/ambulance`, before and after:

- every radius in the after capture is 0, 6, 8, 12 or 16px, or fully rounded
  on an allowed element (round dots, icon-only buttons and badges);
- colours are identical;
- the PR lists every primary button's radius and height.

## Not in scope

- **The app's hand-painted cards.** Owned by #1275's ratchet and the zone
  tidy issues.
- **Control heights in the app.** `Button` is 40px, while `Input` and
  `SelectControl` are 44px. That needs a `packages/ui` Button wrapper and a
  change to the 123 files that import `Button` directly. Filed as #1436.
- **Headline type on the public pages.** It keeps its exemption (#1255 D3).
