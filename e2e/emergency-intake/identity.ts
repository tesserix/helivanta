import { expect, type Page, type Route } from "@playwright/test";

export const fixtureNames: Readonly<Record<string, string>> = {
  "user-admin": "Synthetic administrator",
  "user-doctor": "Synthetic doctor",
  "synthetic-browser-driver": "Synthetic driver",
  "synthetic-browser-medic": "Synthetic paramedic",
};
const tenant = "11111111-1111-1111-1111-111111111111";
const role = (subject: string) =>
  subject === "user-admin"
    ? { key: "tenant_admin", label: "Tenant administrator" }
    : { key: "staff_member", label: "Staff member" };

// IAM metadata is outside the disposable Emergency service fixture; domain commands still use Go handlers.
export async function fixtureIdentityRead(route: Route, actor = "user-admin") {
  const path = new URL(route.request().url()).pathname;
  if (!["/api/v1/iam/membership-candidates", "/api/v1/iam/account-presentations"].includes(path))
    return false;
  const headers = route.request().headers();
  if (
    actor !== "user-admin" ||
    headers["x-helivanta-expected-tenant"] !== tenant ||
    headers["x-helivanta-expected-subject"] !== actor
  ) {
    await route.fulfill({ status: 403, json: { error: "forbidden" } });
    return true;
  }
  if (path.endsWith("membership-candidates")) {
    await route.fulfill({
      json: {
        data: Object.keys(fixtureNames).map((subject, index) => ({
          id: `0199e000-0000-7000-8000-00000000010${index}`,
          subject,
          role_key: role(subject).key,
          membership_epoch: 1,
          created_at: "2026-11-18T00:00:00Z",
        })),
        page: { has_more: false, next_cursor: null },
      },
    });
  } else {
    const { subjects } = route.request().postDataJSON() as { subjects: string[] };
    await route.fulfill({
      json: {
        tenant_id: tenant,
        actor_subject: actor,
        data: subjects
          .filter((subject) => fixtureNames[subject])
          .map((subject) => ({
            subject,
            available: true,
            name: fixtureNames[subject],
            email: `${subject}@example.test`,
            roles: [role(subject)],
          })),
      },
    });
  }
  return true;
}

export async function chooseFixtureAccount(page: Page, label: string, subject: string) {
  await page.getByRole("combobox", { name: label, exact: true }).click();
  const option = page.getByRole("option", {
    name: `${fixtureNames[subject]} · ${role(subject).label}`,
    exact: true,
  });
  await expect(option).not.toContainText(subject);
  await option.click();
}
