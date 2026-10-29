# Pharmacy payment recovery

The registration envelope mismatch is fixed on main by #1456. This change owns
unused verified-UPI recovery before the sale/payment/return browser journey.
Pharmacy owns its payment request and sale ledger; payments remains the owner of
provider confirmation. Tenant and explicit outlet grants apply to every command.

An open or collected verified payment must not silently disappear when the
cashier changes the customer or tender. Hold those controls while the current QR
is open or its paid request is selected. Paid unused requests remain recoverable
after navigation, errors or restart. The cashier either posts the sale or records
a full refund already completed outside Helivanta. The interface must explicitly
say that recording a refund does not send money. Automatic provider refunds are
not implemented and must never be simulated.

Implementation tracking: #1459. No new permission or role is introduced: the
existing pharmacist `pharmacy.sale.return` permission plus the explicit outlet
editor grant authorizes recording; receipt reads need the permission and a
current outlet grant and are audited. The same transaction publishes an ids/
amount/currency-only refund-recorded event, not provider confirmation.

An immutable refund record retains the original request, exact amount/currency,
external refund reference, reason, actor and time. The original request remains
paid. A request can fund one sale or one full unused-payment refund, never both.
Both database guards lock the original request before checking the other ledger,
so concurrent tills cannot consume and refund it. Repeat identical refund commands
return the recorded result; changed content conflicts. No stock movement, sale,
credit note or patient record is invented for a payment without a sale.

Rejected: marking paid money cancelled (erases provider truth); relying on UI
disabling alone (another till can race); creating a fake sale to refund (false
stock and revenue); claiming a typed reference is provider-verified refund.

Validation includes scope denial, unpaid/used requests, changed replay, immutable
records, sale/refund concurrency, exact financial amounts and UI refusal to change
customer/tender. IN-PH-04/05 apply to durable evidence and corrections. No drug or
tax policy is activated by this recovery change.
