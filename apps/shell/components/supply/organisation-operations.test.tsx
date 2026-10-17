import { screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { OrganisationOperations } from "./organisation-operations";
const scope = {
  tenantId: "11111111-1111-1111-1111-111111111111",
  subject: "TEST-owner",
  partyId: "0199e000-0000-7000-8000-000000000001",
};
afterEach(() => vi.unstubAllGlobals());
it("does not request operational records without a current capability", async () => {
  const fetcher = vi.fn(
    async () =>
      new Response(JSON.stringify({ tenant_id: scope.tenantId, subject: scope.subject, data: [] })),
  );
  vi.stubGlobal("fetch", fetcher);
  renderWithProviders(<OrganisationOperations scope={scope} roles={["driver"]} />);
  await waitFor(() => expect(fetcher).toHaveBeenCalled());
  expect(fetcher.mock.calls).toHaveLength(1);
  expect(screen.queryByRole("region", { name: "Delivery tasks" })).not.toBeInTheDocument();
});

const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
const location = {
  id: "0199e000-0000-7000-8000-000000000060",
  party_id: scope.partyId,
  code: "TEST-WAREHOUSE",
  name: "TEST Warehouse",
  kind: "warehouse",
  country_code: "IN",
  time_zone: "Asia/Kolkata",
  status: "active",
  allowed_actions: ["read_assigned_deliveries", "read_bins"],
};
const delivery = {
  id: "0199e000-0000-7000-8000-000000000061",
  party_id: scope.partyId,
  premise_id: location.id,
  code: "TEST-DELIVERY",
  destination_business_name: "TEST Pharmacy",
  destination_line1: "TEST Business road",
  destination_line2: "",
  destination_city: "TEST City",
  destination_region: "TEST Region",
  destination_postal_code: "123456",
  country_code: "IN",
  business_contact_name: "TEST Receiver",
  business_contact_phone: "",
  window_start: "2026-10-17T00:00:00Z",
  window_end: "2026-10-17T12:00:00Z",
  package_count: 2,
  handling_notes: "TEST upright packages",
  assignment_expires_at: "2026-10-18T00:00:00Z",
  status: "assigned",
  version: 3,
  assignment_generation: 2,
  allowed_actions: ["start"],
  created_at: "2026-10-17T00:00:00Z",
  updated_at: "2026-10-17T00:00:00Z",
  source_reference: "TEST PRIVATE SOURCE",
  driver_subject: "TEST OTHER DRIVER",
};
it("uses assigned delivery projection and retries the exact versioned driver command", async () => {
  const writes: Array<{ url: string; body: string }> = [];
  const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
    if (url.endsWith("permissions"))
      return new Response(
        JSON.stringify({
          tenant_id: scope.tenantId,
          subject: scope.subject,
          data: ["supply.delivery.assigned.read", "supply.delivery.assigned.action"],
        }),
      );
    if (init?.method === "PUT") {
      writes.push({ url, body: String(init.body) });
      return new Response(
        JSON.stringify({ error: "unavailable", message: "TEST handoff unavailable" }),
        { status: 500 },
      );
    }
    return new Response(
      JSON.stringify(page(url.endsWith("/operating-locations") ? [location] : [delivery])),
    );
  });
  vi.stubGlobal("fetch", fetcher);
  renderWithProviders(<OrganisationOperations scope={scope} roles={["driver"]} />);
  const user = (await import("@testing-library/user-event")).default.setup();
  await user.click(await screen.findByRole("button", { name: "My deliveries TEST-WAREHOUSE" }));
  await user.click(await screen.findByRole("button", { name: "start TEST-DELIVERY" }));
  expect(screen.queryByRole("button", { name: "deliver TEST-DELIVERY" })).not.toBeInTheDocument();
  expect(screen.queryByText(/TEST PRIVATE SOURCE|TEST OTHER DRIVER/)).not.toBeInTheDocument();
  await user.type(
    screen.getByLabelText("Handoff or exception note"),
    "TEST accepted sealed packages",
  );
  await user.click(screen.getByRole("button", { name: "Record delivery update" }));
  expect(await screen.findByText(/TEST handoff unavailable/)).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Record delivery update" }));
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[0]).toEqual(writes[1]);
  expect(JSON.parse(writes[0].body)).toEqual({
    expected_version: 3,
    expected_assignment_generation: 2,
    action: "start",
    reason: "TEST accepted sealed packages",
  });
  expect(
    fetcher.mock.calls.some(
      ([url]) => url.includes("/delivery-tasks") || url.endsWith(`/parties/${scope.partyId}`),
    ),
  ).toBe(false);
});
it("hides task rows when assigned access is denied", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.endsWith("permissions"))
        return new Response(
          JSON.stringify({
            tenant_id: scope.tenantId,
            subject: scope.subject,
            data: ["supply.delivery.assigned.read"],
          }),
        );
      if (url.endsWith("/operating-locations"))
        return new Response(JSON.stringify(page([location])));
      return new Response(JSON.stringify({ error: "not_found", message: "Not found" }), {
        status: 404,
      });
    }),
  );
  renderWithProviders(<OrganisationOperations scope={scope} roles={["driver"]} />);
  const user = (await import("@testing-library/user-event")).default;
  await user.click(await screen.findByRole("button", { name: "My deliveries TEST-WAREHOUSE" }));
  expect(await screen.findByText(/Delivery tasks are unavailable/)).toBeInTheDocument();
  expect(screen.queryByText("TEST-DELIVERY")).not.toBeInTheDocument();
  expect(screen.queryByRole("form", { name: "Driver delivery update" })).not.toBeInTheDocument();
});
it("validates manual scheduling and reuses one task identity on a failed write", async () => {
  const writes: Array<{ url: string; body: string }> = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.endsWith("permissions"))
        return new Response(
          JSON.stringify({
            tenant_id: scope.tenantId,
            subject: scope.subject,
            data: ["supply.operations.read", "supply.delivery.read", "supply.delivery.write"],
          }),
        );
      if (init?.method === "PUT") {
        writes.push({ url, body: String(init.body) });
        return new Response(
          JSON.stringify({ error: "unavailable", message: "TEST scheduling unavailable" }),
          { status: 500 },
        );
      }
      if (url.endsWith("/operating-locations"))
        return new Response(
          JSON.stringify(
            page([{ ...location, allowed_actions: ["manage_deliveries", "read_deliveries"] }]),
          ),
        );
      return new Response(JSON.stringify(page([])));
    }),
  );
  renderWithProviders(<OrganisationOperations scope={scope} roles={["dispatcher"]} />);
  const user = (await import("@testing-library/user-event")).default.setup();
  await user.click(await screen.findByRole("button", { name: "Manage deliveries TEST-WAREHOUSE" }));
  await user.click(await screen.findByRole("button", { name: "Schedule delivery task" }));
  await user.click(screen.getByRole("button", { name: "Save delivery task" }));
  expect(await screen.findAllByRole("alert")).not.toHaveLength(0);
  expect(writes).toHaveLength(0);
  for (const [field, value] of [
    ["Task code", "TEST-TASK"],
    ["Business handoff reference", "TEST-HANDOFF"],
    ["Destination business", "TEST Pharmacy"],
    ["Destination address", "TEST Business road"],
    ["Destination city", "TEST City"],
    ["Destination state or province", "TEST Region"],
    ["Destination postal code", "123456"],
    ["Reason for scheduling or change", "TEST approved scheduling"],
  ])
    await user.type(screen.getByLabelText(field), value);
  const { fireEvent } = await import("@testing-library/react");
  const { dateTimeInputValue } = await import("./time-input");
  fireEvent.change(screen.getByLabelText("Delivery window starts"), {
    target: { value: dateTimeInputValue(new Date(Date.now() + 3600000).toISOString()) },
  });
  fireEvent.change(screen.getByLabelText("Delivery window ends"), {
    target: { value: dateTimeInputValue(new Date(Date.now() + 7200000).toISOString()) },
  });
  await user.click(screen.getByRole("button", { name: "Save delivery task" }));
  expect(await screen.findByText(/TEST scheduling unavailable/)).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "Save delivery task" }));
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[0]).toEqual(writes[1]);
  expect(JSON.parse(writes[0].body)).toMatchObject({
    expected_version: 0,
    code: "TEST-TASK",
    source_reference: "TEST-HANDOFF",
    country_code: "IN",
    driver_subject: "",
    status: "draft",
    change_reason: "TEST approved scheduling",
  });
  expect(writes[0].url).toMatch(/\/delivery-tasks\/[0-9a-f]{8}-[0-9a-f]{4}-7/);
});

