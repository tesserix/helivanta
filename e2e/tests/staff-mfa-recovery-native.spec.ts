import { test } from "@playwright/test";
import { exerciseNativeStaffMFARecovery } from "./support/staff-mfa-native";

test.use({ trace: "off", video: "off", screenshot: "off" });
test.setTimeout(180_000);

test("hospital owner resets a reviewed Staff authenticator and native re-enrollment admits only fresh access", async (fixtures) => {
  await exerciseNativeStaffMFARecovery(fixtures, "hospital");
});
