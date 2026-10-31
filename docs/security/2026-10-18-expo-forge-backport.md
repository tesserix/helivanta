# Expo CLI: Forge DigestAlgorithm verification backport

## Superseded by upstream 1.4.1 — 2026-10-31

Expo CLI and its code-signing certificate helper now resolve pinned upstream
`node-forge@1.4.1`. The old 1.4.0 patch and its
`GHSA-86w9-cpqp-85rv` audit exception are removed. The actual-consumer RSA
regressions remain: the malformed nested DigestAlgorithm signature is rejected,
a valid SHA-256 signature verifies, and a changed message fails verification.
The advisory metadata still lists no fixed version for that older finding;
the executed public vectors establish the behavior of the installed package.
The production audit also identifies 1.4.1 as the fix for the newly reported
Forge advisories. The unrelated decoder backport and exception remain in place.

This upgrade protects certificate/signature tooling from crafted cryptographic
input crossing a dependency boundary. It changes no Staff permissions, schema,
identity-provider policy or employment lifecycle behavior.

## Historical backport record

The new Expo SDK 57 native toolchain brings `@expo/cli -> node-forge@1.4.0`.
The audit reports [GHSA-86w9-cpqp-85rv / CVE-2026-85393](https://github.com/advisories/GHSA-86w9-cpqp-85rv):
RSA PKCS#1 v1.5 verification can ignore extra nested DigestAlgorithm elements.
As of 18 October 2026 there is no published patched release (latest 1.4.0).
Expo's CLI uses Forge in its iOS certificate tooling; the public call-access
HTTP endpoint does not perform RSA verification with this package.

`patches/node-forge@1.4.0.patch` backports the exact nested element-count check
from [upstream PR #1152](https://github.com/digitalbazaar/forge/pull/1152), reviewed
at head `ceba34402e329f0365134f23fe19898756527d65` (still open, not claimed merged).
The existing ASN.1 validator must succeed, the outer sequence must contain two
elements, and the nested algorithm sequence must contain only the OID and its
optional parameters. No signature bypass or algorithm fallback is introduced.
Pnpm pins the patch in the lockfile and applies it on frozen installation.

`scripts/expo-crypto.test.mjs` loads the actual Forge resolved by the installed
Expo CLI, not a standalone mock. The upstream nested-garbage vector was accepted
before the patch: the rejection assertion failed with “Missing expected exception”.
After patching, it rejects that signature, still accepts the upstream valid
SHA-256 signature, and rejects that valid signature for a changed message. These
checks run in the existing scripts test gate, including CI.

The registry audit cannot account for a pnpm source patch. Its one new advisory
exception is therefore tied to this applied backport and executable regression
coverage, not a claim that unmodified Forge 1.4.0 is safe. Do not remove the patch
or the tests while retaining the exception. No other advisory is suppressed by
this change; the existing unrelated repository exception remains as it was.

Replace the backport with the first verified upstream fixed package release,
rerun the regression and full native gates, then remove the patch and this new
audit exception together. Owner: `sam123ben`. Check the linked native access
story #1221 before release. This is a dependency maintenance action, not a reason
to enable ambulance booking or submit a native store build without device tests.

Public test vectors are from upstream v1.4.0 `tests/unit/rsa.js` and PR #1152;
they contain only public keys, signatures and messages, no operational key.
