import { test } from "@playwright/test";
import { exerciseNativeStaffMFARecovery } from "./support/staff-mfa-native";

test.use({ trace: "off", video: "off", screenshot: "off" });
test.setTimeout(180_000);

test("independent pharmacy owner resets its own native Staff account without crossing tenant scope", async ({
  page,
  browser,
}) => {
  await exerciseNativeStaffMFARecovery({ page, browser }, "pharmacy");
});
