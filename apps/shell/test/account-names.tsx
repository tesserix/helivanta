import type { ReactElement } from "react";
import { vi } from "vitest";
import { AccountPresentationsProvider } from "@helivanta/ui";
import { renderWithProviders } from "@helivanta/api/testing";

const scope = {
  tenantId: "11111111-1111-4111-8111-111111111111",
  subject: "synthetic-display-actor",
};

export function renderWithNamedAccounts(
  ui: ReactElement,
  names: Readonly<Record<string, string>>,
  candidates: readonly string[] = Object.keys(names),
) {
  const original = globalThis.fetch;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.endsWith("/iam/account-presentations")) {
        const subjects = JSON.parse(String(init?.body)).subjects as string[];
        const headers = new Headers(init?.headers);
        return Response.json({
          tenant_id: headers.get("X-Helivanta-Expected-Tenant"),
          actor_subject: headers.get("X-Helivanta-Expected-Subject"),
          data: subjects
            .filter((subject) => names[subject])
            .map((subject) => ({
              subject,
              available: true,
              name: names[subject],
              email: "staff@example.test",
              roles: [{ key: "staff_member", label: "Staff member" }],
            })),
        });
      }
      if (url.includes("/iam/membership-candidates?"))
        return Response.json({
          data: candidates.map((subject, index) => ({
            id: `019aaaaa-aaaa-7aaa-8aaa-${String(index + 1).padStart(12, "0")}`,
            subject,
            role_key: "staff_member",
            membership_epoch: 1,
            created_at: "2026-11-17T00:00:00Z",
          })),
          page: { has_more: false, next_cursor: null },
        });
      const response = await original(input, init);
      if (url.endsWith("/iam/me/permissions") && response.ok) {
        const data = await response.clone().json();
        return Response.json({ tenant_id: scope.tenantId, subject: scope.subject, ...data });
      }
      return response;
    }),
  );
  return renderWithProviders(
    <AccountPresentationsProvider scope={scope}>{ui}</AccountPresentationsProvider>,
  );
}

export function accountText(expected: string | RegExp) {
  const matches = (element: Element) => {
    const text = element.textContent?.replace(/\s+/g, " ").trim() ?? "";
    if (typeof expected === "string") return text === expected;
    expected.lastIndex = 0;
    return expected.test(text);
  };
  return (_: string, element: Element | null) =>
    !!element && matches(element) && !Array.from(element.children).some(matches);
}
