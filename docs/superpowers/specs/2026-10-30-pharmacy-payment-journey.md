# Counter payment and return browser acceptance

Cover #1459 and the retail slice of #1150 with an actual pharmacist browser
journey through Global API and owner APIs: prepare scoped licensed stock, request
UPI, confirm the provider payment, refuse customer/tender changes, post one sale,
reload the invoice and issue a partial return into quarantine. Assert exact
amounts, stock, paid request use and immutable credit-note lineage through owner
reads. A second journey records a completed external refund of an unused payment
and proves it cannot fund a sale. No product HTTP calls are intercepted, no owner
tables are seeded and no internal event outcomes are injected.

PostgreSQL, NATS, OpenFGA, Zitadel, production-built shell and OpenBao Transit are
real isolated services. A loopback-only synthetic Razorpay protocol service is
the external-provider boundary: it returns the API shapes the adapter consumes
and a self-signed local HTTPS image, and a separate test-payer command captures a
synthetic payment. The real payments poller must fetch and verify this before
pharmacy can use it. This proves the internal integration, not live Razorpay or
bank settlement. No real credentials or financial transactions are used.

The fixture refuses non-development execution, binds loopback, rejects live keys
and accepts only its synthetic account. OpenBao is a separate ephemeral dev
container. An ephemeral signed JWT authenticates a narrowly scoped API role at a
test JWT-auth mount named `kubernetes`, so the production KubernetesLogin client
and real derived-key encryption/context checks execute unchanged. Keys/tokens and
the temporary HTTPS certificate stay in ignored temporary files and are cleaned
up. The browser trusts only this spec's local self-signed certificate via its own
test context; production configuration remains unchanged.

Fixtures use two separately authenticated actors for licence and drug-class
review. The journey has its own seeded identities/outlet and does not share or
alter Claude's stack. IN-PH-03/04/05 and IN-RC-11 apply to exact stock, durable
payment attribution, refunds and preserved correction history.