it.each(["administrator", "auditor"])(
  "shows archived delivery history read-only for a global %s",
  async (role) => {
    const reads: string[] = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string) => {
        reads.push(url);
        if (url.endsWith("permissions"))
          return new Response(
            JSON.stringify({
              data: [
                "supply.operations.read",
                "supply.operations.write",
                "supply.delivery.read",
                "supply.delivery.write",
                "supply.delivery.assigned.read",
                "supply.delivery.assigned.action",
              ],
            }),
          );
        if (url.endsWith("/operating-locations"))
          return new Response(
            JSON.stringify(
              page([{ ...location, status: "inactive", allowed_actions: ["read_deliveries"] }]),
            ),
          );
        if (url.endsWith("/revisions"))
          return new Response(
            JSON.stringify(
              page([
                {
                  id: "TEST-history",
                  version: 4,
                  snapshot: { ...delivery, status: "delivered" },
                  actor_subject: "TEST-driver",
                  created_at: "2026-10-17T12:00:00Z",
                },
              ]),
            ),
          );
        return new Response(
          JSON.stringify(page([{ ...delivery, status: "delivered", allowed_actions: [] }])),
        );
      }),
    );
    renderWithProviders(
      <OrganisationOperations scope={scope} roles={[role, "driver", "inventory", "dispatcher"]} />,
    );
    const user = (await import("@testing-library/user-event")).default.setup();
    expect(await screen.findByText("Archived · Read-only")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "View deliveries TEST-WAREHOUSE" }));
    await user.click(await screen.findByRole("button", { name: "History delivery TEST-DELIVERY" }));
    expect(await screen.findByText(/Actor TEST-driver/)).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Schedule delivery task" }),
    ).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Bins TEST-WAREHOUSE" })).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "My deliveries TEST-WAREHOUSE" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", {
        name: /^(edit|reassign|cancel) task|^(start|deliver|exception) TEST-/,
      }),
    ).not.toBeInTheDocument();
    expect(screen.queryByRole("form")).not.toBeInTheDocument();
    expect(
      reads.some(
        (url) =>
          url.includes("driver-candidates") ||
          url.includes("/my-deliveries") ||
          url.endsWith("/bins"),
      ),
    ).toBe(false);
  },
);

