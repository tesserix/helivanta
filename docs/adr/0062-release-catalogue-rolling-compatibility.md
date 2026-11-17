# Disabled future release definitions during rolling deployment

Helivanta validates provider payloads before SDK evaluation. A new deployment
catalogue may create future release keys while older compatible app replicas are
still running. Rejecting an entire snapshot because of an inert future key makes
that rollout unnecessarily unavailable.

Accept at most 100 provider definitions within the existing response-size bound.
A key unknown to this binary must match `helivanta.release.[a-z][a-z_]{0,63}`,
have a literal boolean false default and no rules or extra fields. Remove such
keys before passing the payload to the SDK. Unknown enabled, targeted, malformed,
foreign-product and duplicate-key payloads continue to fail closed. Existing
compiled eligibility, route classification, dependencies and IAM checks apply.

Deploy this compatibility change to every replica before provisioning new keys.
The deployment catalogue can create disabled definitions; activation requires a
reviewed runtime binding and release decision. Rolling back to a pre-compatible
binary while future definitions remain in the reader payload is unsafe and must
be prevented by release operations. No tenant grants, schemas or API routes change.

Regression coverage exercises a released capability alongside an unknown disabled
key, and rejects unknown true/rule-bearing/malformed/foreign definitions.


The full coverage run exposed an existing integration-fixture lifetime bug: the
harness returned its 90-second initialization context to test operations. A
staff-governance integration test reached database assertions after that deadline.
The harness now returns a test-lifetime context with its own cleanup cancellation;
caller cleanups retain access after the workers stop. Setup stays bounded at 90 seconds and application request deadlines are
unchanged. A hosted regression fails against the old deadline and is run early
in CI, without sleeping or relaxing assertions. Local checks compile the fixture
without starting a database; actual integration execution remains in hosted CI.
