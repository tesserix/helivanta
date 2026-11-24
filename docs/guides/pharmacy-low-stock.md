# Outlet low-stock warnings

Availability: the dedicated `pharmacy_low_stock` capability depends on
`pharmacy_setup`. It has its own release checks and
[Console feature management](../runbooks/feature-flags.md#outlet-low-stock-warnings).
General Catalogue, Stock and Reports are not prerequisites for this workspace.
Release eligibility, deployed policy and Console availability must agree before
it can be used; a source merge alone does not activate it.

The [deployed acceptance record](../superpowers/specs/2026-11-24-low-stock-activation.md#production-acceptance--2026-11-24-utc)
identifies the verified release and test scope. This workspace configures existing
active catalogue products; it does not create products or approve them for use.
If no products are available at an outlet, catalogue provisioning remains the
PharmaConnect catalogue owner's responsibility.

1. In PharmaConnect, open **Inventory → Low stock** and select the named pharmacy
   and outlet. **Outlet stock alerts** shows the exact low-stock product count
   and observation time. Its link opens the same scoped warning list.
2. An outlet editor with catalogue-read, catalogue-write and stock-read permission can select an
   active product under **Configure reorder level**. Readers with catalogue-read and stock-read can discover their outlets and view warnings.
3. Enable the warning and enter a whole-number minimum in the displayed issue
   unit. Zero means warn when none is available. Save the reorder level.
4. Products whose available quantity is at or below the minimum appear in the
   list. Products with no batches can appear with zero. The overview's
   **Low-stock products** link opens this same scoped view. Use **Refresh low-stock
   overview** and **Refresh low-stock warnings** to update the observations after
   stock changes elsewhere. Count and list are separately observed requests.
5. To stop a warning, select the product, clear **Enable low-stock warning**, and
   save. The previous configuration remains in accountable history.

Quarantine, expired stock, safety-blocked lots and supplier reservations do not
inflate availability. A batch expiring today is excluded using the outlet's
local date. This is a shortage warning, not a dispensing approval or an order.
An unconfigured product is not counted. If a product's unit changes before it
has any batches, its previous minimum is inactive and must be confirmed again
in the new unit.

Background refreshes retain unsaved values and pending commands. If newer values
arrive, confirm any pending command first, then use **Reload current values**
before another change. If a save conflicts, reload the current product and level
before editing. If a
save cannot be confirmed, keep the form open and choose **Check saved result**.
Only an authoritative not-found response offers **Retry original change**, which
uses the same command and input. A request failure is never shown as an empty
stock list. Switching away from an unsaved change asks for confirmation.

Temporary read failures hide the previous count/list rather than reporting zero.
Use **Try again** for a temporary failure. If access has been revoked, the view
shows an access message with no retry or editing controls. Ask an authorized
administrator to review the exact outlet assignment; changing a feature flag
does not grant access.
