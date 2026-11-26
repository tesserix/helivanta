# Auth copy: plain-language recovery and no raw error text — design

Issue: #1856 (found by the 2026-11-25 auth-page audit)
Date: 2026-11-26
Copy approved by the product owner on 2026-11-26.

## Why

- **Recovery wording.** Password recovery (`components/password-recovery.tsx`) speaks in the recovery state machine's terms, such as "Prepare an attempt", "Cancel prepared attempt", "Retry delivery request" and "Check recovery status". It also spells "organization" the US way.
- **Raw errors.** Sign-in start (`app/login/page.tsx`), the callback (`app/api/auth/callback/page.tsx`) and account discovery show a thrown error's raw message, so a network failure reads "Failed to fetch".
- **Discovery jargon.** Discovery's "Retry account selection" is jargon.

## Decisions

### D1 Recovery and discovery copy

The approved before/after list, items 1–20, is in the PR. In summary:

- The "Check recovery status" button becomes "Check again", and every message that points at it says "Select Check again".
- "Prepare … attempt" wording becomes "Choose a new password", and "Cancel prepared attempt" becomes "Cancel and start again".
- "Retry delivery request" becomes "Try sending again".
- UK spelling is used throughout.
- Discovery's "Retry account selection" becomes "Show my accounts again".

Behaviour, states, step order and requests are unchanged. Only the text changes.

### D2 Only our own words reach the user

Some failures are not an `ApiError` and not an error Helivanta wrote itself, the exchange route's `Error` from its own response body being one that Helivanta does write. Any such failure on sign-in start, the callback exchange and account discovery shows one fixed sentence: "Check your connection and try again."

Messages Helivanta writes are unchanged:

- `ApiError` messages, including the signup continuation refusals shown at the callback's exchange stage;
- `redirectFailure`'s wording;
- the exchange route's own message.

The exchange's own errors are told apart from a `TypeError` raised by `fetch` itself by giving them a dedicated error class, so no fetch or library message can pass through.

## Not covered

- The shared sign-in card (#1853).
- Email templates (#1857–#1859).
