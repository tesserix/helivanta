import { screen, waitFor } from "@testing-library/react";
import { useState } from "react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { AccountPresentationsProvider, NavigationGuardProvider } from "@helivanta/ui";
import { StocktakeDetail } from "./stocktake-detail";

const scope = {
  subject: "TEST-viewer",
  tenantId: "0199e000-0000-7000-8000-0000000000aa",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
  outletId: "0199e000-0000-7000-8000-000000000001",
};
const id = "0199e000-0000-7000-8000-000000000002";
const actor = "TEST-private-starter-subject";
const header = {
  id,
  outlet_id: scope.outletId,
  number: 1,
  selection: "all",
  product_ids: [],
  quarantine_location_id: id,
  reason: "TEST cycle",
  started_by: actor,
  started_at: "2026-11-20T00:00:00Z",
  snapshot_rows: 3,
  counted_rows: 2,
  difference_rows: 2,
  positive_difference_rows: 0,
  review_relationship: "starter",
  version: 1,
  state: "counting",
};
const line = {
  id,
  stocktake_id: id,
  product_id: id,
  product_version: 1,
  sku: "TEST",
  brand_name: "TEST Product",
  unit_of_issue: "tablet",
  batch_id: id,
  lot_number: "TEST-ZERO",
  expires_on: "2030-01-01",
  location_id: id,
  location_code: "TEST-Q",
  location_name: "TEST quarantine",
  stock_status: "quarantine",
  book_units: 10,
  movement_count: 1,
  regulatory_class: "H1",
  ndps_status: "no",
  schedule_declaration_id: id,
  counted_units: 0,
  counted_by: actor,
  count_reason: "TEST empty",
  counted_at: header.started_at,
  difference_units: -10,
  snapshotted_at: header.started_at,
};
const json = (value: unknown, status = 200) => new Response(JSON.stringify(value), { status });
afterEach(() => vi.unstubAllGlobals());
function panel(canConfigure = false) {
  function PermissionFixture() {
    const [allowed, setAllowed] = useState(canConfigure);
    return (
      <>
        <button type="button" onClick={() => setAllowed((value) => !value)}>
          Toggle correction permission
        </button>
        <StocktakeDetail scope={scope} takeId={id} timeZone="Asia/Kolkata" canConfigure={allowed} />
      </>
    );
  }
  return renderWithProviders(
    <NavigationGuardProvider>
      <AccountPresentationsProvider scope={{ tenantId: scope.tenantId, subject: "TEST-viewer" }}>
        <PermissionFixture />
      </AccountPresentationsProvider>
    </NavigationGuardProvider>,
  );
}

it("retains an open physical entry but pauses it when correction permission is withdrawn", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.includes("account-presentations"))
        return json({
          tenant_id: scope.tenantId,
          actor_subject: "TEST-viewer",
          data: [{ subject: actor, available: false, name: null, email: null, roles: [] }],
        });
      if (url.includes("/lines"))
        return json({ data: [line], page: { next_cursor: null, has_more: false } });
      return json(header);
    }),
  );
  const { user } = panel(true);
  await user.click(await screen.findByRole("button", { name: "Record count · TEST-ZERO" }));
  const quantity = screen.getByRole("textbox", { name: "Physical count (tablet)" });
  await user.type(quantity, "8");
  await user.click(screen.getByRole("button", { name: "Toggle correction permission" }));
  expect(quantity).toBeDisabled();
  expect(quantity).toHaveValue("8");
});
it("presents missing counts separately from zero and retains schedule groups and named people", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      if (url.includes("account-presentations"))
        return json({
          tenant_id: scope.tenantId,
          actor_subject: "TEST-viewer",
          data: JSON.parse(String(init?.body)).subjects.map((subject: string) => ({
            subject,
            available: true,
            name: "TEST Stock Lead",
            email: "test@example.invalid",
            roles: [{ key: "pharmacist", label: "Pharmacist" }],
          })),
        });
      if (url.includes("/lines"))
        return json({
          data: [
            line,
            {
              ...line,
              id: "0199e000-0000-7000-8000-000000000003",
              lot_number: "TEST-MISSING",
              regulatory_class: "X",
              ndps_status: "yes",
              counted_units: null,
              counted_by: null,
              count_reason: null,
              counted_at: null,
              difference_units: null,
            },
            {
              ...line,
              id: "0199e000-0000-7000-8000-000000000005",
              lot_number: "TEST-NDPS",
              regulatory_class: "X",
              ndps_status: "yes",
            },
          ],
          page: { next_cursor: null, has_more: false },
        });
      return json(header);
    }),
  );
  panel();
  expect(await screen.findByRole("heading", { name: "Stocktake 1" })).toBeVisible();
  expect(await screen.findByText("Not counted")).toBeVisible();
  expect(await screen.findByRole("heading", { name: "Schedule H1 differences" })).toBeVisible();
  expect(await screen.findByRole("heading", { name: "NDPS differences" })).toBeVisible();
  expect((await screen.findAllByText("TEST Stock Lead")).length).toBeGreaterThan(0);
  expect(screen.queryByText(actor)).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: /Record count/ })).not.toBeInTheDocument();
});
it("shows an unavailable record rather than an empty or editable count", async () => {
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(json({ error: "unavailable" }, 503)));
  panel(true);
  expect(await screen.findByText("The stocktake could not be loaded.")).toBeVisible();
  expect(screen.queryByRole("button", { name: /Record count/ })).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Download stocktake register" }),
  ).not.toBeInTheDocument();
});

