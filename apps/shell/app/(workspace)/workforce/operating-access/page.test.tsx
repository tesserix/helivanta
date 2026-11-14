import { screen, fireEvent, within, waitFor } from "@testing-library/react";
import { useQueryClient } from "@tanstack/react-query";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { NavigationGuardProvider } from "@helivanta/ui";
import Page from "./page";
vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  usePathname: () => "/workforce/operating-access",
  useSearchParams: () => new URLSearchParams(),
}));
afterEach(() => vi.unstubAllGlobals());
it("admits a native operating delegate without legacy product or IAM operating roles", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) =>
      url.endsWith("permissions")
        ? Response.json({
            tenant_id: "11111111-1111-4111-8111-111111111111",
            subject: "synthetic-delegate",
            data: ["iam.governance.operating.manage"],
          })
        : url.includes("/operating-authority-scopes")
          ? Response.json({ data: [], page: { has_more: false, next_cursor: null } })
          : Response.json({ error: "not_found" }, { status: 404 }),
    ),
  );
  renderWithProviders(
    <NavigationGuardProvider>
      <Page />
    </NavigationGuardProvider>,
  );
  await screen.findByText("Choose an employer");
  expect(screen.queryByText(/not authorized|access unavailable/i)).not.toBeInTheDocument();
});

it("retains the original proposal across account changes and recovers without repeating the write", async () => {
  const old = {
    tenant_id: "11111111-1111-4111-8111-111111111111",
    subject: "synthetic-delegate",
    data: ["iam.governance.operating.manage", "iam.governance.operating.review"],
  };
  let identity = old;
  let receiptDenied = true;
  const employer = "019a55c0-1111-7111-8111-111111111112",
    staff = "019a55c0-1111-7111-8111-111111111113",
    link = "019a55c0-1111-7111-8111-111111111114",
    location = "019a55c0-1111-7111-8111-111111111115";
  const page = (data: unknown[]) =>
    Response.json({ data, page: { has_more: false, next_cursor: null } });
  let intent: Record<string, unknown> | undefined,
    path = "",
    writes = 0;
  const recoveries: Array<{
    path: string;
    tenant: string | null;
    subject: string | null;
    method: string;
  }> = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("permissions")) return Response.json(identity);
      if (url.includes("/proposal-receipt")) {
        recoveries.push({
          path: url,
          tenant: new Headers(init?.headers).get("X-Helivanta-Expected-Tenant"),
          subject: new Headers(init?.headers).get("X-Helivanta-Expected-Subject"),
          method: init?.method ?? "GET",
        });
        if (identity.subject !== old.subject)
          return Response.json(
            { error: "context_changed", message: "Restore the original account" },
            { status: 409 },
          );
        if (receiptDenied) {
          receiptDenied = false;
          return Response.json(
            { error: "forbidden", message: "Synthetic original source refusal" },
            { status: 403 },
          );
        }
        return Response.json({
          owner_tenant_id: old.tenant_id,
          id: path.split("/").at(-1),
          input: intent,
          status: "pending_review",
          version: 1,
          prepared_by: old.subject,
          created_at: "2026-11-14T00:00:00Z",
          updated_at: "2026-11-14T00:00:00Z",
        });
      }
      if (init?.method === "PUT") {
        writes++;
        intent = JSON.parse(String(init.body));
        path = url;
        return Response.json(
          { error: "unavailable", message: "Synthetic lost response" },
          { status: 500 },
        );
      }
      if (url.includes("/operating-authority-scopes"))
        return page([
          {
            employer_kind: "supplier",
            employer_id: employer,
            name: "Synthetic supplier",
            can_manage: true,
            can_review: true,
          },
        ]);
      if (url.includes("/staff-choices"))
        return page([
          {
            staff_id: staff,
            display_name: "Synthetic staff",
            staff_version: 3,
            subject: "synthetic-receiver",
            link_id: link,
            link_version: 2,
          },
        ]);
      if (url.includes("/location-choices"))
        return page([
          {
            owner_tenant_id: old.tenant_id,
            employer_kind: "supplier",
            employer_id: employer,
            location_id: location,
            code: "SYN-DEPOT",
            name: "Synthetic depot",
            country_code: "IN",
            employer_version: 1,
            location_version: 1,
          },
        ]);
      if (url.includes("/duties")) return page([]);
      throw new Error(`Unexpected request: ${url}`);
    }),
  );
  function RefreshIdentity() {
    const client = useQueryClient();
    return (
      <button
        onClick={() => void client.invalidateQueries({ queryKey: ["iam", "me", "permissions"] })}
      >
        Refresh test identity
      </button>
    );
  }
  const { user } = renderWithProviders(
    <NavigationGuardProvider>
      <Page />
      <RefreshIdentity />
    </NavigationGuardProvider>,
  );
  await user.click(await screen.findByRole("button", { name: "Manage Synthetic supplier" }));
  await user.click(await screen.findByRole("button", { name: "Open Synthetic staff" }));
  await user.click(await screen.findByRole("button", { name: "Assign operating duty" }));
  await user.click(await screen.findByLabelText("Warehouse or depot"));
  await user.click(await screen.findByRole("option", { name: "SYN-DEPOT — Synthetic depot" }));
  fireEvent.change(screen.getByLabelText("Valid from (UTC)"), {
    target: { value: "2030-11-14T00:00" },
  });
  fireEvent.change(screen.getByLabelText("Expires at (UTC)"), {
    target: { value: "2030-11-15T00:00" },
  });
  await user.type(screen.getByLabelText("Duty reason"), "Synthetic scoped receiving");
  await user.click(screen.getByRole("button", { name: "Propose operating duty" }));
  await screen.findByRole("button", { name: "Recover duty proposal" });
  expect(writes).toBe(1);
  identity = {
    ...old,
    tenant_id: "22222222-2222-4222-8222-222222222222",
    subject: "another-account",
  };
  await user.click(screen.getByRole("button", { name: "Refresh test identity" }));
  await screen.findByText(
    "Your account or access changed. Recover any unconfirmed command in its original account before continuing.",
  );
  expect(screen.getByRole("button", { name: "Change employer" })).toBeDisabled();
  expect(screen.getByRole("button", { name: "Change Staff member" })).toBeDisabled();
  expect(screen.queryByText("Synthetic staff")).not.toBeInTheDocument();
  await user.click(
    within(
      screen.getByText("Duty preparation is unavailable.").closest('[role="alert"]')!,
    ).getByRole("button", { name: "Try again" }),
  );
  await waitFor(() => expect(recoveries).toHaveLength(1));
  expect(recoveries[0]).toEqual({
    path: path + "/proposal-receipt",
    tenant: old.tenant_id,
    subject: old.subject,
    method: "GET",
  });
  expect(writes).toBe(1);
  identity = old;
  await user.click(screen.getByRole("button", { name: "Refresh test identity" }));
  await user.click(await screen.findByRole("button", { name: "Recover duty proposal" }));
  await screen.findByText("This wasn't found, or you don't have access to it.");
  await user.click(await screen.findByRole("button", { name: "Recover duty proposal" }));
  await screen.findByText(
    "Operating duty proposed. A separate reviewer must approve it before activation.",
  );
  expect(writes).toBe(1);
  expect(recoveries).toHaveLength(3);
});
