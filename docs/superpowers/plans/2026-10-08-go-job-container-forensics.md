# Plan: go-job container forensics (#963, diagnosability step)

Spec: `docs/superpowers/specs/2026-10-08-go-job-container-forensics-design.md`

- [x] `ci.yml` go job: background `docker events` recorder before
  `coverage-gate.sh`.
- [x] On failure:
  - container death events;
  - `docker ps -a` and inspect of exited containers;
  - kernel OOM lines;
  - the events artifact.
- [x] Local proof: the workflow's `run:` blocks against a SIGKILLed canary.
- [ ] CI proof: a temporary deliberately-failing commit, observed and then
  reverted.
- [ ] After the next occurrence: root cause and fix under #963.
