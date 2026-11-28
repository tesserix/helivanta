import { test } from "@playwright/test";
import { exerciseNativeStaffMFARecovery } from "./support/staff-mfa-native";

test.use({ trace: "off", video: "off", screenshot: "off" });
test.setTimeout(180_000);

test("hospital owner resets a reviewed Staff security key, and re-enrolment with a key admits only fresh access", async ({
  page,
  browser,
}) => {
  await exerciseNativeStaffMFARecovery({ page, browser }, "hospital", "security_key");
});
