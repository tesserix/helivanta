# AmbulanceConnect #1221 verification

Date: 18 October 2026. Branch: `feat/122-ambulanceconnect`.
Programme #1220; source design and plan are in `docs/superpowers/`.

## Implemented scope

- Source-backed public India calling access on `/ambulance`, linked from the
  public homepage, and exact public `GET /api/emergency/access` configuration.
- Expo SDK 57 citizen app, with bundled calling information, no network/login
  prerequisite, explicit country choice and inline dialler-failure guidance.
- Shared catalogue/strings/review version. Regional 108/102 descriptions are
  informational; no unsupported national coverage or live-availability claim.
- Exact authentication exceptions, no public session renewal, no patient data,
  location request, booking mutation, fleet map, clinical advice or real dispatch.
- Tested Forge CLI dependency security backport; maintenance issue #1225.

## Red/green observations

Before implementation, anonymous `/ambulance` redirected to login and its
session-renewal test scheduled an unwanted renewal. Both regressions now pass.
The homepage entry test failed until its actual link existed. Native screen
assertions failed against the initial screen and now pass for user-initiated
calling, failure, country changes, repeat taps and stale asynchronous results.
Removing the late-result guard reproduced the stale-result assertion failure.

The installed Expo CLI's Forge accepted the malformed nested DigestAlgorithm
signature before patching. The rejection assertion failed with “Missing expected
exception”. After patching, the malformed signature is rejected, the valid one
accepted and the changed-message signature rejected.

An end-to-end HTTP assertion also caught Next's global header overriding the
route handler's `no-referrer`. The exact-route config override fixed the actual
HTTP response; the assertion was retained.

## Commands actually run

All commands used Node 24 / pnpm 10.17.1. These are verification results, not a
production or native-store release claim.

| Check                                                                       | Result                                                                                                                                                                            |
| --------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `pnpm turbo lint format:check type-check test build --concurrency=2`        | 29/29 tasks passed; final native suite has 5 tests; shell 501, API package 279, UI 93, emergency shared package 9, Node scripts 5                                                 |
| `pnpm --filter @helivanta/ambulanceconnect run doctor`                      | 21/21 checks passed                                                                                                                                                               |
| `pnpm --filter @helivanta/ambulanceconnect build`                           | iOS and Android Hermes bundles plus web preview exported; also passed in the full workspace build                                                                                 |
| `pnpm audit --prod --audit-level=high`                                      | Gate passed: 4 moderate and 2 ignored high advisories; one existing exception and the one documented, regression-tested Forge backport exception. Not a zero-vulnerability claim. |
| `node --test scripts/expo-crypto.test.mjs`                                  | 3/3 passed after the observed failure before patching                                                                                                                             |
| Rebuilt `pnpm --filter @helivanta/shell build` after header correction      | Passed; `/ambulance` prerendered, config endpoint dynamic                                                                                                                         |
| Shell `lint` and `type-check` after header correction                       | Passed                                                                                                                                                                            |
| Playwright `emergency-access.spec.ts` against production-built local server | 2/2 passed: anonymous access with hospital API blocked; 390px/150% text; country switching; exact route/header/method and protected-neighbour assertions                          |
| Chrome visual review through browser tooling                                | 390x844 page inspected; country switch inspected; dial action never clicked                                                                                                       |
| `git diff --check`                                                          | Passed                                                                                                                                                                            |

The local Playwright run used a temporary config selecting the two new tests
against `http://127.0.0.1:4340`; it did not run the unrelated clinical journey,
bulk, idle-timeout or renewal projects. The committed test also belongs to the
normal `specs` project for future CI. Tests did not call any emergency number.
The full workspace build used test-shaped public OIDC settings; the public page
needed no login, no real token and no private backend data.

Local raw logs are `/tmp/helivanta-ambulance-{full-gates,final-gates,e2e-final,native-final,doctor-final,audit-final,crypto-red,crypto-green}.log`.
Mobile screenshot: [mobile.png](mobile.png).

## Native limits and release gates

`xcrun simctl list runtimes` returned no installed runtimes and `adb devices -l`
returned no connected devices. Hermes export and native component tests are not
signed binaries or device evidence. VoiceOver/TalkBack, maximum system font size,
no-data/no-voice behaviour, safe areas and real dialler return/cancellation need
both iOS and Android target verification before publication. Do not call emergency
numbers for a test. Only English is implemented; regional language review remains
open. No EAS/store publication or production ambulance activation was performed.

Booking, nearby participating ambulances, hospital preference/acceptance,
provider onboarding, triage, crew workflow, dispatch and CRM remain the linked
programme stories. The parent #122 and programme #1220 stay open. #1221 stays
open for target/device and launch-language evidence; a complete call-access code
slice is not completion of ambulance booking.