it.each([401, 403, 404, 409, 503])(
  "hides previously loaded private stocktake details after a %s refresh",
  async (status) => {
    let refused = false;
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string) => {
        if (url.includes("account-presentations"))
          return json({ tenant_id: scope.tenantId, actor_subject: scope.subject, data: [] });
        if (url.includes("/lines"))
          return json({ data: [line], page: { next_cursor: null, has_more: false } });
        return refused
          ? json({ error: "unavailable", message: "TEST current read refused" }, status)
          : json(header);
      }),
    );
    const { user } = panel(true);
    expect(await screen.findByText("TEST Product")).toBeVisible();
    refused = true;
    await user.click(screen.getByRole("button", { name: "Refresh stocktake" }));
    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent(
      status === 401
        ? "Your session has ended."
        : status === 403 || status === 404
          ? "This wasn't found, or you don't have access to it."
          : "The stocktake could not be loaded.",
    );
    expect(screen.queryByRole("heading", { name: "Stocktake 1" })).not.toBeInTheDocument();
    expect(screen.queryByText("TEST Product")).not.toBeInTheDocument();
    expect(
      screen.queryByRole("heading", { name: "Schedule H1 differences" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Download stocktake register" }),
    ).not.toBeInTheDocument();
    if (status === 401)
      expect(
        screen.queryByRole("button", { name: "Check stocktake access" }),
      ).not.toBeInTheDocument();
    if (status === 403 || status === 404) {
      refused = false;
      await user.click(screen.getByRole("button", { name: "Check stocktake access" }));
      expect(await screen.findByText("TEST Product")).toBeVisible();
    }
  },
);

it("hides cached bucket evidence on line failure and reloads it on retry", async () => {
  let refused = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.includes("account-presentations"))
        return json({ tenant_id: scope.tenantId, actor_subject: scope.subject, data: [] });
      if (url.includes("/lines"))
        return refused
          ? json(
              { error: "authz_unavailable", message: "TEST current bucket authority unavailable" },
              503,
            )
          : json({ data: [line], page: { next_cursor: null, has_more: false } });
      return json(header);
    }),
  );
  const { user } = panel(true);
  expect(await screen.findByText("TEST Product")).toBeVisible();
  refused = true;
  await user.click(screen.getByRole("button", { name: "Refresh stocktake" }));
  expect(
    await screen.findByText("Stocktake buckets could not be loaded. Editing is paused."),
  ).toBeVisible();
  expect(screen.queryByText("TEST Product")).not.toBeInTheDocument();
  expect(
    screen.queryByRole("heading", { name: "Schedule H1 differences" }),
  ).not.toBeInTheDocument();
  refused = false;
  await user.click(screen.getByRole("button", { name: /retry|try again/i }));
  expect(await screen.findByText("TEST Product")).toBeVisible();
  await waitFor(() => expect(screen.getByRole("button", { name: /Record count/ })).toBeEnabled());
});

it("shows refusal rather than an outage when concurrent reads fail differently", async () => {
  let refused = false;
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.includes("account-presentations"))
        return json({ tenant_id: scope.tenantId, actor_subject: scope.subject, data: [] });
      if (url.includes("/lines"))
        return refused
          ? json({ error: "forbidden", message: "TEST revoked bucket access" }, 403)
          : json({ data: [line], page: { next_cursor: null, has_more: false } });
      return refused
        ? json({ error: "authz_unavailable", message: "TEST status unavailable" }, 503)
        : json(header);
    }),
  );
  const { user } = panel(true);
  await screen.findByText("TEST Product");
  refused = true;
  await user.click(screen.getByRole("button", { name: "Refresh stocktake" }));
  await waitFor(() =>
    expect(screen.getByRole("alert")).toHaveTextContent(
      "This wasn't found, or you don't have access to it.",
    ),
  );
  expect(screen.queryByText("TEST Product")).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Try again" })).not.toBeInTheDocument();
});

it("does not offer physical entry or counter decisions to a review-only duty", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      if (url.includes("account-presentations"))
        return json({ tenant_id: scope.tenantId, actor_subject: scope.subject, data: [] });
      if (url.includes("/lines"))
        return json({ data: [line], page: { next_cursor: null, has_more: false } });
      return json(header);
    }),
  );
  renderWithProviders(
    <NavigationGuardProvider>
      <AccountPresentationsProvider scope={{ tenantId: scope.tenantId, subject: scope.subject }}>
        <StocktakeDetail
          scope={{ ...scope, authority: "operating" }}
          takeId={id}
          timeZone="Asia/Kolkata"
          canConfigure
          canCount={false}
          canReview
        />
      </AccountPresentationsProvider>
    </NavigationGuardProvider>,
  );
  await screen.findByText(/TEST-ZERO · expires/);
  expect(
    screen.queryByRole("button", { name: "Record count · TEST-ZERO" }),
  ).not.toBeInTheDocument();
  expect(
    screen.queryByRole("button", { name: "Review or close stocktake" }),
  ).not.toBeInTheDocument();
});

it.each(["counting", "submitted"])(
  "retains %s evidence while withholding work after its source changes",
  async (state) => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string) => {
        if (url.includes("account-presentations"))
          return json({
            tenant_id: scope.tenantId,
            actor_subject: scope.subject,
            data: [{ subject: actor, available: false, name: null, email: null, roles: [] }],
          });
        if (url.includes("/lines"))
          return json({ data: [line], page: { next_cursor: null, has_more: false } });
        return json({
          ...header,
          state,
          source_invalidated: true,
          review_relationship: "independent",
        });
      }),
    );
    panel(true);
    expect(await screen.findByText("Source changed — retained evidence only.")).toBeVisible();
    expect(
      screen.queryByRole("button", { name: "Record count · TEST-ZERO" }),
    ).not.toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Review or close stocktake" }),
    ).not.toBeInTheDocument();
    expect(screen.getByText("Stocktake 1")).toBeVisible();
  },
);
