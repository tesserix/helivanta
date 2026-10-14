# Access review

Control: SOC 2 CC6.2–CC6.3. Quarterly; record each review as a GitHub issue that lists who reviewed,
what was removed, and the date.

| System | Check | Command or place |
|---|---|---|
| GitHub `tesserix/helivanta` | Every collaborator is a current maintainer; admin role is justified | `gh api repos/tesserix/helivanta/collaborators -q '.[] \| [.login,.role_name] \| @tsv'` |
| `main` ruleset | PR and required checks still enforced; bypass list unchanged | `gh api repos/tesserix/helivanta/rulesets` |
| GCP project `tesseracthub-480811` | IAM bindings touching the `helivanta` namespace, backups bucket, and Workload Identity accounts | Cloud Console → IAM |
| OpenBao | `read-helivanta` policy is still bound only to `helivanta-api` | secret-service console |
| Zitadel | Tenant admins and the `IAM_LOGIN_CLIENT` machine user; disable leavers | Zitadel console |
| Hospital roles (OpenFGA) | Each tenant admin confirms their staff role list | Ask each tenant admin; revoke with `POST /v1/iam/subjects/<subject>/revoke` |

Leavers are removed the same day, not at the next review.
