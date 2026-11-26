# Auth forms: useZodForm + Field, no native validation, errors tied to their fields — design

Issue: #1855 (found by the 2026-11-25 auth-page audit; related to #1767)
Date: 2026-11-26

## Why

The 2026-11-25 audit found four gaps in the auth forms:

- **Password recovery** (`components/password-recovery.tsx`) uses raw `useForm` and TanStack `useMutation`.
  - Its forms have no `noValidate`, and the `type="email"` input makes the browser show its own validation bubble.
  - Its error lines are neither styled as errors nor linked to their inputs.
- **Staff acceptance** (`app/staff/accept/acceptance.tsx`) has three problems:
  - It reads its values with `FormData`.
  - Its form has no `noValidate`, and both password inputs are marked `required`.
  - After success it redirects to `/login` on a 7-second timer, with no way to pause it (WCAG 2.2.1). Its button says "Go to login".
- **`Field`** (`packages/ui/src/form.tsx`) announces an error once, as `role="alert"`, but never marks the input itself. A screen-reader user who returns to the field does not hear why it failed. This affects every form, not only auth.
- **Nothing structural prevents a regression.** The workspace lint covers `app/(workspace)/**` and `components/**` for surfaces, but no rule rejects native validation anywhere.

## Decisions

### D1 Field marks its control

`Field` passes two attributes to its control: `aria-invalid` (true while an error is shown) and `aria-describedby` (pointing at `${id}-error`, merged with any existing value).

- **The control is the direct child element whose `id` equals the Field's `id`.** This is the shape all of the 714 usages already have, and `SelectControl` forwards these attributes to its trigger.
- **Any other child is left alone.** That covers wrappers and a missing `id`, because there is no control to point at.
- **The control's own value wins.** A control that sets `aria-invalid` or `aria-describedby` itself keeps it, merged rather than overwritten.
- **Tests** in `packages/ui` cover:
  - `Input`, native `input`, `textarea` and `SelectControl`;
  - an existing `aria-describedby`;
  - a non-matching child.

### D2 Recovery and staff acceptance use the house form stack

Both rebuild on `useZodForm`, `Field` and `useApiMutation`, with `noValidate`, and keep their existing contracts and calls.

- **Recovery:** the email field becomes `type="text"`, with `inputMode="email"` and `autoComplete="email"`. Zod validates it, not the browser.
- **Staff acceptance:** its password inputs lose `required`. The existing schema enforces presence and length; any rule it does not enforce is reported, not invented.
- **Behaviour stays the same:** command ids, retries, status polling, the order of steps, and what is sent all stay as they are.
- **Mutation errors keep their dedicated surfaces.** Where a step already renders its own error next to its fields, it passes `suppressErrorToast`, as the login credential check does.
- **No copy changes**, except the staff-acceptance button and the timer (D3). Recovery's wording is #1856.

### D3 No timed redirect

Staff acceptance stops redirecting after 7 seconds. Success shows a "Sign in" button linking to `/login`, which is the verb every other auth page uses. This is a copy change, "Go to login" → "Sign in", and it is listed in the PR for approval.

### D4 A lint rule holds native validation out of the auth surfaces

A new `hmsNoNativeValidation(files)` in `packages/config` rejects three things:

- the JSX attributes `required`, `minLength`, `maxLength` and `pattern`;
- `type="email"` and `type="url"`;
- a `<form>` without `noValidate`.

The shell applies it to the auth surfaces: `app/login/**`, `app/signup/**`, `app/staff/**`, `app/recover-password/**`, `app/api/auth/**`, `components/password-recovery.tsx`, `components/account-discovery.tsx` and `components/mfa-code-form.tsx`. Its tests prove each case is rejected.

The rule can widen to every zone once the remaining native attributes are removed. About 50 remain, mostly in pharmacy, and they are tracked in a separate issue.

## Not covered

- Recovery copy (#1856).
- The way back and focus on each step (#1854).
- The shared sign-in card (#1853).
- Native attributes outside the auth surfaces (separate issue).
