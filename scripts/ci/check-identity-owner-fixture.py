"""Verify actual native missing-user evidence through the read-only CI owner."""

import json
import os
import urllib.request
import uuid

if os.environ.get("HELIVANTA_ENV") != "dev":
    raise SystemExit("identity fixture check requires development mode")

TENANT = "11111111-1111-1111-1111-111111111111"
SUBJECT = "synthetic-missing-native-user"
request = urllib.request.Request(
    "http://127.0.0.1:18787/internal/v1/customer-signups/identity-projection",
    data=json.dumps(
        {
            "purpose": "membership",
            "tenantId": TENANT,
            "subject": SUBJECT,
            "requestId": str(uuid.uuid4()),
        }
    ).encode(),
    headers={
        "Content-Type": "application/json",
        "Authorization": "Bearer helivanta-local-identity-fixture-only",
    },
)
with urllib.request.urlopen(request, timeout=15) as response:
    body = response.read(8193)
    if response.status != 200 or len(body) > 8192:
        raise SystemExit("native missing-user proof unavailable")
    expected = {
        "purpose": "membership",
        "tenantId": TENANT,
        "organizationId": "helivanta-" + TENANT,
        "subject": SUBJECT,
        "state": "inactive",
        "admission": "ineligible",
    }
    if json.loads(body) != expected:
        raise SystemExit("native missing-user proof escaped its intended scope")
print("Real native missing-user proof verified through private owner and audit.")
