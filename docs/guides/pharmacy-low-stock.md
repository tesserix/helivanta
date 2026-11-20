# Outlet low-stock warnings

Availability: this feature is part of the `pharmacy_stock` release capability,
which depends on `pharmacy_catalogue`. It remains unavailable until those
capabilities have completed their release checks and are enabled through
[Console feature management](../runbooks/feature-flags.md). A source merge or
passing unit test is not evidence that it is available in a tenant.

1. Select the pharmacy and outlet in PharmaConnect. Open **Stock → Low stock**.
2. An outlet editor with catalogue-read, catalogue-write and stock-read permission can select an
   active product under **Configure reorder level**. Readers can view warnings.
3. Enable the warning and enter a whole-number minimum in the displayed issue
   unit. Zero means warn when none is available. Save the reorder level.
4. Products whose available quantity is at or below the minimum appear in the
   list. Products with no batches can appear with zero. The overview's
   **Low-stock products** link opens this same scoped view.
5. To stop a warning, select the product, clear **Enable low-stock warning**, and
   save. The previous configuration remains in accountable history.

Quarantine, expired stock, safety-blocked lots and supplier reservations do not
inflate availability. A batch expiring today is excluded using the outlet's
local date. This is a shortage warning, not a dispensing approval or an order.
An unconfigured product is not counted. If a product's unit changes before it
has any batches, its previous minimum is inactive and must be confirmed again
in the new unit.

If a save conflicts, reload the current product and level before editing. If a
save cannot be confirmed, keep the form open and choose **Check saved result**.
Only an authoritative not-found response offers **Retry original change**, which
uses the same command and input. A request failure is never shown as an empty
stock list. Switching away from an unsaved change asks for confirmation.