it("withdraws already-open driver controls immediately when its location becomes archived", async () => {
  let archived = false;
  let finishPending: ((response: Response) => void) | undefined;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.endsWith("permissions"))
        return new Response(
          JSON.stringify({
            data: [
              "supply.operations.read",
              "supply.delivery.read",
              "supply.delivery.assigned.read",
              "supply.delivery.assigned.action",
            ],
          }),
        );
      if (url.endsWith("/operating-locations"))
        return new Response(
          JSON.stringify(
            page([
              {
                ...location,
                status: archived ? "inactive" : "active",
                allowed_actions: archived
                  ? ["read_deliveries"]
                  : ["read_deliveries", "read_assigned_deliveries"],
              },
            ]),
          ),
        );
      if (archived && url.endsWith("/my-deliveries"))
        return new Promise<Response>((resolve) => {
          finishPending = resolve;
        });
      return new Response(JSON.stringify(page([delivery])));
    }),
  );
  renderWithProviders(<OrganisationOperations scope={scope} roles={["administrator", "driver"]} />);
  const user = (await import("@testing-library/user-event")).default.setup();
  await user.click(await screen.findByRole("button", { name: "My deliveries TEST-WAREHOUSE" }));
  expect(await screen.findByRole("button", { name: "start TEST-DELIVERY" })).toBeInTheDocument();
  archived = true;
  const { fireEvent } = await import("@testing-library/react");
  fireEvent(window, new Event("visibilitychange"));
  try {
    await waitFor(() =>
      expect(
        screen.queryByRole("button", { name: "My deliveries TEST-WAREHOUSE" }),
      ).not.toBeInTheDocument(),
    );
    expect(screen.queryByRole("button", { name: "start TEST-DELIVERY" })).not.toBeInTheDocument();
    expect(screen.queryByRole("region", { name: "Delivery tasks" })).not.toBeInTheDocument();
    expect(screen.queryByRole("form", { name: "Driver delivery update" })).not.toBeInTheDocument();
  } finally {
    finishPending?.(new Response(JSON.stringify(page([]))));
  }
});
