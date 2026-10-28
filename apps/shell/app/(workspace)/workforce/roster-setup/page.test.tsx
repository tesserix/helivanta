import "@/test/select";
import { screen, waitFor, within } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { renderWithProviders } from "@helivanta/api/testing";
import RosterSetup from "./page";

const hospital = "11111111-1111-4111-8111-111111111111";
const shiftID = "22222222-2222-4222-8222-222222222222";
const revision = "33333333-3333-4333-8333-333333333333";
const areaID = "44444444-4444-4444-8444-444444444444";
const areaRevision = "55555555-5555-4555-8555-555555555555";
const shift = {
  revision_id: revision,
  shift_id: shiftID,
  id: shiftID,
  code: "SYN-DAY",
  name: "Synthetic Day",
  active: true,
  start_time: "08:00",
  end_time: "16:00",
  duration_minutes: 480,
};
const area = {
  id: areaID,
  revision_id: areaRevision,
  kind: "area",
  code: "SYN-AREA",
  name: "Synthetic Area",
  description: "Synthetic policy",
  active: true,
  effective_on: "2026-10-14",
  reviewed_by_subject: "synthetic-owner",
  version: 1,
  shifts: [shift],
  days: [],
};
const page = (data: unknown[]) => ({ data, page: { has_more: false, next_cursor: null } });
afterEach(() => vi.unstubAllGlobals());
function mock(allowed = true) {
  const writes: { url: string; body: unknown }[] = [];
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init?: RequestInit) => {
      let status = 200;
      let body: unknown = page([]);
      if (url.includes("permissions")) body = { data: allowed ? ["workforce.roster.manage"] : [] };
      else if (url.endsWith("/hospitals"))
        body = page([{ id: hospital, name: "Synthetic Hospital", access_level: "manager" }]);
      else if (url.includes("/shifts")) body = page([shift]);
      else if (url.includes("/history"))
        body = page([{ ...area, id: areaRevision, config_id: areaID }]);
      else if (url.includes("roster-configs")) body = page(url.includes("kind=area") ? [area] : []);
      if (init?.method === "PUT") {
        writes.push({ url, body: JSON.parse(String(init.body)) });
        status = 409;
        body = { message: "Configuration changed. Reload before saving." };
      }
      return new Response(JSON.stringify(body), { status });
    }),
  );
  return writes;
}
it("reopens saved membership, requires review and preserves a conflict for same-ID retry", async () => {
  const writes = mock();
  const { user } = renderWithProviders(<RosterSetup />);
  await user.click(await screen.findByRole("button", { name: "Edit Synthetic Area" }));
  expect(screen.getByLabelText("Code")).toBeDisabled();
  expect(screen.getByLabelText("Effective date")).toHaveValue("2026-10-14");
  expect(screen.getByRole("region", { name: "Edit roster area" })).toHaveTextContent("SYN-DAY");
  await user.click(screen.getByRole("button", { name: "Save configuration" }));
  expect(
    await screen.findByText("Confirm that you reviewed the configuration."),
  ).toBeInTheDocument();
  expect(writes).toHaveLength(0);
  await user.click(
    screen.getByRole("checkbox", { name: "I reviewed the effective date and scheduling rules" }),
  );
  await user.clear(screen.getByLabelText("Name"));
  await user.paste("Synthetic Area Revised");
  await user.click(screen.getByRole("button", { name: "Save configuration" }));
  expect(await screen.findByRole("alert")).toHaveTextContent("changed");
  expect(screen.getByLabelText("Name")).toHaveValue("Synthetic Area Revised");
  await user.click(screen.getByRole("button", { name: "Save configuration" }));
  await waitFor(() => expect(writes).toHaveLength(2));
  expect(writes[0]).toEqual(writes[1]);
});
it("reads immutable membership history and hides management controls from readers", async () => {
  mock(false);
  const { user } = renderWithProviders(<RosterSetup />);
  expect(await screen.findByText("Synthetic Area")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Add roster area" })).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Edit Synthetic Area" })).not.toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: "History for Synthetic Area" }));
  await waitFor(() =>
    expect(screen.getByRole("region", { name: "Roster configuration history" })).toHaveTextContent(
      "SYN-DAY",
    ),
  );
});
it("uses shared selectors for weekly allocation and restores choices after toggling weekly off", async () => {
  mock();
  const { user } = renderWithProviders(<RosterSetup />);
  await user.click(await screen.findByRole("combobox", { name: "Configuration" }));
  await user.click(screen.getByRole("option", { name: "Weekly templates" }));
  await user.click(await screen.findByRole("button", { name: "Add weekly template" }));
  await waitFor(() => expect(screen.getByLabelText("Name")).toHaveFocus());
  await user.click(screen.getByRole("checkbox", { name: "Monday weekly off" }));
  await user.click(await screen.findByRole("combobox", { name: "Monday first area" }));
  await user.click(screen.getByRole("option", { name: /Synthetic Area/ }));
  await user.click(screen.getByRole("combobox", { name: "Monday first shift" }));
  await user.click(screen.getByRole("option", { name: /SYN-DAY/ }));
  const day = screen.getByRole("group", { name: "Monday" });
  expect(day).toHaveTextContent("SYN-DAY");
  await user.click(screen.getByRole("checkbox", { name: "Monday weekly off" }));
  expect(within(day).queryByRole("combobox")).not.toBeInTheDocument();
  await user.click(screen.getByRole("checkbox", { name: "Monday weekly off" }));
  expect(day).toHaveTextContent("SYN-DAY");
  expect(day).toHaveTextContent("Synthetic Area");
});
