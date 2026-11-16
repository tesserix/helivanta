# Yes Hospital and independent pharmacy: live Staff acceptance

Status: prepared run sheet; execution results are pending. Use the existing Yes Hospital and its existing owner account. Record the deployed commit and running image digest before starting. The identity-owner sign-in repair and Staff implementation are deployed. PR #1669 passed all feature CI and released merge `2a59d46a` through GitOps/Kargo; see the [deployment evidence](../deployment/releases/2026-11-15-staff-governance-release.json). Authenticated execution of this run sheet remains pending.

## People and evidence

Use the existing hospital owner and the four test mailboxes previously authorized by the product owner. Keep their address-to-test-label mapping in the private execution record. Use separate tenant accounts and the account chooser. Assign different people as preparer, independent reviewer, pharmacy operator and hospital receiver; confirm each actual duty in the assignment record. An email address, title or department membership alone grants no operating authority.

Keep passwords, email codes, authenticator codes, tokens and cookies out of the evidence log. Each observation records the actor's test label, selected tenant/hospital/outlet, displayed state, public response status, command reference and expected allowed/denied result. Retain screenshots without private credential material. Record actual failures and their recovery separately.

## 1. Owner login and workspace selection

1. Open `https://helivanta.app/login`, enter the owner email and verify a fresh email code.
2. Confirm the named Yes Hospital account is selected without typing a tenant identifier. With multiple accounts, confirm only eligible named accounts are offered.
3. Use the existing password and the owner's authenticator. Confirm the session enters Yes Hospital and a protected business API succeeds for that tenant.
4. Check the workspace label and account chooser. Switching must reauthorize the selected account and tenant. An obsolete page must refuse a command after a context switch; the switch must not silently reuse authority from the previous account.

## 2. Initiate and independently qualify hospital Staff

For each authorized test person, use the ordinary Staff initiation and identity setup workflow. Record the intended employer, hospital and exact email. Verify required personal and employment evidence, the active account's tenant organization, reviewed login link and effective employment generation. Pending, inactive, ended, wrong-hospital and unreviewed people must be excluded from access preparation. Preserve separate contributors/reviewers and their immutable decisions. Do not repair missing evidence through direct database inserts.

At `/workforce/access`, open Yes Hospital under **Hospital assignment reviews**, then choose **Prepare assignment**. The named **Choose …** option must match the intended reviewed person and account. The **Prepare hospital assignment** form must show their name/email, current department choices and appropriate templates. It must not ask the operator to paste account, Staff, department or membership-epoch identifiers.

Enter **Assignment reason**, **Access starts** and **Access expires**, then **Submit proposal**. Confirm the preparer cannot approve their own contribution. In the independently authorized reviewer account, open **Review assignment**, use **Refresh selected assignment**, and examine the exact person, employer, scope, template and interval. Select **Approve**, enter **Decision reason** and **Confirm approve**. Refresh and inspect **History**; require the acknowledged active version and attributable independent decision.

If the network loses a committed response, use **Retry same request**. Require the same command/result with no duplicate assignment or decision. Inputs and navigation must stay frozen while the result is uncertain. This recovery is a controlled test; stop any network simulation before continuing ordinary use.

## 3. Hospital-owned pharmacy

As the hospital owner, use the hospital's pharmacy department and a pharmacy outlet owned by that hospital. Verify the stock owner on the outlet and every later command. Assign the department head/supervisor/member through reviewed postings, then separately propose and independently approve the required operating access.

Log in as each test Staff account. Require the allowed duty only at the intended hospital department/outlet. Confirm unrelated departments/outlets and another tenant's records are hidden or denied. Changing a posting alone must not create operating permission. End or transfer the source through the ordinary reviewed workflow and confirm old access stops while retained history remains readable only to the appropriate reviewer.

## 4. Independent pharmacy employer

Use the independent pharmacy's own tenant, organization, Staff and outlets. Hospital employment is not reused as independent employment. Verify separate stock ownership even when the same person has separately reviewed accounts with both employers.

At `/workforce/operating-access`, select **Manage …**, open the intended reviewed Staff member, and confirm **Operating duties**. Use **Assign operating duty**, then choose **Operating role**, the named **Pharmacy outlet**, **Valid from (UTC)**, **Expires at (UTC)** and **Duty reason**. Submit **Propose operating duty**. The independent reviewer examines the exact employer, person, role, location and interval, enters **Decision reason**, and uses **Approve duty**. **Refresh duties** must show **Status: active**.

Test **Recover duty proposal** and **Recover duty decision** after controlled response loss; require the original intent and one retained result. Revoke the duty, then use **Manage existing duties**. **Existing operating duties** must retain the revoked record without offering new proposals or approval from that historical view. Refreshing after Staff/source loss must remove private selected details and refuse new duties.

## 5. Supplier collaboration and stock custody

Use authorized synthetic catalogue/batch/stock and business documents. Select the independent supplier/pharmacy relationship and require consent from both organizations. Assign the hospital's recipient users or group through the bounded collaboration grant. Check exact organizations, hospitals, outlets, resource purpose, permitted actions and expiry. Membership in a group must be current; adding an unrelated hospital user must not expand resource scope.

Run each ownership model separately: hospital-owned stock and independent pharmacy-owned stock. For each, record:

| Step                    | Required observation                                                                                                                                          |
| ----------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Order and allocation    | Correct owner, product, unit and available quantity; duplicate commands recover their original intent.                                                        |
| Dispatch                | Only the assigned current operator can dispatch the exact allocated batch/quantity and destination.                                                           |
| Hospital receiving      | Only the designated receiver can record actual accepted, damaged, short or rejected quantities with evidence.                                                 |
| Discrepancy             | The difference remains attributable and unresolved stock cannot be released or silently treated as accepted.                                                  |
| Review and release      | An admitted reviewer resolves the discrepancy and releases only the authorized accepted quantity.                                                             |
| Financial integration   | The correct acquisition/receipt, payable/charge and correction records reference the owning business documents and reconcile quantities, amount and currency. |
| Retry and source cutoff | Replays do not duplicate stock or money; revoked/expired grants, ended employment and stale source versions refuse new commands.                              |

## Completion evidence

Complete means both models pass the ordinary UI/API journey, negative scope checks, independent review, duplicate/recovery cases and source cutoffs on the recorded deployed SHA. CI browser fixtures and source inspection are supporting evidence; they do not replace these live results. Preserve existing business data and retained audit/history. Remove a test fixture only through separately authorized, recoverable product procedures.
