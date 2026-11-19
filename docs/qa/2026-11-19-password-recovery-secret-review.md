# Password recovery secret-scan review

GitGuardian incident `40266894` flagged the password recovery form initialization
in `apps/shell/components/password-recovery.tsx` at commit `068c9bf`, line 66.
Source review confirmed that the finding refers to a React Hook Form binding and
empty initial input values. It contains no embedded password or other credential.

On 19 November 2026, the incident was classified in GitGuardian as
**Ignored — Not a secret (false positive)**. This disposition applies only to the
reviewed incident; secret scanning and the repository's required checks remain
enabled. No credential rotation is indicated by this finding.

The historical GitHub check on PR #1748 retained its failed result after the
provider disposition. A check re-request returned HTTP 404 with the available
GitHub authorization. This documentation commit requests a normal provider rescan
through the pull request workflow; it does not override or fabricate a check.

A passing rescan proves resolution of this scanner finding only. Recovery
acceptance, deployment and live verification remain separate release gates.
