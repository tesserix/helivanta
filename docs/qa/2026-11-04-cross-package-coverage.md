# Source-owner coverage accounting

The backend coverage gate previously checked the percentage printed by each
package's own test binary. Global API integration tests exercise module handlers
through the real gateway, but their execution did not contribute to the source
module's percentage. Draft posting PR #1544 reproduced this: behavioral tests
passed while Workforce's own binary reported 69.2%, below the 70% floor.

The gate now instruments repository packages with `-coverpkg=./...` and reads the
combined Go coverage profile. Each source block contributes its statement count
once to its owning package. Any positive execution count covers that block;
repeated execution across binaries does not increase the denominator or the
covered statement count. Uncovered blocks remain in the denominator. Package
percentages printed by Go under `-coverpkg` describe the instrumented aggregate,
so they are no longer used as package-specific evidence.

The default 70% floor, race detector, uncached execution, 20-minute package
timeout, behavioral-test failure handling, data-only contract exception, and
existing refusal of gated packages without their own test files are retained.
The floor comparison uses statement counts before rounding the displayed
percentage. Failure to read the profile fails the gate.

`bash backend/scripts/coverage-gate.test.sh` creates disposable real Go modules
without dependencies. Its first assertion failed on the original gate with
50.0% even though the integration test exercised the other half of the owner's
behavior. Regression cases cover cross-package execution, repeated calls with
uncovered code, 100% externally covered code without owner tests, failing
behavioral tests, the data-only contract exception, and an unimported gated
package without tests. CI runs these fixtures before the complete backend gate.

This change measures executed source statements; it does not establish workflow
acceptance, browser acceptance, or the quality of assertions. Posting review,
supervision, lifecycle enforcement and production rollout retain their separate
acceptance obligations.
