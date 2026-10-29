# Expo Router: decode-uri-component linear decoder backport

The audit reports [GHSA-qr28-fpjm-wq7f](https://github.com/advisories/GHSA-qr28-fpjm-wq7f):
`decode-uri-component` up to 0.5.0 decodes malformed percent-encoded runs in quadratic time,
so a long crafted string stalls the caller. It reaches production through
`apps/ambulanceconnect > expo-router > query-string@7.1.3 > decode-uri-component@0.2.2`, where
Expo Router parses deep links and URL query strings on the device.

The patched release, 0.5.1, is ESM-only. `query-string` 7 loads it with `require`, so an
override to it would break URL parsing at runtime in the ambulance app. Nothing in the tests
would catch that.

`patches/decode-uri-component@0.2.2.patch` backports 0.5.1's decoder into 0.2.2. That is a
single left-to-right pass that decodes valid UTF-8 runs and leaves invalid bytes literal,
replacing the recursive split and repeated whole-string replacement. It keeps 0.2.2's CommonJS
export and its `+`-as-space handling, both of which `query-string` 7 depends on. Pnpm pins
the patch in the lockfile and applies it on frozen installation.

`scripts/decode-uri-component.test.mjs` loads the copy Expo Router actually resolves:

- **Unpatched:** a 60,000-character malformed run took 2.4 s and then overflowed the stack.
- **Patched:** it decodes in milliseconds.
- **Behaviour:** the assertions on `+`, multi-byte, BOM, truncated `%C2` and mixed valid and
  invalid input pass both before and after.

The test runs in the `scripts` workspace's `test` task, which `pnpm turbo test` runs in CI.

The advisory exception is tied to this applied backport and its test, not to a claim that
unmodified 0.2.2 is safe. Remove the exception, the patch and the test together when Expo
Router moves to a `query-string` that loads a patched decoder.
