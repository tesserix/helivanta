# AmbulanceConnect citizen app

Helivanta EmergencyConnect's public citizen app. Programme
[#1220](https://github.com/tesserix/helivanta/issues/1220), current slice
[#1221](https://github.com/tesserix/helivanta/issues/1221).

Implemented: source-backed India emergency calling, explicit outside-India state,
regional service explanations, source/review version, and manual-call guidance if
the dialler fails. The catalogue is bundled so native calling access needs no API
request or account. No emergency call is automatically placed. Booking, GPS,
nearby availability and tracking are not implemented or advertised as active.

## Local development

From the repository root, use Node 24 and pnpm 10.17.1:

```sh
pnpm install --frozen-lockfile
pnpm --filter @helivanta/ambulanceconnect dev
pnpm --filter @helivanta/ambulanceconnect test
pnpm --filter @helivanta/ambulanceconnect type-check
pnpm --filter @helivanta/ambulanceconnect lint
pnpm --filter @helivanta/ambulanceconnect run doctor
pnpm --filter @helivanta/ambulanceconnect build
```

Expo SDK 57 / React Native 0.86 uses its supported React 19.2.3 independently of
the web app's React version. Metro and Jest resolve React from this app, including
JSX runtime subpaths. Do not remove that boundary while the monorepo has two
React versions. NativeWind's generated runtime import has an explicit dependency.

The build script exports iOS and Android Hermes bundles and a web preview into
ignored `dist/`; this does **not** create a signed binary or prove device behaviour.
The EAS profiles describe development-client, internal preview and production
builds. Signing identities, registered app IDs, EAS project linkage and store
submission require the product owner's accounts. No store ownership is claimed
by the proposed `app.helivanta.ambulanceconnect` identifiers.

The shared package `@helivanta/emergency-access` also powers the public Helivanta
web page `/ambulance` and the read-only `/api/emergency/access?country=IN|OTHER`.
The native app does not need that API to show its dial action.

## Safety and verification

Never call a real emergency number for a test. Interaction tests replace only
the platform dialler and safe-area device boundary. Verify the screen on actual
iOS/Android targets, including largest font size, VoiceOver/TalkBack, no data,
dialler unavailable, no voice service, safe areas and returning from a cancelled
dialler, before a native release. Review English, Odia and Hindi with competent
human reviewers before claiming those languages are supported; only English is
implemented in this slice.

Do not collect location, phone, symptoms or login data in this call-access slice.
Its Android manifest blocks location/microphone permissions. Booking activation
belongs to #1222 and requires staffed dispatch, verified fleet, appropriate
protocols and receiving-hospital coordination. No maps or government integration
are inferred from the emergency numbers.

The [Forge security backport](../../docs/security/2026-10-18-expo-forge-backport.md)
is part of the toolchain. Keep its patch, regression and audit metadata together
until an upstream fixed release is verified.
