# Plan — a failed sign-in says what actually failed (#901)

Spec: `docs/superpowers/specs/2026-10-08-login-failure-taxonomy-design.md`

## Task 1 — typed Zitadel errors

- [x] `loginclient/zitadelerror.go`: `ZitadelError{Method, Path, Status, ID, Kind}`
      with `Unwrap`; `ZitadelErrorID`, `ZitadelStatus`.
- [x] `do()` returns `*ZitadelError` for every non-2xx answer.
- [x] `readZitadelErrorID` falls back to the `"<key> (<id>)"` message suffix
      when `details` carries no plain `id`.

## Task 2 — classify a 400 by its id

- [x] `badRequestKinds` per spec D2; new `ErrAccountLocked`,
      `ErrPasswordNotSet`, `ErrRejected`; an unknown or missing id is
      `ErrRejected`, never `ErrBadCredentials`.
- [x] `IsCredentialRefusal` and `FailureOutcome` as the single definition of
      the credential-class set and its log names.

## Task 3 — handlers

- [x] Password: every credential-class refusal answers the equalised refusal;
      the WARN line carries `outcome`, `zitadel_error_id`, `zitadel_status`.
- [x] Factor: every credential-class refusal of a code bumps the attempt, and
      the attempt log carries the same fields.

## Task 4 — tests

- [x] Table test over every D2 id, both response shapes, non-credential
      statuses, and `FailureOutcome`.
- [x] Handler test: byte-identical response for each refusal, distinct log line.
- [x] Mutations: unknown id back to `ErrBadCredentials`; `zitadel_error_id`
      dropped from the log; `ErrAccountLocked` removed from
      `IsCredentialRefusal`. Each failed the suite.

## Gates

- `make lint-go`, `go test -race ./...`, `./scripts/coverage-gate.sh`.
- Frontend untouched.
