import { screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import { WardIndentQueue } from "./ward-indent-queue";

const scope = {
  tenantId: "0199e000-0000-7000-8000-0000000000aa",
  pharmacyId: "0199e000-0000-7000-8000-000000000010",
  outletId: "0199e000-0000-7000-8000-000000000001",
};
const ids = {
  indent: "0199e000-0000-7000-8000-000000000020",
  hospital: "0199e000-0000-7000-8000-000000000021",
  department: "0199e000-0000-7000-8000-000000000022",
  product: "0199e000-0000-7000-8000-000000000023",
};
const indent = (issued: number, status = "open") => ({
  id: ids.indent,
  outlet_id: scope.outletId,
  hospital_id: ids.hospital,
  department_id: ids.department,
  kind: "patient",
  ward_code: "W1",
  room_code: "R2",
  bed_code: "B3",
  hospital_name: "TEST General",
  department_name: "TEST Ward",
  line_count: 1,
  status,
  version: 1,
  close_reason: null,
  raised_at: "2026-11-26T04:00:00Z",
  closed_at: null,
  lines: [
    {
      line_number: 1,
      product_id: ids.product,
      brand_name: "TEST Brand",
      generic_name: "TEST generic",
      requested_units: 10,
      issued_units: issued,
      returned_units: 0,
    },
  ],
});
const page = (rows: unknown[]) =>
  new Response(JSON.stringify({ data: rows, page: { has_more: false, next_cursor: null } }));
afterEach(() => vi.unstubAllGlobals());

it("lists open indents, sends the close-short reason and refuses a short reason", async () => {
  const writes: { url: string; body: unknown }[] = [];
  const fetch = vi.fn(async (url: string, init?: RequestInit) => {
    if (init?.method === "PUT") {
      writes.push({ url, body: JSON.parse(String(init.body)) });
      return new Response(
        JSON.stringify({ ...indent(4, "closed"), close_reason: "supplier delay" }),
      );
    }
    if (url.includes("/ward-indents?")) return page([indent(4)]);
    if (url.includes(`/ward-indents/${ids.indent}`)) return new Response(JSON.stringify(indent(4)));
    throw new Error(`Unexpected TEST endpoint ${url}`);
  });
  vi.stubGlobal("fetch", fetch);
  const { user } = renderWithProviders(
    <WardIndentQueue scope={scope} timeZone="Asia/Kolkata" canFulfil />,
  );
  await user.click(await screen.findByRole("button", { name: /TEST General · TEST Ward/ }));
  expect(await screen.findByRole("link", { name: "Issue at the counter" })).toHaveAttribute(
    "href",
    `/pharmacy/counter?indent=${ids.indent}`,
  );
  await user.type(screen.getByLabelText("Reason for closing short"), "no");
  await user.click(screen.getByRole("button", { name: "Close short" }));
  expect(await screen.findByText(/at least 5 characters/)).toBeVisible();
  expect(writes).toEqual([]);
  await user.clear(screen.getByLabelText("Reason for closing short"));
  await user.type(screen.getByLabelText("Reason for closing short"), "supplier delay");
  await user.click(screen.getByRole("button", { name: "Close short" }));
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]?.url).toContain(`/ward-indents/${ids.indent}/closure`);
  expect(writes[0]?.body).toEqual({ reason: "supplier delay" });
});

it("closes a fully issued indent without a reason and hides actions from a reader", async () => {
  const writes: unknown[] = [];
  const fetch = vi.fn(async (url: string, init?: RequestInit) => {
    if (init?.method === "PUT") {
      writes.push(JSON.parse(String(init.body)));
      return new Response(JSON.stringify(indent(10, "closed")));
    }
    if (url.includes("/ward-indents?")) return page([indent(10)]);
    return new Response(JSON.stringify(indent(10)));
  });
  vi.stubGlobal("fetch", fetch);
  const full = renderWithProviders(
    <WardIndentQueue scope={scope} timeZone="Asia/Kolkata" canFulfil />,
  );
  await full.user.click(await screen.findByRole("button", { name: /TEST General/ }));
  await full.user.click(await screen.findByRole("button", { name: "Close fully issued indent" }));
  await waitFor(() => expect(writes).toEqual([{}]));
  full.unmount();
  const reader = renderWithProviders(
    <WardIndentQueue scope={scope} timeZone="Asia/Kolkata" canFulfil={false} />,
  );
  await reader.user.click(await screen.findByRole("button", { name: /TEST General/ }));
  expect(await screen.findByText("TEST Brand")).toBeVisible();
  expect(screen.queryByRole("link", { name: "Issue at the counter" })).toBeNull();
  expect(screen.queryByRole("button", { name: /Close/ })).toBeNull();
});

it("says so when nothing is open", async () => {
  vi.stubGlobal(
    "fetch",
    vi.fn(async () => page([])),
  );
  renderWithProviders(<WardIndentQueue scope={scope} timeZone="Asia/Kolkata" canFulfil />);
  expect(await screen.findByText("No open ward indents at this outlet.")).toBeVisible();
});
