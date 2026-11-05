# Employment evidence concurrent-capacity fixture

A full Global API run for the posting-chain candidate failed the existing fifth
employment-evidence download assertion (expected503, actual200). Four admitted
requests were started and paused sequentially; each storage read has a five-second
production timeout, so the setup can outlive an earlier request and release its
slot. The production channel capacity remains four and the timeout remains five
seconds.

The protocol fixture now arms a bounded number of media-read callbacks atomically.
Its previous single-read API retains one-shot behavior. The capacity scenario
starts all four requests before collecting their storage barriers, then preserves
all existing503 refusal, returned-byte, successful-completion and released-capacity
assertions. No production handler, permission, migration or timeout changes.

Validation: the new helper assertion first failed to compile because the bounded
barrier API was absent. The new real GCS protocol test passes with two simultaneous
held reads and an unheld third read. Full test-infrastructure race suite passes
without verbose/JSON mode in26.230s. A JSON-mode full run failed its unchanged
logger-writer assertion; that result is not counted as passing. The real focused
Global API capacity scenario passes in48.779s, with the held-capacity subtest1.73s.
Broader integrated posting-chain API verification still requires a new full run
with this fixture correction. No deployment or full Staff acceptance is claimed.
