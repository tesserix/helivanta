# A failed sign-in says what actually failed

**Issue:** [#901](https://github.com/tesserix/helivanta/issues/901)
**Adjacent, not changed:** #856 (`passwordChangeRequired` is invisible to a login
client). D5 below records why this change does not touch it.

## The problem, stated precisely

`loginclient.do` maps a Zitadel response to a sentinel **by HTTP status alone**.
Every 400 becomes `ErrBadCredentials`, whatever Zitadel said. The sentinel's own
doc records one observation ("HTTP 400, COMMAND-3M0fs") and the code applies it
as a rule to every 400.

Zitadel's own source (`internal/command`, read at zitadel `8a54a2a`) shows that
the password and TOTP checks a login client drives answer 400 for several
distinct conditions. All `InvalidArgument` and `FailedPrecondition` errors
surface as 400 through the gateway:

| Zitadel id | Meaning | Source |
|---|---|---|
| `COMMAND-3M0fs` | password does not match (`Errors.User.Password.Invalid`) | `user_human_password.go`, `ErrPasswordInvalid` |
| `EVENT-8isk2` | TOTP code invalid | `domain/human_otp.go`, `VerifyTOTP` |
| `TOTP-Auw0a` | TOTP code replayed | `domain/human_otp.go`, `CheckReuse` |
| `COMMAND-JLK35`, `COMMAND-SFA3t` | user locked (password path) | `verifyPasswordWithLockoutPolicy` |
| `COMMAND-SF3fg` | user locked (TOTP path) | `checkTOTP` |
| `COMMAND-3nJ4t` | user has no password set | `verifyPasswordWithLockoutPolicy` |
| `COMMAND-3n77z` | user not found (precondition form) | `verifyPasswordWithLockoutPolicy` |

A locked account and a wrong password are indistinguishable in our logs. The
handler then logs a two-valued `outcome` and drops the Zitadel id that
`readZitadelErrorID` had already parsed. Diagnosing a real failure on
2026-08-19 needed Zitadel's own logs.

## Decisions

### D1 — Every Zitadel refusal is a typed error that keeps its id

`do` returns a `*ZitadelError{Method, Path, Status, ID, Kind}` for every
non-2xx response. Its `Unwrap` returns `Kind`, so `errors.Is(err,
ErrBadCredentials)` keeps working at every call site unchanged. `ZitadelErrorID(err)`
reads the id back via `errors.As`, and is the only way a handler gets it, never
by parsing an error string. The id is still the only thing kept from the body;
`failedAttempts` and the raw body never reach the error, as before.

### D2 — A 400 is classified by its id, and an unknown id is not "bad credentials"

| 400 id | Sentinel |
|---|---|
| `COMMAND-3M0fs`, `EVENT-8isk2`, `TOTP-Auw0a` | `ErrBadCredentials` |
| `COMMAND-JLK35`, `COMMAND-SFA3t`, `COMMAND-SF3fg` | `ErrAccountLocked` (new) |
| `COMMAND-3nJ4t` | `ErrPasswordNotSet` (new) |
| `COMMAND-3n77z` | `ErrUserNotFound` |
| anything else, including no id | `ErrRejected` (new) |

`ErrRejected` is the honest name for "Zitadel refused and we do not recognise
why". It replaces the old fold into `ErrBadCredentials`, which asserted a cause
nobody had observed. It is still a refusal, never a success: the direction is
unchanged, and only the label stops lying.

`COMMAND-3M0fs` is also used by Zitadel for `Errors.IDMissing` and
`Errors.User.Password.Empty`. Both mean the submitted credential was unusable,
which is what `ErrBadCredentials` means to every caller, so no separate class is
warranted.

### D3 — The browser is told exactly what it was told before

All five credential-class sentinels (`ErrBadCredentials`, `ErrUserNotFound`,
`ErrAccountLocked`, `ErrPasswordNotSet`, `ErrRejected`) get spec D5's single
equalised refusal: same status, same body, same timing floor. Telling the caller
"locked" or "no password set" would confirm the account exists, which is the
enumeration oracle D5 closes. The set is defined once,
`loginclient.IsCredentialRefusal`, and both the password and factor handlers use
it. A sentinel added later and forgotten in one handler would otherwise reopen a
500, or worse, a different answer.

On the factor path, every credential-class refusal of a TOTP code still counts
against the attempt's five-try budget (native-MFA spec D6), exactly as every 400
did before.

### D4 — The log line carries the real outcome and the id

`login password attempt failed` and the factor path's equivalent log:

- `outcome`: `bad_credentials`, `user_not_found`, `account_locked`,
  `password_not_set`, or `rejected`;
- `zitadel_error_id`, for example `COMMAND-3M0fs`;
- `zitadel_status`, for example `400`.

The id is a Zitadel code, not a credential and not PHI.

### D5 — Password-change-required is not in this fold, and stays #856

The issue asks what the UI should do with "a password that must be changed".
Zitadel never signals `passwordChangeRequired` to a login client at all
(verified live 2026-08-16, #854 Task 8; still true in the source read here:
`checkPassword` has no such branch). So it is not one of the 400s this change
separates, and nothing here can see it. It stays #856, which needs a
`GET /v2/users/{id}` read. This change does make any future 400 that Zitadel
adds for it diagnosable: it would log as `rejected` with its id.

## Not covered

- What the browser shows for a locked account: deliberately unchanged (D3).
- Rate limiting and lockout on our side (#855, #861).
- #856, as above.

## Tests

- `loginclient`: each D2 row maps to its sentinel, a 400 with an unknown id and
  a 400 with an unparseable body map to `ErrRejected`, `ZitadelErrorID` returns
  the id, and `failedAttempts` never appears in `err.Error()` (existing test,
  kept).
- `loginui`: each credential-class sentinel answers the identical equalised body
  and status. The log line carries `outcome`, `zitadel_error_id` and
  `zitadel_status`, asserted on captured output. A locked account's TOTP refusal
  bumps the attempt.
- Mutation: map an unknown id back to `ErrBadCredentials`, or drop
  `zitadel_error_id` from the log; the suite must fail.
